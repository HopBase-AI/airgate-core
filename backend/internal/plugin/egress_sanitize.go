package plugin

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/DouDOU-start/airgate-core/ent"
)

// egress_sanitize.go —— 出网错误文本 / 错误体清洗。
//
// 上游 4xx 会被原样透传给客户端（这是有意的：参数错误必须让调用方看懂自己错在哪），
// 但上游原文里常夹带供应商身份：中继产品名、供应商工单号、上游域名、厂商专有错误码体系。
// 这里做的是「保留可操作信息、剥掉供应商标识」，而不是一刀切换成我方文案。
//
// 落库不清洗：usage_logs.error_message 仍存原文，管理员排障要靠它；
// 只有对外那一路过清洗，与 appusage.ErrorMessageVisibleToUser 的口径一致。
//
// 设计取舍（2026-09-02 加固）：这套清洗跑在自然语言上，错误模式是双向的——漏了泄漏、
// 过了把客户的报错改花。30 天生产语料回放显示全部防泄漏效果都来自通用规则，账号推导
// token 一次都没多剥掉任何东西；所以账号推导这一半按「宁可漏、不可误伤」收紧：
// 词边界匹配、短 ASCII 名不当 token、通用词停用。

var (
	// 供应商工单号尾注：(Request-ID: USA-20434252906100) / [trace-id: xxx]
	reUpstreamRequestIDNote = regexp.MustCompile(`(?i)[\(\[]\s*(?:request[-_ ]?id|trace[-_ ]?id|req[-_ ]?id|x-request-id)\s*[:=]\s*[^)\]]*[\)\]]`)
	// 中继前缀：Error from provider (Console Go):
	// 不锚定行首——我方会在前面加 "HTTP 400: " 之类的前缀。
	reProviderPrefix = regexp.MustCompile(`(?i)error\s+from\s+provider\s*\([^)]*\)\s*[:：]\s*`)
	// 中继前缀：Upstream request failed: / Upstream submit failed (400):
	reUpstreamFailedPrefix = regexp.MustCompile(`(?i)upstream\s+(?:request|submit)\s+failed\s*(?:\([^)]*\))?\s*[:：]\s*`)
	// 厂商专有错误码前缀：<400> InternalError.Algo.InvalidParameter:
	reVendorCodePrefix = regexp.MustCompile(`^\s*<\d{3}>\s*[A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z][A-Za-z0-9_]*)+\s*[:：]\s*`)
	// 裸 URL
	reURL = regexp.MustCompile(`(?i)\bhttps?://[^\s"'<>)）\]]+`)
	// 裸域名。分两档：
	//   - com/cn/net/org 这类几乎只当域名用的 TLD，一级即认（aijws.com）；
	//   - io/ai/co/me/top/app/dev/tech 这类同时是常见英文词/JSON 路径尾段的 TLD，
	//     至少两级才认（api.minimax.io 认，parameters.top / data.io 不认）。
	// 我方上游的主机名另由账号 base_url 推导（见 newIdentityScrubber），这里只兜第三方。
	reBareDomainStrict = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+(?:com|cn|net|org)\b`)
	reBareDomainLoose  = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.){2,}(?:io|ai|co|dev|app|xyz|top|me|tech)\b`)
	// 上游 HTML 错误页：从第一个 HTML 标签起整段截断。
	// 生产实测：中继 502/429 时会把 nginx/openresty 的错误页原样塞进报错文案，
	// 里面带着上游 Web 服务器指纹。
	reHTMLPageStart = regexp.MustCompile(`(?i)<\s*(?:!\s*doctype|/?\s*(?:html|head|title|body|center|h1|hr|div|p|br)\b)`)
	// Web 服务器 / 边缘设施指纹
	reServerFingerprint = regexp.MustCompile(`(?i)\b(?:openresty|nginx|apache|tengine|envoy|caddy|cloudflare|akamai|fastly)(?:[/ ][0-9][0-9a-z.\-]*)?\b`)
	// IPv4（可带端口）：上游内网地址不该出现在客户端看到的文案里
	reIPv4 = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}(?::\d{1,5})?\b`)
	// 清洗后残留的空括号
	reEmptyParens = regexp.MustCompile(`\s*[\(（]\s*[\)）]`)
	// 连续空白
	reSpaces = regexp.MustCompile(`[ \t]{2,}`)
)

// urlPlaceholder 删 URL 时的占位。直接删空会留下断句（生产实测腾讯 402 的
// "…enable postpaid billing. See: https://console.cloud.tencent.com/…" 被删成结尾一个光秃秃的 See）。
const urlPlaceholder = "[link removed]"

// upstreamVendorTokens 账号数据里推导不出、但会出现在上游错误文案里的中继/供应商标识。
// 绝大多数标识由 identityScrubber 从当前请求的账号（名称、base_url、邮箱）自动推导，
// 这里只补那些「产品名」类的漏网之鱼。新增供应商时若发现新的自称，补在这里。
var upstreamVendorTokens = []string{
	"console go",
	"new-api",
	"new_api",
	"newapi",
	"one-api",
	"one_api",
	"oneapi",
	"onehub",
}

// upstreamInfraHardSignals 上游把自己的基础设施内部错误（数据库连接串、进程崩溃栈）
// 当成 4xx 文案回给我们。这类文案对客户毫无用处，却会暴露上游的技术栈与内网结构，
// 命中即整条判为不可用，回落我方兜底文案。这一档是「只会出现在内部错误里」的强信号。
// 生产实测样本：failed to connect to `user=postgres database=new-api`: 10.0.1.10:5432 …
// no pg_hba.conf entry for host "10.0.25.41"。
var upstreamInfraHardSignals = []string{
	"pg_hba", "user=postgres", "dial tcp",
	"goroutine ", "traceback (most recent call last)",
}

// upstreamInfraSoftSignals 同样指向上游内部故障，但这些词也可能出现在上游回显用户
// prompt 的审核/校验类 4xx 里（"prompt mentions mysql injection"、"connection refused by
// policy engine"）。单独出现不判废，须与内网地址 / 连接串特征同时出现才整条判废。
var upstreamInfraSoftSignals = []string{
	"postgresql", "mysql", "redis://",
	"connection refused", "no such host", "panic:",
}

// upstreamInfraCorroboration 佐证软信号确实是基础设施错误的特征：连接串键、常见数据库端口。
var upstreamInfraCorroboration = []string{
	"host=", "dbname=", "database=", ":5432", ":3306", ":6379",
}

// upstreamIDBodyKeys 上游错误体里纯属供应商内部追踪的键，直接删掉而不是清洗值。
var upstreamIDBodyKeys = map[string]struct{}{
	"request_id":          {},
	"requestid":           {},
	"trace_id":            {},
	"traceid":             {},
	"req_id":              {},
	"upstream_request_id": {},
}

// scrubGenericTokens 账号名恰好是协议/报错常用词时不能当 token：
// 一个叫 stream 的账号会把客户看的 "stream_options must be…" 删成 "options must be…"，
// 且只在该账号被调度到时发生、落库还是原文，几乎不可能靠日志发现。
var scrubGenericTokens = map[string]struct{}{
	"stream": {}, "tokens": {}, "server": {}, "client": {}, "models": {},
	"request": {}, "response": {}, "default": {}, "message": {}, "messages": {},
	"content": {}, "function": {}, "functions": {}, "system": {}, "assistant": {},
	"gateway": {}, "upstream": {}, "provider": {}, "channel": {}, "account": {},
	"balance": {}, "quota": {}, "timeout": {}, "invalid": {}, "unknown": {},
	"internal": {}, "error": {}, "errors": {}, "openai": {}, "claude": {},
	"gemini": {}, "codex": {}, "standard": {}, "premium": {}, "enterprise": {},
}

// minASCIITokenLen 纯 ASCII 的账号推导 token 至少这么长才参与清洗：
// max / pro / api / new / tool / luna / spark 这类短名与正文词高度重合。
// 含 CJK 的名字（贾克斯 / 小草）三个字就足够独特，按 rune 数 ≥ 3 放行。
const minASCIITokenLen = 6

// identityScrubber 按「当前这次请求实际用的上游账号」推导要抹掉的标识。
// 相比维护一张全局供应商名单，这样零维护、也不会误伤无关文本。
type identityScrubber struct {
	tokens []string // 账号推导 token：全小写，按长度降序，先长后短避免残留；词边界严格
}

// newIdentityScrubber 按账号推导要抹掉的标识。model 是本次请求的模型名，
// 用来防呆：账号常按模型命名（生产上有 seedance-inference-1、腾讯tokenhub-GLM5.3-7折
// 这类），万一账号名恰好就是模型名（或模型名的一段），抹掉它会把客户最需要看的
// "哪个模型不支持什么"一起删掉——而模型名本来就是客户自己传的，也谈不上泄漏。
//
// 只在「token 是模型名的子串」时跳过；账号名**包含**模型名（腾讯tokenhub-GLM5.3-7折）
// 仍是完整的供应商标识，照常剥。早先双向 Contains 会让这类命名整个退出清洗。
func newIdentityScrubber(acc *ent.Account, model string) *identityScrubber {
	s := &identityScrubber{}
	seen := make(map[string]struct{})
	lowerModel := strings.ToLower(strings.TrimSpace(model))
	add := func(token string) {
		token = strings.ToLower(strings.TrimSpace(token))
		if !eligibleIdentityToken(token) {
			return
		}
		if lowerModel != "" && strings.Contains(lowerModel, token) {
			return
		}
		if _, dup := seen[token]; dup {
			return
		}
		seen[token] = struct{}{}
		s.tokens = append(s.tokens, token)
	}

	if acc != nil {
		add(acc.Name)
		if base := acc.Credentials["base_url"]; base != "" {
			if host := hostFromBaseURL(base); host != "" {
				add(host)
				add(registrableDomain(host))
			}
		}
		add(acc.Credentials["email"])
	}
	// 长 token 先替换，避免 "api.example.com" 被 "example.com" 先吃掉一半。
	for i := 1; i < len(s.tokens); i++ {
		for j := i; j > 0 && len(s.tokens[j]) > len(s.tokens[j-1]); j-- {
			s.tokens[j], s.tokens[j-1] = s.tokens[j-1], s.tokens[j]
		}
	}
	return s
}

// eligibleIdentityToken 账号推导 token 的准入：太短、纯 ASCII 太短、通用词一律不要。
func eligibleIdentityToken(token string) bool {
	if utf8.RuneCountInString(token) < 3 {
		return false
	}
	if _, generic := scrubGenericTokens[token]; generic {
		return false
	}
	ascii := true
	for i := 0; i < len(token); i++ {
		if token[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii && len(token) < minASCIITokenLen {
		return false
	}
	return true
}

func hostFromBaseURL(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	if !strings.Contains(base, "//") {
		base = "https://" + base
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// registrableDomain 取主域（api.aijws.com → aijws.com），供应商换子域也照样命中。
func registrableDomain(host string) string {
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return ""
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

// scrubText 清洗一段对外文案。返回清洗后的文本；文本被清空时返回空串，
// 由调用方回落到我方兜底文案。
func (s *identityScrubber) scrubText(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	// nil 接收者仍执行通用规则（前缀 / 尾注 / URL / 产品名），只是没有账号推导出的 token。
	var tokens []string
	if s != nil {
		tokens = s.tokens
	}
	out := text

	// 1. 结构化前缀 / 尾注
	out = reProviderPrefix.ReplaceAllString(out, "")
	out = reUpstreamFailedPrefix.ReplaceAllString(out, "")
	out = reVendorCodePrefix.ReplaceAllString(out, "")
	out = reUpstreamRequestIDNote.ReplaceAllString(out, "")

	// 2. 上游基础设施内部错误整条判废
	if looksLikeUpstreamInfraError(out) {
		return ""
	}

	// 3. 上游 HTML 错误页整段截断（保留我方加的前缀，如 "Asset provider returned non-JSON:"）
	if loc := reHTMLPageStart.FindStringIndex(out); loc != nil {
		out = out[:loc[0]]
	}

	// 4. URL、域名、Web 服务器指纹与内网地址
	out = reURL.ReplaceAllString(out, urlPlaceholder)
	out = reBareDomainStrict.ReplaceAllString(out, "")
	out = reBareDomainLoose.ReplaceAllString(out, "")
	out = reServerFingerprint.ReplaceAllString(out, "")
	out = reIPv4.ReplaceAllString(out, "")

	// 5. 账号推导出的供应商标识（严格词边界：账号名是自由文本，宁漏勿伤）
	//    + 产品名兜底清单（宽松边界：new_api_error 这种带下划线的自称也要剥，
	//    产品名是固定短表，不存在撞上客户正文的风险）
	for _, token := range tokens {
		out = replaceFold(out, token, "", boundaryWord)
	}
	for _, token := range upstreamVendorTokens {
		out = replaceFold(out, token, "", boundaryAlnum)
	}

	// 6. 收尾：清掉替换后残留的空括号、空白与孤立标点
	out = reEmptyParens.ReplaceAllString(out, "")
	out = reSpaces.ReplaceAllString(out, " ")
	out = strings.TrimSpace(out)
	out = strings.Trim(out, " :：-—,，、|")
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	return out
}

// looksLikeUpstreamInfraError 判断文案是不是上游基础设施内部错误：
// 强信号单独命中即是；软信号须有内网 IPv4 或连接串特征佐证。
func looksLikeUpstreamInfraError(text string) bool {
	lowerAll := strings.ToLower(text)
	for _, signal := range upstreamInfraHardSignals {
		if strings.Contains(lowerAll, signal) {
			return true
		}
	}
	soft := false
	for _, signal := range upstreamInfraSoftSignals {
		if strings.Contains(lowerAll, signal) {
			soft = true
			break
		}
	}
	if !soft {
		return false
	}
	if reIPv4.MatchString(lowerAll) {
		return true
	}
	for _, mark := range upstreamInfraCorroboration {
		if strings.Contains(lowerAll, mark) {
			return true
		}
	}
	return false
}

// replaceFold 大小写不敏感地删除 token（Go 标准库没有现成的 fold 替换）。
//
// 只在词边界上匹配：token 两侧不能紧邻 ASCII 字母 / 数字 / 下划线。否则一个叫 max 的
// 账号会把客户看的 "max_tokens must be…" 删成 "tokens must be…"。CJK 没有词边界，
// 所以边界只看 ASCII 词字符——"账号贾克斯拒绝" 里的 贾克斯 照常命中。
//
// ⚠️ 下标必须取自原串：不能用 strings.Index(strings.ToLower(text), token) 的结果去切 text。
// ToLower 会改变字节长度（İ Ⱥ Ⱦ ẞ Ω K Å 七个字符），下标一旦错位，轻则删错位置、
// 输出非法 UTF-8（土耳其语大写文本实测「İÇERİK acme」被删成「İÇERİ」），
// 重则 text[:idx] 越界 panic。这里逐 rune 折叠比较，匹配区间始终是原串的字节下标。
func replaceFold(text, token, replacement string, mode boundaryMode) string {
	if token == "" {
		return text
	}
	folded := foldRunes(token)
	var b strings.Builder
	for {
		start, end := indexFold(text, folded, mode)
		if start < 0 {
			b.WriteString(text)
			return b.String()
		}
		b.WriteString(text[:start])
		b.WriteString(replacement)
		// 连带吃掉紧邻的连接符：贾克斯-pro 删掉后不该剩 "-0.15"，new_api 删掉后不该剩 "_error"
		text = strings.TrimLeft(text[end:], mode.connectors())
	}
}

// boundaryMode 词边界口径。
type boundaryMode int

const (
	// boundaryWord 字母 / 数字 / 下划线都算词字符（正则 \w 的 ASCII 子集）：
	// 账号推导 token 用，max 不能命中 max_tokens。
	boundaryWord boundaryMode = iota
	// boundaryAlnum 只有字母 / 数字算词字符，下划线视为边界：
	// 产品名固定表用，new_api 要能命中 new_api_error。
	boundaryAlnum
)

func (m boundaryMode) isWordByte(b byte) bool {
	if b == '_' {
		return m == boundaryWord
	}
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func (m boundaryMode) connectors() string {
	if m == boundaryAlnum {
		return "_-"
	}
	return "-"
}

// foldRunes 把字符串逐 rune 折叠成小写，供 indexFold 做等长比较。
func foldRunes(s string) []rune {
	runes := make([]rune, 0, len(s))
	for _, r := range s {
		runes = append(runes, unicode.ToLower(r))
	}
	return runes
}

// indexFold 在 text 中大小写不敏感、按词边界查找 token（已折叠为小写 rune 序列），
// 返回匹配在 text 中的起止字节下标；未命中返回 -1, -1。
func indexFold(text string, token []rune, mode boundaryMode) (int, int) {
	if len(token) == 0 {
		return -1, -1
	}
	// range string 的下标天然落在 rune 边界上，不会切出半个字符。
	for start := range text {
		end, ok := matchFoldAt(text, start, token)
		if !ok {
			continue
		}
		if (start > 0 && mode.isWordByte(text[start-1])) || (end < len(text) && mode.isWordByte(text[end])) {
			continue
		}
		return start, end
	}
	return -1, -1
}

// matchFoldAt 判断 text 从 start 起是否折叠后匹配 token，返回匹配结束的字节下标。
func matchFoldAt(text string, start int, token []rune) (int, bool) {
	pos := start
	for _, want := range token {
		if pos >= len(text) {
			return 0, false
		}
		got, size := utf8.DecodeRuneInString(text[pos:])
		if unicode.ToLower(got) != want {
			return 0, false
		}
		pos += size
	}
	return pos, true
}

// scrubErrorBody 清洗上游错误体。保留错误体结构（error.type / code / param 等
// 客户端要用的字段），只清洗其中的字符串值并删掉纯供应商追踪键。
//
// 返回 ok=false 表示这个体不可清洗（非 JSON、或清洗后信息为空），
// 调用方应改用我方生成的协议错误体。
func (s *identityScrubber) scrubErrorBody(body []byte) ([]byte, bool) {
	if len(body) == 0 {
		return nil, false
	}
	payload, ok := decodeJSONPreservingNumbers(body)
	if !ok {
		return nil, false
	}
	walk := &scrubWalk{scrubber: s}
	cleaned, kept := walk.value(payload)
	// 没有任何内容被改动时返回原始字节：不重新序列化，避免无谓地改变
	// 键序与格式——绝大多数上游 4xx 本来就不含供应商标识。
	// 这一判断必须先于 kept：{"code":400,"success":false} 这种没有字符串的体
	// 同样"没信息可清洗"，但它是客户端要读的合法错误体，不能被当成空体回落我方文案。
	if !walk.changed {
		return body, true
	}
	if !kept {
		return nil, false
	}
	encoded, err := json.Marshal(cleaned)
	if err != nil {
		return nil, false
	}
	return encoded, true
}

// decodeJSONPreservingNumbers 解析错误体，数字以 json.Number 原样保留：
// 默认 float64 会把 task_id 这类大整数改值（9007199254740993 → …992），
// 而 hostForwardPayload 服务的异步任务型平台恰恰最可能在 4xx 体里带任务 ID。
func decodeJSONPreservingNumbers(body []byte) (any, bool) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var payload any
	if err := dec.Decode(&payload); err != nil {
		return nil, false
	}
	// 与 json.Unmarshal 口径一致：正文后不得有多余内容。
	if _, err := dec.Token(); err != io.EOF {
		return nil, false
	}
	return payload, true
}

// scrubWalk 递归清洗时携带「是否真的改动过」，用于零改动时保留原始字节。
type scrubWalk struct {
	scrubber *identityScrubber
	changed  bool
}

// value 递归清洗 JSON 值。kept=false 表示这个值清洗后已无信息量。
func (w *scrubWalk) value(value any) (any, bool) {
	switch typed := value.(type) {
	case string:
		cleaned := w.scrubber.scrubText(typed)
		if cleaned != typed {
			w.changed = true
		}
		return cleaned, cleaned != ""
	case []any:
		out := make([]any, 0, len(typed))
		kept := false
		for _, item := range typed {
			cleanedItem, itemKept := w.value(item)
			out = append(out, cleanedItem)
			kept = kept || itemKept
		}
		return out, kept
	case map[string]any:
		out := make(map[string]any, len(typed))
		kept := false
		for key, item := range typed {
			if _, drop := upstreamIDBodyKeys[strings.ToLower(key)]; drop {
				w.changed = true
				continue
			}
			cleanedItem, itemKept := w.value(item)
			out[key] = cleanedItem
			kept = kept || itemKept
		}
		return out, kept
	default:
		// 数字（json.Number）/ 布尔 / null 不承载供应商身份，原样保留但不单独构成"有信息"。
		return value, false
	}
}

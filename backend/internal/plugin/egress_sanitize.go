package plugin

import (
	"encoding/json"
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
	// 裸 URL 与裸域名
	reURL        = regexp.MustCompile(`(?i)\bhttps?://[^\s"'<>)）\]]+`)
	reBareDomain = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+(?:com|cn|net|org|io|ai|co|dev|app|xyz|top|me|tech)\b`)
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

// upstreamInfraSignals 上游把自己的基础设施内部错误（数据库连接、进程崩溃栈）
// 当成 4xx 文案回给我们。这类文案对客户毫无用处，却会暴露上游的技术栈与内网结构，
// 命中即整条判为不可用，回落我方兜底文案。
// 生产实测样本：failed to connect to `user=postgres database=new-api`: 10.0.1.10:5432 …
// no pg_hba.conf entry for host "10.0.25.41"。
var upstreamInfraSignals = []string{
	"pg_hba", "user=postgres", "postgresql", "mysql", "redis://",
	"goroutine ", "panic:", "traceback (most recent call last)",
	"connection refused", "no such host",
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

// identityScrubber 按「当前这次请求实际用的上游账号」推导要抹掉的标识。
// 相比维护一张全局供应商名单，这样零维护、也不会误伤无关文本。
type identityScrubber struct {
	tokens []string // 全小写，按长度降序，先长后短避免残留
}

// newIdentityScrubber 按账号推导要抹掉的标识。model 是本次请求的模型名，
// 用来防呆：账号常按模型命名（生产上有 seedance-inference-1、腾讯tokenhub-GLM5.3-7折
// 这类），万一账号名恰好就是模型名，抹掉它会把客户最需要看的"哪个模型不支持什么"
// 一起删掉——而模型名本来就是客户自己传的，也谈不上泄漏。
func newIdentityScrubber(acc *ent.Account, model string) *identityScrubber {
	s := &identityScrubber{}
	seen := make(map[string]struct{})
	lowerModel := strings.ToLower(strings.TrimSpace(model))
	add := func(token string) {
		token = strings.ToLower(strings.TrimSpace(token))
		// 太短的 token（如账号名 "a"）会把正常文本打穿，直接跳过。
		if len([]rune(token)) < 3 {
			return
		}
		// 与模型名重合的 token 不抹（见函数注释）。
		if lowerModel != "" && (strings.Contains(lowerModel, token) || strings.Contains(token, lowerModel)) {
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
	// 中继产品名与模型无关，不走上面的模型防呆。
	for _, token := range upstreamVendorTokens {
		token = strings.ToLower(token)
		if _, dup := seen[token]; dup {
			continue
		}
		seen[token] = struct{}{}
		s.tokens = append(s.tokens, token)
	}

	// 长 token 先替换，避免 "api.example.com" 被 "example.com" 先吃掉一半。
	for i := 1; i < len(s.tokens); i++ {
		for j := i; j > 0 && len(s.tokens[j]) > len(s.tokens[j-1]); j-- {
			s.tokens[j], s.tokens[j-1] = s.tokens[j-1], s.tokens[j]
		}
	}
	return s
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
	} else {
		tokens = upstreamVendorTokens
	}
	out := text

	// 1. 结构化前缀 / 尾注
	out = reProviderPrefix.ReplaceAllString(out, "")
	out = reUpstreamFailedPrefix.ReplaceAllString(out, "")
	out = reVendorCodePrefix.ReplaceAllString(out, "")
	out = reUpstreamRequestIDNote.ReplaceAllString(out, "")

	// 2. 上游基础设施内部错误整条判废
	lowerAll := strings.ToLower(out)
	for _, signal := range upstreamInfraSignals {
		if strings.Contains(lowerAll, signal) {
			return ""
		}
	}

	// 3. 上游 HTML 错误页整段截断（保留我方加的前缀，如 "Asset provider returned non-JSON:"）
	if loc := reHTMLPageStart.FindStringIndex(out); loc != nil {
		out = out[:loc[0]]
	}

	// 4. URL、域名、Web 服务器指纹与内网地址
	out = reURL.ReplaceAllString(out, "")
	out = reBareDomain.ReplaceAllString(out, "")
	out = reServerFingerprint.ReplaceAllString(out, "")
	out = reIPv4.ReplaceAllString(out, "")

	// 5. 账号推导出的供应商标识 + 产品名兜底清单（大小写不敏感）
	for _, token := range tokens {
		out = replaceFold(out, token, "")
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

// replaceFold 大小写不敏感地删除 token（Go 标准库没有现成的 fold 替换）。
//
// ⚠️ 下标必须取自原串：不能用 strings.Index(strings.ToLower(text), token) 的结果去切 text。
// ToLower 会改变字节长度（İ Ⱥ Ⱦ ẞ Ω K Å 七个字符），下标一旦错位，轻则删错位置、
// 输出非法 UTF-8（土耳其语大写文本实测「İÇERİK acme」被删成「İÇERİ」），
// 重则 text[:idx] 越界 panic。这里逐 rune 折叠比较，匹配区间始终是原串的字节下标。
func replaceFold(text, token, replacement string) string {
	if token == "" {
		return text
	}
	folded := foldRunes(token)
	var b strings.Builder
	for {
		start, end := indexFold(text, folded)
		if start < 0 {
			b.WriteString(text)
			return b.String()
		}
		b.WriteString(text[:start])
		b.WriteString(replacement)
		// 连带吃掉紧邻的连接符：new_api_error 删掉 new_api 后不该剩 "_error"
		text = strings.TrimLeft(text[end:], "_-")
	}
}

// foldRunes 把字符串逐 rune 折叠成小写，供 indexFold 做等长比较。
func foldRunes(s string) []rune {
	runes := make([]rune, 0, len(s))
	for _, r := range s {
		runes = append(runes, unicode.ToLower(r))
	}
	return runes
}

// indexFold 在 text 中大小写不敏感地查找 token（已折叠为小写 rune 序列），
// 返回匹配在 text 中的起止字节下标；未命中返回 -1, -1。
func indexFold(text string, token []rune) (int, int) {
	if len(token) == 0 {
		return -1, -1
	}
	// range string 的下标天然落在 rune 边界上，不会切出半个字符。
	for start := range text {
		if end, ok := matchFoldAt(text, start, token); ok {
			return start, end
		}
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
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false
	}
	walk := &scrubWalk{scrubber: s}
	cleaned, kept := walk.value(payload)
	if !kept {
		return nil, false
	}
	// 没有任何内容被改动时返回原始字节：不重新序列化，避免无谓地改变
	// 键序与格式——绝大多数上游 4xx 本来就不含供应商标识。
	if !walk.changed {
		return body, true
	}
	encoded, err := json.Marshal(cleaned)
	if err != nil {
		return nil, false
	}
	return encoded, true
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
		// 数字 / 布尔 / null 不承载供应商身份，原样保留但不单独构成"有信息"。
		return value, false
	}
}

package plugin

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/i18n"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// 网关对外报错必须走 i18n key（locales/*.json 的 gw.* 条目），禁止裸中文——
// 2026-09-10 西语客户在控制台看到「当前平台不支持该 API 路径」就是这么来的。
//
// 守卫范围：
//  1. 本包与 server/middleware 里所有写对外错误响应的出口函数的实参；
//  2. usageFailure{message: …} 字面量；
//  3. 本包所有 status.Error / status.Errorf 的实参——Host 的 gRPC 报错会被插件
//     原样写进 tasks.error_message 展示给终端用户（2026-08~09 创作工作坊 10746 条
//     报错里 9935 条是中文，就是这条路漏的）；
//  4. 先赋给变量/常量、再喂进上述出口的中文字面量（旧守卫的盲区：
//     hostErrMemberQuotaExhausted = "团队成员额度已用尽…" 这种两跳写法）。
//
// 命中 CJK 字符串字面量即失败。
//
// 未纳入守卫（都不面向网关终端用户，故意留中文）：
//   - manager_*.go / marketplace.go / extension_proxy.go：插件安装、市场、控制台
//     反代的报错，只出现在管理员后台与日志；
//   - relay.go 的媒体中继 HTTP 报错（另一条对外面，见 PR 说明的后续项）；
//   - probeForward 的 errProbeResp：健康探测插件写进 group_health_probes，
//     只在后台异常监控页展示。
var gatewayErrorEmitters = map[string]bool{
	"protocolError":          true,
	"protocolStreamError":    true,
	"protocolRateLimitError": true,
	"abortWithOpenAIError":   true,
	"writeUnauthenticated":   true,
}

// cjkTaintNameHints 会被跟踪的"文案变量"名片段（小写包含即算）。
// 只有右值确实是中文字符串字面量时才染色，所以把 err 放进来不会误伤 error 变量。
var cjkTaintNameHints = []string{"msg", "message", "reason", "text", "desc", "err"}

func isMessageIdentName(name string) bool {
	lower := strings.ToLower(name)
	for _, hint := range cjkTaintNameHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

// guardedDirs 守卫覆盖的包目录。status.Error 守卫只对本包（internal/plugin）生效，
// middleware 里没有 gRPC 出口。
var guardedDirs = []string{".", filepath.Join("..", "server", "middleware")}

// isStatusErrorCall 识别 status.Error / status.Errorf（按 pkg.Sel 精确匹配，
// 避免把任意 x.Error() 也算进来）。
func isStatusErrorCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "status" {
		return false
	}
	return sel.Sel.Name == "Error" || sel.Sel.Name == "Errorf"
}

func parseGuardedFiles(t *testing.T, fset *token.FileSet) map[string]*ast.File {
	t.Helper()
	files := map[string]*ast.File{}
	for _, dir := range guardedDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			files[path] = file
		}
	}
	return files
}

// derivedEmitters 找出"把自己的 string 形参直接转手给出口函数"的包内薄封装
// （如 hostForwardQuotaExhaustedError(message) → status.Error(code, message)），
// 让守卫能穿透这一跳。返回 函数名 → 需要检查的形参下标集合。
func derivedEmitters(files map[string]*ast.File) map[string]map[int]bool {
	derived := map[string]map[int]bool{}
	isEmitterCall := func(call *ast.CallExpr) bool {
		return gatewayErrorEmitters[calleeName(call)] || isStatusErrorCall(call) || derived[calleeName(call)] != nil
	}
	// 迭代到不动点：薄封装可能再套一层薄封装。
	for round := 0; round < 3; round++ {
		changed := false
		for _, file := range files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil || fn.Type.Params == nil {
					continue
				}
				params := map[string]int{}
				idx := 0
				for _, field := range fn.Type.Params.List {
					for _, ident := range field.Names {
						params[ident.Name] = idx
						idx++
					}
				}
				if len(params) == 0 {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok || !isEmitterCall(call) {
						return true
					}
					for _, arg := range call.Args {
						ident, ok := arg.(*ast.Ident)
						if !ok {
							continue
						}
						pos, ok := params[ident.Name]
						if !ok {
							continue
						}
						if derived[fn.Name.Name] == nil {
							derived[fn.Name.Name] = map[int]bool{}
						}
						if !derived[fn.Name.Name][pos] {
							derived[fn.Name.Name][pos] = true
							changed = true
						}
					}
					return true
				})
			}
		}
		if !changed {
			break
		}
	}
	return derived
}

// packageCJKIdents 收集包级 const/var 里含中文的字符串标识符。
func packageCJKIdents(files map[string]*ast.File) map[string]string {
	tainted := map[string]string{}
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range value.Names {
					if i >= len(value.Values) {
						continue
					}
					if lit := firstCJKLiteral(value.Values[i]); lit != "" {
						tainted[name.Name] = lit
					}
				}
			}
		}
	}
	return tainted
}

func TestGatewayErrorMessagesNeverHardcodeCJK(t *testing.T) {
	fset := token.NewFileSet()
	files := parseGuardedFiles(t, fset)
	derived := derivedEmitters(files)
	pkgTainted := packageCJKIdents(files)

	var violations []string
	report := func(pos token.Pos, lit string) {
		violations = append(violations, fset.Position(pos).String()+": "+lit)
	}

	for path, file := range files {
		_ = path
		// localTainted 按整个文件累计：函数内 msg := "中文" 之后再喂给出口即算命中。
		localTainted := map[string]string{}
		taint := func(names []ast.Expr, values []ast.Expr) {
			for i, lhs := range names {
				ident, ok := lhs.(*ast.Ident)
				if !ok || i >= len(values) || !isMessageIdentName(ident.Name) {
					continue
				}
				if lit := firstCJKLiteral(values[i]); lit != "" {
					localTainted[ident.Name] = lit
				}
			}
		}
		checkArgs := func(args []ast.Expr, want map[int]bool) {
			for i, arg := range args {
				if want != nil && !want[i] {
					continue
				}
				if lit := firstCJKLiteral(arg); lit != "" {
					report(arg.Pos(), lit)
					continue
				}
				if ident, ok := arg.(*ast.Ident); ok {
					if lit, bad := localTainted[ident.Name]; bad {
						report(arg.Pos(), ident.Name+" = "+lit)
					} else if lit, bad := pkgTainted[ident.Name]; bad {
						report(arg.Pos(), ident.Name+" = "+lit)
					}
				}
			}
		}

		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.AssignStmt:
				taint(node.Lhs, node.Rhs)
			case *ast.ValueSpec:
				names := make([]ast.Expr, 0, len(node.Names))
				for _, ident := range node.Names {
					names = append(names, ident)
				}
				taint(names, node.Values)
			case *ast.CallExpr:
				name := calleeName(node)
				switch {
				case gatewayErrorEmitters[name]:
					checkArgs(node.Args, nil)
				case isStatusErrorCall(node):
					checkArgs(node.Args, nil)
				case derived[name] != nil:
					checkArgs(node.Args, derived[name])
				}
			case *ast.KeyValueExpr:
				if key, ok := node.Key.(*ast.Ident); ok && key.Name == "message" {
					if lit := firstCJKLiteral(node.Value); lit != "" {
						report(node.Value.Pos(), lit)
					}
				}
			}
			return true
		})
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("对外报错文案禁止裸中文，请改为 i18n.Tc(c, \"gw.xxx\") / i18n.En(\"gw.xxx\") 并在 internal/i18n/locales/*.json 补齐五种语言:\n  %s",
			strings.Join(violations, "\n  "))
	}
}

func calleeName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

// firstCJKLiteral 深入表达式（含字符串拼接）找第一个含 CJK 的字符串字面量。
func firstCJKLiteral(expr ast.Expr) string {
	var found string
	ast.Inspect(expr, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		for _, r := range lit.Value {
			if unicode.Is(unicode.Han, r) {
				found = lit.Value
				return false
			}
		}
		return true
	})
	return found
}

// 每个对外 key 在五种语言里都要有：缺一种就会回落到 zh 默认语言，等于又漏中文。
func TestGatewayMessageKeysCoverAllLocales(t *testing.T) {
	if err := i18n.LoadEmbedded(); err != nil {
		t.Fatal(err)
	}
	usedKeys := map[string]bool{}
	fset := token.NewFileSet()
	for _, dir := range []string{".", filepath.Join("..", "server", "middleware")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if ok && lit.Kind == token.STRING && strings.HasPrefix(lit.Value, `"gw.`) {
					usedKeys[strings.Trim(lit.Value, `"`)] = true
				}
				return true
			})
		}
	}
	if len(usedKeys) == 0 {
		t.Fatal("未扫描到任何 gw.* key，守卫本身失效")
	}
	for key := range usedKeys {
		for _, lang := range i18n.SupportedLanguages() {
			if !i18n.Has(lang, key) {
				t.Errorf("locales/%s.json 缺少 %s", lang, key)
			}
		}
	}
}

// 对外文案跟随 Accept-Language：无头默认英文，es/zh 各取各的词典，落库固定英文。
func TestGatewayMessagesFollowAcceptLanguage(t *testing.T) {
	if err := i18n.LoadEmbedded(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		header string
		lang   string
	}{
		{"", "en"},
		{"es-MX,es;q=0.9", "es"},
		{"zh-CN", "zh"},
		{"ja", "ja"},
		{"zh-TW", "zh-HK"},
		{"fr", "en"},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		if tc.header != "" {
			c.Request.Header.Set("Accept-Language", tc.header)
		}
		got := sanitizedMessage(c, sdk.OutcomeUpstreamTransient)
		want := i18n.Tf(tc.lang, "gw.upstream_error")
		if got != want || strings.HasPrefix(got, "gw.") {
			t.Errorf("Accept-Language %q: got %q, want %q", tc.header, got, want)
		}
	}
	if got := allRoutesFailureLogMessage("gw.no_available_account", 1); got != "No upstream account is available right now, please retry later (1 upstream attempt(s) made)" {
		t.Errorf("落库文案应固定英文，得到 %q", got)
	}
}

// Host（gRPC）报错没有请求上下文，一律英文；插件会把这段文案原样写进
// tasks.error_message 展示给终端用户，所以这里逐条钉死生产上出现过的高频文案。
func TestHostErrorsSpeakEnglish(t *testing.T) {
	if err := i18n.LoadEmbedded(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		err  error
		code codes.Code
		want string
	}{
		{
			name: "insufficient_quota",
			err:  hostForwardInsufficientQuotaError(),
			code: codes.ResourceExhausted,
			want: "Insufficient balance",
		},
		{
			name: "member_quota",
			err:  hostForwardQuotaExhaustedError("gw.member_quota_exhausted"),
			code: codes.ResourceExhausted,
			want: "Member quota exhausted, please contact your enterprise administrator",
		},
		{
			name: "department_quota",
			err:  hostForwardQuotaExhaustedError("gw.department_quota_exhausted"),
			code: codes.ResourceExhausted,
			want: "Department quota exhausted, please contact your enterprise administrator",
		},
		{
			name: "group_offline",
			err:  hostSchedulerError(scheduler.ErrGroupOffline),
			code: codes.NotFound,
			want: i18n.En("gw.group_offline"),
		},
		{
			name: "model_not_served",
			err:  hostSchedulerError(scheduler.ErrModelNotServed),
			code: codes.NotFound,
			want: i18n.En("gw.model_not_served"),
		},
		{
			name: "no_available_account",
			err:  hostSchedulerError(scheduler.ErrAllCandidatesDisabled),
			code: codes.NotFound,
			want: i18n.En("gw.no_available_account"),
		},
		{
			name: "rate_limited",
			err:  hostSchedulerError(&scheduler.AccountsRateLimitedError{RetryAt: time.Now().Add(time.Minute)}),
			code: codes.NotFound,
			want: i18n.En("gw.all_routes_rate_limited"),
		},
		{
			name: "member_disabled",
			err:  hostMemberGateError(auth.ErrMemberDisabled),
			code: codes.PermissionDenied,
			want: i18n.En("gw.member_disabled"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, ok := status.FromError(tc.err)
			if !ok {
				t.Fatalf("%v 不是 gRPC status error", tc.err)
			}
			if st.Code() != tc.code {
				t.Errorf("code = %v, want %v", st.Code(), tc.code)
			}
			if st.Message() != tc.want {
				t.Errorf("message = %q, want %q", st.Message(), tc.want)
			}
			if containsHan(st.Message()) {
				t.Errorf("Host 报错仍是中文: %q", st.Message())
			}
		})
	}
}

// hostSchedulerError 只认调度哨兵：其它错误返回 nil，由调用方按内部错误兜底。
func TestHostSchedulerErrorIgnoresNonSentinel(t *testing.T) {
	if got := hostSchedulerError(errors.New("boom")); got != nil {
		t.Fatalf("hostSchedulerError(非哨兵) = %v, want nil", got)
	}
	if got := hostSchedulerError(nil); got != nil {
		t.Fatalf("hostSchedulerError(nil) = %v, want nil", got)
	}
}

func containsHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

package plugin

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/gin-gonic/gin"

	"github.com/DouDOU-start/airgate-core/internal/i18n"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// 网关对外报错必须走 i18n key（locales/*.json 的 gw.* 条目），禁止裸中文——
// 2026-09-10 西语客户在控制台看到「当前平台不支持该 API 路径」就是这么来的。
//
// 守卫范围：本包与 server/middleware 里所有写对外错误响应的出口函数的实参、
// 以及 usageFailure{message: …} 字面量。命中 CJK 字符串字面量即失败。
var gatewayErrorEmitters = map[string]bool{
	"protocolError":          true,
	"protocolStreamError":    true,
	"protocolRateLimitError": true,
	"abortWithOpenAIError":   true,
	"writeUnauthenticated":   true,
}

func TestGatewayErrorMessagesNeverHardcodeCJK(t *testing.T) {
	dirs := []string{".", filepath.Join("..", "server", "middleware")}
	fset := token.NewFileSet()
	var violations []string
	for _, dir := range dirs {
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
			ast.Inspect(file, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.CallExpr:
					if gatewayErrorEmitters[calleeName(node)] {
						for _, arg := range node.Args {
							if lit := firstCJKLiteral(arg); lit != "" {
								violations = append(violations, fset.Position(arg.Pos()).String()+": "+lit)
							}
						}
					}
				case *ast.KeyValueExpr:
					if key, ok := node.Key.(*ast.Ident); ok && key.Name == "message" {
						if lit := firstCJKLiteral(node.Value); lit != "" {
							violations = append(violations, fset.Position(node.Value.Pos()).String()+": "+lit)
						}
					}
				}
				return true
			})
		}
	}
	if len(violations) > 0 {
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

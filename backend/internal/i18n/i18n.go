// Package i18n 提供国际化支持
//
// 翻译文件 internal/i18n/locales/*.json 通过 //go:embed 嵌入二进制
// （见 embed.go），调用方使用 LoadEmbedded() 在启动时加载，无需关心运行
// 目录中是否存在 locales/ 目录。
//
// 两套受众、两个默认语言：
//   - 控制台 REST（middleware.I18n 写入 gin ctx 的 lang）：默认 zh；
//   - 网关 API 路径（ClientLang / Tc）：默认 en——API 调用方是程序，多数不带
//     Accept-Language，国际惯例是英文；带了 zh/es/ja/zh-HK 才按其语言返回。
//
// 网关对外报错一律经 key（locales 里的 gw.* 条目）取文案，禁止在 protocolError
// 等出口直接写裸中文（internal/plugin 有守卫测试拦截）。
package i18n

import (
	"fmt"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

var (
	translations = map[string]map[string]string{}
	mu           sync.RWMutex
	defaultLang  = "zh"
)

// LangEN 网关 API 路径的默认语言：所有对外 API 报错在客户端未声明语言时的兜底。
const LangEN = "en"

// ctxKeyClientLang gin ctx 里缓存的网关客户端语言，避免每条报错重复解析头。
const ctxKeyClientLang = "airgate.client_lang"

// T 获取翻译文本
func T(lang, key string) string {
	mu.RLock()
	defer mu.RUnlock()
	if msgs, ok := translations[lang]; ok {
		if val, ok := msgs[key]; ok {
			return val
		}
	}
	// 回退到默认语言
	if msgs, ok := translations[defaultLang]; ok {
		if val, ok := msgs[key]; ok {
			return val
		}
	}
	return key
}

// Tf 取翻译文本并按 fmt.Sprintf 填充占位符；无参数时等价于 T。
func Tf(lang, key string, args ...any) string {
	text := T(lang, key)
	if len(args) == 0 {
		return text
	}
	return fmt.Sprintf(text, args...)
}

// En 取英文文案。使用日志 error_message 等落库字段统一存英文，
// 与请求方语言无关，运营与客户在控制台看到的是同一份记录。
func En(key string, args ...any) string {
	return Tf(LangEN, key, args...)
}

// DetectLanguage 解析 Accept-Language 头，返回受支持的语言代码；
// 头为空或没有任何受支持语言时返回 fallback。
//
// 支持：zh（含 zh-CN 等简体）、zh-HK（zh-HK / zh-TW / zh-Hant 繁体）、en、es、ja。
// 按声明顺序取第一个受支持的语言（不解析 q 权重，客户端极少乱序）。
func DetectLanguage(header, fallback string) string {
	if header == "" {
		return fallback
	}
	for _, part := range strings.Split(strings.ToLower(header), ",") {
		tag := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		switch {
		case tag == "":
			continue
		case strings.HasPrefix(tag, "en"):
			return "en"
		case strings.HasPrefix(tag, "zh-hk"), strings.HasPrefix(tag, "zh-tw"),
			strings.HasPrefix(tag, "zh-hant"), strings.HasPrefix(tag, "zh-mo"):
			return "zh-HK"
		case strings.HasPrefix(tag, "zh"):
			return "zh"
		case strings.HasPrefix(tag, "es"):
			return "es"
		case strings.HasPrefix(tag, "ja"):
			return "ja"
		}
	}
	return fallback
}

// ClientLang 网关 API 请求方的语言：由 Accept-Language 解析，默认英文。
// 结果缓存在 gin ctx；c 为 nil 时返回英文。
func ClientLang(c *gin.Context) string {
	if c == nil {
		return LangEN
	}
	if v, ok := c.Get(ctxKeyClientLang); ok {
		if lang, ok := v.(string); ok && lang != "" {
			return lang
		}
	}
	header := ""
	if c.Request != nil {
		header = c.Request.Header.Get("Accept-Language")
	}
	lang := DetectLanguage(header, LangEN)
	c.Set(ctxKeyClientLang, lang)
	return lang
}

// Tc 按网关请求方语言取文案（网关对外报错的唯一出口）。
func Tc(c *gin.Context, key string, args ...any) string {
	return Tf(ClientLang(c), key, args...)
}

// SupportedLanguages 已加载词典的语言列表（与 locales/*.json 一一对应）。
func SupportedLanguages() []string {
	mu.RLock()
	defer mu.RUnlock()
	langs := make([]string, 0, len(translations))
	for lang := range translations {
		langs = append(langs, lang)
	}
	return langs
}

// Has 指定语言的词典里是否存在该 key（不回退默认语言）。
func Has(lang, key string) bool {
	mu.RLock()
	defer mu.RUnlock()
	msgs, ok := translations[lang]
	if !ok {
		return false
	}
	_, ok = msgs[key]
	return ok
}

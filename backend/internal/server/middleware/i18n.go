package middleware

import (
	"github.com/DouDOU-start/airgate-core/internal/i18n"
	"github.com/gin-gonic/gin"
)

// Context Key 常量
const CtxKeyLang = "lang"

// I18n 国际化中间件
// 从 Accept-Language 头检测语言，设置到 Context
func I18n() gin.HandlerFunc {
	return func(c *gin.Context) {
		lang := detectLanguage(c.GetHeader("Accept-Language"))
		c.Set(CtxKeyLang, lang)
		c.Next()
	}
}

// detectLanguage 从 Accept-Language 头检测语言（控制台 REST 口径：默认 zh）。
// 解析规则与网关共用 i18n.DetectLanguage；网关 API 路径默认英文，见 i18n.ClientLang。
func detectLanguage(header string) string {
	return i18n.DetectLanguage(header, "zh")
}

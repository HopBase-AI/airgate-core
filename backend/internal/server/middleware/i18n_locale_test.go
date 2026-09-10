package middleware

import "github.com/DouDOU-start/airgate-core/internal/i18n"

// 中间件对外报错走 i18n key，测试断言真实文案前先加载词典。
func init() {
	if err := i18n.LoadEmbedded(); err != nil {
		panic(err)
	}
}

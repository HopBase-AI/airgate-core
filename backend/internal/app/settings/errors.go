package settings

import "errors"

// ErrSMTPConnection SMTP 连接/发送失败（用户可修正的配置错误）。
var ErrSMTPConnection = errors.New("SMTP 连接或发送失败")

// ErrGenerateKey 密钥生成失败。
var ErrGenerateKey = errors.New("密钥生成失败")

// ErrEncryptKey 密钥加密失败。
var ErrEncryptKey = errors.New("密钥加密失败")

// ErrModelCatalogCurrency 模型目录覆盖层写入了 currency=CNY：USD 账本下「人民币牌价按 1:1 记账」
// 已无意义，人民币牌价模型须在插件侧折算成美元基准价。管理员可修正的配置错误。
var ErrModelCatalogCurrency = errors.New("模型目录覆盖层不再接受 currency=CNY")

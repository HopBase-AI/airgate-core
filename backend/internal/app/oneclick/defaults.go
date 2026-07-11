// Package oneclick 提供「一键接入 Claude Code」的领域用例。
//
// 流程：控制台签发一次性 setup token → 用户终端执行个性化命令 →
// 脚本用 token 兑换真实 API Key → 配置 ~/.claude/settings.json →
// 自检通过后回执 verify → 控制台轮询到 verified 展示成功。
//
// 设计要点：
//   - token 状态存 Redis（TTL 内单次兑换），不落库、不新增 ent 表；
//   - Redis 里只存 user_id/key_id/状态，不存 key 明文——兑换时经
//     apikey.Service.RevealOwned 按落库密文即时解密；
//   - BaseURL/SiteName 回退链与 openclaw 域一致：setting 显式值优先，
//     为空时由 handler 按请求 Host 推导。
package oneclick

import (
	_ "embed"
	"time"
)

// setupTokenTTL 一次性接入令牌的有效期。
//
// 覆盖"复制命令→切终端→（可能顺路装 Claude Code）→脚本回执"的全过程；
// 兑换/回执各阶段都会以该 TTL 重置过期时间，令牌本身只能兑换一次。
const setupTokenTTL = 15 * time.Minute

// setupTokenKeyPrefix Redis key 前缀，完整格式 oneclick:setup:{token}。
const setupTokenKeyPrefix = "oneclick:setup:"

// SetupTokenTTL 返回令牌有效期，供 handler 组装 expires_in 响应。
func SetupTokenTTL() time.Duration { return setupTokenTTL }

// token 生命周期状态。pending → exchanged → verified 单向推进。
const (
	StatusPending   = "pending"   // 已签发，等待脚本兑换
	StatusExchanged = "exchanged" // 已兑换出 API Key，等待脚本自检回执
	StatusVerified  = "verified"  // 脚本自检通过并回执，接入完成
	StatusExpired   = "expired"   // 查询不到（过期/不存在），仅作为状态查询的返回值
)

// setupScriptTemplate 是 /oneclick/setup.sh 返回的 bash 脚本模板，go:embed 打进二进制。
//
//go:embed assets/setup.sh.tmpl
var setupScriptTemplate string

// SetupScriptTemplate 返回 bash 接入脚本模板原文。
func SetupScriptTemplate() string {
	return setupScriptTemplate
}

// setupScriptPowerShellTemplate 是 /oneclick/setup.ps1 返回的 PowerShell 脚本模板。
//
//go:embed assets/setup.ps1.tmpl
var setupScriptPowerShellTemplate string

// SetupScriptPowerShellTemplate 返回 PowerShell 接入脚本模板原文。
func SetupScriptPowerShellTemplate() string {
	return setupScriptPowerShellTemplate
}

package upstreamalert

import "strings"

// outOfCreditPhrases 上游「余额/额度花光」的判据。
//
// 只收**明确指向计费**的措辞。上游挂了、限流、鉴权失败都不算——那些换个号就能绕过，
// 而欠费不会自愈，必须有人去充值，这正是要单独报警的原因。
//
// 中文串来自生产实测：锦坤东中继 403 返回「用户额度不足, 剩余额度: ¥-1.297390」，
// 2026-09-10 组 1 连挂 9.5 小时没人知道，就是被它拖的。
var outOfCreditPhrases = []string{
	// 中文中继
	"额度不足",
	"余额不足",
	"余额已用完",
	"欠费",
	"请充值",
	// OpenAI
	"exceeded your current quota",
	"insufficient_quota",
	"insufficient quota",
	// Anthropic
	"credit balance is too low",
	// 通用中继
	"insufficient balance",
	"out of credit",
	"no credit",
	"balance is not enough",
	"not enough balance",
}

// IsOutOfCredit 判断一段上游错误原文是不是「我们欠上游钱了」。
//
// 判据只看措辞不看状态码：同一件事各家包在 401 / 403 / 402 / 5xx 里都见过，
// 拿状态码当条件只会漏。调用方保证传进来的是上游返回的原文。
func IsOutOfCredit(reason string) bool {
	lowered := strings.ToLower(strings.TrimSpace(reason))
	if lowered == "" {
		return false
	}
	for _, phrase := range outOfCreditPhrases {
		if strings.Contains(lowered, phrase) {
			return true
		}
	}
	return false
}

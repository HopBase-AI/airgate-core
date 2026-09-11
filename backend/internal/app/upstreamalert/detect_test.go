package upstreamalert

import "testing"

func TestIsOutOfCredit(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		want   bool
	}{
		// 生产实测原文：2026-09-10 组 1 的锦坤东，403 连挂 9.5 小时
		{"锦坤东中继欠费原文", "HTTP 403: 用户额度不足, 剩余额度: ¥-1.297390 (request id: 2026091016223913745)", true},
		{"中文余额不足", "上游返回：账户余额不足，请充值后重试", true},
		{"中文欠费", "该密钥已欠费停用", true},
		{"OpenAI 配额用尽", "You exceeded your current quota, please check your plan and billing details.", true},
		{"OpenAI 结构化码", "insufficient_quota", true},
		{"Anthropic 余额过低", "Your credit balance is too low to access the Claude API", true},
		{"通用中继", "Insufficient balance, please recharge", true},
		{"大小写混杂", "OUT OF CREDIT", true},

		// 以下都不是欠费：换个号就能绕过，不该打扰人
		{"上游过载", "Our servers are currently overloaded. Please try again later.", false},
		{"零输出被守卫掐断", "upstream produced no output before the gateway idle limit", false},
		{"限流", "Rate limit reached for requests", false},
		{"凭证失效", "Incorrect API key provided", false},
		{"连接重置", "write: connection reset by peer", false},
		{"空字符串", "", false},
		{"只有空白", "   ", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsOutOfCredit(c.reason); got != c.want {
				t.Errorf("IsOutOfCredit(%q) = %v, want %v", c.reason, got, c.want)
			}
		})
	}
}

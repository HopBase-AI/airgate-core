package billing

// AvailableBalance 用户视角的真实可用余额:
//   - Key 设有花费上限(quotaUSD>0)时,Key 剩余额度和账户余额任一耗尽都会让
//     请求不可用,取两者较小值;第二个返回值为 Key 剩余额度;
//   - Key 无上限时,直接返回账户余额(两个返回值相同)。
//
// cc-switch 兼容端点(/v1/usage)与 MCP 管理面共用这一口径。
func AvailableBalance(userBalance, quotaUSD, usedQuota float64) (available, keyRemaining float64) {
	if userBalance < 0 {
		userBalance = 0
	}
	if quotaUSD <= 0 {
		return userBalance, userBalance
	}
	keyRemaining = quotaUSD - usedQuota
	if keyRemaining < 0 {
		keyRemaining = 0
	}
	if userBalance < keyRemaining {
		return userBalance, keyRemaining
	}
	return keyRemaining, keyRemaining
}

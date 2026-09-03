package billing

// AvailableBalance 持有者视角的可用额度:
//   - Key 设有花费上限(quotaUSD>0)时,只回 Key 剩余额度,第二个返回值同值;
//   - Key 无上限时,直接返回账户余额(两个返回值相同)。
//
// 2026-09-03 起不再与账户余额取 min:reseller / 企业主账号余额低于下游 Key 剩余额度时,
// 取 min 会把主账号余额原样露给 Key 持有者(end customer / 团队成员)。主账号余额
// 耗尽由转发时的 402 兜底,展示口径只认持有者自己的额度。
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
	return keyRemaining, keyRemaining
}

// CapByMemberQuota 团队成员设有额度时,把可用额度再压到成员本期剩余(两者取小);
// 成员不限额(memberQuota<=0)时原样返回。
func CapByMemberQuota(available, memberQuota, memberUsed float64) float64 {
	if memberQuota <= 0 {
		return available
	}
	remaining := memberQuota - memberUsed
	if remaining < 0 {
		remaining = 0
	}
	if remaining < available {
		return remaining
	}
	return available
}

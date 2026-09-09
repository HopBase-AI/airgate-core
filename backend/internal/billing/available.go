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

// CapByQuotas 三层取小：在 Key/余额口径的可用额度之上，再按团队成员本期剩余与部门本期剩余
// 各压一层（额度为 0 表示该层不限）。一次请求的可用额度 = min(余额/Key, 成员剩余, 部门剩余)，
// 任何展示与拦截都走这一个函数，不允许各页自己算。
func CapByQuotas(available, memberQuota, memberUsed, deptQuota, deptUsed float64) float64 {
	available = capByQuota(available, memberQuota, memberUsed)
	return capByQuota(available, deptQuota, deptUsed)
}

func capByQuota(available, quota, used float64) float64 {
	if quota <= 0 {
		return available
	}
	remaining := quota - used
	if remaining < 0 {
		remaining = 0
	}
	if remaining < available {
		return remaining
	}
	return available
}

package subscription

import "errors"

var (
	// ErrSubscriptionNotFound 订阅不存在。
	ErrSubscriptionNotFound = errors.New("订阅不存在")
	// ErrInvalidExpiresAt 分配订阅时过期时间格式错误。
	ErrInvalidExpiresAt = errors.New("过期时间格式错误，请使用 RFC3339 格式")
	// ErrInvalidAdjustExpiresAt 调整订阅时过期时间格式错误。
	ErrInvalidAdjustExpiresAt = errors.New("过期时间格式错误")

	// ErrPlanNotFound 套餐不存在（分组不存在或不是订阅制分组）。
	ErrPlanNotFound = errors.New("套餐不存在")
	// ErrPlanNotPurchasable 套餐未开放自助购买（对应周期未定价或分组已下架）。
	ErrPlanNotPurchasable = errors.New("该套餐暂不支持自助购买")
	// ErrInvalidBillingCycle 购买周期非法。
	ErrInvalidBillingCycle = errors.New("购买周期只能是 monthly 或 annual")
	// ErrInsufficientBalance 余额不足以支付套餐或加购包。
	ErrInsufficientBalance = errors.New("余额不足，请先充值")
	// ErrTopupUnavailable 套餐未提供加购包。
	ErrTopupUnavailable = errors.New("该套餐不提供加购包")

	// ErrSubscriptionRequired 分组为订阅制但用户没有有效订阅。
	ErrSubscriptionRequired = errors.New("该分组需要有效订阅")
	// ErrSubscriptionExpired 订阅已到期。
	ErrSubscriptionExpired = errors.New("订阅已到期，请续费")
	// ErrSubscriptionSuspended 订阅被管理员暂停。
	ErrSubscriptionSuspended = errors.New("订阅已暂停")
	// ErrCreditsExhausted 本期点数已用完。
	ErrCreditsExhausted = errors.New("本期点数已用完，可加购或等待下期重置")
	// ErrVideoNotIncluded 套餐不含视频生成。
	ErrVideoNotIncluded = errors.New("当前套餐不包含视频生成")
	// ErrImageLimitReached 本期生图张数已达上限。
	ErrImageLimitReached = errors.New("本期生图张数已达套餐上限")
)

// Package mcp 是 MCP 管理面的业务层:鉴权委托、四个只读查询的口径与参数校验。
// JSON-RPC 协议编解码留在 handler;本层不感知传输细节,也不 import ent——
// key 验证经 bootstrap 注入的 ValidateFunc(实现在 internal/auth)完成。
package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	appmodelpricing "github.com/DouDOU-start/airgate-core/internal/app/modelpricing"
	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/billing"
)

// ValidateFunc 管理面 key 验证;bootstrap 用 auth.ValidateAPIKeyForManagement 包一层注入。
type ValidateFunc func(ctx context.Context, rawKey string) (*auth.APIKeyInfo, error)

// ErrInvalidUsageArgs 用量查询参数非法(日期格式/顺序/跨度/时区)。
// 参数来自 LLM 生成的自由文本,必须显式拒绝而不是静默丢谓词——
// usage store 对解析失败的日期会直接丢掉时间过滤,全表统计还谎报覆盖区间。
var ErrInvalidUsageArgs = errors.New("invalid usage args")

// usageMaxSpanDays 用量查询最大跨度,防 LLM 传超长区间打全表。
const usageMaxSpanDays = 92

type Service struct {
	validate ValidateFunc
	pricing  *appmodelpricing.Service
	usage    *appusage.Service
}

func NewService(validate ValidateFunc, pricing *appmodelpricing.Service, usage *appusage.Service) *Service {
	return &Service{validate: validate, pricing: pricing, usage: usage}
}

// Authenticate 验证 key 并返回其管理面视图。错误语义见 auth.ValidateAPIKeyForManagement。
func (s *Service) Authenticate(ctx context.Context, rawKey string) (*auth.APIKeyInfo, error) {
	return s.validate(ctx, rawKey)
}

// BalanceResult 余额查询结果,口径与 cc-switch 兼容端点一致(billing.AvailableBalance)。
type BalanceResult struct {
	BalanceUSD        float64
	AvailableUSD      float64
	QuotaTotalUSD     float64
	QuotaUsedUSD      float64
	QuotaRemainingUSD float64
	QuotaUnlimited    bool
}

func (s *Service) Balance(info *auth.APIKeyInfo) BalanceResult {
	available, keyRemaining := billing.AvailableBalance(info.UserBalance, info.QuotaUSD, info.UsedQuota)
	balance := info.UserBalance
	if balance < 0 {
		balance = 0
	}
	return BalanceResult{
		BalanceUSD:        balance,
		AvailableUSD:      available,
		QuotaTotalUSD:     info.QuotaUSD,
		QuotaUsedUSD:      info.UsedQuota,
		QuotaRemainingUSD: keyRemaining,
		QuotaUnlimited:    info.QuotaUSD <= 0,
	}
}

// KeyInfoResult Key 元信息查询结果。
type KeyInfoResult struct {
	Name           string
	KeyHint        string
	Status         string
	QuotaUSD       float64
	UsedQuotaUSD   float64
	MaxConcurrency int
	CreatedAt      time.Time
	ExpiresAt      *time.Time
	GroupID        int
	GroupName      string
}

func (s *Service) KeyInfo(info *auth.APIKeyInfo) KeyInfoResult {
	return KeyInfoResult{
		Name:           info.KeyName,
		KeyHint:        info.KeyHint,
		Status:         info.KeyStatus,
		QuotaUSD:       info.QuotaUSD,
		UsedQuotaUSD:   info.UsedQuota,
		MaxConcurrency: info.KeyMaxConcurrency,
		CreatedAt:      info.KeyCreatedAt,
		ExpiresAt:      info.KeyExpiresAt,
		GroupID:        info.GroupID,
		GroupName:      info.GroupName,
	}
}

// Models 返回该 key 作用域的模型与实付价视图(与控制台 /models/pricing/me 同源)。
func (s *Service) Models(ctx context.Context, info *auth.APIKeyInfo) (appmodelpricing.Result, error) {
	return s.pricing.APIKeyPricing(ctx, info.UserID, info.KeyID)
}

// UsageArgs 用量查询参数(全部来自 LLM 自由文本,逐项严格校验)。
type UsageArgs struct {
	StartDate string
	EndDate   string
	TZ        string
	Platform  string
	Model     string
}

// UsageResult 用量查询结果:回显生效区间 + end customer 账面投影。
type UsageResult struct {
	StartDate string
	EndDate   string
	TZ        string
	Stats     appusage.CustomerStats
}

func (s *Service) Usage(ctx context.Context, info *auth.APIKeyInfo, args UsageArgs) (UsageResult, error) {
	loc := time.UTC
	if args.TZ != "" {
		parsed, err := time.LoadLocation(args.TZ)
		if err != nil {
			return UsageResult{}, fmt.Errorf("%w: 未知时区 %q,请用 IANA 名称如 Asia/Shanghai", ErrInvalidUsageArgs, args.TZ)
		}
		loc = parsed
	}
	now := time.Now().In(loc)
	if args.EndDate == "" {
		args.EndDate = now.Format("2006-01-02")
	}
	if args.StartDate == "" {
		args.StartDate = now.AddDate(0, 0, -6).Format("2006-01-02")
	}
	start, err := time.ParseInLocation("2006-01-02", args.StartDate, loc)
	if err != nil {
		return UsageResult{}, fmt.Errorf("%w: start_date %q 不是 YYYY-MM-DD", ErrInvalidUsageArgs, args.StartDate)
	}
	end, err := time.ParseInLocation("2006-01-02", args.EndDate, loc)
	if err != nil {
		return UsageResult{}, fmt.Errorf("%w: end_date %q 不是 YYYY-MM-DD", ErrInvalidUsageArgs, args.EndDate)
	}
	if end.Before(start) {
		return UsageResult{}, fmt.Errorf("%w: end_date 早于 start_date", ErrInvalidUsageArgs)
	}
	if end.Sub(start) > usageMaxSpanDays*24*time.Hour {
		return UsageResult{}, fmt.Errorf("%w: 区间超过 %d 天上限", ErrInvalidUsageArgs, usageMaxSpanDays)
	}

	keyID := int64(info.KeyID)
	result, err := s.usage.UserStatsWithModels(ctx, int64(info.UserID), appusage.StatsFilter{
		APIKeyID:    &keyID,
		Platform:    args.Platform,
		Model:       args.Model,
		StartDate:   args.StartDate,
		EndDate:     args.EndDate,
		TZ:          args.TZ,
		ScopedToKey: true,
	})
	if err != nil {
		return UsageResult{}, err
	}
	return UsageResult{
		StartDate: args.StartDate,
		EndDate:   args.EndDate,
		TZ:        loc.String(),
		Stats:     appusage.CustomerViewOf(result),
	}, nil
}

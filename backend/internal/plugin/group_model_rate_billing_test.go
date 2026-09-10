package plugin

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	"github.com/DouDOU-start/airgate-core/ent/migrate"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/routing"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

const (
	modelRateGroupRate   = 6.8  // 分组牌价倍率
	modelRateProRate     = 3.74 // deepseek-v4-pro 按模型 5.5 折
	modelRateAccountRate = 0.5
	modelRateBaseCost    = 0.01 // 插件回报的官方基准费用（美元）
)

// openModelRateTestDB 内存 SQLite 必须单连接：recorder 异步写与主链路并发落在
// shared-cache 的不同连接上会偶发 SQLITE_LOCKED（模板见 scheduler/events_test.go）。
func openModelRateTestDB(t *testing.T, name string) *ent.Client {
	t.Helper()
	drv, err := entsql.Open("sqlite3", "file:group_model_rate_"+name+"?mode=memory&cache=shared&_fk=1")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	db := enttest.NewClient(t,
		enttest.WithOptions(ent.Driver(drv)),
		enttest.WithMigrateOptions(migrate.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func createModelRateFixtures(t *testing.T, ctx context.Context, db *ent.Client, suffix string, userGroupRates map[int64]float64) (*ent.User, *ent.Group, *ent.Account) {
	t.Helper()
	userBuilder := db.User.Create().
		SetEmail("model-rate-" + suffix + "@example.com").
		SetPasswordHash("hash").
		SetBalance(100)
	if userGroupRates != nil {
		userBuilder = userBuilder.SetGroupRates(userGroupRates)
	}
	user := userBuilder.SaveX(ctx)
	group := db.Group.Create().
		SetName("DeepSeek " + suffix).
		SetPlatform("openai").
		SetRateMultiplier(modelRateGroupRate).
		SetModelRates(map[string]float64{"deepseek-v4-pro": modelRateProRate}).
		SaveX(ctx)
	account := db.Account.Create().
		SetName("DeepSeek account " + suffix).
		SetPlatform("openai").
		SetRateMultiplier(modelRateAccountRate).
		SaveX(ctx)
	return user, group, account
}

func modelRateUsage(model string) *sdk.Usage {
	return &sdk.Usage{
		Model:       model,
		AccountCost: modelRateBaseCost,
		Currency:    "USD",
		Metrics: []sdk.UsageMetric{
			{Key: "input_tokens", Kind: "token", Unit: "token", Value: 1000},
			{Key: "output_tokens", Kind: "token", Unit: "token", Value: 100},
		},
	}
}

func keyInfoFromGroup(user *ent.User, group *ent.Group, keyID int) *auth.APIKeyInfo {
	return &auth.APIKeyInfo{
		KeyID:               keyID,
		UserID:              user.ID,
		UserEmail:           user.Email,
		GroupID:             group.ID,
		GroupPlatform:       group.Platform,
		UserGroupRates:      user.GroupRates,
		GroupRateMultiplier: group.RateMultiplier,
		GroupModelRates:     group.ModelRates,
	}
}

// TestForwarderRecordUsageAppliesGroupModelRate ToB 公开转发落账：按模型倍率进 actual_cost 与
// usage_logs.rate_multiplier；未列出的模型沿用分组倍率；用户专属倍率压过按模型倍率。
func TestForwarderRecordUsageAppliesGroupModelRate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name           string
		requestModel   string
		usageModel     string
		userGroupRates func(groupID int) map[int64]float64
		wantRate       float64
	}{
		{name: "listed model takes model rate", requestModel: "deepseek-v4-pro", usageModel: "deepseek-v4-pro", wantRate: modelRateProRate},
		{name: "unlisted model keeps group rate", requestModel: "deepseek-v4.1-flash", usageModel: "deepseek-v4.1-flash", wantRate: modelRateGroupRate},
		{name: "lookup uses requested public model not upstream reported name", requestModel: "deepseek-v4-pro", usageModel: "upstream-real-name", wantRate: modelRateProRate},
		{name: "model-less request falls back to usage model", requestModel: "", usageModel: "deepseek-v4-pro", wantRate: modelRateProRate},
		{
			name: "user override beats model rate", requestModel: "deepseek-v4-pro", usageModel: "deepseek-v4-pro",
			userGroupRates: func(groupID int) map[int64]float64 { return map[int64]float64{int64(groupID): 2.0} },
			wantRate:       2.0,
		},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db := openModelRateTestDB(t, fmt.Sprintf("fwd_%d", i))
			user, group, account := createModelRateFixtures(t, ctx, db, fmt.Sprintf("fwd-%d", i), nil)
			if tt.userGroupRates != nil {
				user = db.User.UpdateOneID(user.ID).SetGroupRates(tt.userGroupRates(group.ID)).SaveX(ctx)
			}
			key := db.APIKey.Create().
				SetName("model-rate-key").
				SetKeyHash(fmt.Sprintf("model-rate-hash-%d", i)).
				SetUserID(user.ID).
				SetGroupID(group.ID).
				SaveX(ctx)

			recorder := billing.NewRecorder(db, 0)
			recorder.Start()
			forwarder := &Forwarder{
				scheduler:  scheduler.NewScheduler(db, nil),
				calculator: billing.NewCalculator(),
				recorder:   recorder,
			}
			response := httptest.NewRecorder()
			ginCtx, _ := gin.CreateTestContext(response)
			ginCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

			forwarder.recordUsage(ginCtx, &forwardState{
				requestPath: "/v1/chat/completions",
				model:       tt.requestModel,
				plugin:      &PluginInstance{Name: "gateway-openai", Platform: "openai"},
				account:     account,
				keyInfo:     keyInfoFromGroup(user, group, key.ID),
			}, forwardExecution{
				outcome:  sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess, Usage: modelRateUsage(tt.usageModel)},
				duration: time.Second,
			})
			recorder.Stop()

			rows := db.UsageLog.Query().AllX(ctx)
			if len(rows) != 1 {
				t.Fatalf("usage logs = %d, want 1", len(rows))
			}
			row := rows[0]
			if math.Abs(row.RateMultiplier-tt.wantRate) > 1e-9 {
				t.Fatalf("usage_logs.rate_multiplier = %v, want %v", row.RateMultiplier, tt.wantRate)
			}
			if math.Abs(row.ActualCost-modelRateBaseCost*tt.wantRate) > 1e-9 {
				t.Fatalf("ActualCost = %v, want %v", row.ActualCost, modelRateBaseCost*tt.wantRate)
			}
			if math.Abs(row.AccountCost-modelRateBaseCost*modelRateAccountRate) > 1e-9 {
				t.Fatalf("AccountCost = %v, want %v (account rate must not be touched)", row.AccountCost, modelRateBaseCost*modelRateAccountRate)
			}
			if got := db.User.GetX(ctx, user.ID).Balance; math.Abs(got-(100-modelRateBaseCost*tt.wantRate)) > 1e-9 {
				t.Fatalf("user balance = %v, want %v", got, 100-modelRateBaseCost*tt.wantRate)
			}
		})
	}
}

// TestHostForwardRecordUsageAppliesGroupModelRate ToC/插件内部转发落账（studio/playground 走
// gateway.forward）：候选分组携带按模型倍率，落账用 RateForModel 而非分组级 EffectiveRate。
func TestHostForwardRecordUsageAppliesGroupModelRate(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		route    func(group *ent.Group) routing.Candidate
		wantRate float64
	}{
		{
			name:  "listed model takes model rate",
			model: "deepseek-v4-pro",
			route: func(group *ent.Group) routing.Candidate {
				return routing.Candidate{GroupID: group.ID, Platform: "openai", EffectiveRate: modelRateGroupRate, GroupRateMultiplier: modelRateGroupRate, GroupModelRates: group.ModelRates}
			},
			wantRate: modelRateProRate,
		},
		{
			name:  "unlisted model keeps group rate",
			model: "deepseek-v4.1-flash",
			route: func(group *ent.Group) routing.Candidate {
				return routing.Candidate{GroupID: group.ID, Platform: "openai", EffectiveRate: modelRateGroupRate, GroupRateMultiplier: modelRateGroupRate, GroupModelRates: group.ModelRates}
			},
			wantRate: modelRateGroupRate,
		},
		{
			name:  "user override beats model rate",
			model: "deepseek-v4-pro",
			route: func(group *ent.Group) routing.Candidate {
				return routing.Candidate{GroupID: group.ID, Platform: "openai", EffectiveRate: 2.0, GroupRateMultiplier: modelRateGroupRate, UserGroupRate: 2.0, GroupModelRates: group.ModelRates}
			},
			wantRate: 2.0,
		},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db := openModelRateTestDB(t, fmt.Sprintf("host_%d", i))
			user, group, account := createModelRateFixtures(t, ctx, db, fmt.Sprintf("host-%d", i), nil)
			host := &HostService{
				scheduler:  scheduler.NewScheduler(db, nil),
				calculator: billing.NewCalculator(),
				recorder:   billing.NewRecorder(db, 0),
			}
			usageID, err := host.recordHostForwardUsage(
				ctx,
				hostForwardRequest{UserID: int64(user.ID), Path: "/v1/chat/completions", Model: tt.model},
				tt.route(group),
				account.ID,
				"openai",
				tt.model,
				account,
				user.Email,
				sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess, Usage: modelRateUsage(tt.model)},
				time.Second,
			)
			if err != nil {
				t.Fatalf("recordHostForwardUsage: %v", err)
			}
			row := db.UsageLog.GetX(ctx, usageID)
			if math.Abs(row.RateMultiplier-tt.wantRate) > 1e-9 {
				t.Fatalf("usage_logs.rate_multiplier = %v, want %v", row.RateMultiplier, tt.wantRate)
			}
			if math.Abs(row.ActualCost-modelRateBaseCost*tt.wantRate) > 1e-9 {
				t.Fatalf("ActualCost = %v, want %v", row.ActualCost, modelRateBaseCost*tt.wantRate)
			}
		})
	}
}

// TestKeyInfoRouteCarriesGroupModelRates 鉴权预载的按模型倍率经候选往返后仍在 keyInfo 上，
// failover 换候选不会丢掉按模型倍率。
func TestKeyInfoRouteCarriesGroupModelRates(t *testing.T) {
	base := &auth.APIKeyInfo{
		GroupID:             28,
		GroupPlatform:       "openai",
		GroupRateMultiplier: modelRateGroupRate,
		GroupModelRates:     map[string]float64{"deepseek-v4-pro": modelRateProRate},
		UserGroupRates:      map[int64]float64{28: 2.0},
	}
	route := keyInfoRoute(base)
	if route.UserGroupRate != 2.0 || route.GroupModelRates["deepseek-v4-pro"] != modelRateProRate {
		t.Fatalf("keyInfoRoute() = %+v", route)
	}
	if got := route.RateForModel("deepseek-v4-pro"); got != 2.0 {
		t.Fatalf("RateForModel with user override = %v, want 2.0", got)
	}
	info := keyInfoForRoute(&auth.APIKeyInfo{GroupID: 1}, route)
	if info.GroupModelRates["deepseek-v4-pro"] != modelRateProRate {
		t.Fatalf("keyInfoForRoute lost model rates: %+v", info.GroupModelRates)
	}
}

func TestBillingRateModelPrefersRequestedModel(t *testing.T) {
	tests := []struct {
		requested, actual, want string
	}{
		{"deepseek-v4-pro", "upstream-real-name", "deepseek-v4-pro"},
		{"  ", "deepseek-v4-pro", "deepseek-v4-pro"},
		{"", "", ""},
	}
	for _, tt := range tests {
		if got := billingRateModel(tt.requested, tt.actual); got != tt.want {
			t.Errorf("billingRateModel(%q, %q) = %q, want %q", tt.requested, tt.actual, got, tt.want)
		}
	}
}

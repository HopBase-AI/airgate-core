package plugin

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/routing"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

const (
	seedreamOutputBasePrice = 0.09
	seedreamReferencePrice  = 0.003
	seedreamBillingRate     = 2.0
	seedreamAccountRate     = 0.5
)

func TestSeedreamReferenceBilling_ToCHostForwarding(t *testing.T) {
	for _, referenceCount := range []int{0, 1, 3} {
		referenceCount := referenceCount
		t.Run(fmt.Sprintf("references_%d", referenceCount), func(t *testing.T) {
			ctx := context.Background()
			db := openImageBillingTestDB(t, fmt.Sprintf("toc_%d", referenceCount))
			user, group, account := createImageBillingFixtures(t, ctx, db, fmt.Sprintf("toc-%d", referenceCount))
			usage, upstreamCost, retailBaseCost := seedreamReferenceUsage(referenceCount)

			host := &HostService{
				scheduler:  scheduler.NewScheduler(db, nil),
				calculator: billing.NewCalculator(),
				recorder:   billing.NewRecorder(db, 0),
			}
			usageID, err := host.recordHostForwardUsage(
				ctx,
				hostForwardRequest{UserID: int64(user.ID), Path: "/v1/images/generations"},
				routing.Candidate{GroupID: group.ID, EffectiveRate: seedreamBillingRate},
				account.ID,
				"seedance",
				"seedream-5-0-pro",
				account,
				user.Email,
				sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess, Usage: usage},
				time.Second,
			)
			if err != nil {
				t.Fatalf("recordHostForwardUsage: %v", err)
			}

			row := db.UsageLog.GetX(ctx, usageID)
			assertImageBillingCosts(t, row, upstreamCost, retailBaseCost, retailBaseCost*seedreamBillingRate)
			if math.Abs(usage.UserCost-retailBaseCost*seedreamBillingRate) > 1e-9 {
				t.Fatalf("Usage.UserCost = %v, want %v", usage.UserCost, retailBaseCost*seedreamBillingRate)
			}
			assertImageBillingBalance(t, ctx, db, user.ID, 100-retailBaseCost*seedreamBillingRate)
		})
	}
}

func TestSeedreamReferenceBilling_ToBPublicForwarding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const sellRate = 3.0

	for _, referenceCount := range []int{0, 1, 3} {
		referenceCount := referenceCount
		t.Run(fmt.Sprintf("references_%d", referenceCount), func(t *testing.T) {
			ctx := context.Background()
			db := openImageBillingTestDB(t, fmt.Sprintf("tob_%d", referenceCount))
			user, group, account := createImageBillingFixtures(t, ctx, db, fmt.Sprintf("tob-%d", referenceCount))
			key := db.APIKey.Create().
				SetName("seedream-test-key").
				SetKeyHash(fmt.Sprintf("seedream-test-hash-%d", referenceCount)).
				SetUserID(user.ID).
				SetGroupID(group.ID).
				SaveX(ctx)
			usage, upstreamCost, retailBaseCost := seedreamReferenceUsage(referenceCount)

			recorder := billing.NewRecorder(db, 0)
			recorder.Start()
			forwarder := &Forwarder{
				scheduler:  scheduler.NewScheduler(db, nil),
				calculator: billing.NewCalculator(),
				recorder:   recorder,
			}
			response := httptest.NewRecorder()
			ginCtx, _ := gin.CreateTestContext(response)
			ginCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

			forwarder.recordUsage(ginCtx, &forwardState{
				requestPath: "/v1/images/generations",
				model:       "seedream-5-0-pro",
				plugin:      &PluginInstance{Name: "gateway-seedance", Platform: "seedance"},
				account:     account,
				keyInfo: &auth.APIKeyInfo{
					KeyID:               key.ID,
					UserID:              user.ID,
					UserEmail:           user.Email,
					GroupID:             group.ID,
					GroupRateMultiplier: seedreamBillingRate,
					SellRate:            sellRate,
				},
			}, forwardExecution{
				outcome:  sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess, Usage: usage},
				duration: time.Second,
			})
			recorder.Stop()

			rows := db.UsageLog.Query().AllX(ctx)
			if len(rows) != 1 {
				t.Fatalf("usage logs = %d, want 1", len(rows))
			}
			assertImageBillingCosts(t, rows[0], upstreamCost, retailBaseCost, retailBaseCost*sellRate)
			assertImageBillingBalance(t, ctx, db, user.ID, 100-retailBaseCost*seedreamBillingRate)
		})
	}
}

func openImageBillingTestDB(t *testing.T, name string) *ent.Client {
	t.Helper()
	db := enttest.Open(t, "sqlite3", "file:image_billing_"+name+"?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	return db
}

func createImageBillingFixtures(t *testing.T, ctx context.Context, db *ent.Client, suffix string) (*ent.User, *ent.Group, *ent.Account) {
	t.Helper()
	user := db.User.Create().
		SetEmail("seedream-billing-" + suffix + "@example.com").
		SetPasswordHash("hash").
		SetBalance(100).
		SaveX(ctx)
	group := db.Group.Create().
		SetName("Seedream billing " + suffix).
		SetPlatform("seedance").
		SetRateMultiplier(seedreamBillingRate).
		SaveX(ctx)
	account := db.Account.Create().
		SetName("Seedream account " + suffix).
		SetPlatform("seedance").
		SetRateMultiplier(seedreamAccountRate).
		SaveX(ctx)
	return user, group, account
}

func seedreamReferenceUsage(referenceCount int) (*sdk.Usage, float64, float64) {
	officialBillableReferences := referenceCount - 1
	if officialBillableReferences < 0 {
		officialBillableReferences = 0
	}
	upstreamCost := seedreamOutputBasePrice + float64(officialBillableReferences)*seedreamReferencePrice
	retailBaseCost := seedreamOutputBasePrice + float64(referenceCount)*seedreamReferencePrice
	metrics := []sdk.UsageMetric{{
		Key:         "images",
		Label:       "图片张数",
		Kind:        "image",
		Unit:        "image",
		Value:       1,
		AccountCost: seedreamOutputBasePrice,
		Currency:    "USD",
	}}
	if officialBillableReferences > 0 {
		metrics = append(metrics, sdk.UsageMetric{
			Key:         "input_reference_images",
			Label:       "官方计费参考图",
			Kind:        "image",
			Unit:        "image",
			Value:       float64(officialBillableReferences),
			AccountCost: float64(officialBillableReferences) * seedreamReferencePrice,
			Currency:    "USD",
		})
	}
	return &sdk.Usage{
		Model:       "seedream-5-0-pro",
		AccountCost: upstreamCost,
		Currency:    "USD",
		Metrics:     metrics,
		Metadata: map[string]string{
			imageBillingBaseCostOverrideMetadataKey: fmt.Sprintf("%g", retailBaseCost),
			"reference_image_count":                 fmt.Sprintf("%d", referenceCount),
		},
	}, upstreamCost, retailBaseCost
}

func assertImageBillingCosts(t *testing.T, row *ent.UsageLog, upstreamCost, retailBaseCost, wantBilledCost float64) {
	t.Helper()
	if math.Abs(row.TotalCost-upstreamCost) > 1e-9 {
		t.Fatalf("TotalCost = %v, want upstream %v", row.TotalCost, upstreamCost)
	}
	if math.Abs(row.AccountCost-upstreamCost*seedreamAccountRate) > 1e-9 {
		t.Fatalf("AccountCost = %v, want %v", row.AccountCost, upstreamCost*seedreamAccountRate)
	}
	if math.Abs(row.ActualCost-retailBaseCost*seedreamBillingRate) > 1e-9 {
		t.Fatalf("ActualCost = %v, want %v", row.ActualCost, retailBaseCost*seedreamBillingRate)
	}
	if math.Abs(row.BilledCost-wantBilledCost) > 1e-9 {
		t.Fatalf("BilledCost = %v, want %v", row.BilledCost, wantBilledCost)
	}
}

func assertImageBillingBalance(t *testing.T, ctx context.Context, db *ent.Client, userID int, want float64) {
	t.Helper()
	got := db.User.GetX(ctx, userID).Balance
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("user balance = %v, want %v", got, want)
	}
}

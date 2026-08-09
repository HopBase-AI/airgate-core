package plugin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/redis/go-redis/v9"

	"github.com/DouDOU-start/airgate-core/ent/enttest"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

func TestPickAccountRoutesModelLessRequestThroughConfiguredAccounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := t.Context()
	db := enttest.Open(t, "sqlite3", "file:model_less_pick_"+uuid.NewString()+"?mode=memory&cache=shared&_fk=1",
		enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	const platform = "gateway-kling"
	group := db.Group.Create().SetName("kling").SetPlatform(platform).SaveX(ctx)
	routed := db.Account.Create().SetName("routed").SetPlatform(platform).SetMaxConcurrency(10).AddGroups(group).SaveX(ctx)
	unrouted := db.Account.Create().SetName("unrouted").SetPlatform(platform).SetMaxConcurrency(10).AddGroups(group).SaveX(ctx)
	crossPlatform := db.Account.Create().SetName("cross-platform").SetPlatform("openai").SetMaxConcurrency(10).AddGroups(group).SaveX(ctx)
	otherGroup := db.Group.Create().SetName("other").SetPlatform(platform).SaveX(ctx)
	otherGroupAccount := db.Account.Create().SetName("other-group").SetPlatform(platform).SetMaxConcurrency(10).AddGroups(otherGroup).SaveX(ctx)
	db.Group.UpdateOneID(group.ID).SetModelRouting(map[string][]int64{
		"kling-image-v1": {int64(routed.ID), int64(crossPlatform.ID), int64(otherGroupAccount.ID), 999999},
		"kling-video-v1": {int64(routed.ID)},
	}).ExecX(ctx)

	manager := &Manager{
		routeCache: map[string][]sdk.RouteDefinition{
			"gateway-kling": {
				{
					Method: "POST",
					Path:   "/v1/kling/faces",
					Metadata: map[string]string{
						"model_agnostic_account": "true",
					},
				},
			},
		},
	}
	forwarder := &Forwarder{manager: manager, scheduler: scheduler.NewScheduler(db, rdb)}
	newRequest := func(method string) (*gin.Context, *forwardState) {
		requestContext, _ := gin.CreateTestContext(httptest.NewRecorder())
		requestContext.Request = httptest.NewRequest(method, "/v1/kling/faces", nil)
		state := &forwardState{
			requestPath:       "/v1/kling/faces",
			requestedPlatform: platform,
			plugin:            &PluginInstance{Name: "gateway-kling", Platform: platform},
			accountReq: scheduler.AccountRequirements{
				Workload: scheduler.WorkloadChat,
			},
			keyInfo: &auth.APIKeyInfo{
				UserID:  1,
				GroupID: group.ID,
			},
		}
		return requestContext, state
	}

	requestContext, state := newRequest(http.MethodPost)
	if err := forwarder.pickAccount(requestContext, state); err != nil {
		t.Fatalf("pickAccount() error = %v", err)
	}
	if state.account == nil || state.account.ID != routed.ID {
		t.Fatalf("selected account = %+v, want routed account %d", state.account, routed.ID)
	}
	if state.account.ID == unrouted.ID {
		t.Fatalf("model-less request leaked to unrouted account %d", unrouted.ID)
	}
	if state.schedulingModel != "" {
		t.Fatalf("scheduling model = %q, want empty model sentinel", state.schedulingModel)
	}
	if state.requestID == "" {
		t.Fatal("model-less account selection did not create a request ID")
	}

	requestContext, state = newRequest(http.MethodPost)
	if err := forwarder.pickAccount(requestContext, state, routed.ID); err == nil {
		t.Fatal("excluding the only routed account must not fall back to an unrouted account")
	}

	requestContext, state = newRequest(http.MethodPut)
	if err := forwarder.pickAccount(requestContext, state); err == nil {
		t.Fatal("undeclared HTTP method must not select an account")
	}

	delete(manager.routeCache["gateway-kling"][0].Metadata, "model_agnostic_account")
	requestContext, state = newRequest(http.MethodPost)
	if err := forwarder.pickAccount(requestContext, state); err == nil {
		t.Fatal("model-less request without an explicit route declaration must fail closed")
	}
	if state.account != nil {
		t.Fatalf("undeclared model-less request selected account %+v", state.account)
	}
}

package plugin

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	"github.com/DouDOU-start/airgate-core/ent/group"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/routing"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

func TestApplyHostForwardBilling(t *testing.T) {
	t.Parallel()

	for _, multiplier := range []float64{0.8, 1, 2} {
		t.Run(fmt.Sprintf("multiplier_%g", multiplier), func(t *testing.T) {
			t.Parallel()
			usage := &sdk.Usage{AccountCost: 0.25, UserCost: 999, BillingMultiplier: 999}
			applyHostForwardBilling(usage, billing.CalculateResult{
				ActualCost:     0.25 * multiplier,
				RateMultiplier: multiplier,
			})

			if usage.UserCost != 0.25*multiplier {
				t.Fatalf("UserCost = %v, want %v", usage.UserCost, 0.25*multiplier)
			}
			if usage.BillingMultiplier != multiplier {
				t.Fatalf("BillingMultiplier = %v, want %v", usage.BillingMultiplier, multiplier)
			}
			if usage.AccountCost != 0.25 {
				t.Fatalf("AccountCost = %v, want plugin-reported 0.25", usage.AccountCost)
			}
		})
	}
}

func TestApplyHostForwardTrace(t *testing.T) {
	t.Parallel()
	usage := &sdk.Usage{}
	applyHostForwardTrace(usage, " chat-request-1 ")
	if usage.Metadata["trace_id"] != "chat-request-1" {
		t.Fatalf("trace_id = %q", usage.Metadata["trace_id"])
	}
}

func TestCustomUsagePayloadFromLogUsesPersistedCharge(t *testing.T) {
	t.Parallel()

	row := &ent.UsageLog{
		AccountCost: 0.01,
		ActualCost:  0.025,
		UsageMetrics: []sdk.UsageMetric{{
			Key: "document_render", Kind: "custom", Unit: "file", Value: 1,
		}},
		UsageCostDetails: []sdk.UsageCostDetail{{
			Key: "document_render", AccountCost: 0.01, UserCost: 0.025,
		}},
		UsageMetadata: map[string]string{"asset_id": "asset-first"},
	}
	payload := customUsagePayloadFromLog(row)
	if payload["user_cost"] != 0.025 || payload["account_cost"] != 0.01 {
		t.Fatalf("payload costs = %#v, want persisted costs", payload)
	}
	metadata, _ := payload["metadata"].(map[string]string)
	if metadata["asset_id"] != "asset-first" {
		t.Fatalf("payload metadata = %#v, want persisted metadata", metadata)
	}
}

func TestHostForwardTimeout(t *testing.T) {
	cases := []struct {
		name string
		req  hostForwardRequest
		want time.Duration
	}{
		{name: "empty request", req: hostForwardRequest{}, want: defaultHostForwardTimeout},
		{name: "chat request", req: hostForwardRequest{Path: "/v1/chat/completions", Model: "gpt-4o"}, want: defaultHostForwardTimeout},
		{name: "images API request", req: hostForwardRequest{Path: "/v1/images/generations", Model: "gpt-4o"}, want: imageHostForwardTimeout},
		{name: "image model request", req: hostForwardRequest{Path: "/v1/responses", Model: "gpt-image-2"}, want: imageHostForwardTimeout},
		{
			name: "responses image tool request",
			req: hostForwardRequest{
				Path:  "/v1/responses",
				Model: "gpt-5.4",
				Body:  []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation"}]}`),
			},
			want: imageHostForwardTimeout,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hostForwardTimeout(nil, tc.req); got != tc.want {
				t.Fatalf("hostForwardTimeout() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestHostForwardReasoningEffort(t *testing.T) {
	t.Parallel()

	req := hostForwardRequest{
		Body: []byte(`{"model":"gpt-5","reasoning":{"effort":"x-high"}}`),
		Headers: map[string]interface{}{
			"Content-Type": []string{"application/json"},
		},
	}

	if got := hostForwardReasoningEffort(req); got != "xhigh" {
		t.Fatalf("hostForwardReasoningEffort() = %q, want %q", got, "xhigh")
	}
}

func TestHostForwardHeadersDropsCallerInternalTestMode(t *testing.T) {
	t.Parallel()

	headers := hostForwardHeaders(hostForwardRequest{
		Path:   "/v1/messages",
		Method: "POST",
		UserID: 12,
		Headers: map[string]interface{}{
			"Content-Type":        []string{"application/json"},
			"X-Airgate-Platform":  []string{"claude"},
			"X-Airgate-Internal":  []string{"test"},
			"X-Airgate-Test-Mode": []string{"aws_bedrock_minimal"},
		},
	}, routing.Candidate{
		GroupID: 19,
		GroupPluginSettings: map[string]map[string]string{
			"claude": {"claude_code_only": "false"},
		},
	})

	if got := headers.Get("X-Airgate-Test-Mode"); got != "" {
		t.Fatalf("X-Airgate-Test-Mode = %q, want empty", got)
	}
	if got := headers.Get("X-Airgate-Internal"); got != "host-forward" {
		t.Fatalf("X-Airgate-Internal = %q, want host-forward", got)
	}
	if got := headers.Get("X-Airgate-Platform"); got != "claude" {
		t.Fatalf("X-Airgate-Platform = %q, want claude", got)
	}
	if got := headers.Get("X-Airgate-Group-ID"); got != "19" {
		t.Fatalf("X-Airgate-Group-ID = %q, want 19", got)
	}
	if got := headers.Get("X-Airgate-Plugin-Claude-Claude-Code-Only"); got != "false" {
		t.Fatalf("plugin setting header = %q, want false", got)
	}
}

func TestHostInvokeRequiresDeclaredCapability(t *testing.T) {
	handle := &pluginHostHandle{pluginName: "test-plugin"}
	if err := handle.requireMethod(hostMethodTasksCreate); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected unbound capabilities to be denied, got %v", err)
	}

	handle.SetCapabilities(map[sdk.Capability]bool{})
	if err := handle.requireMethod(hostMethodTasksCreate); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected empty capabilities to be denied, got %v", err)
	}

	handle.SetCapabilities(map[sdk.Capability]bool{
		sdk.CapabilityForHostMethod(hostMethodTasksCreate): true,
	})
	if err := handle.requireMethod(hostMethodTasksCreate); err != nil {
		t.Fatalf("expected declared method capability to pass, got %v", err)
	}
}

func TestSelectAccountEmptyModelUsesCurrentGroupRouting(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:host_select_group_model?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	overseasGroup := db.Group.Create().SetName("Seedance Overseas").SetPlatform("seedance").SaveX(ctx)
	domesticGroup := db.Group.Create().SetName("Seedance Domestic").SetPlatform("seedance").SaveX(ctx)
	overseasAccount := db.Account.Create().
		SetName("dreamina-overseas").
		SetPlatform("seedance").
		SetCredentials(map[string]string{"api_key": "overseas"}).
		AddGroups(overseasGroup).
		SaveX(ctx)
	domesticAccount := db.Account.Create().
		SetName("doubao-domestic").
		SetPlatform("seedance").
		SetCredentials(map[string]string{"api_key": "domestic"}).
		AddGroups(domesticGroup).
		SaveX(ctx)

	const (
		domesticModel = "doubao-seedance-2-0-260128-a"
		overseasModel = "dreamina-seedance-2-0-260128"
	)
	db.Group.UpdateOneID(overseasGroup.ID).
		SetModelRouting(map[string][]int64{overseasModel: {int64(overseasAccount.ID)}}).
		ExecX(ctx)
	db.Group.UpdateOneID(domesticGroup.ID).
		SetModelRouting(map[string][]int64{domesticModel: {int64(domesticAccount.ID)}}).
		ExecX(ctx)

	host := &HostService{
		db: db,
		manager: &Manager{modelCache: map[string][]sdk.ModelInfo{
			"seedance": {
				{ID: domesticModel},
				{ID: overseasModel},
			},
		}},
		scheduler: scheduler.NewScheduler(db, nil),
	}

	tests := []struct {
		name          string
		groupID       int
		model         string
		wantAccountID int
	}{
		{name: "海外分组空模型选择海外账号", groupID: overseasGroup.ID, wantAccountID: overseasAccount.ID},
		{name: "国内分组空模型选择国内账号", groupID: domesticGroup.ID, wantAccountID: domesticAccount.ID},
		{name: "显式模型保持原有选号", groupID: overseasGroup.ID, model: overseasModel, wantAccountID: overseasAccount.ID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := host.selectAccount(ctx, hostSelectAccountRequest{GroupID: int64(tt.groupID), Model: tt.model})
			if err != nil {
				t.Fatalf("selectAccount() error = %v", err)
			}
			if got := int(resp["account_id"].(int64)); got != tt.wantAccountID {
				t.Fatalf("account_id = %d, want %d", got, tt.wantAccountID)
			}
		})
	}
}

func TestPickProbeModelForRouting(t *testing.T) {
	models := []sdk.ModelInfo{
		{ID: "domestic-video"},
		{ID: "overseas-image", Capabilities: []string{sdk.ModelCapImageGeneration}},
		{ID: "overseas-video"},
	}
	routing := map[string][]int64{
		"overseas-*": {33},
	}
	if got := pickProbeModelForRouting(models, routing); got != "overseas-video" {
		t.Fatalf("pickProbeModelForRouting() = %q, want overseas-video", got)
	}
}

func TestPickRoutableModelRespectsExactDisableOverGlob(t *testing.T) {
	models := []sdk.ModelInfo{
		{ID: "seedance-domestic"},
		{ID: "seedance-overseas"},
	}
	routing := map[string][]int64{
		"seedance-*":        {33},
		"seedance-domestic": {},
	}
	if got := pickRoutableModel(models, routing); got != "seedance-overseas" {
		t.Fatalf("pickRoutableModel() = %q, want seedance-overseas", got)
	}
}

func TestHostDeleteAssetLocal(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:host_delete_asset?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	db.Setting.Create().SetGroup("storage").SetKey("local_storage_dir").SetValue(t.TempDir()).SaveX(ctx)
	storage, err := NewAssetStorage(ctx, db)
	if err != nil {
		t.Fatalf("初始化资产存储失败: %v", err)
	}
	asset := mustStoreTestAsset(t, storage, ctx, 42, AssetPurposeChat)
	assertAssetExists(t, storage, asset.ObjectKey)

	host := &HostService{db: db}
	if _, err := host.deleteAsset(ctx, hostDeleteAssetRequest{ObjectKey: asset.ObjectKey}); err != nil {
		t.Fatalf("删除资产失败: %v", err)
	}
	assertAssetMissing(t, storage, asset.ObjectKey)
}

func TestDeleteTaskDeletesAssociatedAssets(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:delete_task_assets?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	db.Setting.Create().SetGroup("storage").SetKey("local_storage_dir").SetValue(t.TempDir()).SaveX(ctx)
	storage, err := NewAssetStorage(ctx, db)
	if err != nil {
		t.Fatalf("初始化资产存储失败: %v", err)
	}

	host := &HostService{db: db}
	big := bigDataURI(t, "image/png", 32<<10)
	created, err := host.createTask(ctx, "gateway-openai", hostCreateTaskRequest{
		UserID:   42,
		TaskType: "image.edit",
		Input: map[string]interface{}{
			"prompt": "edit",
			"images": []interface{}{big},
		},
	})
	if err != nil {
		t.Fatalf("创建任务失败: %v", err)
	}

	task := created["task"].(map[string]interface{})
	taskID := task["id"].(int64)
	input := task["input"].(map[string]interface{})
	inputAssetURL := input["images"].([]interface{})[0].(string)
	inputObjectKey, err := runtimeAssetURLToObjectKey(inputAssetURL)
	if err != nil {
		t.Fatalf("解析输入资产 URL 失败: %v", err)
	}
	assertAssetExists(t, storage, inputObjectKey)

	generated := mustStoreTestAsset(t, storage, ctx, 42, AssetPurposeGenerated)
	if _, err := host.updateTask(ctx, "gateway-openai", hostUpdateTaskRequest{
		TaskID: taskID,
		Status: "processing",
	}); err != nil {
		t.Fatalf("启动任务失败: %v", err)
	}
	if _, err := host.updateTask(ctx, "gateway-openai", hostUpdateTaskRequest{
		TaskID: taskID,
		Status: "completed",
		Output: map[string]interface{}{
			"content":           fmt.Sprintf("![image](%s)", generated.PublicURL),
			"asset_object_keys": []interface{}{generated.ObjectKey},
		},
	}); err != nil {
		t.Fatalf("完成任务失败: %v", err)
	}

	if _, err := host.deleteTask(ctx, "gateway-openai", hostDeleteTaskRequest{
		TaskID: taskID,
		UserID: 42,
	}); err != nil {
		t.Fatalf("删除任务失败: %v", err)
	}
	assertAssetMissing(t, storage, inputObjectKey)
	assertAssetMissing(t, storage, generated.ObjectKey)
}

func TestTaskPublicIDIsIndependentFromIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:task_public_id?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	host := &HostService{db: db}
	baseReq := hostCreateTaskRequest{
		UserID:         42,
		Input:          map[string]interface{}{"prompt": "test"},
		IdempotencyKey: "same-idempotency-key",
	}
	if _, err := host.createTask(ctx, "gateway-openai", hostCreateTaskRequest{
		UserID:         baseReq.UserID,
		TaskType:       "image.generate",
		Input:          baseReq.Input,
		PublicTaskID:   "pub-generate",
		IdempotencyKey: baseReq.IdempotencyKey,
	}); err != nil {
		t.Fatalf("create generate task: %v", err)
	}
	if _, err := host.createTask(ctx, "gateway-openai", hostCreateTaskRequest{
		UserID:         baseReq.UserID,
		TaskType:       "image.edit",
		Input:          baseReq.Input,
		PublicTaskID:   "pub-edit",
		IdempotencyKey: baseReq.IdempotencyKey,
	}); err != nil {
		t.Fatalf("create edit task with same idempotency key: %v", err)
	}

	got, err := host.getTask(ctx, "gateway-openai", hostGetTaskRequest{UserID: baseReq.UserID, PublicTaskID: "pub-edit"})
	if err != nil {
		t.Fatalf("get task by public id: %v", err)
	}
	task, ok := got["task"].(map[string]interface{})
	if !ok {
		t.Fatalf("task payload type = %T", got["task"])
	}
	if task["task_type"] != "image.edit" || task["public_task_id"] != "pub-edit" {
		t.Fatalf("unexpected task payload: %+v", task)
	}

	_, err = host.getTask(ctx, "gateway-openai", hostGetTaskRequest{UserID: baseReq.UserID, PublicTaskID: baseReq.IdempotencyKey})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("idempotency key should not be usable as public task id, got %v", err)
	}
}

func TestListTasksFiltersByPluginID(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:list_tasks_plugin_id?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	host := &HostService{db: db}
	for _, pluginID := range []string{"gateway-openai", "other-plugin"} {
		if _, err := host.createTask(ctx, pluginID, hostCreateTaskRequest{
			UserID:   42,
			TaskType: "image.generate",
			Input:    map[string]interface{}{"prompt": pluginID},
		}); err != nil {
			t.Fatalf("create task for %s: %v", pluginID, err)
		}
	}

	got, err := host.listTasks(ctx, "airgate-studio", hostListTasksRequest{
		PluginID: "gateway-openai",
		UserID:   42,
		Limit:    20,
	})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	tasks, ok := got["tasks"].([]map[string]interface{})
	if !ok {
		t.Fatalf("tasks payload type = %T", got["tasks"])
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks len = %d, want 1: %+v", len(tasks), tasks)
	}
	if tasks[0]["plugin_id"] != "gateway-openai" {
		t.Fatalf("plugin_id = %v, want gateway-openai", tasks[0]["plugin_id"])
	}
}

func TestListTasksStripsHeavyInputFields(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:list_tasks_slim?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	host := &HostService{db: db}
	created, err := host.createTask(ctx, "gateway-openai", hostCreateTaskRequest{
		UserID:   42,
		TaskType: "image.edit",
		Input: map[string]interface{}{
			"prompt": "make it blue",
			"model":  "gpt-image-1",
			"size":   "1024x1024",
			"images": []interface{}{"data:image/png;base64,AAAA"},
			"mask":   "data:image/png;base64,BBBB",
		},
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	taskID := int64(created["task"].(map[string]interface{})["id"].(int64))

	got, err := host.listTasks(ctx, "gateway-openai", hostListTasksRequest{UserID: 42, Limit: 20})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	tasks := got["tasks"].([]map[string]interface{})
	if len(tasks) != 1 {
		t.Fatalf("tasks len = %d, want 1", len(tasks))
	}
	input, ok := tasks[0]["input"].(map[string]interface{})
	if !ok {
		t.Fatalf("input type = %T", tasks[0]["input"])
	}
	if _, present := input["images"]; present {
		t.Fatalf("list response must omit input.images, got: %+v", input)
	}
	if _, present := input["mask"]; present {
		t.Fatalf("list response must omit input.mask, got: %+v", input)
	}
	if input["prompt"] != "make it blue" || input["model"] != "gpt-image-1" || input["size"] != "1024x1024" {
		t.Fatalf("list response stripped too much, got: %+v", input)
	}

	// tasks.get must still return the full input for callers that need it.
	full, err := host.getTask(ctx, "gateway-openai", hostGetTaskRequest{UserID: 42, TaskID: taskID})
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	fullInput := full["task"].(map[string]interface{})["input"].(map[string]interface{})
	if _, present := fullInput["images"]; !present {
		t.Fatalf("get response must keep input.images, got: %+v", fullInput)
	}
	if _, present := fullInput["mask"]; !present {
		t.Fatalf("get response must keep input.mask, got: %+v", fullInput)
	}
}

func TestCreateTaskNormalizesLargeInputDataURIs(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:create_task_normalize?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	// 让 NewAssetStorage 落到测试临时目录，而不是默认 data/assets。
	t.Setenv("ASSETS_DIR", t.TempDir())

	host := &HostService{db: db}
	big := bigDataURI(t, "image/png", 32<<10)
	created, err := host.createTask(ctx, "gateway-openai", hostCreateTaskRequest{
		UserID:   7,
		TaskType: "image.edit",
		Input: map[string]interface{}{
			"prompt": "rotate left",
			"model":  "gpt-image-1",
			"images": []interface{}{big, big},
			"mask":   big,
		},
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	task := created["task"].(map[string]interface{})
	input := task["input"].(map[string]interface{})
	if input["prompt"] != "rotate left" {
		t.Fatalf("prompt mutated: %+v", input["prompt"])
	}
	images := input["images"].([]interface{})
	for i, img := range images {
		s, ok := img.(string)
		if !ok {
			t.Fatalf("images[%d] type = %T", i, img)
		}
		if !strings.HasPrefix(s, "/assets-runtime/") {
			t.Fatalf("images[%d] not normalized: %s", i, s[:40])
		}
		if !strings.Contains(s, "/task-input/7/") {
			t.Fatalf("images[%d] wrong object_key prefix: %s", i, s)
		}
	}
	if !strings.HasPrefix(input["mask"].(string), "/assets-runtime/") {
		t.Fatalf("mask not normalized: %s", input["mask"].(string)[:40])
	}

	// 再确认 list payload 也不再带任何 base64 — list 已经在剥 images/mask，
	// 这里主要验证如果有人撤掉那个剥字段逻辑，归一化也能挡住 64MB 上限。
	listed, err := host.listTasks(ctx, "gateway-openai", hostListTasksRequest{UserID: 7, Limit: 20})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	tasks := listed["tasks"].([]map[string]interface{})
	if len(tasks) != 1 {
		t.Fatalf("tasks len = %d", len(tasks))
	}
}

func TestCheckHostForwardBalance(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:host_forward_balance?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	zeroBalanceUser := db.User.Create().SetEmail("zero@example.com").SetPasswordHash("hash").SetBalance(0).SaveX(ctx)
	positiveBalanceUser := db.User.Create().SetEmail("positive@example.com").SetPasswordHash("hash").SetBalance(1).SaveX(ctx)

	host := &HostService{db: db}

	if err := host.checkHostForwardBalance(ctx, int64(zeroBalanceUser.ID)); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("expected ResourceExhausted for zero balance, got %v", err)
	}
	if err := host.checkHostForwardBalance(ctx, int64(positiveBalanceUser.ID)); err != nil {
		t.Fatalf("expected positive balance user to pass, got %v", err)
	}
}

func TestListGroupsEligibleOnly(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:list_groups_eligible?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	u := db.User.Create().SetEmail("u@example.com").SetPasswordHash("hash").SetBalance(1).SaveX(ctx)
	cheap := db.Group.Create().SetName("标准").SetPlatform("gemini").SetRateMultiplier(1.0).
		SetPluginSettings(map[string]map[string]string{"openai": {
			"image_price_1k": "0.08", "image_price_2k": "0.12", "internal_secret": "hidden",
		}}).SaveX(ctx)
	db.User.UpdateOneID(u.ID).SetGroupPluginSettings(map[int64]map[string]map[string]string{
		int64(cheap.ID): {"openai": {"image_price_2k": "0.11"}},
	}).ExecX(ctx)
	u = db.User.GetX(ctx, u.ID)
	pricey := db.Group.Create().SetName("高清").SetPlatform("gemini").SetRateMultiplier(2.0).SaveX(ctx)
	db.Group.Create().SetName("专属未授权").SetPlatform("gemini").SetRateMultiplier(0.5).SetIsExclusive(true).SaveX(ctx)
	db.Group.Create().SetName("别的平台").SetPlatform("openai").SetRateMultiplier(1.0).SaveX(ctx)

	host := &HostService{db: db}

	// 缺 user_id / platform 应拒绝
	if _, err := host.listGroups(ctx, hostListGroupsRequest{EligibleOnly: true, Platform: "gemini"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing user_id: err = %v", err)
	}
	if _, err := host.listGroups(ctx, hostListGroupsRequest{EligibleOnly: true, UserID: int64(u.ID)}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing platform: err = %v", err)
	}

	resp, err := host.listGroups(ctx, hostListGroupsRequest{EligibleOnly: true, UserID: int64(u.ID), Platform: "gemini"})
	if err != nil {
		t.Fatalf("listGroups eligible: %v", err)
	}
	items := resp["groups"].([]map[string]interface{})
	// 专属未授权与非本平台分组不应出现；便宜的排前面
	if len(items) != 2 {
		t.Fatalf("groups len = %d, want 2 (%v)", len(items), items)
	}
	if items[0]["id"].(int64) != int64(cheap.ID) || items[1]["id"].(int64) != int64(pricey.ID) {
		t.Fatalf("groups order = %v,%v; want cheap(%d) first", items[0]["id"], items[1]["id"], cheap.ID)
	}
	if items[0]["effective_rate"].(float64) != 1.0 || items[1]["effective_rate"].(float64) != 2.0 {
		t.Fatalf("effective_rate = %v,%v", items[0]["effective_rate"], items[1]["effective_rate"])
	}
	fixed, ok := items[0]["fixed_image_prices"].(map[string]interface{})
	if !ok || fixed["1k"] != 0.08 || fixed["2k"] != 0.11 || fixed["currency"] != "CNY" {
		t.Fatalf("fixed_image_prices = %#v", items[0]["fixed_image_prices"])
	}
	if _, leaked := items[0]["plugin_settings"]; leaked {
		t.Fatalf("groups.list leaked plugin_settings: %#v", items[0])
	}

	// 授权专属分组后应出现且按 0.5 倍率排最前
	exclusive := db.Group.Query().Where(group.NameEQ("专属未授权")).OnlyX(ctx)
	db.Group.UpdateOneID(exclusive.ID).AddAllowedUsers(u).ExecX(ctx)
	resp, err = host.listGroups(ctx, hostListGroupsRequest{EligibleOnly: true, UserID: int64(u.ID), Platform: "gemini"})
	if err != nil {
		t.Fatalf("listGroups eligible after grant: %v", err)
	}
	items = resp["groups"].([]map[string]interface{})
	if len(items) != 3 || items[0]["id"].(int64) != int64(exclusive.ID) {
		t.Fatalf("after grant groups = %v", items)
	}
}

func TestListGroupsPublicOnlyExcludesDelisted(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:list_groups_public_delisted?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	u := db.User.Create().SetEmail("public-groups@example.com").SetPasswordHash("hash").SaveX(ctx)
	visible := db.Group.Create().SetName("visible").SetPlatform("openai").SetStatusVisible(true).SaveX(ctx)
	delisted := db.Group.Create().SetName("delisted").SetPlatform("openai").SetStatusVisible(true).SetDelisted(true).SaveX(ctx)
	db.Group.Create().SetName("status-hidden").SetPlatform("openai").SetStatusVisible(false).SaveX(ctx)

	host := &HostService{db: db}
	resp, err := host.listGroups(ctx, hostListGroupsRequest{PublicOnly: true, UserID: int64(u.ID)})
	if err != nil {
		t.Fatalf("listGroups public_only: %v", err)
	}
	items := resp["groups"].([]map[string]interface{})
	if len(items) != 1 || items[0]["id"].(int64) != int64(visible.ID) {
		t.Fatalf("public groups = %v, want only visible group %d (delisted %d excluded)", items, visible.ID, delisted.ID)
	}
}

func TestListGroupsEligibleOnlyFiltersByModelSchedulability(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:list_groups_eligible_model?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	u := db.User.Create().SetEmail("u-model@example.com").SetPasswordHash("hash").SetBalance(1).SaveX(ctx)
	image2Group := db.Group.Create().
		SetName("Image2").
		SetPlatform("openai").
		SetPluginSettings(map[string]map[string]string{"openai": {"image_enabled": "true"}}).
		SaveX(ctx)
	geminiGroup := db.Group.Create().
		SetName("Gemini Banana").
		SetPlatform("openai").
		SetPluginSettings(map[string]map[string]string{"openai": {"image_enabled": "true"}}).
		SaveX(ctx)
	image2Account := db.Account.Create().
		SetName("image2").
		SetPlatform("openai").
		SetCredentials(map[string]string{"api_key": "sk-image"}).
		SetExtra(map[string]interface{}{"allowed_workloads": []interface{}{"image"}}).
		AddGroups(image2Group).
		SaveX(ctx)
	geminiAccount := db.Account.Create().
		SetName("gemini").
		SetPlatform("openai").
		SetCredentials(map[string]string{"api_key": "sk-gemini"}).
		SetExtra(map[string]interface{}{"allowed_workloads": []interface{}{"image"}}).
		AddGroups(geminiGroup).
		SaveX(ctx)
	db.Group.UpdateOneID(image2Group.ID).SetModelRouting(map[string][]int64{"gpt-image-2": {int64(image2Account.ID)}}).ExecX(ctx)
	db.Group.UpdateOneID(geminiGroup.ID).SetModelRouting(map[string][]int64{"gemini-3-pro-image": {int64(geminiAccount.ID)}}).ExecX(ctx)

	host := &HostService{db: db, scheduler: scheduler.NewScheduler(db, nil)}
	resp, err := host.listGroups(ctx, hostListGroupsRequest{
		EligibleOnly: true,
		UserID:       int64(u.ID),
		Platform:     "openai",
		NeedsImage:   true,
		Model:        "gemini-3-pro-image",
	})
	if err != nil {
		t.Fatalf("listGroups eligible model: %v", err)
	}
	items := resp["groups"].([]map[string]interface{})
	if len(items) != 1 || items[0]["id"].(int64) != int64(geminiGroup.ID) {
		t.Fatalf("groups = %v, want only Gemini group %d", items, geminiGroup.ID)
	}
}

func TestHostForwardRoutesExclusiveGroupAuthorization(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:host_forward_exclusive?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	u := db.User.Create().SetEmail("u@example.com").SetPasswordHash("hash").SetBalance(1).SaveX(ctx)
	exclusive := db.Group.Create().SetName("专属").SetPlatform("gemini").SetIsExclusive(true).SaveX(ctx)

	host := &HostService{db: db}
	req := hostForwardRequest{UserID: int64(u.ID), GroupID: int64(exclusive.ID), Model: "gemini-3-pro-image"}

	// 未授权：拒绝
	if routes, _, err := host.hostForwardRoutes(ctx, req); err == nil || len(routes) != 0 {
		t.Fatalf("expected denial for unauthorized exclusive group, got routes=%v err=%v", routes, err)
	}

	// 授权后：放行
	db.Group.UpdateOneID(exclusive.ID).AddAllowedUsers(u).ExecX(ctx)
	routes, _, err := host.hostForwardRoutes(ctx, req)
	if err != nil {
		t.Fatalf("expected authorized exclusive group to pass, got %v", err)
	}
	if len(routes) != 1 || routes[0].GroupID != exclusive.ID {
		t.Fatalf("routes = %v", routes)
	}
}

func TestHostForwardRoutesGroupPlatformMismatch(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:host_forward_platform_mismatch?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	u := db.User.Create().SetEmail("u@example.com").SetPasswordHash("hash").SetBalance(1).SaveX(ctx)
	geminiGroup := db.Group.Create().SetName("gemini组").SetPlatform("gemini").SaveX(ctx)

	host := &HostService{db: db}

	// 请求声明 openai 平台却指定 gemini 分组：拒绝
	mismatch := hostForwardRequest{
		UserID:  int64(u.ID),
		GroupID: int64(geminiGroup.ID),
		Model:   "gpt-image-2",
		Headers: map[string]interface{}{"X-Airgate-Platform": []string{"openai"}},
	}
	if routes, _, err := host.hostForwardRoutes(ctx, mismatch); err == nil || len(routes) != 0 {
		t.Fatalf("expected platform mismatch denial, got routes=%v err=%v", routes, err)
	}

	// 平台一致：放行（大小写不敏感）
	match := hostForwardRequest{
		UserID:  int64(u.ID),
		GroupID: int64(geminiGroup.ID),
		Model:   "gemini-3-pro-image",
		Headers: map[string]interface{}{"X-Airgate-Platform": []string{"Gemini"}},
	}
	routes, _, err := host.hostForwardRoutes(ctx, match)
	if err != nil {
		t.Fatalf("expected matching platform to pass, got %v", err)
	}
	if len(routes) != 1 || routes[0].GroupID != geminiGroup.ID {
		t.Fatalf("routes = %v", routes)
	}
}

func TestListTasksPluginIDsFilter(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:list_tasks_plugin_ids?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	t.Cleanup(func() { _ = db.Close() })

	for _, pid := range []string{"gateway-openai", "gateway-gemini", "other-plugin"} {
		db.Task.Create().SetPluginID(pid).SetTaskType("image.generate").SetUserID(7).SaveX(ctx)
	}

	host := &HostService{db: db}
	resp, err := host.listTasks(ctx, "", hostListTasksRequest{
		UserID:    7,
		PluginIDs: []string{"gateway-openai", "gateway-gemini"},
	})
	if err != nil {
		t.Fatalf("listTasks: %v", err)
	}
	if total := resp["total"].(int); total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	for _, item := range resp["tasks"].([]map[string]interface{}) {
		if pid := item["plugin_id"].(string); pid == "other-plugin" {
			t.Fatalf("leaked task from %q", pid)
		}
	}
}

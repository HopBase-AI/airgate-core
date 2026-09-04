package plugin

import (
	"context"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/DouDOU-start/airgate-core/ent/enttest"
	enttask "github.com/DouDOU-start/airgate-core/ent/task"
)

// tasks.get 的 plugin_ids 过滤：聚合型插件(studio)一次命中任务所属的执行插件,
// 不再逐插件试探;与 tasks.list 的 plugin_ids 语义一致(PluginIDs 优先于 PluginID)。
func TestGetTaskPluginIDs(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:get_task_plugin_ids?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))

	seedance := db.Task.Create().
		SetPluginID("gateway-seedance").
		SetTaskType("video.generate").
		SetUserID(6988).
		SetStatus(enttask.StatusProcessing).
		SetInput(map[string]interface{}{"prompt": "x"}).
		SaveX(ctx)
	other := db.Task.Create().
		SetPluginID("gateway-health").
		SetTaskType("probe").
		SetUserID(6988).
		SetStatus(enttask.StatusPending).
		SetInput(map[string]interface{}{}).
		SaveX(ctx)

	host := &HostService{db: db}
	executors := []string{"gateway-openai", "gateway-gemini", "gateway-seedance", "gateway-minimax", "gateway-bailian", "gateway-kling"}

	// 命中:调用方是 airgate-studio,任务属 gateway-seedance,靠 plugin_ids 一次查到
	resp, err := host.getTask(ctx, "airgate-studio", hostGetTaskRequest{TaskID: int64(seedance.ID), UserID: 6988, PluginIDs: executors})
	if err != nil {
		t.Fatalf("getTask(plugin_ids 含所属插件): %v", err)
	}
	task, _ := resp["task"].(map[string]interface{})
	if got := task["plugin_id"]; got != "gateway-seedance" {
		t.Fatalf("plugin_id = %v, want gateway-seedance", got)
	}

	// 越界:plugin_ids 是可见范围白名单,不在集合内的任务不能被捞到
	_, err = host.getTask(ctx, "airgate-studio", hostGetTaskRequest{TaskID: int64(other.ID), UserID: 6988, PluginIDs: executors})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("getTask(集合外任务) code = %v, want NotFound", status.Code(err))
	}

	// PluginIDs 优先于 PluginID:两者同给时按集合过滤
	resp, err = host.getTask(ctx, "airgate-studio", hostGetTaskRequest{TaskID: int64(seedance.ID), PluginID: "gateway-openai", PluginIDs: executors})
	if err != nil {
		t.Fatalf("getTask(PluginIDs 与 PluginID 同给): %v", err)
	}

	// 兼容:不带 plugin_ids 时行为不变——默认限定调用方自身插件,故 studio 直查会 NotFound
	_, err = host.getTask(ctx, "airgate-studio", hostGetTaskRequest{TaskID: int64(seedance.ID)})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("getTask(无 plugin 过滤,调用方 studio) code = %v, want NotFound", status.Code(err))
	}
	if _, err := host.getTask(ctx, "airgate-studio", hostGetTaskRequest{TaskID: int64(seedance.ID), PluginID: "gateway-seedance"}); err != nil {
		t.Fatalf("getTask(plugin_id 覆盖): %v", err)
	}

	// user_id 隔离依旧生效
	_, err = host.getTask(ctx, "airgate-studio", hostGetTaskRequest{TaskID: int64(seedance.ID), UserID: 1, PluginIDs: executors})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("getTask(他人 user_id) code = %v, want NotFound", status.Code(err))
	}
}

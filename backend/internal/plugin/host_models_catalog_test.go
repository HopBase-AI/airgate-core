package plugin

import (
	"context"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent/enttest"
)

func TestGetModelsCatalog(t *testing.T) {
	ctx := context.Background()
	db := enttest.Open(t, "sqlite3", "file:models_catalog?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(schema.WithGlobalUniqueID(false)))
	db.Setting.Create().SetGroup("models").SetKey("models.catalog.claude").SetValue(`[{"id":"claude-x","pricing":{"input":1,"output":5}}]`).SaveX(ctx)
	host := &HostService{db: db}

	// 命中:原样返回存储的 JSON 字符串(core 不解析)
	resp, err := host.getModelsCatalog(ctx, hostModelsCatalogRequest{Platform: "claude"})
	if err != nil {
		t.Fatalf("getModelsCatalog(claude): %v", err)
	}
	if got := resp["catalog_json"]; got != `[{"id":"claude-x","pricing":{"input":1,"output":5}}]` {
		t.Fatalf("catalog_json = %v, want 原样存储的 JSON", got)
	}

	// 未配置的平台 → 空字符串(非报错),插件据此纯用硬编码
	resp2, err := host.getModelsCatalog(ctx, hostModelsCatalogRequest{Platform: "openai"})
	if err != nil {
		t.Fatalf("getModelsCatalog(openai): %v", err)
	}
	if got := resp2["catalog_json"]; got != "" {
		t.Fatalf("未配置平台应返回空字符串,得到 %v", got)
	}

	// 空 platform → 报错
	if _, err := host.getModelsCatalog(ctx, hostModelsCatalogRequest{Platform: ""}); err == nil {
		t.Fatal("空 platform 应报错")
	}
}

func TestModelCatalogSettingKey(t *testing.T) {
	if got := modelCatalogSettingKey("claude"); got != "models.catalog.claude" {
		t.Fatalf("modelCatalogSettingKey(claude) = %q, want models.catalog.claude", got)
	}
	if got := modelCatalogSettingKey("openai"); got != "models.catalog.openai" {
		t.Fatalf("modelCatalogSettingKey(openai) = %q, want models.catalog.openai", got)
	}
	if got := modelCatalogSettingKey("kiro"); got != "models.catalog.kiro" {
		t.Fatalf("modelCatalogSettingKey(kiro) = %q, want models.catalog.kiro", got)
	}
}

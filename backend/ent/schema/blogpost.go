package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// BlogPost 博客文章：后台撰写、落地页 SSR 展示的内容营销文章。
type BlogPost struct {
	ent.Schema
}

func (BlogPost) Fields() []ent.Field {
	return []ent.Field{
		field.String("title").NotEmpty().
			Comment("文章标题。"),
		field.String("slug").NotEmpty().MaxLen(200).
			Comment("URL 短标识；落地页 /blog/<slug> 定位用，全局唯一。"),
		field.String("summary").Default("").
			Comment("列表摘要 / meta description 兜底。"),
		field.String("cover_image").Default("").
			Comment("封面图 URL（AssetStorage 公开地址）。"),
		field.String("content_html").Default("").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Comment("正文 HTML；已经后端 bluemonday 净化后落库。"),
		field.Enum("status").Values("draft", "published").Default("draft").
			Comment("草稿 / 已发布；公开页仅渲染 published。"),
		field.String("invite_code").Optional().Nillable().MaxLen(16).
			Comment("本文关联的推广邀请码；文章页注册/登录 CTA 自动拼 ?inv=。"),
		field.Bool("gate_enabled").Default(false).
			Comment("软性注册墙开关：开启后读到 gate_position% 弹注册遮罩。"),
		field.Int("gate_position").Default(50).Min(0).Max(100).
			Comment("注册墙触发位置（正文百分比 0~100）。"),
		field.String("lang").Default("zh").MaxLen(16).
			Comment("文章语言；多语言预留，MVP 单语言。"),
		field.JSON("tags", []string{}).Optional().
			Comment("轻量标签/分类。"),
		field.JSON("sites", []string{}).Optional().
			Comment("发布站点 key 列表;空=所有站点可见。SSR 按当前实例 site key(设置 blog_site_key)过滤,选项来自设置 blog_sites。"),
		field.String("seo_title").Default("").
			Comment("SEO 标题覆盖;空则回退 title。"),
		field.String("seo_description").Default("").
			Comment("SEO 描述覆盖;空则回退 summary。"),
		field.String("og_image").Default("").
			Comment("社交分享图覆盖;空则回退 cover_image。"),
		field.Int("author_id").Optional().Nillable().
			Comment("作者 user id;刻意不做外键,作者删除后保留归属。"),
		field.Int("view_count").Default(0).Min(0).
			Comment("阅读量。"),
		field.Time("published_at").Optional().Nillable().
			Comment("发布时间;首次转为 published 时写入。"),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (BlogPost) Indexes() []ent.Index {
	return []ent.Index{
		// slug 唯一,详情页按 slug 定位
		index.Fields("slug").Unique(),
		// 列表页:按状态过滤 + 发布时间倒序
		index.Fields("status", "published_at"),
	}
}

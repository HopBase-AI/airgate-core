package blogssr

// 多落地页站点支持(ToC 舰队):一份 core 实例同时服务 N 个落地页域名时,
// 博客按请求 Host 匹配 sites_branding 条目,各站渲染各自的品牌/皮肤/chrome,
// 并以站点键过滤文章(文章编辑器的「投放站点」多选)。
// sites_branding 与控制台 SPA 共用同一份 site 设置:name/logo/doc_url 为既有字段,
// host/blog_theme/blog_chrome 为博客侧扩展;未配 host 的实例行为与旧版完全一致。

import (
	"encoding/json"
	"strings"
)

// SiteBrandingEntry sites_branding 里一个落地页站点的条目(仅列博客侧消费的字段,
// 其余键如 doc_url 由控制台 SPA 消费,JSON 反序列化自动忽略)。
type SiteBrandingEntry struct {
	Name string `json:"name"`
	Logo string `json:"logo"`
	// Host 站点规范域名(如 late.essevin.com);博客按请求 Host 匹配该条目。
	Host string `json:"host"`
	// BlogTheme 站点皮肤覆盖(""/"ember"/"ink");非法值忽略,沿用全局 blog_theme。
	BlogTheme string `json:"blog_theme"`
	// BlogChrome 站点 chrome 覆盖(JSON,schema 同全局 blog_chrome);
	// 按「出现的键」字段级覆盖全局配置,未出现的键沿用全局值。
	BlogChrome json.RawMessage `json:"blog_chrome"`
}

// parseSitesBranding 解析 sites_branding JSON;空串或非法 JSON 返回 nil(行为同未配置)。
func parseSitesBranding(raw string) map[string]SiteBrandingEntry {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var m map[string]SiteBrandingEntry
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil
	}
	return m
}

// normalizeHost 归一域名用于匹配:小写、剥端口、剥 www. 前缀。
func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if i := strings.IndexByte(h, ':'); i >= 0 {
		h = h[:i]
	}
	return strings.TrimPrefix(h, "www.")
}

// resolveBrandingSite 依次按 ?site= 显式键(预览用)、请求 Host 匹配站点条目。
// Host 匹配只认配置了 host 的条目;都未命中返回 false(走实例级默认品牌)。
func resolveBrandingSite(entries map[string]SiteBrandingEntry, host, siteQuery string) (string, SiteBrandingEntry, bool) {
	if key := strings.TrimSpace(siteQuery); key != "" {
		if e, ok := entries[key]; ok {
			return key, e, true
		}
	}
	h := normalizeHost(host)
	if h == "" {
		return "", SiteBrandingEntry{}, false
	}
	for key, e := range entries {
		if eh := normalizeHost(e.Host); eh != "" && eh == h {
			return key, e, true
		}
	}
	return "", SiteBrandingEntry{}, false
}

// mergeChromeOverride 把站点级 blog_chrome 按「JSON 里出现的键」覆盖到全局 chrome;
// 未出现的键保持全局值,非法 JSON 返回原值(不因配置错误 5xx)。
// 与 resolveChrome(语言 i18n 覆盖)不同:这里 present 即覆盖,允许显式置空(如 signup_label:"")。
func mergeChromeOverride(base Chrome, raw json.RawMessage) Chrome {
	if len(raw) == 0 {
		return base
	}
	var present map[string]json.RawMessage
	if err := json.Unmarshal(raw, &present); err != nil {
		return base
	}
	var ov Chrome
	if err := json.Unmarshal(raw, &ov); err != nil {
		return base
	}
	has := func(key string) bool { _, ok := present[key]; return ok }
	if has("brand_label") {
		base.BrandLabel = ov.BrandLabel
	}
	if has("home_url") {
		base.HomeHref = ov.HomeHref
	}
	if has("eyebrow") {
		base.Eyebrow = ov.Eyebrow
	}
	if has("title") {
		base.Title = ov.Title
	}
	if has("subtitle") {
		base.Subtitle = ov.Subtitle
	}
	if has("nav") {
		base.Nav = ov.Nav
	}
	if has("footer") {
		base.Footer = ov.Footer
	}
	if has("footer_note") {
		base.FooterNote = ov.FooterNote
	}
	if has("login_label") {
		base.LoginLabel = ov.LoginLabel
	}
	if has("signup_label") {
		base.SignupLabel = ov.SignupLabel
	}
	if has("cta_desc") {
		base.CTADesc = ov.CTADesc
	}
	if has("show_langs") {
		base.ShowLangs = ov.ShowLangs
	}
	if has("default_lang") {
		base.DefaultLang = ov.DefaultLang
	}
	if has("i18n") {
		base.I18n = ov.I18n
	}
	return base
}

package blog

import (
	"regexp"
	"sync"

	"github.com/microcosm-cc/bluemonday"
)

var (
	blogSanitizerOnce sync.Once
	blogSanitizer     *bluemonday.Policy

	// allowedIframeSrc 限制视频嵌入 iframe 仅指向受信任的视频站点,
	// 防止任意 iframe 注入(点击劫持/钓鱼)。TipTap 视频扩展输出的正是这些 embed 地址。
	allowedIframeSrc = regexp.MustCompile(`^https://(www\.youtube\.com/embed/|www\.youtube-nocookie\.com/embed/|player\.bilibili\.com/player\.html|player\.vimeo\.com/video/)`)
)

// sanitizerPolicy 构造并缓存博客正文净化策略。
// 基线用 bluemonday.UGCPolicy(允许常见富文本标签、图片、带 nofollow 的链接,
// 剥离 script/style/on* 事件处理器),在其上追加 TipTap 会产出的少量元素/属性。
func sanitizerPolicy() *bluemonday.Policy {
	blogSanitizerOnce.Do(func() {
		p := bluemonday.UGCPolicy()

		// TipTap 富文本结构元素。
		p.AllowElements("figure", "figcaption", "section", "hr", "s", "u", "mark", "sup", "sub", "span", "div", "iframe")

		// 文本对齐:TipTap TextAlign 用内联 style="text-align:..."。
		p.AllowStyles("text-align").MatchingEnum("left", "center", "right", "justify").Globally()

		// class:代码块语言标记 / 高亮 / TipTap 节点类名。
		p.AllowAttrs("class").Globally()

		// 图片(UGCPolicy 已允许 img[src];这里补充展示属性)。
		p.AllowImages()
		p.AllowAttrs("alt", "title", "width", "height", "loading").OnElements("img")

		// 链接在新标签打开(UGCPolicy 已给外链加 rel=nofollow)。
		p.AllowAttrs("target").OnElements("a")

		// 视频嵌入 iframe:src 受 host 白名单约束。
		p.AllowAttrs("src").Matching(allowedIframeSrc).OnElements("iframe")
		p.AllowAttrs("width", "height", "frameborder", "allow", "allowfullscreen", "title", "loading").OnElements("iframe")

		blogSanitizer = p
	})
	return blogSanitizer
}

// SanitizeHTML 净化用户提交的正文 HTML,移除 <script>、事件处理器、危险标签与非白名单 iframe,
// 防止存储型 XSS。落库前调用。
func SanitizeHTML(raw string) string {
	return sanitizerPolicy().Sanitize(raw)
}

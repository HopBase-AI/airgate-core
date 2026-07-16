package blogssr

// 内联模板:保持自包含,无外链资源(避免大陆访问外链 render-blocking,皮肤字体用系统字栈近似主站)。
// html/template 自动按上下文转义;正文 Content 已在 service 层净化,以 template.HTML 注入。
//
// 模板按「head 元信息 + 皮肤样式 + 皮肤顶栏 + 页面主体 + 皮肤页脚」拼装,
// 每个皮肤一套完整模板,启动期解析(见 themes.go 与 renderer.go)。
// 默认皮肤("")即本文件的 default* 部件,渲染效果与历史版本一致。

const baseStyle = `
:root{color-scheme:light dark;--bg:#ffffff;--fg:#0f172a;--muted:#64748b;--border:#e5e7eb;--card:#f8fafc;--accent:#4f46e5;--accent-fg:#ffffff;--accent-soft:#eef2ff;--code-bg:#0b1020;--code-fg:#e5e9f2;--maxw:704px;--mono:"SFMono-Regular",ui-monospace,"JetBrains Mono",Menlo,Consolas,monospace;--shadow:0 1px 2px rgba(16,24,40,.05),0 16px 40px rgba(16,24,40,.08)}
@media (prefers-color-scheme:dark){:root{--bg:#0c0d10;--fg:#e7e9ee;--muted:#9aa2b1;--border:#23262d;--card:#15171c;--accent:#818cf8;--accent-fg:#0c0d10;--accent-soft:rgba(129,140,248,.14);--shadow:0 1px 2px rgba(0,0,0,.3),0 16px 40px rgba(0,0,0,.4)}}
*{box-sizing:border-box}
html{-webkit-text-size-adjust:100%}
html,body{margin:0;padding:0}
body{background:var(--bg);color:var(--fg);font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"PingFang SC","Hiragino Sans GB","Microsoft YaHei",sans-serif;line-height:1.75;-webkit-font-smoothing:antialiased;text-rendering:optimizeLegibility}
a{color:var(--accent);text-decoration:none}
a:hover{text-decoration:underline}
img{max-width:100%}
.blog-header{border-bottom:1px solid var(--border);position:sticky;top:0;z-index:20;background:color-mix(in srgb,var(--bg) 85%,transparent);backdrop-filter:saturate(1.4) blur(10px);-webkit-backdrop-filter:saturate(1.4) blur(10px)}
.blog-header-inner,.blog-wrap{max-width:var(--maxw);margin:0 auto;padding:0 22px}
.blog-header-inner{display:flex;align-items:center;gap:10px;height:58px}
.blog-header img{height:26px;width:auto;display:block}
.blog-logo-link{display:inline-flex;align-items:center}
.blog-brand{font-weight:600;font-size:15.5px;color:var(--fg);letter-spacing:-.01em}
.blog-wrap{padding-top:46px;padding-bottom:90px}
.blog-intro{margin-bottom:34px}
.blog-intro-title{font-size:32px;font-weight:750;letter-spacing:-.025em;margin:0 0 9px;line-height:1.15}
.blog-intro-sub{color:var(--muted);font-size:16px;margin:0}
.blog-list{display:grid;grid-template-columns:repeat(auto-fill,minmax(300px,1fr));gap:22px;margin-top:8px}
.blog-card{border:1px solid var(--border);border-radius:16px;overflow:hidden;background:var(--card);display:flex;flex-direction:column;transition:transform .18s ease,box-shadow .18s ease,border-color .18s ease}
.blog-card:hover{transform:translateY(-3px);box-shadow:var(--shadow);border-color:color-mix(in srgb,var(--accent) 40%,var(--border));text-decoration:none}
.blog-card img{width:100%;aspect-ratio:16/9;object-fit:cover;display:block}
.blog-card-body{padding:15px 18px 18px}
.blog-card-title{font-size:17px;font-weight:600;margin:0 0 8px;color:var(--fg);line-height:1.4;letter-spacing:-.01em}
.blog-card-summary{font-size:14px;color:var(--muted);margin:0 0 12px;line-height:1.6;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden}
.blog-card-date{font-size:12.5px;color:var(--muted)}
.blog-empty{display:flex;flex-direction:column;align-items:center;text-align:center;padding:72px 20px;color:var(--muted)}
.blog-empty svg{width:44px;height:44px;opacity:.5;margin-bottom:16px}
.blog-empty-title{font-size:17px;font-weight:600;color:var(--fg);margin:0 0 6px}
.blog-empty-sub{font-size:14px;margin:0}
.article-eyebrow{display:inline-block;font-size:12.5px;font-weight:600;letter-spacing:.08em;text-transform:uppercase;color:var(--accent);margin:0 0 14px}
.article-title{font-size:39px;font-weight:750;line-height:1.16;letter-spacing:-.025em;margin:0 0 16px;text-wrap:balance}
.article-byline{display:flex;flex-wrap:wrap;align-items:center;gap:8px;color:var(--muted);font-size:14px;margin-bottom:30px}
.article-byline .dot{width:3px;height:3px;border-radius:50%;background:currentColor;opacity:.5}
.article-avatar{width:26px;height:26px;border-radius:50%;background:linear-gradient(135deg,var(--accent),color-mix(in srgb,var(--accent) 45%,#000));display:inline-block;flex:none}
.article-cover{width:100%;border-radius:16px;margin:0 0 36px;display:block;box-shadow:var(--shadow)}
.article-content{font-size:18px;line-height:1.78;overflow-wrap:break-word}
.article-content>*:first-child{margin-top:0}
.article-content p{margin:0 0 1.35em}
.article-content>p:first-child{font-size:20px;line-height:1.6;color:var(--fg)}
.article-content h2{font-size:27px;font-weight:700;letter-spacing:-.02em;line-height:1.3;margin:2em 0 .7em;scroll-margin-top:76px}
.article-content h3{font-size:21px;font-weight:600;line-height:1.4;margin:1.7em 0 .5em;scroll-margin-top:76px}
.article-content h4{font-size:17.5px;font-weight:600;margin:1.4em 0 .4em}
.article-content a{color:var(--accent);text-decoration:underline;text-underline-offset:3px;text-decoration-thickness:1px;text-decoration-color:color-mix(in srgb,var(--accent) 40%,transparent)}
.article-content a:hover{text-decoration-color:var(--accent)}
.article-content strong{font-weight:650;color:var(--fg)}
.article-content ul,.article-content ol{padding-left:1.5em;margin:0 0 1.35em}
.article-content li{margin:.5em 0;padding-left:.25em}
.article-content li::marker{color:var(--accent)}
.article-content img{display:block;height:auto;margin:0 auto;border-radius:14px}
.article-content p>img,.article-content>img{margin:2.4em auto;border:1px solid var(--border);box-shadow:var(--shadow)}
.article-content figure{margin:2.4em 0}
.article-content figure img{border:1px solid var(--border);box-shadow:var(--shadow)}
.article-content figcaption{text-align:center;font-size:13.5px;color:var(--muted);margin-top:12px;font-style:italic;line-height:1.55}
.article-content :not(pre)>code{background:var(--accent-soft);color:var(--accent);border-radius:6px;padding:.12em .42em;font-family:var(--mono);font-size:.82em;font-weight:500}
.article-content pre{background:var(--code-bg);color:var(--code-fg);border-radius:14px;padding:18px 20px;overflow-x:auto;margin:1.8em 0;line-height:1.7;box-shadow:var(--shadow)}
.article-content pre code{font-family:var(--mono);font-size:.85em;color:inherit;background:none;border:0;padding:0}
.article-content blockquote{margin:2em 0;padding:4px 0 4px 24px;border-left:3px solid var(--accent);font-size:21px;line-height:1.5;color:var(--fg)}
.article-content blockquote p{margin:0}
.article-content hr{border:0;height:1px;background:var(--border);margin:3em 0}
.article-content iframe{max-width:100%;aspect-ratio:16/9;width:100%;border:0;border-radius:14px;margin:2em 0;display:block}
.article-content table{border-collapse:collapse;width:100%;font-size:15px;margin:1.8em 0;display:block;overflow-x:auto}
.article-content thead th{text-align:left;font-weight:600;color:var(--muted);font-size:12.5px;letter-spacing:.04em;text-transform:uppercase;padding:0 14px 10px;border-bottom:1px solid var(--border)}
.article-content tbody td{padding:11px 14px;border-bottom:1px solid var(--border)}
.article-tags{margin-top:40px;display:flex;flex-wrap:wrap;gap:8px}
.article-tag{font-size:12.5px;color:var(--muted);border:1px solid var(--border);border-radius:999px;padding:5px 13px}
.blog-back{display:inline-block;margin-bottom:22px;font-size:14px;color:var(--muted)}
.blog-back:hover{color:var(--accent);text-decoration:none}
.blog-footer{border-top:1px solid var(--border);color:var(--muted);font-size:13px;text-align:center;padding:26px 20px;margin-top:48px}
.blog-gate{position:fixed;left:0;right:0;bottom:0;height:56vh;pointer-events:none;display:flex;align-items:flex-end;justify-content:center;background:linear-gradient(to bottom,rgba(0,0,0,0) 0%,var(--bg) 58%);z-index:50}
.blog-gate-card{pointer-events:auto;text-align:center;max-width:428px;width:calc(100% - 40px);margin-bottom:8vh;padding:26px;border:1px solid var(--border);border-radius:18px;background:var(--card);box-shadow:0 14px 44px rgba(0,0,0,.2)}
.blog-gate-title{font-size:18px;font-weight:650;margin:0 0 8px}
.blog-gate-desc{font-size:14px;color:var(--muted);margin:0 0 16px;line-height:1.6}
.blog-gate-btn{display:inline-block;background:var(--accent);color:var(--accent-fg);font-weight:600;padding:11px 28px;border-radius:10px}
.blog-gate-btn:hover{text-decoration:none;opacity:.92}
.blog-gate-dismiss{display:block;margin:12px auto 0;background:none;border:0;color:var(--muted);font-size:13px;cursor:pointer}
.blog-cta{margin:52px 0 8px;border:1px solid color-mix(in srgb,var(--accent) 24%,var(--border));border-radius:18px;background:linear-gradient(135deg,var(--accent-soft),var(--card) 75%);padding:30px 28px}
.blog-cta-title{font-size:19px;font-weight:700;margin:0 0 8px;color:var(--fg);letter-spacing:-.01em}
.blog-cta-desc{font-size:14.5px;color:var(--muted);margin:0 0 18px;line-height:1.6}
.blog-cta-btn{display:inline-block;background:var(--accent);color:var(--accent-fg);font-weight:600;padding:11px 26px;border-radius:11px;font-size:15px}
.blog-cta-btn:hover{text-decoration:none;opacity:.92}
@media (max-width:560px){.article-title{font-size:30px}.article-content{font-size:17px}.article-content>p:first-child{font-size:18px}.blog-intro-title{font-size:27px}}
`

// ―――――― 共享 head(所有皮肤一致,SEO 元信息) ――――――

const listHeadStr = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.PageTitle}}</title>
<meta name="description" content="{{.SiteName}} 博客:AI 使用方法、模型技巧与实践分享。">
<meta name="robots" content="index, follow, max-image-preview:large">
<link rel="canonical" href="{{.OriginBase}}/blog">
<meta property="og:type" content="website">
<meta property="og:title" content="{{.PageTitle}}">
<meta property="og:url" content="{{.OriginBase}}/blog">
`

const detailHeadStr = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}} · {{.SiteName}}</title>
<meta name="description" content="{{.MetaDescription}}">
<meta name="robots" content="index, follow, max-image-preview:large, max-snippet:-1">
<link rel="canonical" href="{{.Canonical}}">
<meta property="og:type" content="article">
<meta property="og:site_name" content="{{.SiteName}}">
<meta property="og:title" content="{{.Title}}">
<meta property="og:description" content="{{.MetaDescription}}">
<meta property="og:url" content="{{.Canonical}}">
{{if .OGImage}}<meta property="og:image" content="{{.OGImage}}">{{end}}
{{if .PublishedISO}}<meta property="article:published_time" content="{{.PublishedISO}}">{{end}}
{{if .ModifiedISO}}<meta property="article:modified_time" content="{{.ModifiedISO}}">{{end}}
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="{{.Title}}">
<meta name="twitter:description" content="{{.MetaDescription}}">
{{if .OGImage}}<meta name="twitter:image" content="{{.OGImage}}">{{end}}
<script type="application/ld+json">{{.JSONLD}}</script>
<script type="application/ld+json">{{.BreadcrumbLD}}</script>
`

const notFoundHeadStr = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>文章不存在 · {{.SiteName}}</title>
<meta name="robots" content="noindex">
`

// ―――――― 默认皮肤("")的顶栏/页脚 ――――――

const defaultHeaderStr = `<header class="blog-header"><div class="blog-header-inner">
<a href="{{.HomeURL}}" class="blog-logo-link">{{if .LogoURL}}<img src="{{.LogoSrc}}" alt="{{.SiteName}}">{{end}}</a>
<a href="{{.HomeURL}}" class="blog-brand">{{if .SiteName}}{{.SiteName}}{{end}} Blog</a>
</div></header>
`

const defaultFooterStr = `<footer class="blog-footer">© {{.SiteName}}</footer>
`

// ―――――― 页面主体(列表:默认皮肤;详情/空态:全皮肤共享) ――――――

const emptyStateStr = `<div class="blog-empty">
<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"><path d="M4 5a2 2 0 0 1 2-2h8l6 6v10a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2z"/><path d="M14 3v6h6"/><path d="M8 13h8M8 17h5"/></svg>
<p class="blog-empty-title">文章正在路上</p>
<p class="blog-empty-sub">我们正在准备第一批内容,敬请期待。</p>
</div>
`

const defaultListBodyStr = `<main class="blog-wrap">
<div class="blog-intro">
<h1 class="blog-intro-title">{{.Heading}}</h1>
<p class="blog-intro-sub">{{.Subtitle}}</p>
</div>
{{if .Posts}}
<div class="blog-list">
{{range .Posts}}
<a class="blog-card" href="{{.URL}}">
{{if .CoverImage}}<img src="{{.CoverImage}}" alt="{{.Title}}" loading="lazy">{{end}}
<div class="blog-card-body">
<h2 class="blog-card-title">{{.Title}}</h2>
{{if .Summary}}<p class="blog-card-summary">{{.Summary}}</p>{{end}}
{{if .PublishedAt}}<div class="blog-card-date">{{.PublishedAt}}</div>{{end}}
</div>
</a>
{{end}}
</div>
{{else}}
` + emptyStateStr + `{{end}}
</main>
`

// detailBodyStr 详情主体:全部皮肤共享同一结构,视觉差异全部由皮肤 CSS 承担。
const detailBodyStr = `<main class="blog-wrap">
<a href="{{.HomeURL}}" class="blog-back">← 返回博客</a>
<article>
{{if .Eyebrow}}<div class="article-eyebrow">{{.Eyebrow}}</div>{{end}}
<h1 class="article-title">{{.Title}}</h1>
<div class="article-byline">
<span class="article-avatar" aria-hidden="true"></span>
<span>{{.AuthorName}}</span>
{{if .PublishedHuman}}<span class="dot" aria-hidden="true"></span><span>{{.PublishedHuman}}</span>{{end}}
{{if .ReadingTime}}<span class="dot" aria-hidden="true"></span><span>{{.ReadingTime}}</span>{{end}}
</div>
{{if .CoverImage}}<img class="article-cover" src="{{.CoverImage}}" alt="{{.Title}}">{{end}}
<div class="article-content" id="blog-content">{{.Content}}</div>
{{if .Tags}}<div class="article-tags">{{range .Tags}}<span class="article-tag">{{.}}</span>{{end}}</div>{{end}}
</article>
<aside class="blog-cta">
<p class="blog-cta-title">{{.CTATitle}}</p>
<p class="blog-cta-desc">{{.CTADesc}}</p>
<a class="blog-cta-btn" href="{{.RegisterURL}}">免费开始 →</a>
</aside>
</main>
`

// gateStr 软墙遮罩 + 滚动触发脚本(全皮肤共享)。
const gateStr = `{{if .GateEnabled}}
<div id="blog-gate" class="blog-gate" hidden>
<div class="blog-gate-card">
<p class="blog-gate-title">注册后继续阅读全文</p>
<p class="blog-gate-desc">免费注册即可读完本文,并获得 API 额度体验。</p>
<a class="blog-gate-btn" href="{{.RegisterURL}}">免费注册 / 登录</a>
<button type="button" class="blog-gate-dismiss" id="blog-gate-dismiss">以后再说</button>
</div>
</div>
<script>
(function(){
var pos={{.GatePosition}};
var content=document.getElementById('blog-content');
var gate=document.getElementById('blog-gate');
if(!content||!gate)return;
var dismissed=false;
var btn=document.getElementById('blog-gate-dismiss');
if(btn)btn.addEventListener('click',function(){dismissed=true;gate.setAttribute('hidden','');});
function onScroll(){
if(dismissed)return;
var rect=content.getBoundingClientRect();
var total=content.offsetHeight||1;
var scrolled=Math.min(Math.max(-rect.top+window.innerHeight*0.5,0),total);
var pct=(scrolled/total)*100;
if(pct>=pos){gate.removeAttribute('hidden');}else{gate.setAttribute('hidden','');}
}
window.addEventListener('scroll',onScroll,{passive:true});
window.addEventListener('resize',onScroll,{passive:true});
onScroll();
})();
</script>
{{end}}
`

const notFoundBodyStr = `<main class="blog-wrap">
<h1 class="article-title">文章不存在</h1>
<p class="blog-empty">该文章可能已下线或链接有误。</p>
<a href="/blog" class="blog-back">← 返回博客</a>
</main>
`

// ―――――― 模板装配 ――――――

// assemble 拼一个完整页面:head + <style> + 顶栏 + 主体(+页脚+尾部片段)。
func assemble(head, css, header, body, footer, tail string) string {
	return head + "<style>" + css + "</style>\n</head>\n<body>\n" + header + body + footer + tail + "</body>\n</html>"
}

// listTemplateStr 按皮肤返回列表页模板串。
func listTemplateStr(theme string) string {
	switch theme {
	case themeEmber:
		return assemble(listHeadStr, baseStyle+emberVars+emberChromeCSS, skinHeaderStr, emberListBodyStr, skinFooterStr, skinLangScriptStr)
	case themeInk:
		return assemble(listHeadStr, baseStyle+inkVars+inkChromeCSS, skinHeaderStr, inkListBodyStr, skinFooterStr, skinLangScriptStr)
	default:
		return assemble(listHeadStr, baseStyle, defaultHeaderStr, defaultListBodyStr, defaultFooterStr, "")
	}
}

// detailTemplateStr 按皮肤返回详情页模板串。
func detailTemplateStr(theme string) string {
	switch theme {
	case themeEmber:
		return assemble(detailHeadStr, baseStyle+emberVars+emberChromeCSS, skinHeaderStr, detailBodyStr, skinFooterStr, gateStr)
	case themeInk:
		return assemble(detailHeadStr, baseStyle+inkVars+inkChromeCSS, skinHeaderStr, detailBodyStr, skinFooterStr, gateStr)
	default:
		return assemble(detailHeadStr, baseStyle, defaultHeaderStr, detailBodyStr, defaultFooterStr, gateStr)
	}
}

// notFoundTemplateStr 按皮肤返回 404 页模板串。
func notFoundTemplateStr(theme string) string {
	switch theme {
	case themeEmber:
		return assemble(notFoundHeadStr, baseStyle+emberVars+emberChromeCSS, skinHeaderStr, notFoundBodyStr, skinFooterStr, "")
	case themeInk:
		return assemble(notFoundHeadStr, baseStyle+inkVars+inkChromeCSS, skinHeaderStr, notFoundBodyStr, skinFooterStr, "")
	default:
		return assemble(notFoundHeadStr, baseStyle, "", notFoundBodyStr, "", "")
	}
}

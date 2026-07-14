package blogssr

// 内联模板:保持自包含,无外链资源(避免大陆访问外链 render-blocking)。
// html/template 自动按上下文转义;正文 Content 已在 service 层净化,以 template.HTML 注入。

const baseStyle = `
:root{color-scheme:light dark;--bg:#ffffff;--fg:#1a1a1a;--muted:#666;--border:#e5e7eb;--card:#fafafa;--accent:#2563eb;--accent-fg:#fff}
@media (prefers-color-scheme:dark){:root{--bg:#0f0f10;--fg:#ededed;--muted:#9a9a9a;--border:#2a2a2c;--card:#17181a;--accent:#3b82f6;--accent-fg:#fff}}
*{box-sizing:border-box}
html,body{margin:0;padding:0}
body{background:var(--bg);color:var(--fg);font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"PingFang SC","Microsoft YaHei",sans-serif;line-height:1.7;-webkit-font-smoothing:antialiased}
a{color:var(--accent);text-decoration:none}
a:hover{text-decoration:underline}
.blog-header{border-bottom:1px solid var(--border)}
.blog-header-inner,.blog-wrap{max-width:760px;margin:0 auto;padding:0 20px}
.blog-header-inner{display:flex;align-items:center;gap:10px;height:60px}
.blog-header img{height:26px;width:auto}
.blog-brand{font-weight:600;font-size:16px;color:var(--fg)}
.blog-wrap{padding-top:32px;padding-bottom:64px}
.blog-list{display:grid;grid-template-columns:repeat(auto-fill,minmax(320px,1fr));gap:20px;margin-top:24px}
.blog-card{border:1px solid var(--border);border-radius:12px;overflow:hidden;background:var(--card);display:flex;flex-direction:column}
.blog-card img{width:100%;height:160px;object-fit:cover;display:block}
.blog-card-body{padding:16px}
.blog-card-title{font-size:17px;font-weight:600;margin:0 0 8px;color:var(--fg)}
.blog-card-summary{font-size:14px;color:var(--muted);margin:0 0 10px;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden}
.blog-card-date{font-size:12px;color:var(--muted)}
.blog-empty{color:var(--muted);margin-top:32px}
.article-title{font-size:30px;font-weight:700;line-height:1.3;margin:8px 0 12px}
.article-meta{color:var(--muted);font-size:14px;margin-bottom:24px}
.article-cover{width:100%;border-radius:12px;margin:8px 0 24px;display:block}
.article-content{font-size:17px}
.article-content img{max-width:100%;height:auto;border-radius:8px}
.article-content h2{font-size:23px;margin-top:1.8em}
.article-content h3{font-size:20px;margin-top:1.6em}
.article-content pre{background:var(--card);border:1px solid var(--border);border-radius:8px;padding:14px;overflow-x:auto}
.article-content code{font-family:"SFMono-Regular",Consolas,monospace;font-size:.9em}
.article-content blockquote{border-left:3px solid var(--border);margin:1em 0;padding:0 1em;color:var(--muted)}
.article-content iframe{max-width:100%;aspect-ratio:16/9;width:100%;border:0;border-radius:8px}
.article-content table{border-collapse:collapse;width:100%;overflow-x:auto;display:block}
.article-content th,.article-content td{border:1px solid var(--border);padding:8px 12px}
.article-tags{margin-top:32px;display:flex;flex-wrap:wrap;gap:8px}
.article-tag{font-size:12px;color:var(--muted);border:1px solid var(--border);border-radius:999px;padding:3px 10px}
.blog-back{display:inline-block;margin-bottom:20px;font-size:14px}
.blog-footer{border-top:1px solid var(--border);color:var(--muted);font-size:13px;text-align:center;padding:24px 20px;margin-top:40px}
.blog-gate{position:fixed;left:0;right:0;bottom:0;height:52vh;pointer-events:none;display:flex;align-items:flex-end;justify-content:center;background:linear-gradient(to bottom,rgba(0,0,0,0) 0%,var(--bg) 55%);z-index:50}
.blog-gate-card{pointer-events:auto;text-align:center;max-width:420px;width:calc(100% - 40px);margin-bottom:8vh;padding:24px;border:1px solid var(--border);border-radius:16px;background:var(--card);box-shadow:0 12px 40px rgba(0,0,0,.18)}
.blog-gate-title{font-size:18px;font-weight:600;margin:0 0 8px}
.blog-gate-desc{font-size:14px;color:var(--muted);margin:0 0 16px}
.blog-gate-btn{display:inline-block;background:var(--accent);color:var(--accent-fg);font-weight:600;padding:11px 28px;border-radius:10px}
.blog-gate-btn:hover{text-decoration:none;opacity:.92}
.blog-gate-dismiss{display:block;margin:12px auto 0;background:none;border:0;color:var(--muted);font-size:13px;cursor:pointer}
`

const listTmplStr = `<!doctype html>
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
<style>` + baseStyle + `</style>
</head>
<body>
<header class="blog-header"><div class="blog-header-inner">
<a href="/blog">{{if .LogoURL}}<img src="{{.LogoURL}}" alt="{{.SiteName}}">{{end}}</a>
<a href="/blog" class="blog-brand">{{if .SiteName}}{{.SiteName}}{{end}} Blog</a>
</div></header>
<main class="blog-wrap">
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
<p class="blog-empty">暂无文章。</p>
{{end}}
</main>
<footer class="blog-footer">© {{.SiteName}}</footer>
</body>
</html>`

const detailTmplStr = `<!doctype html>
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
<style>` + baseStyle + `</style>
</head>
<body>
<header class="blog-header"><div class="blog-header-inner">
<a href="/blog">{{if .LogoURL}}<img src="{{.LogoURL}}" alt="{{.SiteName}}">{{end}}</a>
<a href="/blog" class="blog-brand">{{if .SiteName}}{{.SiteName}}{{end}} Blog</a>
</div></header>
<main class="blog-wrap">
<a href="/blog" class="blog-back">← 返回博客</a>
<article>
<h1 class="article-title">{{.Title}}</h1>
{{if .PublishedHuman}}<div class="article-meta">{{.PublishedHuman}}</div>{{end}}
{{if .CoverImage}}<img class="article-cover" src="{{.CoverImage}}" alt="{{.Title}}">{{end}}
<div class="article-content" id="blog-content">{{.Content}}</div>
{{if .Tags}}<div class="article-tags">{{range .Tags}}<span class="article-tag">{{.}}</span>{{end}}</div>{{end}}
</article>
</main>
<footer class="blog-footer">© {{.SiteName}}</footer>
{{if .GateEnabled}}
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
</body>
</html>`

const notFoundTmplStr = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>文章不存在 · {{.SiteName}}</title>
<meta name="robots" content="noindex">
<style>` + baseStyle + `</style>
</head>
<body>
<main class="blog-wrap">
<h1 class="article-title">文章不存在</h1>
<p class="blog-empty">该文章可能已下线或链接有误。</p>
<a href="/blog" class="blog-back">← 返回博客</a>
</main>
</body>
</html>`

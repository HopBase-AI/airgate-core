package blogssr

// HopBase uses a dedicated paper-terminal theme. It deliberately does not
// replace ember because ember remains the Open Late editorial theme.

const hopBaseHeaderStr = `<a class="hb-skip" href="#main-content">Skip to content</a>
{{if .AnnouncementEnabled}}<div class="hb-announcement" role="status"><span class="hb-announcement-badge">{{.AnnouncementBadge}}</span><span class="hb-announcement-text">{{.AnnouncementText}}</span><a href="{{.AnnouncementHref}}"><span>{{.AnnouncementLink}}</span><span aria-hidden="true">&#8594;</span></a></div>{{end}}
<header class="hb-header" data-hb-header><div class="hb-header-inner">
<a href="{{.SiteURL}}" class="hb-brand" aria-label="{{.BrandLabel}} home">{{if .LogoURL}}<img src="{{.LogoSrc}}" alt="">{{end}}<b>HopBase</b></a>
<nav class="hb-nav" aria-label="Primary navigation">{{range .Nav}}<a href="{{.Href}}"{{if .Active}} class="act" aria-current="page"{{end}}>{{.Label}}</a>{{end}}</nav>
<div class="hb-actions">
{{if .ShowLangs}}<div class="hb-language" data-hb-language>
<button class="hb-control hb-language-trigger" type="button" aria-haspopup="menu" aria-expanded="false"><span>{{range .LangNav}}{{if .Active}}{{.LongLabel}}{{end}}{{end}}</span><i aria-hidden="true"></i></button>
<div class="hb-language-menu" role="menu" hidden>{{range .LangNav}}<a href="{{.Href}}" role="menuitemradio" aria-checked="{{.Active}}"{{if .Active}} class="act"{{end}}><span>{{.LongLabel}}</span><i aria-hidden="true">&#10003;</i></a>{{end}}</div>
</div>{{end}}
<span class="hb-auth" data-blog-auth data-console-url="{{.ConsoleURL}}" data-auth-state="loading">
<span class="hb-auth-guest" data-blog-auth-guest hidden><a class="hb-login" href="{{.RegisterURL}}">{{.LoginLabel}}</a></span>
<a class="hb-user" data-blog-auth-user href="{{.ConsoleURL}}" aria-label="{{.LoginLabel}}" title="{{.LoginLabel}}" hidden><span class="hb-user-avatar" data-blog-auth-avatar aria-hidden="true">U</span><span class="hb-user-name" data-blog-auth-name></span></a>
</span>
<button class="hb-menu-button" type="button" aria-label="Menu" aria-expanded="false" data-hb-menu-button><span aria-hidden="true"></span></button>
</div></div>
<div class="hb-mobile-menu" data-hb-mobile-menu hidden><nav aria-label="Mobile navigation">{{range .Nav}}<a href="{{.Href}}"{{if .Active}} class="act" aria-current="page"{{end}}>{{.Label}}</a>{{end}}</nav></div>
</header>
`

// hopBaseFooterStr 与主站落地页页脚(landing/index.html 的 <footer class="ft">)保持同构:
// 三段式 = 品牌+链接列(产品/合作/社区/合作伙伴)+ 模型矩阵。改主站页脚时同步这里。
const hopBaseFooterStr = `<footer class="ft"><div class="ft-in">
<div class="ft-top">
<div class="ft-col ft-col--brand">
<div class="ft-brand">{{if .LogoURL}}<img src="{{.LogoSrc}}" alt="">{{end}}<span>HopBase</span></div>
<p class="ft-desc">{{.FT.desc}}</p>
</div>
<div class="ft-col">
<h4>{{.FT.product}}</h4>
<a href="{{.ConsoleURL}}/login">{{.FT.console}}</a>
<a href="/models">{{.FT.plaza}}</a>
<a href="/pricing">{{.FT.pricing}}</a>
<a href="{{.FT.docsHref}}">{{.FT.docs}}</a>
<a href="/blog">{{.FT.blog}}</a>
</div>
<div class="ft-col">
<h4>{{.FT.coop}}</h4>
<a class="ft-link-featured" href="/oumomo">{{.FT.tiktok}}</a>
<a href="/workbuddy">{{.FT.workbuddy}}</a>
<a href="/#contact">{{.FT.coopContact}}</a>
</div>
<div class="ft-col">
<h4>{{.FT.community}}</h4>
<a href="https://t.me/Aaqhop" target="_blank" rel="noopener noreferrer">Telegram</a>
<a href="https://qm.qq.com/q/ppgfhlFbxK" target="_blank" rel="noopener noreferrer">{{.FT.qq}}</a>
<a href="/#contact">{{.FT.contact}}</a>
</div>
<div class="ft-col ft-col--partners">
<h4>{{.FT.partners}}</h4>
<a href="/workbuddy" class="ft-partner-mark"><img src="/assets/partner-marks/tencent-wordmark.png" alt="Tencent 腾讯"></a>
<a href="/oumomo" class="ft-partner-mark ft-partner-mark--fastmoss"><img src="/assets/partner-marks/fastmoss-wordmark.svg" alt="FastMoss"></a>
<a href="/oumomo" class="ft-partner-mark ft-partner-mark--oumomo"><img src="/assets/partner-marks/oumomo-wordmark.png" alt="Oumomo 欧漠漠"></a>
<a href="/#digital-human" class="ft-partner-mark ft-partner-mark--text">MiniMax</a>
<a href="/#partners" class="ft-partner-mark ft-partner-mark--text ft-partner-mark--cjk" title="Alibaba Cloud">阿里云</a>
</div>
</div>
<div class="ft-taxonomy" aria-label="Model directory">
<div class="ft-col ft-model-col">
<h4>Claude</h4>
<a class="ft-model-featured" href="/models#claude-fable-5-1">claude-fable-5-1</a>
<a href="/models#claude-fable-5">claude-fable-5</a>
<a href="/models#claude-opus-5">claude-opus-5</a>
<a href="/models#claude-sonnet-5">claude-sonnet-5</a>
<a href="/models#claude-opus-4-8">claude-opus-4-8</a>
</div>
<div class="ft-col ft-model-col">
<h4>OpenAI / Codex</h4>
<a class="ft-model-featured" href="/models#gpt-5-6-sol">gpt-5.6-sol</a>
<a href="/models#codex-auto-review">codex-auto-review</a>
<a href="/models#gpt-5-6-terra">gpt-5.6-terra</a>
<a href="/models#gpt-5-4">gpt-5.4</a>
</div>
<div class="ft-col ft-model-col">
<h4>Gemini</h4>
<a class="ft-model-featured" href="/models#gemini-3-6-flash">gemini-3.6-flash</a>
<a href="/models#gemini-3-5-flash">gemini-3.5-flash</a>
<a href="/models#gemini-3-1-pro-preview">gemini-3.1-pro-preview</a>
<a href="/models#gemini-2-5-pro">gemini-2.5-pro</a>
</div>
<div class="ft-col ft-model-col">
<h4>GLM / DeepSeek</h4>
<a class="ft-model-featured" href="/models#glm-5-3">glm-5.3</a>
<a class="ft-model-featured" href="/models#deepseek-v4-flash-202605">deepseek-v4-flash-202605</a>
<a href="/models#glm-5-2">glm-5.2</a>
</div>
<div class="ft-col ft-model-col">
<h4>{{.FT.image}}</h4>
<a class="ft-model-featured" href="/models#seedream-5-0-pro">seedream-5-0-pro</a>
<a href="/models#gpt-image-2">gpt-image-2</a>
<a href="/models#gemini-3-pro-image">gemini-3-pro-image</a>
<a href="/models#kling-image-v3-omni">kling-image-v3-omni</a>
</div>
<div class="ft-col ft-model-col">
<h4>{{.FT.video}}</h4>
<a class="ft-model-featured" href="/models#MiniMax-H3">MiniMax-H3</a>
<a class="ft-model-featured" href="/models#dreamina-seedance-2-5-260628">Seedance 2.5</a>
<a class="ft-model-featured" href="/models#kling-v3-omni">kling-v3-omni</a>
<a href="/models#dreamina-seedance-2-0-260128">dreamina-seedance-2-0</a>
<a href="/models#kling-v3-motion-control">kling-v3-motion-control</a>
</div>
</div>
</div></footer>
`

// hopBaseFooterText 页脚三段式的本地化文案;键与 hopBaseFooterStr 模板一一对应。
func hopBaseFooterText(lang string) map[string]string {
	switch canonicalLang(lang) {
	case "en":
		return map[string]string{
			"desc":        "A stable, high-concurrency AI gateway for enterprises and agent services.",
			"product":     "Product",
			"console":     "Console",
			"plaza":       "Model catalog",
			"pricing":     "Pricing",
			"docs":        "Docs",
			"docsHref":    "/en/docs",
			"blog":        "Blog",
			"coop":        "Cooperation",
			"tiktok":      "TikTok-ecosystem AI products",
			"workbuddy":   "Learn about WorkBuddy",
			"coopContact": "Enterprise cooperation",
			"community":   "Community",
			"qq":          "QQ Community",
			"contact":     "Contact",
			"partners":    "Partners",
			"image":       "Image",
			"video":       "Video",
		}
	case "zh":
		return map[string]string{
			"desc":        "面向企业与 Agent 服务的稳定高并发 AI 网关。",
			"product":     "产品",
			"console":     "控制台",
			"plaza":       "模型广场",
			"pricing":     "模型价格",
			"docs":        "接入文档",
			"docsHref":    "/zh-cn/docs",
			"blog":        "博客",
			"coop":        "合作",
			"tiktok":      "TikTok 生态 AI 产品",
			"workbuddy":   "了解 WorkBuddy",
			"coopContact": "企业合作咨询",
			"community":   "社区",
			"qq":          "QQ 社群",
			"contact":     "联系方式",
			"partners":    "合作伙伴",
			"image":       "生图",
			"video":       "视频",
		}
	default:
		return map[string]string{
			"desc":        "面向企業與 Agent 服務的穩定高並發 AI 閘道。",
			"product":     "產品",
			"console":     "控制台",
			"plaza":       "模型廣場",
			"pricing":     "模型價格",
			"docs":        "接入文檔",
			"docsHref":    "/zh-tw/docs",
			"blog":        "博客",
			"coop":        "合作",
			"tiktok":      "TikTok 生態 AI 產品",
			"workbuddy":   "了解 WorkBuddy",
			"coopContact": "企業合作諮詢",
			"community":   "社區",
			"qq":          "QQ 社群",
			"contact":     "聯繫方式",
			"partners":    "合作夥伴",
			"image":       "生圖",
			"video":       "影片",
		}
	}
}

const hopBaseListBodyStr = `<main class="hb-main" id="main-content">
<div class="hb-frame hb-ledger">
<h1 class="hb-sr-only">{{.Heading}}</h1>
{{if .Posts}}<div class="hb-grid">{{range .Posts}}
<a class="hb-card" href="{{.URL}}">
{{if .CoverImage}}<span class="hb-card-cover hb-cover-real" aria-hidden="true"><img src="{{.CoverImage}}" alt="" loading="lazy" decoding="async"></span>{{else}}<span class="hb-card-cover" aria-hidden="true"><span class="hb-cover-path">{{.CoverPath}}</span><span class="hb-cover-glyph">{{.CoverLine1}}
{{.CoverLine2}}</span><i class="hb-cover-bar"></i></span>{{end}}
<span class="hb-card-body">
<span class="hb-card-meta">{{if .Tag}}<b>{{.Tag}}</b><i></i>{{end}}{{if .PublishedAt}}<time datetime="{{.PublishedISO}}">{{.PublishedAt}}</time>{{end}}</span>
<h2>{{.Title}}</h2>
{{if .Summary}}<p>{{.Summary}}</p>{{end}}
<span class="hb-card-foot">{{if .ReadingTime}}<span>{{.ReadingTime}}</span>{{end}}<i aria-hidden="true">&#8594;</i></span>
</span>
</a>
{{end}}</div>{{else}}` + emptyStateStr + `{{end}}
</div></main>
`

const hopBaseVars = `
@font-face{font-family:"IBM Plex Sans";src:url("/assets/fonts/ibm-plex-sans-latin.woff2") format("woff2");font-style:normal;font-weight:400 700;font-display:swap}
@font-face{font-family:"IBM Plex Mono";src:url("/assets/fonts/ibm-plex-mono-500-latin.woff2") format("woff2");font-style:normal;font-weight:500;font-display:swap}
:root{color-scheme:light;--bg:#f2f2f0;--fg:#0b0d0c;--muted:#62685f;--border:#c8c8c2;--card:#fbfbfa;--accent:#e2600f;--accent-fg:#fff;--accent-soft:#fdf0e6;--code-bg:#14150f;--code-fg:#d7dbd3;--maxw:760px;--mono:"IBM Plex Mono","SFMono-Regular",Menlo,Consolas,monospace;--shadow:0 14px 32px rgba(11,13,12,.11);--hb-canvas:#f2f2f0;--hb-paper:#fbfbfa;--hb-paper-2:#f4f4f2;--hb-hair:#e0e0dc;--hb-rule:#c8c8c2;--hb-ink:#0b0d0c;--hb-body:#4a4f49;--hb-muted:#62685f;--hb-accent-ink:#b94d08;--hb-terminal:#14150f;--hb-terminal-text:#d7dbd3;--hb-terminal-muted:#92998f;--hb-terminal-accent:#f28c3a;--hb-navw:1400px;--hb-ledgerw:1240px;--hb-ease:cubic-bezier(.16,1,.3,1)}
`

const hopBaseCSS = `
*{letter-spacing:0}
html{scroll-behavior:smooth;scroll-padding-top:78px;background:var(--hb-canvas)}
body{min-width:320px;overflow-x:hidden;background:var(--hb-canvas);color:var(--hb-ink);font-family:"IBM Plex Sans","Noto Sans SC","PingFang SC","Microsoft YaHei",sans-serif;line-height:1.65}
body,h1,h2,h3,h4,p{margin-top:0}
a{color:inherit}
a:hover{text-decoration:none}
:where(a,button):focus-visible{outline:2px solid var(--accent);outline-offset:3px}
.hb-skip{position:fixed;z-index:80;top:8px;left:8px;padding:10px 14px;border:1px solid var(--hb-rule);border-radius:4px;background:var(--hb-paper);color:var(--hb-ink);transform:translateY(-160%)}
.hb-skip:focus{transform:none}
.hb-announcement{position:fixed;z-index:40;inset:0 0 auto;height:34px;padding:0 16px;display:flex;align-items:center;justify-content:center;gap:14px;background:var(--hb-terminal);color:#f0f0ea;font:400 11px/1 var(--mono)}
.hb-announcement-text{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.hb-announcement a{display:inline-flex;align-items:center;gap:5px;color:#ff9b4f;font-weight:500;white-space:nowrap}.hb-announcement a:hover{color:#ffb37a}
.hb-announcement-badge{position:relative;flex:none;display:inline-flex;align-items:center;height:20px;padding:0 11px;border-radius:999px;overflow:hidden;background:linear-gradient(135deg,#ff9440 0%,#e2600f 55%,#c9540b 100%);box-shadow:0 2px 10px rgba(226,96,15,.5),inset 0 1px 0 rgba(255,255,255,.28);color:#fff;font:700 11px/1 var(--mono);letter-spacing:.1em}
.hb-announcement-badge::after{content:"";position:absolute;top:-4px;bottom:-4px;left:-70%;width:42%;background:linear-gradient(105deg,transparent,rgba(255,255,255,.6),transparent);transform:skewX(-20deg);animation:hbRailShine 3.2s cubic-bezier(.4,0,.2,1) infinite}
@keyframes hbRailShine{0%,58%{left:-70%}88%,100%{left:140%}}
@media(prefers-reduced-motion:reduce){.hb-announcement-badge::after{animation:none}}
.hb-header{position:fixed;z-index:30;top:0;right:0;left:0;height:70px;border-bottom:1px solid transparent;background:rgba(242,242,240,.88);-webkit-backdrop-filter:saturate(160%) blur(14px);backdrop-filter:saturate(160%) blur(14px);transition:border-color 300ms var(--hb-ease),background 300ms var(--hb-ease)}
.hb-announcement+.hb-header{top:34px}.hb-header.scrolled{border-bottom-color:var(--hb-hair);background:rgba(242,242,240,.97)}
.hb-header+main{margin-top:70px;scroll-margin-top:78px}.hb-announcement+.hb-header+main{margin-top:104px;scroll-margin-top:112px}
.hb-header+main .article-content h2,.hb-header+main .article-content h3{scroll-margin-top:78px}.hb-announcement+.hb-header+main .article-content h2,.hb-announcement+.hb-header+main .article-content h3{scroll-margin-top:112px}
.hb-header-inner{width:min(100%,var(--hb-navw));height:70px;margin:0 auto;padding:0 32px;display:flex;align-items:center;justify-content:space-between;gap:32px}
.hb-brand{min-height:44px;display:inline-flex;align-items:center;gap:9px;color:var(--hb-ink);text-decoration:none;white-space:nowrap}
.hb-brand img{width:26px;height:26px;display:block;object-fit:contain}
.hb-brand b{font-size:16px;font-weight:600;letter-spacing:.055em;text-transform:uppercase}
.hb-nav{display:flex;align-items:center;justify-content:center;gap:30px;margin-left:auto}
.hb-nav a{min-height:44px;display:inline-flex;align-items:center;color:var(--hb-body);font-family:var(--mono);font-size:12px;font-weight:500;letter-spacing:.08em;text-transform:uppercase;white-space:nowrap}
.hb-nav a:hover,.hb-nav a.act{color:var(--hb-ink)}
.hb-actions{display:flex;align-items:center;gap:10px}
.hb-control,.hb-login,.hb-user,.hb-menu-button{box-sizing:border-box;height:32px;min-height:32px;border:1px solid var(--hb-rule);border-radius:4px;background:transparent}
.hb-language{position:relative}
.hb-language-trigger{min-width:88px;padding:0 12px;display:inline-flex;align-items:center;justify-content:space-between;gap:10px;color:var(--hb-ink);font-size:12px;font-weight:500;cursor:pointer}
.hb-language-trigger i{width:7px;height:7px;border-right:1.5px solid currentColor;border-bottom:1.5px solid currentColor;transform:translateY(-2px) rotate(45deg);transition:transform 120ms var(--hb-ease)}
.hb-language.open .hb-language-trigger i{transform:translateY(2px) rotate(225deg)}
.hb-language-menu{position:absolute;z-index:40;top:calc(100% + 6px);right:0;width:176px;padding:4px;border:1px solid var(--hb-rule);border-radius:6px;background:var(--hb-paper);box-shadow:var(--shadow)}
.hb-language-menu[hidden]{display:none}
.hb-language-menu a{min-height:44px;padding:0 10px;display:flex;align-items:center;justify-content:space-between;border-radius:4px;color:var(--hb-body);font-size:13px}
.hb-language-menu a:hover,.hb-language-menu a.act{background:var(--hb-paper-2);color:var(--hb-ink)}
.hb-language-menu i{color:var(--hb-accent-ink);font-style:normal;opacity:0}
.hb-language-menu a.act i{opacity:1}
.hb-login{padding:0 16px;display:inline-flex;align-items:center;justify-content:center;border-color:var(--hb-ink);background:var(--hb-ink);color:#fff;font-size:13px;font-weight:600}
.hb-login:hover{background:#292c28;color:#fff}
.hb-auth,.hb-auth-guest{display:inline-flex;align-items:center}.hb-auth{min-width:72px;height:32px;justify-content:flex-end}
.hb-auth-guest[hidden],.hb-user[hidden],[data-blog-acquisition][hidden]{display:none!important}
.hb-auth[data-auth-state=loading] .hb-auth-guest,.hb-auth[data-auth-state=loading] .hb-user,.hb-auth[data-auth-state=guest] .hb-user,.hb-auth[data-auth-state=authenticated] .hb-auth-guest{display:none!important}
.hb-user{max-width:176px;padding:3px 11px 3px 3px;display:inline-flex;align-items:center;gap:8px;color:var(--hb-ink)}
.hb-user-avatar{width:24px;height:24px;display:grid;place-items:center;border-radius:3px;background:var(--hb-accent-ink);color:#fff;font:500 11px/1 var(--mono)}
.hb-user-name{overflow:hidden;font-size:13px;font-weight:600;text-overflow:ellipsis;white-space:nowrap}
.hb-menu-button{width:32px;padding:0;display:none;align-items:center;justify-content:center;color:var(--hb-ink);cursor:pointer}
.hb-menu-button>span,.hb-menu-button>span::before,.hb-menu-button>span::after{width:17px;height:1.5px;display:block;content:"";background:currentColor;transition:transform 120ms var(--hb-ease),opacity 120ms linear}
.hb-menu-button>span{position:relative}.hb-menu-button>span::before{position:absolute;top:-5px}.hb-menu-button>span::after{position:absolute;top:5px}
.hb-menu-button[aria-expanded=true]>span{background:transparent}.hb-menu-button[aria-expanded=true]>span::before{transform:translateY(5px) rotate(45deg)}.hb-menu-button[aria-expanded=true]>span::after{transform:translateY(-5px) rotate(-45deg)}
.hb-mobile-menu{position:absolute;top:69px;right:0;left:0;padding:8px max(18px,env(safe-area-inset-right)) 18px max(18px,env(safe-area-inset-left));border-bottom:1px solid var(--hb-rule);background:var(--hb-paper)}
.hb-mobile-menu[hidden]{display:none}.hb-mobile-menu nav{display:grid}.hb-mobile-menu a{min-height:48px;display:flex;align-items:center;border-bottom:1px solid var(--hb-hair);color:var(--hb-body);font-family:var(--mono);font-size:13px}.hb-mobile-menu a.act{color:var(--hb-ink);font-weight:500}
.hb-frame{width:min(100%,var(--hb-ledgerw));margin:0 auto;padding-right:32px;padding-left:32px}
.hb-sr-only{position:absolute;width:1px;height:1px;margin:-1px;padding:0;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap;border:0}
.hb-ledger{padding-top:44px;padding-bottom:88px}
.hb-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:32px 36px}
.hb-card{display:flex;flex-direction:column;overflow:hidden;border:1px solid var(--hb-rule);border-radius:8px;background:var(--hb-paper);color:inherit;text-decoration:none;transition:border-color 160ms var(--hb-ease),box-shadow 160ms var(--hb-ease),transform 160ms var(--hb-ease)}
.hb-card:hover{border-color:#a3a39c;box-shadow:var(--shadow);transform:translateY(-2px)}
.hb-card-cover{position:relative;display:block;aspect-ratio:16/9;overflow:hidden;background:var(--hb-terminal)}
.hb-card-cover:not(.hb-cover-real)::before{position:absolute;inset:0;content:"";background-image:linear-gradient(rgba(215,219,211,.05) 1px,transparent 1px),linear-gradient(90deg,rgba(215,219,211,.05) 1px,transparent 1px);background-size:44px 44px}
.hb-cover-real img{width:100%;height:100%;display:block;object-fit:cover;transition:transform 400ms var(--hb-ease)}
.hb-card:hover .hb-cover-real img{transform:scale(1.03)}
.hb-cover-path{position:absolute;top:18px;right:22px;left:22px;overflow:hidden;color:var(--hb-terminal-muted);font:500 11px/1 var(--mono);text-overflow:ellipsis;white-space:nowrap}
.hb-cover-glyph{position:absolute;left:22px;bottom:18px;max-width:calc(100% - 44px);overflow:hidden;color:var(--hb-terminal-text);font:500 15px/1.5 var(--mono);white-space:pre-line}
.hb-cover-bar{position:absolute;right:22px;bottom:22px;width:56px;height:2px;background:var(--hb-terminal-accent);transition:width 200ms var(--hb-ease)}
.hb-card:hover .hb-cover-bar{width:76px}
.hb-card-body{min-width:0;padding:18px 24px 16px;display:flex;flex-direction:column;flex:1}
.hb-card-meta{margin-bottom:10px;display:flex;align-items:center;gap:12px;color:var(--hb-muted);font-family:var(--mono);font-size:11px;font-variant-numeric:tabular-nums}
.hb-card-meta b{color:var(--hb-accent-ink);font-weight:500}
.hb-card-meta i{width:12px;height:1px;background:var(--hb-rule)}
.hb-card h2{margin:0 0 8px;color:var(--hb-ink);font-size:20px;font-weight:600;line-height:1.32;overflow-wrap:anywhere;transition:color 120ms var(--hb-ease)}
.hb-card-body>p{margin:0 0 18px;color:var(--hb-body);font-size:13.5px;line-height:1.62;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;overflow:hidden}
.hb-card-foot{margin-top:auto;padding-top:12px;display:flex;align-items:center;justify-content:space-between;border-top:1px solid var(--hb-hair);color:var(--hb-muted);font-family:var(--mono);font-size:11px}
.hb-card-foot i{color:var(--hb-ink);font-size:16px;font-style:normal;transition:color 120ms var(--hb-ease),transform 120ms var(--hb-ease)}
.hb-card:hover h2{color:var(--hb-accent-ink)}.hb-card:hover .hb-card-foot i{color:var(--hb-accent-ink);transform:translateX(2px)}
.blog-empty{min-height:280px;padding:72px 20px;display:flex;flex-direction:column;align-items:center;justify-content:center;border-bottom:1px solid var(--hb-rule);color:var(--hb-muted);text-align:center}.blog-empty svg{width:36px;height:36px;margin-bottom:16px}.blog-empty-title{margin:0 0 6px;color:var(--hb-ink);font-size:18px;font-weight:600}.blog-empty-sub{margin:0;color:var(--hb-muted);font-size:14px}
.blog-wrap{width:min(100%,var(--maxw));margin:0 auto;padding:64px 24px 96px}
.blog-back{min-height:44px;margin:0 0 24px;display:inline-flex;align-items:center;color:var(--hb-muted);font-family:var(--mono);font-size:12px}.blog-back:hover{color:var(--hb-accent-ink)}
.article-title{margin:0 0 26px;color:var(--hb-ink);font-size:48px;font-weight:600;line-height:1.12;text-wrap:balance}
.article-byline{margin:0 0 36px;padding:14px 0;display:flex;flex-wrap:wrap;align-items:center;gap:10px;border-top:1px solid var(--hb-rule);border-bottom:1px solid var(--hb-hair);color:var(--hb-muted);font-family:var(--mono);font-size:11px}.article-avatar{display:none}.article-byline .dot{width:12px;height:1px;border-radius:0;background:var(--hb-rule);opacity:1}
.article-cover{width:100%;margin:0 0 48px;display:block;border:1px solid var(--hb-rule);border-radius:6px;box-shadow:none}
.article-content{color:var(--hb-body);font-size:18px;line-height:1.82;overflow-wrap:anywhere}.article-content>*:first-child{margin-top:0}.article-content p{margin:0 0 1.35em}.article-content>p:first-child{color:var(--hb-ink);font-size:19px;line-height:1.75}.article-content h2,.article-content h3,.article-content h4{color:var(--hb-ink);font-weight:600}.article-content h2{margin:2.1em 0 .72em;font-size:28px;line-height:1.25}.article-content h3{margin:1.8em 0 .6em;font-size:22px}.article-content h4{margin:1.5em 0 .5em;font-size:18px}.article-content a{color:var(--hb-accent-ink);text-decoration:underline;text-decoration-thickness:1px;text-underline-offset:3px}.article-content strong{color:var(--hb-ink);font-weight:600}.article-content ul,.article-content ol{margin:0 0 1.35em;padding-left:1.5em}.article-content li{margin:.45em 0}.article-content li::marker{color:var(--accent)}.article-content :not(pre)>code{padding:.12em .38em;border-radius:4px;background:var(--accent-soft);color:var(--hb-accent-ink);font-family:var(--mono);font-size:.84em}.article-content pre{margin:1.8em 0;padding:20px 22px;overflow-x:auto;border:1px solid #2b2e27;border-radius:6px;background:var(--code-bg);color:var(--code-fg);box-shadow:none;line-height:1.7}.article-content pre code{padding:0;background:none;color:inherit;font-family:var(--mono);font-size:.84em}.article-content blockquote{margin:2em 0;padding:4px 0 4px 22px;border-left:3px solid var(--accent);color:var(--hb-ink);font-size:20px;line-height:1.55}.article-content blockquote p{margin:0}.article-content hr{height:1px;margin:3em 0;border:0;background:var(--hb-rule)}.article-content img{height:auto;display:block;margin:2.2em auto;border:1px solid var(--hb-rule);border-radius:6px;box-shadow:none}.article-content figcaption{margin-top:10px;color:var(--hb-muted);font-size:13px;text-align:center}.article-content iframe{width:100%;max-width:100%;aspect-ratio:16/9;margin:2em 0;border:0;border-radius:6px}.article-content table{width:100%;margin:1.8em 0;display:block;overflow-x:auto;border-collapse:collapse;font-size:15px}.article-content thead th{padding:0 14px 10px;border-bottom:1px solid var(--hb-rule);color:var(--hb-muted);font-family:var(--mono);font-size:11px;font-weight:500;text-align:left;text-transform:uppercase}.article-content tbody td{padding:11px 14px;border-bottom:1px solid var(--hb-hair)}
.article-tags{margin-top:44px;padding-top:16px;display:flex;flex-wrap:wrap;gap:0;border-top:1px solid var(--hb-rule)}.article-tag{padding:4px 12px 4px 0;border:0;border-radius:0;color:var(--hb-muted);font-family:var(--mono);font-size:11px}.article-tag+.article-tag::before{margin-right:12px;content:"/";color:var(--hb-rule)}
.blog-cta{margin:64px 0 0;padding:28px 30px;border:1px solid #2b2e27;border-radius:6px;background:var(--hb-terminal);color:var(--hb-terminal-text);text-align:left}.blog-cta-title{margin:0 0 8px;color:var(--hb-terminal-text);font-size:22px;font-weight:600}.blog-cta-desc{max-width:58ch;margin:0 0 20px;color:var(--hb-terminal-muted);font-size:14px;line-height:1.7}.blog-cta-btn,.blog-gate-btn{min-height:48px;padding:0 20px;display:inline-flex;align-items:center;justify-content:center;border-radius:4px;background:var(--hb-accent-ink);color:#fff;font-size:14px;font-weight:600}.blog-cta-btn:hover,.blog-gate-btn:hover{background:#8f3c06;color:#fff}
.blog-gate{position:fixed;z-index:45;inset:0;padding:20px;display:flex;align-items:flex-end;justify-content:center;background:rgba(242,242,240,.86);-webkit-backdrop-filter:blur(4px);backdrop-filter:blur(4px);pointer-events:none}.blog-gate-card{width:min(100%,420px);margin-bottom:max(20px,env(safe-area-inset-bottom));padding:24px;border:1px solid var(--hb-rule);border-radius:6px;background:#fff;box-shadow:var(--shadow);pointer-events:auto;text-align:left}.blog-gate-title{margin:0 0 8px;color:var(--hb-ink);font-size:20px;font-weight:600}.blog-gate-desc{margin:0 0 18px;color:var(--hb-body);font-size:14px;line-height:1.65}
.blog-wrap>.blog-empty{min-height:0;padding:0 0 36px;display:block;border:0;color:var(--hb-body);text-align:left}
.article-title,.article-content h2,.article-content h3,.article-content h4,.article-content thead th,.blog-cta-title{letter-spacing:0}
/* 页脚:主站落地页 <footer class="ft"> 的同构移植(改主站页脚时同步)。 */
.ft{background:#14150f;color:#9aa093;padding:56px 0 36px;border-top:1px solid #2c2f28}
.ft a{text-decoration:none}
.ft-in{width:min(100%,var(--hb-navw));margin:0 auto;padding:0 32px}
.ft-top{display:grid;grid-template-columns:minmax(280px,1.4fr) repeat(4,minmax(120px,1fr));gap:32px 48px;margin-bottom:52px}
.ft-col h4{font:500 10px/1.2 var(--mono);letter-spacing:.14em;text-transform:uppercase;color:#6e7468;margin:0 0 14px}
.ft-col a{display:block;padding:2px 0;font:400 12.5px/1.6 var(--mono);color:#9aa093;transition:color .15s}
.ft-col a:hover{color:#e2600f}
.ft-col a.ft-link-featured,.ft-model-col a.ft-model-featured{position:relative;padding-left:12px;color:#d9ddd2;font-weight:500}
.ft-model-col a.ft-model-featured::before,.ft-col a.ft-link-featured::before{content:"";position:absolute;left:0;top:.72em;width:4px;height:4px;border-radius:50%;background:#e2600f}
.ft-brand{display:flex;align-items:center;gap:9px}
.ft-brand img{width:22px;height:22px;display:block;object-fit:contain}
.ft-brand span{font-size:14px;font-weight:600;letter-spacing:.055em;text-transform:uppercase;color:#e6e8df}
.ft-desc{font-size:13px;line-height:1.7;color:#9aa093;margin:14px 0 0;max-width:34ch}
.ft-taxonomy{display:grid;grid-template-columns:minmax(0,.9fr) minmax(0,1fr) minmax(0,.95fr) minmax(0,1.35fr) minmax(0,.95fr) minmax(0,1fr);gap:20px;min-width:0;padding-bottom:30px;margin-bottom:22px;border-bottom:1px solid #2c2f28}
.ft-model-col{min-width:0}
.ft-model-col a{padding:2px 0;font-size:11.5px;line-height:1.45;overflow-wrap:anywhere}
@media(min-width:768px){.ft-model-col a.ft-model-featured{white-space:nowrap}}
.ft-col--partners .ft-partner-mark{display:block;margin:2px 0 6px;padding:0}
.ft-col--partners .ft-partner-mark img{width:auto;height:15px;filter:brightness(0) invert(1);opacity:.68;transition:opacity .15s}
.ft-col--partners .ft-partner-mark:hover img{opacity:1}
.ft-col--partners .ft-partner-mark--oumomo img{height:17px}
.ft-col--partners .ft-partner-mark--fastmoss img{height:18px}
.ft-col--partners .ft-partner-mark--text{font:700 17px/1.2 "IBM Plex Sans","Noto Sans SC","PingFang SC",sans-serif;letter-spacing:.01em;color:#fff;opacity:.68;white-space:nowrap;transition:opacity .15s}
.ft-col--partners .ft-partner-mark--cjk{font-size:13px}
.ft-col--partners .ft-partner-mark--text:hover{opacity:1;color:#fff}
@media(max-width:1080px){.hb-nav{gap:18px}.hb-card-body{padding-right:20px;padding-left:20px}}
@media(max-width:900px){.hb-header-inner{padding-right:20px;padding-left:20px}.hb-nav{display:none}.hb-actions{gap:0}.hb-language,.hb-auth{min-height:48px;display:inline-flex;align-items:center}.hb-language-trigger,.hb-login,.hb-user{position:relative}.hb-language-trigger::before,.hb-login::before,.hb-user::before{position:absolute;inset:-8px 0;content:""}.hb-menu-button{width:48px;height:48px;min-height:48px;display:inline-flex;border-color:transparent}.hb-grid{grid-template-columns:1fr;gap:24px}.ft-top{grid-template-columns:1fr 1fr}.ft-top .ft-col--brand{grid-column:1/-1}.ft-taxonomy{grid-template-columns:repeat(3,minmax(0,1fr));gap:36px 28px}}
@media(max-width:640px){.hb-header-inner{padding-right:max(14px,env(safe-area-inset-right));padding-left:max(14px,env(safe-area-inset-left));gap:4px}.hb-brand{gap:6px}.hb-brand b{font-size:14px}.hb-language-trigger{min-width:68px;padding:0 9px}.hb-login{padding:0 12px;font-size:12px}.hb-user{width:32px;padding:3px}.hb-user-name{display:none}.hb-frame{padding-right:18px;padding-left:18px}.hb-ledger{padding-top:24px;padding-bottom:56px}.hb-grid{gap:18px}.hb-cover-path{top:14px;right:16px;left:16px}.hb-cover-glyph{left:16px;bottom:14px;max-width:calc(100% - 32px);font-size:13px}.hb-cover-bar{right:16px;bottom:18px}.hb-card-body{padding:16px 18px 14px}.hb-card h2{font-size:18px}.blog-wrap{padding:40px 18px 72px}.article-title{font-size:34px;line-height:1.16}.article-byline{margin-bottom:28px}.article-cover{margin-bottom:36px}.article-content{font-size:17px}.article-content>p:first-child{font-size:18px}.article-content pre{margin-right:-8px;margin-left:-8px;padding:17px 16px}.article-content pre code{white-space:pre-wrap;overflow-wrap:anywhere}.blog-cta{margin-top:48px;padding:24px 20px}.ft-in{padding:0 18px}.ft-top{gap:28px 24px}.ft-taxonomy{grid-template-columns:repeat(2,minmax(0,1fr));gap:24px 20px}}
@media(max-width:370px){.hb-brand b{display:none}.hb-language-trigger{min-width:62px}.hb-login{padding:0 10px}}
@media(prefers-reduced-motion:reduce){html{scroll-behavior:auto}.hb-header,.hb-language-trigger i,.hb-menu-button>span,.hb-menu-button>span::before,.hb-menu-button>span::after,.hb-card,.hb-card h2,.hb-card-foot i,.hb-cover-bar,.hb-cover-real img{transition:none!important}.hb-card:hover{transform:none}.hb-card:hover .hb-card-foot i{transform:none}.hb-card:hover .hb-cover-bar{width:56px}.hb-card:hover .hb-cover-real img{transform:none}}
`

const hopBaseChromeScriptStr = `<script>
(function(){
'use strict';
var header=document.querySelector('[data-hb-header]');
if(!header)return;
var mobileButton=header.querySelector('[data-hb-menu-button]');
var mobileMenu=header.querySelector('[data-hb-mobile-menu]');
var language=header.querySelector('[data-hb-language]');
var languageButton=language?language.querySelector('.hb-language-trigger'):null;
var languageMenu=language?language.querySelector('.hb-language-menu'):null;
function syncHeader(){header.classList.toggle('scrolled',window.scrollY>16);}
syncHeader();window.addEventListener('scroll',syncHeader,{passive:true});
function persistLandingLang(blogLang){
if(!blogLang)return;
try{
var landingLang=blogLang==='zh-Hant'?'zh-HK':blogLang;
localStorage.setItem('lang',landingLang);
var secure=location.protocol==='https:'?'; secure':'';
var domain=location.hostname==='hop-base.com'||location.hostname.endsWith('.hop-base.com')?'; domain=.hop-base.com':'';
document.cookie='lang='+encodeURIComponent(landingLang)+'; path=/; max-age=31536000; samesite=lax'+secure+domain;
}catch(e){}
}
function closeLanguage(restore){
if(!languageButton||!languageMenu)return;
language.classList.remove('open');languageButton.setAttribute('aria-expanded','false');languageMenu.hidden=true;
if(restore&&languageMenu.contains(document.activeElement))languageButton.focus();
}
function setMobile(open){
if(!mobileButton||!mobileMenu)return;
mobileButton.setAttribute('aria-expanded',String(open));mobileMenu.hidden=!open;document.body.classList.toggle('hb-menu-open',open);
if(open){closeLanguage(false);var first=mobileMenu.querySelector('a');if(first)first.focus();}
else if(mobileMenu.contains(document.activeElement))mobileButton.focus();
}
if(languageButton&&languageMenu){
languageButton.addEventListener('click',function(event){event.stopPropagation();var open=languageMenu.hidden;closeLanguage(false);if(open){language.classList.add('open');languageButton.setAttribute('aria-expanded','true');languageMenu.hidden=false;}});
languageMenu.addEventListener('click',function(event){
var link=event.target.closest('a');if(!link)return;
try{
var blogLang=new URL(link.href,location.href).searchParams.get('lang')||'en';
persistLandingLang(blogLang);
}catch(e){}
});
var activeLanguage=languageMenu?languageMenu.querySelector('a.act'):null;
try{
var initialBlogLang=new URL(location.href).searchParams.get('lang');
if(!initialBlogLang&&activeLanguage)initialBlogLang=new URL(activeLanguage.href,location.href).searchParams.get('lang');
persistLandingLang(initialBlogLang);
}catch(e){}
}
if(mobileButton&&mobileMenu){
mobileButton.addEventListener('click',function(event){event.stopPropagation();setMobile(mobileMenu.hidden);});
mobileMenu.addEventListener('click',function(event){if(event.target.closest('a'))setMobile(false);});
}
document.addEventListener('click',function(event){if(language&&!language.contains(event.target))closeLanguage(false);if(mobileMenu&&!mobileMenu.hidden&&!header.contains(event.target))setMobile(false);});
document.addEventListener('keydown',function(event){if(event.key!=='Escape')return;if(languageMenu&&!languageMenu.hidden){closeLanguage(true);return;}if(mobileMenu&&!mobileMenu.hidden)setMobile(false);});
window.addEventListener('resize',function(){if(window.innerWidth>900&&mobileMenu&&!mobileMenu.hidden)setMobile(false);});
})();
</script>`

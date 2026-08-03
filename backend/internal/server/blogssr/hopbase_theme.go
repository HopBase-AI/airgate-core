package blogssr

// HopBase uses a dedicated paper-terminal theme. It deliberately does not
// replace ember because ember remains the Open Late editorial theme.

const hopBaseHeaderStr = `<a class="hb-skip" href="#main-content">Skip to content</a>
{{if .AnnouncementEnabled}}<div class="hb-announcement" role="status"><span class="hb-announcement-text">{{.AnnouncementText}}</span><a href="{{.AnnouncementHref}}"><span>{{.AnnouncementLink}}</span><span aria-hidden="true">&#8594;</span></a></div>{{end}}
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

const hopBaseFooterStr = `<footer class="hb-footer"><div class="hb-footer-inner">
<div class="hb-footer-brand"><a href="{{.SiteURL}}" class="hb-brand" aria-label="{{.BrandLabel}} home">{{if .LogoURL}}<img src="{{.LogoSrc}}" alt="">{{end}}<b>HopBase</b></a>{{if .FooterNote}}<p>{{.FooterNote}}</p>{{end}}</div>
<nav aria-label="Footer navigation">{{range .Nav}}<a href="{{.Href}}">{{.Label}}</a>{{end}}</nav>
<p class="hb-copyright">&copy; HopBase</p>
</div></footer>
`

const hopBaseListBodyStr = `<main class="hb-main" id="main-content">
<section class="hb-intro"><div class="hb-frame">
<p class="hb-micro">{{.Eyebrow}}</p>
<div class="hb-intro-copy"><h1>{{.Heading}}</h1>{{if .Subtitle}}<p>{{.Subtitle}}</p>{{end}}</div>
</div></section>
<div class="hb-frame hb-ledger">
{{if .Featured}}{{with .Featured}}
<a class="hb-featured" href="{{.URL}}">
<span class="hb-featured-meta">{{if .PublishedAt}}<time datetime="{{.PublishedISO}}">{{.PublishedAt}}</time>{{end}}{{if .ReadingTime}}<span>{{.ReadingTime}}</span>{{end}}</span>
<span class="hb-featured-copy">{{if .Tag}}<span class="hb-tag">{{.Tag}}</span>{{end}}<h2>{{.Title}}</h2>{{if .Summary}}<p>{{.Summary}}</p>{{end}}<span class="hb-read-arrow" aria-hidden="true">&#8599;</span></span>
{{if .CoverImage}}<img class="hb-featured-cover" src="{{.CoverImage}}" alt="" loading="eager">{{else}}<span class="hb-terminal-cover">{{if $.LogoURL}}<img src="{{$.LogoSrc}}" alt="">{{end}}<span>hopbase / article.log</span><i aria-hidden="true"></i></span>{{end}}
</a>
{{end}}{{end}}
{{if .Rest}}<div class="hb-rows">{{range .Rest}}
<a class="hb-row" href="{{.URL}}">
<span class="hb-row-date">{{if .PublishedAt}}<time datetime="{{.PublishedISO}}">{{.PublishedAt}}</time>{{end}}</span>
<span class="hb-row-copy"><h2>{{.Title}}</h2>{{if .Summary}}<p>{{.Summary}}</p>{{end}}</span>
<span class="hb-row-side">{{if .Tag}}<span>{{.Tag}}</span>{{end}}{{if .ReadingTime}}<span>{{.ReadingTime}}</span>{{end}}<i aria-hidden="true">&#8594;</i></span>
</a>
{{end}}</div>{{end}}
{{if not .Posts}}` + emptyStateStr + `{{end}}
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
.hb-intro{position:relative;border-bottom:1px solid var(--hb-hair);overflow:hidden;background-image:radial-gradient(circle,rgba(11,13,12,.1) .5px,transparent .5px),linear-gradient(rgba(11,13,12,.03) 1px,transparent 1px),linear-gradient(90deg,rgba(11,13,12,.03) 1px,transparent 1px);background-size:5px 5px,120px 120px,120px 120px}
.hb-intro .hb-frame{min-height:220px;padding-top:48px;padding-bottom:48px;display:grid;grid-template-columns:128px minmax(0,1fr);align-items:end}
.hb-micro{align-self:start;margin:4px 0 0;color:var(--hb-muted);font-family:var(--mono);font-size:11px;font-weight:500;text-transform:uppercase}
.hb-intro-copy{max-width:760px}
.hb-intro h1{margin:0 0 12px;font-size:44px;font-weight:600;line-height:1.08;text-wrap:balance;overflow-wrap:anywhere}
.hb-intro-copy>p{max-width:68ch;margin:0;color:var(--hb-body);font-size:16px;line-height:1.65}
.hb-ledger{padding-bottom:80px}
.hb-featured{min-height:280px;display:grid;grid-template-columns:128px minmax(0,1fr) minmax(280px,35%);border-bottom:1px solid var(--hb-rule);color:inherit;text-decoration:none}
.hb-featured-meta{padding:24px 20px 24px 0;display:flex;flex-direction:column;gap:10px;border-right:1px solid var(--hb-hair);color:var(--hb-muted);font-family:var(--mono);font-size:11px;font-variant-numeric:tabular-nums}
.hb-featured-copy{position:relative;min-width:0;padding:32px 40px;display:flex;flex-direction:column;align-items:flex-start;justify-content:center;border-right:1px solid var(--hb-hair)}
.hb-tag{margin-bottom:12px;color:var(--hb-accent-ink);font-family:var(--mono);font-size:11px;font-weight:500}
.hb-featured h2{max-width:760px;margin:0 0 14px;color:var(--hb-ink);font-size:31px;font-weight:600;line-height:1.2;text-wrap:balance;overflow-wrap:anywhere;transition:color 120ms var(--hb-ease)}
.hb-featured-copy>p{max-width:64ch;margin:0;color:var(--hb-body);font-size:15px;line-height:1.65;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;overflow:hidden}
.hb-read-arrow{position:absolute;right:22px;bottom:20px;color:var(--hb-ink);font-size:22px;transition:color 120ms var(--hb-ease),transform 120ms var(--hb-ease)}
.hb-featured:hover h2{color:var(--hb-accent-ink)}.hb-featured:hover .hb-read-arrow{color:var(--hb-accent-ink);transform:translate(4px,-4px)}
.hb-featured-cover{width:100%;height:100%;min-height:0;display:block;object-fit:cover;background:var(--hb-paper-2)}
.hb-terminal-cover{position:relative;min-height:280px;padding:24px;display:flex;flex-direction:column;align-items:flex-start;justify-content:space-between;overflow:hidden;background:var(--hb-terminal);color:var(--hb-terminal-muted);font:500 11px/1.4 var(--mono)}
.hb-terminal-cover::before{position:absolute;inset:0;content:"";background-image:linear-gradient(rgba(215,219,211,.05) 1px,transparent 1px),linear-gradient(90deg,rgba(215,219,211,.05) 1px,transparent 1px);background-size:48px 48px}
.hb-terminal-cover img,.hb-terminal-cover span,.hb-terminal-cover i{position:relative;z-index:1}.hb-terminal-cover img{width:48px;height:48px}.hb-terminal-cover i{width:72%;height:2px;background:var(--hb-terminal-accent)}
.hb-rows{border-bottom:1px solid var(--hb-rule)}
.hb-row{min-height:112px;display:grid;grid-template-columns:112px minmax(0,1fr) 156px;align-items:center;border-bottom:1px solid var(--hb-hair);color:inherit;text-decoration:none}
.hb-row:last-child{border-bottom:0}.hb-row-date{align-self:stretch;padding:24px 20px 24px 0;border-right:1px solid var(--hb-hair);color:var(--hb-muted);font-family:var(--mono);font-size:11px;font-variant-numeric:tabular-nums}.hb-row-copy{min-width:0;padding:22px 36px}.hb-row h2{margin:0 0 6px;color:var(--hb-ink);font-size:22px;font-weight:600;line-height:1.3;overflow-wrap:anywhere;transition:color 120ms var(--hb-ease)}.hb-row-copy p{max-width:68ch;margin:0;color:var(--hb-body);font-size:14px;line-height:1.6;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;overflow:hidden}.hb-row-side{align-self:stretch;padding:22px 0 22px 20px;display:flex;flex-direction:column;justify-content:center;gap:6px;border-left:1px solid var(--hb-hair);color:var(--hb-muted);font-family:var(--mono);font-size:11px;overflow-wrap:anywhere}.hb-row-side i{margin-top:2px;color:var(--hb-ink);font-size:18px;font-style:normal;transition:color 120ms var(--hb-ease),transform 120ms var(--hb-ease)}.hb-row:hover h2,.hb-row:hover .hb-row-side i{color:var(--hb-accent-ink)}.hb-row:hover .hb-row-side i{transform:translateX(4px)}
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
.hb-footer{border-top:1px solid var(--hb-rule);background:var(--hb-paper)}.hb-footer-inner{width:min(100%,var(--hb-navw));margin:0 auto;padding:44px 32px;display:grid;grid-template-columns:minmax(220px,1fr) auto;gap:32px}.hb-footer-brand p{max-width:440px;margin:10px 0 0;color:var(--hb-muted);font-size:13px}.hb-footer nav{display:flex;flex-wrap:wrap;align-items:flex-start;justify-content:flex-end;gap:20px}.hb-footer nav a{min-height:44px;display:inline-flex;align-items:center;color:var(--hb-body);font-family:var(--mono);font-size:11px}.hb-footer nav a:hover{color:var(--hb-accent-ink)}.hb-copyright{grid-column:1/-1;margin:0;padding-top:20px;border-top:1px solid var(--hb-hair);color:var(--hb-muted);font-family:var(--mono);font-size:11px}
@media(max-width:1080px){.hb-nav{gap:18px}.hb-featured{grid-template-columns:112px minmax(0,1fr) 34%}.hb-featured-copy{padding-right:34px;padding-left:34px}.hb-row-copy{padding-right:30px;padding-left:30px}}
@media(max-width:900px){.hb-header-inner{padding-right:20px;padding-left:20px}.hb-nav{display:none}.hb-actions{gap:0}.hb-language,.hb-auth{min-height:48px;display:inline-flex;align-items:center}.hb-language-trigger,.hb-login,.hb-user{position:relative}.hb-language-trigger::before,.hb-login::before,.hb-user::before{position:absolute;inset:-8px 0;content:""}.hb-menu-button{width:48px;height:48px;min-height:48px;display:inline-flex;border-color:transparent}.hb-featured{grid-template-columns:112px minmax(0,1fr)}.hb-featured-cover,.hb-terminal-cover{grid-column:2;min-height:240px;border-top:1px solid var(--hb-hair)}.hb-featured-copy{border-right:0}.hb-footer-inner{grid-template-columns:1fr}.hb-footer nav{justify-content:flex-start}.hb-copyright{grid-column:1}}
@media(max-width:640px){.hb-header-inner{padding-right:max(14px,env(safe-area-inset-right));padding-left:max(14px,env(safe-area-inset-left));gap:4px}.hb-brand{gap:6px}.hb-brand b{font-size:14px}.hb-language-trigger{min-width:68px;padding:0 9px}.hb-login{padding:0 12px;font-size:12px}.hb-user{width:32px;padding:3px}.hb-user-name{display:none}.hb-frame{padding-right:18px;padding-left:18px}.hb-intro .hb-frame{min-height:184px;padding-top:32px;padding-bottom:32px;display:block}.hb-micro{margin-bottom:20px}.hb-intro h1{font-size:32px;line-height:1.12}.hb-intro-copy>p{font-size:15px;line-height:1.6}.hb-ledger{padding-bottom:56px}.hb-featured{min-height:0;grid-template-columns:1fr}.hb-featured-meta{grid-row:1;padding:20px 0 12px;flex-direction:row;border-right:0;border-bottom:1px solid var(--hb-hair)}.hb-featured-copy{grid-row:2;padding:24px 0 44px;border-right:0;border-bottom:1px solid var(--hb-hair)}.hb-featured-cover,.hb-terminal-cover{width:100%;height:auto;min-height:0;aspect-ratio:16/9;grid-column:1;grid-row:3;border-top:0}.hb-featured h2{font-size:28px;line-height:1.22}.hb-read-arrow{right:0;bottom:12px}.hb-row{min-height:112px;padding:22px 0;grid-template-columns:1fr}.hb-row-date{align-self:auto;padding:0 0 10px;border:0}.hb-row-copy{padding:0}.hb-row h2{font-size:21px;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:3;overflow:hidden}.hb-row-side{padding:14px 0 0;flex-direction:row;align-items:center;gap:10px;border:0}.hb-row-side i{margin:0 0 0 auto}.blog-wrap{padding:40px 18px 72px}.article-title{font-size:34px;line-height:1.16}.article-byline{margin-bottom:28px}.article-cover{margin-bottom:36px}.article-content{font-size:17px}.article-content>p:first-child{font-size:18px}.article-content pre{margin-right:-8px;margin-left:-8px;padding:17px 16px}.article-content pre code{white-space:pre-wrap;overflow-wrap:anywhere}.blog-cta{margin-top:48px;padding:24px 20px}.hb-footer-inner{padding:36px 18px}.hb-footer nav{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:0}.hb-footer nav a{border-bottom:1px solid var(--hb-hair)}}
@media(max-width:370px){.hb-brand b{display:none}.hb-language-trigger{min-width:62px}.hb-login{padding:0 10px}}
@media(prefers-reduced-motion:reduce){html{scroll-behavior:auto}.hb-header,.hb-language-trigger i,.hb-menu-button>span,.hb-menu-button>span::before,.hb-menu-button>span::after,.hb-featured h2,.hb-read-arrow,.hb-row h2,.hb-row-side i{transition:none!important}.hb-featured:hover .hb-read-arrow,.hb-row:hover .hb-row-side i{transform:none}}
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

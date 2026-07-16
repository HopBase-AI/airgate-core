package blogssr

// 站点皮肤:博客页穿上落地页的设计语言,让 /blog 不再是「孤儿页」。
// - ember:HopBase 主站同源(暖黑底 #0c0c0b + 品牌橙渐变 + 青色点缀,系统字栈近似 Sora/Manrope);
// - ink  :Essevin ink 站同源(暖米白 #F4F1EA + 衬线标题 + 玫瑰陶土 #B5836F + 发丝线,Georgia/宋体近似 Newsreader)。
// 皮肤 CSS 叠加在 baseStyle 之上:先覆盖 CSS 变量(正文 Prose 排版自动换色),再补 chrome(顶栏/hero/头条/文章流/页脚)样式。
// 两皮肤共用同一套 chrome 标记(sk-* 类),视觉差异全部在 CSS。

const (
	themeEmber = "ember"
	themeInk   = "ink"
)

// ―――――― 皮肤共用 chrome 标记 ――――――

// skinHeaderStr 主站同款顶栏:logo/品牌回主站、导航项、登录(+可选注册)按钮。
const skinHeaderStr = `<header class="sk-nav"><div class="sk-nav-inner">
<a href="{{.SiteURL}}" class="sk-logo">{{if .LogoURL}}<img src="{{.LogoSrc}}" alt="{{.BrandLabel}}">{{end}}<b class="sk-brand">{{.BrandLabel}}</b></a>
<nav class="sk-links">{{range .Nav}}<a href="{{.Href}}"{{if .Active}} class="act"{{end}}>{{.Label}}</a>{{end}}</nav>
<div class="sk-nav-right">
{{if .ShowLangs}}<span class="sk-langs">{{range $i, $l := .LangNav}}{{if $i}}<i>/</i>{{end}}<a href="{{$l.Href}}"{{if $l.Active}} class="act"{{end}}>{{$l.Label}}</a>{{end}}</span>{{end}}
<a class="sk-login" href="{{.RegisterURL}}">{{.LoginLabel}}</a>
{{if .SignupLabel}}<a class="sk-signup" href="{{.RegisterURL}}">{{.SignupLabel}}</a>{{end}}
</div>
</div></header>
`

// skinLangScriptStr 访客语言自动跳转(仅列表页):无显式 ?lang= 且无既往选择时,
// 按浏览器语言一次性 302(客户端 replace)到对应语言列表;显式选择会记入 localStorage。
const skinLangScriptStr = `{{if .ShowLangs}}
<script>
(function(){
try{
var cur={{.Lang}};
var qs=new URLSearchParams(location.search);
if(qs.has('lang')){localStorage.setItem('blog_lang',cur);return;}
var want=localStorage.getItem('blog_lang');
if(!want){
var nl=(navigator.language||'').toLowerCase();
want=nl.indexOf('en')===0?'en':(nl==='zh-tw'||nl==='zh-hk'||nl==='zh-mo'||nl.indexOf('hant')>=0)?'zh-Hant':(nl.indexOf('zh')===0?'zh':'');
}
if(want&&want!==cur){qs.set('lang',want);location.replace(location.pathname+'?'+qs.toString());}
}catch(e){}
})();
</script>
{{end}}
`

// skinFooterStr 主站风页脚:品牌 + 一句话 + 版权,右侧链接组。
const skinFooterStr = `<footer class="sk-footer"><div class="sk-footer-inner">
<div class="sk-footer-brand">
<a href="{{.SiteURL}}" class="sk-logo">{{if .LogoURL}}<img src="{{.LogoSrc}}" alt="{{.BrandLabel}}">{{end}}<b class="sk-brand">{{.BrandLabel}}</b></a>
{{if .FooterNote}}<p class="sk-footer-note">{{.FooterNote}}</p>{{end}}
<p class="sk-footer-copy">© {{.BrandLabel}}</p>
</div>
{{if .FooterNav}}<nav class="sk-footer-links">{{range .FooterNav}}<a href="{{.Href}}">{{.Label}}</a>{{end}}</nav>{{end}}
</div></footer>
`

// emberListBodyStr HopBase 皮列表:hero + 头条大卡 + 卡片网格。
const emberListBodyStr = `<main class="sk-main">
<section class="sk-hero"><div class="sk-wrap">
<p class="sk-eyebrow">{{.Eyebrow}}</p>
<h1 class="sk-title">{{.Heading}}</h1>
{{if .Subtitle}}<p class="sk-sub">{{.Subtitle}}</p>{{end}}
</div></section>
<div class="sk-wrap">
{{if .Featured}}{{with .Featured}}
<a class="sk-featured" href="{{.URL}}">
<div class="sk-featured-body">
{{if .Tag}}<span class="sk-pill">{{.Tag}}</span>{{end}}
<h2 class="sk-featured-title">{{.Title}}</h2>
{{if .Summary}}<p class="sk-featured-sum">{{.Summary}}</p>{{end}}
<p class="sk-meta">{{if .PublishedAt}}<span>{{.PublishedAt}}</span>{{end}}{{if .ReadingTime}}<span class="d"></span><span>{{.ReadingTime}}</span>{{end}}</p>
</div>
{{if .CoverImage}}<img class="sk-cover" src="{{.CoverImage}}" alt="" loading="lazy">{{else}}<div class="sk-cover {{.CoverClass}}"></div>{{end}}
</a>
{{end}}{{end}}
{{if .Rest}}
<div class="sk-grid">
{{range .Rest}}
<a class="sk-card" href="{{.URL}}">
{{if .CoverImage}}<img class="sk-cover" src="{{.CoverImage}}" alt="" loading="lazy">{{else}}<div class="sk-cover {{.CoverClass}}"></div>{{end}}
<div class="sk-card-body">
<h3 class="sk-card-title">{{.Title}}</h3>
{{if .Summary}}<p class="sk-card-sum">{{.Summary}}</p>{{end}}
<p class="sk-meta">{{if .PublishedAt}}<span>{{.PublishedAt}}</span>{{end}}{{if .ReadingTime}}<span class="d"></span><span>{{.ReadingTime}}</span>{{end}}</p>
</div>
</a>
{{end}}
</div>
{{end}}
{{if not .Posts}}
` + emptyStateStr + `{{end}}
</div>
</main>
`

// inkListBodyStr Essevin 皮列表:hero + 编辑式头条 + 发丝线文章流(去卡片化)。
const inkListBodyStr = `<main class="sk-main">
<section class="sk-hero"><div class="sk-wrap">
<p class="sk-eyebrow">{{.Eyebrow}}</p>
<h1 class="sk-title">{{.Heading}}</h1>
{{if .Subtitle}}<p class="sk-sub">{{.Subtitle}}</p>{{end}}
</div></section>
<div class="sk-wrap">
{{if .Featured}}{{with .Featured}}
<a class="sk-featured" href="{{.URL}}">
<div class="sk-featured-body">
{{if .Tag}}<span class="sk-pill">{{.Tag}}</span>{{end}}
<h2 class="sk-featured-title">{{.Title}}</h2>
{{if .Summary}}<p class="sk-featured-sum">{{.Summary}}</p>{{end}}
<p class="sk-meta">{{if .PublishedAt}}<span>{{.PublishedAt}}</span>{{end}}{{if .ReadingTime}}<span class="d"></span><span>{{.ReadingTime}}</span>{{end}}</p>
</div>
{{if .CoverImage}}<img class="sk-cover" src="{{.CoverImage}}" alt="" loading="lazy">{{else}}<div class="sk-cover {{.CoverClass}}"></div>{{end}}
</a>
{{end}}{{end}}
{{if .Rest}}
<div class="sk-rows">
{{range .Rest}}
<a class="sk-row" href="{{.URL}}">
<span class="sk-row-date">{{.PublishedAt}}</span>
<span class="sk-row-main">
<h3 class="sk-row-title">{{.Title}}</h3>
{{if .Summary}}<p class="sk-row-sum">{{.Summary}}</p>{{end}}
</span>
<span class="sk-row-side">{{if .Tag}}<span class="sk-pill">{{.Tag}}</span>{{end}}<span class="sk-arrow">→</span></span>
</a>
{{end}}
</div>
{{end}}
{{if not .Posts}}
` + emptyStateStr + `{{end}}
</div>
</main>
`

// ―――――― ember(HopBase 暗色)――――――

// emberVars 覆盖 baseStyle 变量:钉死暗色(color-scheme:dark),Prose 自动换上暖黑+橙配色。
const emberVars = `
:root{color-scheme:dark;--bg:#0c0c0b;--fg:#f7f4ef;--muted:#a8a29a;--border:rgba(255,255,255,.09);--card:#171511;--accent:#ffb594;--accent-fg:#231206;--accent-soft:rgba(196,81,0,.16);--code-bg:#121110;--code-fg:#e5e9f2;--maxw:724px;--shadow:0 1px 2px rgba(0,0,0,.35),0 16px 40px rgba(0,0,0,.45);--navw:1240px;--brand:#c45100;--brand-light:#ffb594;--cyan:#5eead4}
`

// emberChromeCSS HopBase 皮 chrome + Prose 细节覆盖(品牌橙渐变字标/青色小标/发光按钮,对齐 landing)。
const emberChromeCSS = `
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Hiragino Sans GB","Microsoft YaHei",sans-serif}
.sk-nav{background:rgba(28,28,25,.72);backdrop-filter:blur(12px);-webkit-backdrop-filter:blur(12px);border-bottom:1px solid rgba(255,255,255,.05);position:sticky;top:0;z-index:20}
.sk-nav-inner{max-width:var(--navw);margin:0 auto;padding:0 24px;height:64px;display:flex;align-items:center;gap:28px}
.sk-logo{display:inline-flex;align-items:center;gap:10px;text-decoration:none}
.sk-logo:hover{text-decoration:none}
.sk-logo img{width:32px;height:32px;display:block}
.sk-brand{font-size:21px;font-weight:800;letter-spacing:-.01em;background:linear-gradient(95deg,#ffd0b3,#ffb594 30%,#c45100);-webkit-background-clip:text;background-clip:text;color:transparent}
.sk-links{display:flex;gap:26px;font-size:13.5px;font-weight:500;flex:1}
.sk-links a{color:var(--muted);position:relative;padding:6px 0;transition:color .2s;text-decoration:none}
.sk-links a:hover{color:#fff;text-decoration:none}
.sk-links a.act{color:#fff}
.sk-links a.act::after{content:"";position:absolute;left:0;right:0;bottom:-2px;height:2px;background:linear-gradient(90deg,transparent,var(--cyan),transparent)}
.sk-nav-right{display:flex;align-items:center;gap:12px;margin-left:auto}
.sk-langs{display:inline-flex;align-items:center;gap:6px;font-size:12.5px;color:var(--muted)}
.sk-langs i{font-style:normal;opacity:.4}
.sk-langs a{color:var(--muted);text-decoration:none;transition:color .2s}
.sk-langs a:hover{color:#fff;text-decoration:none}
.sk-langs a.act{color:#fff;font-weight:600}
.sk-login{height:39px;display:inline-flex;align-items:center;padding:0 18px;border-radius:9999px;background:rgba(255,255,255,.035);border:1px solid rgba(255,181,148,.14);color:var(--fg);font-size:13.5px;font-weight:600;transition:border-color .2s,box-shadow .2s;text-decoration:none;white-space:nowrap}
.sk-login:hover{border-color:rgba(255,181,148,.5);box-shadow:0 0 18px rgba(196,81,0,.25);text-decoration:none}
.sk-signup{height:39px;display:inline-flex;align-items:center;padding:0 20px;border-radius:9999px;color:#fff;font-size:13.5px;font-weight:700;background:linear-gradient(180deg,#f0741b 0%,#cf5400 52%,#a83f00 100%);box-shadow:inset 0 1px 0 rgba(255,214,178,.55),0 0 0 1px rgba(255,181,148,.24),0 6px 18px rgba(196,81,0,.3);text-decoration:none}
.sk-signup:hover{text-decoration:none;opacity:.94}
@media (max-width:900px){.sk-links{display:none}}
.sk-wrap{max-width:var(--navw);margin:0 auto;padding:0 24px}
.sk-hero{position:relative;padding:64px 0 40px;overflow:hidden}
.sk-hero::before{content:"";position:absolute;inset:0;background-image:linear-gradient(rgba(255,255,255,.028) 1px,transparent 1px),linear-gradient(90deg,rgba(255,255,255,.028) 1px,transparent 1px);background-size:58px 58px;-webkit-mask-image:radial-gradient(720px 340px at 28% 0%,#000 30%,transparent 78%);mask-image:radial-gradient(720px 340px at 28% 0%,#000 30%,transparent 78%);pointer-events:none}
.sk-eyebrow{position:relative;font-family:var(--mono);font-size:.72rem;font-weight:600;letter-spacing:.18em;text-transform:uppercase;color:var(--cyan);margin:0 0 14px}
.sk-title{position:relative;font-size:44px;font-weight:800;letter-spacing:-.02em;line-height:1.12;margin:0 0 12px;text-wrap:balance}
.sk-sub{position:relative;color:var(--muted);font-size:16.5px;margin:0;max-width:560px}
.sk-pill{display:inline-flex;align-items:center;border-radius:9999px;padding:3px 11px;font-size:.7rem;font-weight:700;letter-spacing:.04em;background:rgba(196,81,0,.16);color:var(--brand-light);border:1px solid rgba(255,181,148,.28);width:fit-content}
.sk-meta{color:var(--muted);font-size:12.5px;display:flex;align-items:center;gap:8px;margin:0}
.sk-meta .d{width:3px;height:3px;border-radius:50%;background:currentColor;opacity:.5}
.sk-cover{width:100%;aspect-ratio:16/9;object-fit:cover;display:block;position:relative}
div.sk-cover::after{content:"";position:absolute;inset:0;background-image:linear-gradient(rgba(255,255,255,.03) 1px,transparent 1px),linear-gradient(90deg,rgba(255,255,255,.03) 1px,transparent 1px);background-size:44px 44px}
.cv1{background:radial-gradient(120% 130% at 18% 20%,rgba(196,81,0,.62),transparent 55%),radial-gradient(110% 120% at 85% 80%,rgba(94,234,212,.22),transparent 55%),#121110}
.cv2{background:radial-gradient(120% 130% at 80% 15%,rgba(94,234,212,.3),transparent 55%),radial-gradient(120% 130% at 15% 85%,rgba(196,81,0,.5),transparent 60%),#121110}
.cv3{background:radial-gradient(140% 120% at 50% -10%,rgba(255,181,148,.4),transparent 55%),radial-gradient(100% 100% at 90% 90%,rgba(196,81,0,.35),transparent 55%),#14120f}
.cv4{background:radial-gradient(130% 130% at 12% 88%,rgba(94,234,212,.26),transparent 50%),radial-gradient(130% 110% at 85% 12%,rgba(240,116,27,.42),transparent 55%),#121110}
.cv5{background:radial-gradient(150% 140% at 70% 110%,rgba(196,81,0,.55),transparent 60%),radial-gradient(90% 90% at 15% 10%,rgba(255,208,179,.2),transparent 50%),#131110}
.cv6{background:radial-gradient(120% 120% at 30% 0%,rgba(94,234,212,.24),transparent 55%),radial-gradient(140% 120% at 95% 60%,rgba(196,81,0,.45),transparent 58%),#121110}
.sk-featured{display:grid;grid-template-columns:1fr 1.05fr;border-radius:16px;overflow:hidden;background:linear-gradient(180deg,rgba(255,255,255,.035),rgba(255,255,255,.012));border:1px solid rgba(255,255,255,.07);transition:border-color .28s,transform .28s,box-shadow .28s;margin-bottom:26px;text-decoration:none;color:inherit}
.sk-featured:hover{border-color:rgba(196,81,0,.45);transform:translateY(-4px);box-shadow:0 16px 50px rgba(0,0,0,.5);text-decoration:none}
.sk-featured-body{padding:32px 34px;display:flex;flex-direction:column;justify-content:center;gap:14px}
.sk-featured-title{font-size:26px;font-weight:750;line-height:1.3;letter-spacing:-.015em;margin:0;color:var(--fg);transition:color .2s}
.sk-featured:hover .sk-featured-title{color:var(--brand-light)}
.sk-featured-sum{color:var(--muted);font-size:14.5px;line-height:1.7;margin:0;display:-webkit-box;-webkit-line-clamp:3;-webkit-box-orient:vertical;overflow:hidden}
@media (max-width:820px){.sk-featured{grid-template-columns:1fr}.sk-featured-body{padding:22px}}
.sk-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(300px,1fr));gap:22px;padding-bottom:72px}
.sk-card{display:flex;flex-direction:column;border-radius:16px;overflow:hidden;background:linear-gradient(180deg,rgba(255,255,255,.035),rgba(255,255,255,.012));border:1px solid rgba(255,255,255,.07);transition:border-color .28s,transform .28s,box-shadow .28s;text-decoration:none;color:inherit}
.sk-card:hover{border-color:rgba(196,81,0,.45);transform:translateY(-4px);box-shadow:0 16px 50px rgba(0,0,0,.5);text-decoration:none}
.sk-card-body{padding:17px 19px 19px;display:flex;flex-direction:column;gap:9px;flex:1}
.sk-card-title{font-size:16.5px;font-weight:650;line-height:1.45;letter-spacing:-.01em;margin:0;color:var(--fg);transition:color .2s}
.sk-card:hover .sk-card-title{color:var(--brand-light)}
.sk-card-sum{color:var(--muted);font-size:13.5px;line-height:1.65;margin:0;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden}
.sk-card .sk-meta{margin-top:auto;padding-top:4px}
.sk-main .blog-empty{padding-bottom:110px}
.sk-footer{border-top:1px solid rgba(255,255,255,.05);padding:42px 0;margin-top:40px}
.sk-footer-inner{max-width:var(--navw);margin:0 auto;padding:0 24px;display:flex;flex-wrap:wrap;gap:24px;align-items:center;justify-content:space-between}
.sk-footer-brand .sk-logo img{width:28px;height:28px}
.sk-footer-note{color:var(--muted);font-size:13px;margin:10px 0 0;max-width:420px}
.sk-footer-copy{color:var(--muted);font-size:13px;margin:6px 0 0}
.sk-footer-links{display:flex;flex-wrap:wrap;gap:24px;font-size:13.5px;font-weight:500}
.sk-footer-links a{color:var(--muted);transition:color .2s;text-decoration:none}
.sk-footer-links a:hover{color:#fff;text-decoration:none}
.blog-wrap{padding-top:44px}
.article-eyebrow{font-family:var(--mono);letter-spacing:.18em;color:var(--cyan)}
.article-title{font-weight:800;letter-spacing:-.02em}
.article-avatar{background:linear-gradient(135deg,#ffb594,#c45100 70%)}
.article-cover{border:1px solid rgba(255,255,255,.08);box-shadow:0 20px 60px rgba(0,0,0,.55),0 0 40px rgba(196,81,0,.08)}
.article-content li::marker{color:var(--brand)}
.article-content blockquote{border-left-color:var(--brand)}
.article-content pre{border:1px solid rgba(255,255,255,.07)}
.blog-back:hover{color:var(--cyan)}
.blog-cta{border:1px solid rgba(255,181,148,.24);background:linear-gradient(135deg,rgba(196,81,0,.16),rgba(255,255,255,.02) 70%);border-radius:16px}
.blog-cta-btn,.blog-gate-btn{color:#fff;background:linear-gradient(180deg,#f0741b 0%,#cf5400 52%,#a83f00 100%);box-shadow:inset 0 1px 0 rgba(255,214,178,.55),0 0 0 1px rgba(255,181,148,.24),0 10px 30px rgba(196,81,0,.34);border-radius:12px;transition:transform .2s,box-shadow .2s}
.blog-cta-btn:hover,.blog-gate-btn:hover{transform:translateY(-2px);opacity:1;box-shadow:inset 0 1px 0 rgba(255,214,178,.6),0 0 0 1px rgba(255,181,148,.4),0 14px 38px rgba(196,81,0,.45)}
@media (max-width:560px){.sk-title{font-size:31px}.sk-hero{padding:44px 0 28px}}
`

// ―――――― ink(Essevin 水墨纸感)――――――

// inkVars 覆盖 baseStyle 变量:钉死亮色纸感(color-scheme:light),Prose 换上米白+陶土配色。
const inkVars = `
:root{color-scheme:light;--bg:#F4F1EA;--fg:#1A1A17;--muted:#8C887E;--border:#E6E2D6;--card:#FBFAF6;--accent:#9E6E5C;--accent-fg:#ffffff;--accent-soft:#EFE2DC;--code-bg:#1A1A17;--code-fg:#F4F1EA;--maxw:712px;--shadow:0 12px 40px -18px rgba(26,26,23,.18);--navw:1120px;--rose:#B5836F;--rose-dk:#9E6E5C;--blush:#E0C9C1;--serif:Georgia,"Songti TC","Songti SC","Noto Serif TC",serif}
`

// inkChromeCSS Essevin 皮 chrome + Prose 细节覆盖(衬线标题/发丝线/胶囊按钮,对齐 ink 站)。
const inkChromeCSS = `
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang TC","PingFang SC","Microsoft YaHei",sans-serif;font-size:17px;line-height:1.65}
.sk-nav{background:rgba(244,241,234,.82);backdrop-filter:saturate(1.2) blur(12px);-webkit-backdrop-filter:saturate(1.2) blur(12px);border-bottom:1px solid var(--border);position:sticky;top:0;z-index:20}
.sk-nav-inner{max-width:var(--navw);margin:0 auto;padding:0 28px;height:66px;display:flex;align-items:center;gap:30px}
.sk-logo{display:inline-flex;align-items:center;gap:10px;text-decoration:none}
.sk-logo:hover{text-decoration:none}
.sk-logo img{width:30px;height:30px;display:block}
.sk-brand{font-family:var(--serif);font-size:1.3rem;font-weight:500;letter-spacing:.03em;color:var(--fg)}
.sk-links{display:flex;gap:26px;font-size:.95rem;flex:1}
.sk-links a{color:var(--fg);position:relative;padding:6px 0;text-decoration:none}
.sk-links a::after{content:"";position:absolute;left:0;right:0;bottom:0;height:1.5px;background:var(--rose);transform:scaleX(0);transform-origin:left;transition:transform .3s cubic-bezier(.16,1,.3,1)}
.sk-links a:hover{text-decoration:none}
.sk-links a:hover::after,.sk-links a.act::after{transform:scaleX(1)}
.sk-nav-right{display:flex;align-items:center;gap:18px;font-size:.9rem;margin-left:auto}
.sk-langs{display:inline-flex;align-items:center;gap:6px;font-size:.85rem;color:var(--muted)}
.sk-langs i{font-style:normal;opacity:.45}
.sk-langs a{color:var(--muted);text-decoration:none;transition:color .25s}
.sk-langs a:hover{color:var(--rose);text-decoration:none}
.sk-langs a.act{color:var(--fg);font-weight:500}
.sk-login{color:var(--fg);text-decoration:none;transition:color .25s;white-space:nowrap}
.sk-login:hover{color:var(--rose);text-decoration:none}
.sk-signup{display:inline-flex;align-items:center;border-radius:9999px;background:var(--rose);color:#fff;font-size:.9rem;font-weight:500;padding:9px 18px;transition:background .25s;text-decoration:none;white-space:nowrap}
.sk-signup:hover{background:var(--rose-dk);text-decoration:none}
@media (max-width:900px){.sk-links{display:none}}
@media (max-width:480px){.sk-login{display:none}.sk-nav-inner{gap:16px}}
.sk-wrap{max-width:var(--navw);margin:0 auto;padding:0 28px}
.sk-hero{padding:68px 0 16px}
.sk-eyebrow{font-size:.78rem;font-weight:500;letter-spacing:.18em;text-transform:uppercase;color:var(--rose);margin:0 0 16px}
.sk-title{font-family:var(--serif);font-weight:400;font-size:48px;letter-spacing:-.01em;line-height:1.14;margin:0 0 14px;text-wrap:balance}
.sk-sub{color:var(--muted);font-size:1.02rem;margin:0;max-width:520px}
.sk-pill{display:inline-flex;border-radius:9999px;background:var(--accent-soft);color:var(--rose-dk);font-size:.75rem;font-weight:500;padding:4px 12px;width:fit-content}
.sk-meta{color:var(--muted);font-size:.85rem;display:flex;align-items:center;gap:9px;margin:0;font-variant-numeric:tabular-nums}
.sk-meta .d{width:3px;height:3px;border-radius:50%;background:currentColor;opacity:.55}
.sk-cover{width:100%;aspect-ratio:16/10;object-fit:cover;display:block;border-radius:18px;border:1px solid var(--border);position:relative;overflow:hidden}
div.sk-cover::after{content:"";position:absolute;inset:0;background:radial-gradient(90% 70% at 50% 115%,rgba(251,250,246,.75),transparent 60%)}
.cv1{background:radial-gradient(95% 110% at 22% 18%,#E0C9C1 0%,transparent 58%),radial-gradient(90% 100% at 82% 78%,rgba(94,122,107,.3),transparent 60%),#EFE2DC}
.cv2{background:radial-gradient(100% 110% at 78% 14%,rgba(74,107,138,.26),transparent 55%),radial-gradient(110% 110% at 18% 88%,#E0C9C1,transparent 62%),#F0EAE0}
.cv3{background:radial-gradient(110% 100% at 30% 90%,rgba(94,122,107,.24),transparent 58%),radial-gradient(100% 110% at 80% 10%,#E0C9C1,transparent 60%),#EFE2DC}
.cv4{background:radial-gradient(120% 110% at 85% 85%,rgba(181,131,111,.34),transparent 58%),radial-gradient(90% 90% at 15% 15%,rgba(74,107,138,.18),transparent 55%),#F0EAE0}
.cv5{background:radial-gradient(100% 120% at 50% -10%,#E0C9C1,transparent 58%),radial-gradient(110% 100% at 88% 90%,rgba(94,122,107,.22),transparent 60%),#EFE2DC}
.cv6{background:radial-gradient(120% 120% at 12% 80%,rgba(181,131,111,.3),transparent 55%),radial-gradient(100% 110% at 82% 20%,rgba(74,107,138,.2),transparent 58%),#F0EAE0}
.sk-featured{display:grid;grid-template-columns:1.05fr 1fr;gap:44px;align-items:center;padding:40px 0 44px;border-bottom:1px solid var(--border);text-decoration:none;color:inherit}
.sk-featured:hover{text-decoration:none}
.sk-featured-body{display:flex;flex-direction:column;gap:16px}
.sk-featured-title{font-family:var(--serif);font-weight:400;font-size:32px;line-height:1.3;letter-spacing:-.01em;margin:0;color:var(--fg);transition:color .25s}
.sk-featured:hover .sk-featured-title{color:var(--rose-dk)}
.sk-featured-sum{color:#5d594f;font-size:.98rem;line-height:1.7;margin:0}
@media (max-width:820px){.sk-featured{grid-template-columns:1fr;gap:22px}}
.sk-rows{padding-bottom:64px}
.sk-row{display:grid;grid-template-columns:110px 1fr auto;gap:26px;align-items:baseline;padding:30px 0;border-bottom:1px solid var(--border);text-decoration:none;color:inherit}
.sk-row:hover{text-decoration:none}
.sk-row-date{color:var(--muted);font-size:.85rem;font-variant-numeric:tabular-nums}
.sk-row-title{font-family:var(--serif);font-weight:400;font-size:22px;line-height:1.4;margin:0 0 6px;color:var(--fg);transition:color .25s}
.sk-row:hover .sk-row-title{color:var(--rose-dk)}
.sk-row-sum{color:var(--muted);font-size:.92rem;line-height:1.65;margin:0;max-width:640px}
.sk-row-side{display:flex;align-items:center;gap:14px}
.sk-arrow{color:var(--rose);font-size:1.1rem;transition:transform .3s cubic-bezier(.16,1,.3,1)}
.sk-row:hover .sk-arrow{transform:translateX(5px)}
@media (max-width:720px){.sk-row{grid-template-columns:1fr;gap:8px}.sk-row-side{display:none}}
.sk-main .blog-empty{padding-bottom:110px}
.sk-footer{background:var(--card);border-top:1px solid var(--border);margin-top:48px}
.sk-footer-inner{max-width:var(--navw);margin:0 auto;padding:44px 28px;display:flex;flex-wrap:wrap;gap:36px;align-items:flex-start;justify-content:space-between}
.sk-footer-brand .sk-logo img{width:26px;height:26px}
.sk-footer-note{color:var(--muted);font-size:.92rem;line-height:1.7;margin:12px 0 0;max-width:360px}
.sk-footer-copy{color:var(--muted);font-size:.86rem;margin:14px 0 0}
.sk-footer-links{display:flex;flex-direction:column;gap:10px;font-size:.92rem}
.sk-footer-links a{color:var(--fg);transition:color .25s;text-decoration:none}
.sk-footer-links a:hover{color:var(--rose);text-decoration:none}
.blog-wrap{padding-top:48px}
.article-eyebrow{letter-spacing:.18em;font-weight:500;color:var(--rose)}
.article-title{font-family:var(--serif);font-weight:400;font-size:42px;line-height:1.2;letter-spacing:-.01em}
.article-avatar{background:linear-gradient(135deg,#E0C9C1,#B5836F 75%)}
.article-cover{border-radius:18px;border:1px solid var(--border)}
.article-content{font-size:17px;line-height:1.85}
.article-content>p:first-child{font-family:var(--serif);font-size:20px;line-height:1.7}
.article-content h2{font-family:var(--serif);font-weight:500;font-size:28px}
.article-content h3{font-family:var(--serif);font-weight:500;font-size:22px}
.article-content li::marker{color:var(--rose)}
.article-content blockquote{border-left:2px solid var(--rose);font-family:var(--serif);font-style:italic;font-size:21px;line-height:1.6}
.blog-back:hover{color:var(--rose)}
.blog-cta{background:var(--accent-soft);border:0;border-radius:18px;padding:36px 30px;text-align:center}
.blog-cta-title{font-family:var(--serif);font-weight:500;font-size:24px}
.blog-cta-btn,.blog-gate-btn{border-radius:9999px;background:var(--rose);color:#fff;font-weight:500;transition:background .25s}
.blog-cta-btn:hover,.blog-gate-btn:hover{background:var(--rose-dk);opacity:1}
@media (max-width:560px){.sk-title{font-size:34px}.article-title{font-size:31px}.sk-hero{padding:46px 0 12px}}
`

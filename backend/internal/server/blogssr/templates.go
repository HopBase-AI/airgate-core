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
.blog-gate{position:fixed;inset:0;pointer-events:none;display:flex;align-items:flex-end;justify-content:center;background:linear-gradient(to bottom,rgba(0,0,0,0) 0%,var(--bg) 58%);z-index:50}
#blog-gate[hidden]{display:none!important}
.blog-gate-card{pointer-events:auto;text-align:center;max-width:428px;width:calc(100% - 40px);margin-bottom:8vh;padding:26px;border:1px solid var(--border);border-radius:18px;background:var(--card);box-shadow:0 14px 44px rgba(0,0,0,.2)}
.blog-gate-title{font-size:18px;font-weight:650;margin:0 0 8px}
.blog-gate-desc{font-size:14px;color:var(--muted);margin:0 0 16px;line-height:1.6}
.blog-gate-btn{display:inline-block;background:var(--accent);color:var(--accent-fg);font-weight:600;padding:11px 28px;border-radius:10px}
.blog-gate-btn:hover{text-decoration:none;opacity:.92}
.blog-cta{margin:52px 0 8px;border:1px solid color-mix(in srgb,var(--accent) 24%,var(--border));border-radius:18px;background:linear-gradient(135deg,var(--accent-soft),var(--card) 75%);padding:30px 28px}
.blog-cta-title{font-size:19px;font-weight:700;margin:0 0 8px;color:var(--fg);letter-spacing:-.01em}
.blog-cta-desc{font-size:14.5px;color:var(--muted);margin:0 0 18px;line-height:1.6}
.blog-cta-btn{display:inline-block;background:var(--accent);color:var(--accent-fg);font-weight:600;padding:11px 26px;border-radius:11px;font-size:15px}
.blog-cta-btn:hover{text-decoration:none;opacity:.92}
@media (max-width:560px){.article-title{font-size:30px}.article-content{font-size:17px}.article-content>p:first-child{font-size:18px}.blog-intro-title{font-size:27px}}
`

// ―――――― 共享 head(所有皮肤一致,SEO 元信息) ――――――

const listHeadStr = `<!doctype html>
<html lang="{{if .HTMLLang}}{{.HTMLLang}}{{else}}zh-Hant{{end}}">
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
<html lang="{{if .HTMLLang}}{{.HTMLLang}}{{else}}zh-Hant{{end}}">
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
<html lang="{{if .HTMLLang}}{{.HTMLLang}}{{else}}zh-Hant{{end}}">
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
<p class="blog-empty-title">{{.UI.EmptyTitle}}</p>
<p class="blog-empty-sub">{{.UI.EmptySub}}</p>
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
<a href="{{.HomeURL}}" class="blog-back">{{.UI.Back}}</a>
<article>
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
<aside class="blog-cta" data-blog-acquisition>
<p class="blog-cta-title">{{.CTATitle}}</p>
<p class="blog-cta-desc">{{.CTADesc}}</p>
<a class="blog-cta-btn" href="{{.RegisterURL}}">{{.UI.CTAButton}}</a>
</aside>
</main>
`

// gateStr 注册墙遮罩 + 单向滚动边界(全皮肤共享)。达到阈值后禁止继续向下,但允许返回上文。
const gateStr = `{{if .GateEnabled}}
<div id="blog-gate" class="blog-gate" role="dialog" aria-labelledby="blog-gate-title" hidden>
<div class="blog-gate-card">
<p class="blog-gate-title" id="blog-gate-title">{{.UI.GateTitle}}</p>
<p class="blog-gate-desc" id="blog-gate-desc">{{.UI.GateDesc}}</p>
<a class="blog-gate-btn" id="blog-gate-btn" href="{{.RegisterURL}}">{{.UI.GateButton}}</a>
</div>
</div>
<script>
(function(){
var pos={{.GatePosition}};
var content=document.getElementById('blog-content');
var gate=document.getElementById('blog-gate');
if(!content||!gate)return;
// 注册墙跟随主站/控制台共用的语言偏好。SSR 文案只负责无 JS 兜底；弹窗打开前
// 即完成替换，避免 CDN 缓存按首位访客的 Cookie 分叉或串语言。
var gateCopy={
'zh-HK':['註冊後繼續閱讀全文','免費註冊即可讀完本文，並獲得 API 額度體驗。','免費註冊 / 登入'],
'zh':['注册后继续阅读全文','免费注册即可读完本文，并获得 API 额度体验。','免费注册 / 登录'],
'en':['Sign up to keep reading','Create a free account to finish this article and get trial API credits.','Sign up / Log in'],
'ja':['登録して続きを読む','無料アカウントを作成すると、記事の全文を読め、API のトライアルクレジットも利用できます。','無料登録 / ログイン']
};
function gateLang(){
try{
var queryLang=new URLSearchParams(location.search).get('lang')||'';
if(queryLang)return normalizeGateLang(queryLang,false);
var match=document.cookie.match(/(?:^|;\s*)lang=([^;]+)/);
var value=match?decodeURIComponent(match[1]||''):(localStorage.getItem('lang')||'');
if(value)return normalizeGateLang(value,false);
var essevinLang=localStorage.getItem('essevin-lang')||'';
if(essevinLang)return normalizeGateLang(essevinLang,true);
if(localStorage.getItem('essevin-script')==='hans')return 'zh';
}catch(e){}
return 'zh-HK';
}
function normalizeGateLang(value,essevin){
value=(value||'').toLowerCase();
// Essevin SPA 以 zh=繁体、zh-CN=简体；HopBase/控制台则以 zh=简体、zh-HK=繁体。
if(essevin&&value==='zh')return 'zh-HK';
if(value==='zh'||value.indexOf('zh-cn')===0||value.indexOf('zh-sg')===0||value.indexOf('hans')>=0)return 'zh';
if(value.indexOf('en')===0)return 'en';
if(value.indexOf('ja')===0)return 'ja';
return 'zh-HK';
}
var copy=gateCopy[gateLang()]||gateCopy['zh-HK'];
var gateTitle=document.getElementById('blog-gate-title');
var gateDesc=document.getElementById('blog-gate-desc');
var gateBtn=document.getElementById('blog-gate-btn');
if(gateTitle)gateTitle.textContent=copy[0];
if(gateDesc)gateDesc.textContent=copy[1];
if(gateBtn)gateBtn.textContent=copy[2];
var gateOpen=false;
var gateStarted=false;
var limitY=0;
var touchY=null;
var downKeys={ArrowDown:1,PageDown:1,End:1};
function hasReaderSession(){
return /(?:^|;\s*)airgate_reader_session=1(?:;|$)/.test(document.cookie||'');
}
function thresholdScrollY(){
var rect=content.getBoundingClientRect();
var total=content.offsetHeight||1;
var contentTop=rect.top+window.scrollY;
var target=contentTop-window.innerHeight*0.5+total*(pos/100);
var max=Math.max(0,(document.documentElement.scrollHeight||0)-window.innerHeight);
return Math.max(0,Math.min(target,max));
}
function showGate(){
if(gateOpen)return;
gateOpen=true;
gate.removeAttribute('hidden');
}
function hideGate(){
if(!gateOpen)return;
gateOpen=false;
gate.setAttribute('hidden','');
}
function onScroll(){
if(hasReaderSession()){hideGate();return;}
limitY=thresholdScrollY();
if(window.scrollY>limitY+1){showGate();window.scrollTo(0,limitY);return;}
if(window.scrollY>=limitY-1){showGate();}else{hideGate();}
}
function syncGateWithSession(){
onScroll();
}
function syncGateOnVisibility(){
if(!document.hidden)syncGateWithSession();
}
function preventDownwardWheel(event){
if(gateOpen&&event.deltaY>0)event.preventDefault();
}
function rememberTouch(event){
if(event.touches.length)touchY=event.touches[0].clientY;
}
function preventDownwardTouch(event){
if(!event.touches.length)return;
var currentY=event.touches[0].clientY;
var delta=touchY===null?0:touchY-currentY;
touchY=currentY;
if(gateOpen&&delta>0)event.preventDefault();
}
function clearTouch(){touchY=null;}
function preventDownwardKey(event){
var downward=downKeys[event.key]||(event.key===' '&&!event.shiftKey);
if(gateOpen&&downward)event.preventDefault();
}
function onResize(){
onScroll();
}
var navEntries=window.performance&&performance.getEntriesByType?performance.getEntriesByType('navigation'):[];
var reloaded=navEntries.length&&navEntries[0].type==='reload';
var resetAfterReload=function(){hideGate();window.scrollTo(0,0);};
var resetOnPageShow=function(){setTimeout(resetAfterReload,0);};
function startGate(){
if(gateStarted)return;
gateStarted=true;
window.addEventListener('scroll',onScroll,{passive:true});
window.addEventListener('resize',onResize,{passive:true});
window.addEventListener('wheel',preventDownwardWheel,{passive:false});
window.addEventListener('touchstart',rememberTouch,{passive:true});
window.addEventListener('touchmove',preventDownwardTouch,{passive:false});
window.addEventListener('touchend',clearTouch,{passive:true});
window.addEventListener('touchcancel',clearTouch,{passive:true});
window.addEventListener('keydown',preventDownwardKey);
window.addEventListener('pageshow',syncGateWithSession);
window.addEventListener('focus',syncGateWithSession);
document.addEventListener('visibilitychange',syncGateOnVisibility);
if(reloaded){
if('scrollRestoration' in history)history.scrollRestoration='manual';
resetAfterReload();
window.addEventListener('load',resetAfterReload,{once:true});
window.addEventListener('pageshow',resetOnPageShow,{once:true});
}
syncGateWithSession();
}
function stopGate(){
hideGate();
if(!gateStarted)return;
gateStarted=false;
window.removeEventListener('scroll',onScroll);
window.removeEventListener('resize',onResize);
window.removeEventListener('wheel',preventDownwardWheel);
window.removeEventListener('touchstart',rememberTouch);
window.removeEventListener('touchmove',preventDownwardTouch);
window.removeEventListener('touchend',clearTouch);
window.removeEventListener('touchcancel',clearTouch);
window.removeEventListener('keydown',preventDownwardKey);
window.removeEventListener('pageshow',syncGateWithSession);
window.removeEventListener('focus',syncGateWithSession);
document.removeEventListener('visibilitychange',syncGateOnVisibility);
window.removeEventListener('load',resetAfterReload);
window.removeEventListener('pageshow',resetOnPageShow);
}
// 默认皮肤没有登录按钮/跨域会话桥，保持原有注册墙即时行为。
if(!document.querySelector('[data-blog-auth]')){startGate();return;}
var authSeen=false;
function onBlogSession(event){
authSeen=true;
var session=event&&event.detail?event.detail:{};
if(session.authenticated){stopGate();return;}
if(session.resolved!==false)startGate();
}
document.addEventListener('airgate:blog-session',onBlogSession);
if(window.__airgateBlogSession)onBlogSession({detail:window.__airgateBlogSession});
// 会话脚本异常或 iframe 被策略拦截时仍保证访客注册墙可用。
setTimeout(function(){if(!authSeen)startGate();},3500);
})();
</script>
{{end}}
`

// authSessionScriptStr 在公开博客与控制台之间同步展示级登录态：父域 Cookie 负责
// Safari/返回导航的快速恢复，控制台同源 iframe 再校验真实 Token。仅传昵称/邮箱，
// 不让博客源接触 Bearer Token；登录用户同时隐藏注册按钮、常驻 CTA 和注册墙。
const authSessionScriptStr = `<script>
(function(){
'use strict';
var roots=Array.prototype.slice.call(document.querySelectorAll('[data-blog-auth]'));
if(!roots.length)return;
var cookieName='airgate_blog_session_v1';
var consoleURL=roots[0].getAttribute('data-console-url')||'';
var consoleOrigin='';
try{consoleOrigin=new URL(consoleURL,location.href).origin;}catch(e){}
var frame=null;
var timer=0;
var probeID=0;
var lastProbe=0;
function readHint(){
try{
var prefix=cookieName+'=';
var item=document.cookie.split(';').map(function(v){return v.trim();}).find(function(v){return v.indexOf(prefix)===0;});
if(!item)return null;
var hint=JSON.parse(decodeURIComponent(item.slice(prefix.length)));
if(hint&&hint.v===1&&typeof hint.exp==='number'&&hint.exp>Date.now()/1000){
return {authenticated:true,name:String(hint.name||'User').slice(0,80),email:String(hint.email||'').slice(0,160),resolved:false};
}
}catch(e){}
return null;
}
function clearHint(){
try{
var expired=cookieName+'=; Path=/blog; Max-Age=0; SameSite=Lax';
document.cookie=expired;
var host=consoleOrigin?new URL(consoleOrigin).hostname:'';
var labels=host.split('.');
if(labels.length>=3)document.cookie=expired+'; Domain='+labels.slice(1).join('.');
}catch(e){}
}
function render(session){
var authenticated=session&&session.authenticated===true;
var name=authenticated?String(session.name||session.email||'User').slice(0,80):'';
var email=authenticated?String(session.email||'').slice(0,160):'';
var chars=Array.from(name||email||'U');
var initial=(chars[0]||'U').toUpperCase();
roots.forEach(function(root){
var guest=root.querySelector('[data-blog-auth-guest]');
var user=root.querySelector('[data-blog-auth-user]');
if(guest)guest.hidden=authenticated;
if(user){
user.hidden=!authenticated;
if(authenticated){
var avatar=user.querySelector('[data-blog-auth-avatar]');
var label=user.querySelector('[data-blog-auth-name]');
if(avatar)avatar.textContent=initial;
if(label)label.textContent=name;
var title=email&&email!==name?name+' · '+email:name;
user.setAttribute('aria-label',title);
user.setAttribute('title',title);
}
}
});
document.querySelectorAll('[data-blog-acquisition]').forEach(function(node){node.hidden=authenticated;});
var state={authenticated:authenticated,name:name,email:email,resolved:session&&session.resolved!==false};
window.__airgateBlogSession=state;
document.dispatchEvent(new CustomEvent('airgate:blog-session',{detail:state}));
}
function removeFrame(){
if(frame){frame.remove();frame=null;}
}
function probe(){
lastProbe=Date.now();
probeID+=1;
var id=probeID;
var hint=readHint();
if(hint)render(hint);
clearTimeout(timer);
removeFrame();
if(!consoleOrigin){render(hint||{authenticated:false,resolved:true});return;}
frame=document.createElement('iframe');
frame.hidden=true;
frame.tabIndex=-1;
frame.setAttribute('aria-hidden','true');
var src=new URL('/blog/session-bridge',consoleOrigin);
src.searchParams.set('origin',location.origin);
src.searchParams.set('_',String(Date.now()));
frame.src=src.toString();
document.body.appendChild(frame);
timer=setTimeout(function(){
if(id!==probeID)return;
removeFrame();
if(!hint)render({authenticated:false,resolved:true});
},2500);
}
window.addEventListener('message',function(event){
if(!frame||event.source!==frame.contentWindow||event.origin!==consoleOrigin)return;
var data=event.data;
if(!data||data.type!=='airgate:blog-session'||typeof data.authenticated!=='boolean')return;
clearTimeout(timer);
removeFrame();
if(data.authenticated){
render({authenticated:true,name:data.name,email:data.email,resolved:true});
}else{
var fallback=readHint();
if(fallback){render(fallback);return;}
clearHint();
render({authenticated:false,resolved:true});
}
});
window.addEventListener('pageshow',function(event){if(event.persisted)probe();});
document.addEventListener('visibilitychange',function(){
if(document.visibilityState==='visible'&&Date.now()-lastProbe>1000)probe();
});
probe();
})();
</script>
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
		return assemble(listHeadStr, baseStyle+emberVars+emberChromeCSS, skinHeaderStr, emberListBodyStr, skinFooterStr, skinLangScriptStr+authSessionScriptStr)
	case themeInk:
		return assemble(listHeadStr, baseStyle+inkVars+inkChromeCSS, skinHeaderStr, inkListBodyStr, skinFooterStr, skinLangScriptStr+authSessionScriptStr)
	default:
		return assemble(listHeadStr, baseStyle, defaultHeaderStr, defaultListBodyStr, defaultFooterStr, "")
	}
}

// detailTemplateStr 按皮肤返回详情页模板串。
func detailTemplateStr(theme string) string {
	switch theme {
	case themeEmber:
		return assemble(detailHeadStr, baseStyle+emberVars+emberChromeCSS, skinHeaderStr, detailBodyStr, skinFooterStr, gateStr+authSessionScriptStr)
	case themeInk:
		return assemble(detailHeadStr, baseStyle+inkVars+inkChromeCSS, skinHeaderStr, detailBodyStr, skinFooterStr, gateStr+authSessionScriptStr)
	default:
		return assemble(detailHeadStr, baseStyle, defaultHeaderStr, detailBodyStr, defaultFooterStr, gateStr)
	}
}

// notFoundTemplateStr 按皮肤返回 404 页模板串。
func notFoundTemplateStr(theme string) string {
	switch theme {
	case themeEmber:
		return assemble(notFoundHeadStr, baseStyle+emberVars+emberChromeCSS, skinHeaderStr, notFoundBodyStr, skinFooterStr, authSessionScriptStr)
	case themeInk:
		return assemble(notFoundHeadStr, baseStyle+inkVars+inkChromeCSS, skinHeaderStr, notFoundBodyStr, skinFooterStr, authSessionScriptStr)
	default:
		return assemble(notFoundHeadStr, baseStyle, "", notFoundBodyStr, "", "")
	}
}

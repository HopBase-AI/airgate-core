import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import zh from './zh.json';
import zhHK from './zh-HK.json';
import en from './en.json';
import ja from './ja.json';

const SUPPORTED_LANGUAGES = new Set(['zh', 'zh-HK', 'en', 'ja']);
const DEFAULT_LANGUAGE = 'en';

function normalizeLanguage(lang: string | null | undefined) {
  return lang && SUPPORTED_LANGUAGES.has(lang) ? lang : null;
}

// 按浏览器语言推断首选界面语言（无 cookie/localStorage 存储时）：
// 大陆简体、港澳台繁体、日本日语、其余英文。出海默认英文且不牺牲既有区域体验。
function detectBrowserLanguage(): string {
  if (typeof navigator === 'undefined') return DEFAULT_LANGUAGE;
  const candidates = Array.isArray(navigator.languages) && navigator.languages.length
    ? navigator.languages
    : [navigator.language];
  for (const raw of candidates) {
    const lang = (raw || '').toLowerCase();
    if (!lang) continue;
    if (lang === 'zh-hk' || lang === 'zh-tw' || lang === 'zh-mo' || lang.includes('hant')) return 'zh-HK';
    if (lang === 'zh' || lang.startsWith('zh-cn') || lang.startsWith('zh-sg') || lang.includes('hans')) return 'zh';
    if (lang.startsWith('zh')) return 'zh';
    if (lang.startsWith('ja')) return 'ja';
    if (lang.startsWith('en')) return 'en';
  }
  return DEFAULT_LANGUAGE;
}

function getCookieLanguage() {
  if (typeof document === 'undefined') return null;
  const match = document.cookie.match(/(?:^|;\s*)lang=([^;]+)/);
  if (!match) return null;
  return normalizeLanguage(decodeURIComponent(match[1] ?? ''));
}

function setCookieLanguage(lang: string) {
  if (typeof document === 'undefined') return;
  const secure = window.location.protocol === 'https:' ? '; secure' : '';
  const rootDomain = window.location.hostname.endsWith('.hop-base.com') || window.location.hostname === 'hop-base.com'
    ? '; domain=.hop-base.com'
    : '';
  document.cookie = `lang=${encodeURIComponent(lang)}; path=/; max-age=31536000; samesite=lax${secure}${rootDomain}`;
}

export function getStoredLanguage() {
  if (typeof window === 'undefined') return DEFAULT_LANGUAGE;
  const cookieLang = getCookieLanguage();
  if (cookieLang) {
    try {
      window.localStorage.setItem('lang', cookieLang);
    } catch {
      // localStorage can be unavailable in restricted browser modes.
    }
    return cookieLang;
  }

  try {
    const localLang = normalizeLanguage(window.localStorage.getItem('lang'));
    if (localLang) {
      setCookieLanguage(localLang);
      return localLang;
    }
  } catch {
    // localStorage can be unavailable in restricted browser modes.
  }
  // 无显式存储：按浏览器语言智能推断（出海默认英文）
  return detectBrowserLanguage();
}

export function setStoredLanguage(lang: string) {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem('lang', lang);
  } catch {
    // Language switching should keep working when storage is unavailable.
  }
  setCookieLanguage(lang);
}

i18n.use(initReactI18next).init({
  resources: {
    zh: { translation: zh },
    'zh-HK': { translation: zhHK },
    en: { translation: en },
    ja: { translation: ja },
  },
  lng: getStoredLanguage(),
  fallbackLng: DEFAULT_LANGUAGE,
  interpolation: { escapeValue: false },
});

// 切换语言时同步 <html lang>（无障碍与 SEO：初始 index.html 静态 lang 不会自动更新）
const HTML_LANG: Record<string, string> = { zh: 'zh-CN', 'zh-HK': 'zh-HK', en: 'en', ja: 'ja' };
function syncDocumentLang(lang: string) {
  if (typeof document !== 'undefined') {
    document.documentElement.lang = HTML_LANG[lang] || lang || 'en';
  }
}
syncDocumentLang(i18n.language);
i18n.on('languageChanged', syncDocumentLang);

export default i18n;

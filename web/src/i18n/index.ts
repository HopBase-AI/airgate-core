import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import zh from './zh.json';
import en from './en.json';

const SUPPORTED_LANGUAGES = new Set(['zh', 'en']);

function normalizeLanguage(lang: string | null | undefined) {
  return lang && SUPPORTED_LANGUAGES.has(lang) ? lang : null;
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
  if (typeof window === 'undefined') return 'zh';
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
  return 'zh';
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
    en: { translation: en },
  },
  lng: getStoredLanguage(),
  fallbackLng: 'zh',
  interpolation: { escapeValue: false },
});

export default i18n;

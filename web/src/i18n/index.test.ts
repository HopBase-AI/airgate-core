import { describe, expect, it } from 'vitest';
import { normalizeLanguage } from './index';
import en from './en.json';
import es from './es.json';
import ja from './ja.json';
import zh from './zh.json';
import zhHK from './zh-HK.json';

describe('normalizeLanguage', () => {
  it('maps public blog keys to console locale keys', () => {
    expect(normalizeLanguage('zh-Hant')).toBe('zh-HK');
    expect(normalizeLanguage('zh-TW')).toBe('zh-HK');
    expect(normalizeLanguage('zh-Hans')).toBe('zh');
    expect(normalizeLanguage('zh-CN')).toBe('zh');
    expect(normalizeLanguage('en-US')).toBe('en');
    expect(normalizeLanguage('es')).toBe('es');
    expect(normalizeLanguage('es-MX')).toBe('es');
  });

  it('rejects unsupported keys', () => {
    expect(normalizeLanguage('fr')).toBeNull();
  });
});

// —— i18n SOP 硬约束(docs/i18n-sop.md) ——
// ① 五个语言包的键集合必须完全一致:新增 key 必须一次补齐五语,禁止只写 zh。
// ② 代码里静态引用的 t('ns.key') 必须存在于语言包:defaultValue 只是兜底,
//    key 不进包意味着所有语言都裸显中文默认值(2026-08-31 排查出 5 个此类 key)。

const PACKS = { zh, 'zh-HK': zhHK, en, ja, es };

function flattenKeys(obj: Record<string, unknown>, prefix = ''): string[] {
  return Object.entries(obj).flatMap(([k, v]) =>
    v !== null && typeof v === 'object' ? flattenKeys(v as Record<string, unknown>, `${prefix}${k}.`) : [`${prefix}${k}`],
  );
}

describe('language pack integrity', () => {
  const zhKeys = flattenKeys(PACKS.zh).sort();

  it.each(Object.entries(PACKS).filter(([lang]) => lang !== 'zh'))('%s has the same key set as zh', (_lang, pack) => {
    expect(flattenKeys(pack).sort()).toEqual(zhKeys);
  });

  it('every static t() key referenced in src exists in the packs', () => {
    const sources: Record<string, string> = import.meta.glob('../**/*.{ts,tsx}', { query: '?raw', import: 'default', eager: true });

    // 叶子 key + 所有中间节点(returnObjects 用法取整个节点,如 legal.privacy.sections)
    const zhKeySet = new Set<string>();
    for (const key of zhKeys) {
      const parts = key.split('.');
      for (let i = 1; i <= parts.length; i += 1) zhKeySet.add(parts.slice(0, i).join('.'));
    }
    const namespaces = new Set(Object.keys(PACKS.zh));
    const missing = new Set<string>();
    for (const [path, src] of Object.entries(sources)) {
      if (path.includes('/i18n/') || path.includes('.test.')) continue;
      for (const m of src.matchAll(/\bt\(\s*['"]([A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)+)['"]/g)) {
        const key = m[1] ?? '';
        // 只校验首段命中已知命名空间的 key,避免把非 i18n 的 t() 调用误报进来
        if (namespaces.has(key.split('.')[0] ?? '') && !zhKeySet.has(key)) missing.add(key);
      }
    }
    expect([...missing].sort()).toEqual([]);
  });
});

import { describe, expect, it } from 'vitest';
import {
  activeGroupRatesByPlatform,
  parseTocPricing,
  serializeTocPricing,
  validateTocPricing,
} from './TocPricingEditor';
import {
  isSitesBrandingShapeValid,
  parseSitesBranding,
  serializeSitesBranding,
  validateSitesBranding,
} from './SitesBrandingEditor';
import { isBlogSitesShapeValid, parseBlogSites, serializeBlogSites, validateBlogSites } from './BlogSitesEditor';

// 结构化编辑器把 JSON setting 拆成表单再拼回去。这里锁住最关键的性质：
// 用生产上的真实值走一遍 parse → serialize，语义必须逐字段等价——否则管理员打开
// 设置页什么都不改，光是渲染就会把线上配置改坏。

const t = (k: string, o?: Record<string, unknown>) => (o ? `${k}:${JSON.stringify(o)}` : k);

describe('toc_landing_pricing 圆环往返', () => {
  // 生产实际值（2026-07-27 ToC 实例）。
  const prod =
    '{"fx": 6.8, "board": [{"id": "glm-5.2", "multiplier": 0.55}, {"id": "seedream-4-5", "multiplier": 4.624}, '
    + '{"id": "seedream-5-0-lite", "multiplier": 4.624}, {"id": "seedream-5-0-pro", "multiplier": 4.624}], '
    + '"multipliers": {"kiro": 2.5, "claude": 2.13, "gemini": 5.1, "openai": 0.45, "seedance": 6.12}, '
    + '"plaza_currency": "USD"}';

  it('parse → serialize 语义等价', () => {
    const round = serializeTocPricing(parseTocPricing(prod));
    expect(JSON.parse(round)).toEqual(JSON.parse(prod));
  });

  it('生产值本身无校验错误', () => {
    expect(validateTocPricing(parseTocPricing(prod), t)).toEqual([]);
  });

  it('小数倍率不被截断', () => {
    const v = parseTocPricing(prod);
    expect(v.board.find((r) => r.id === 'seedream-4-5')?.multiplier).toBe('4.624');
    expect(v.multipliers['claude']).toBe('2.13');
  });

  it('空值序列化成空串而非空对象', () => {
    expect(serializeTocPricing(parseTocPricing(''))).toBe('');
  });

  it('非法 JSON 不抛异常', () => {
    expect(() => parseTocPricing('{"fx": 6.8,')).not.toThrow();
  });

  it('非法 JSON 阻塞保存，而不是当成空配置', () => {
    const errors = validateTocPricing(parseTocPricing('{"fx": 6.8,'), t);
    expect(errors).toContain('settings.toc_landing_pricing_invalid');
  });

  it('倍率为 0 或负数被判错', () => {
    const v = parseTocPricing('{"multipliers": {"claude": 2.13}}');
    v.multipliers['claude'] = '0';
    expect(validateTocPricing(v, t).length).toBeGreaterThan(0);
  });

  it('board 模型 ID 重复被判错', () => {
    const v = parseTocPricing('{"board":[{"id":"glm-5.2","multiplier":0.55},{"id":"glm-5.2","multiplier":0.6}]}');
    expect(validateTocPricing(v, t).some((e) => e.startsWith('settings.toc_pricing_err_board_dup'))).toBe(true);
  });

  it('留空的平台倍率不会写成 0', () => {
    const v = parseTocPricing('{"multipliers": {"claude": 2.13}}');
    v.multipliers['openai'] = '';
    const out = JSON.parse(serializeTocPricing(v)) as { multipliers: Record<string, number> };
    expect(out.multipliers).not.toHaveProperty('openai');
  });

  it('保留未知的扩展字段', () => {
    const raw = '{"fx":6.8,"future_option":{"enabled":true}}';
    expect(JSON.parse(serializeTocPricing(parseTocPricing(raw)))).toEqual(JSON.parse(raw));
  });

  it('保留单个模型牌价的未知扩展字段', () => {
    const raw = '{"board":[{"id":"glm-5.2","multiplier":0.55,"future_option":{"enabled":true}}]}';
    expect(JSON.parse(serializeTocPricing(parseTocPricing(raw)))).toEqual(JSON.parse(raw));
  });

  it('损坏的倍率或 board 条目会阻塞保存', () => {
    expect(validateTocPricing(parseTocPricing('{"multipliers":{"claude":true}}'), t))
      .toContain('settings.toc_landing_pricing_invalid');
    expect(validateTocPricing(parseTocPricing('{"board":[null]}'), t))
      .toContain('settings.toc_landing_pricing_invalid');
  });

  it('价格偏差提示忽略已下架分组', () => {
    const rates = activeGroupRatesByPlatform([
      { platform: 'seedance', rate_multiplier: 6.12, delisted: true },
      { platform: 'seedance', rate_multiplier: 5.78, delisted: false },
    ]);
    expect(rates.get('seedance')).toBe(5.78);
  });
});

describe('sites_branding 圆环往返', () => {
  // 生产实际值（三站 + 博客扩展字段）。
  const prod = JSON.stringify({
    ink: {
      name: 'Essevin',
      logo: 'https://essevin.com/logo.svg',
      doc_url: 'https://essevin.com/docs',
      host: 'essevin.com',
      blog_theme: 'ink',
      blog_chrome: { brand_label: 'Essevin' },
    },
    'open-late': {
      name: 'Essevin',
      logo: 'https://late.essevin.com/logo.svg',
      doc_url: 'https://late.essevin.com/docs',
      host: 'late.essevin.com',
    },
    kite: { name: 'KITE', logo: 'data:image/svg+xml;base64,AAAA', doc_url: 'https://kite.essevin.com/docs' },
  });

  it('parse → serialize 语义等价（含嵌套 blog_chrome）', () => {
    const round = serializeSitesBranding(parseSitesBranding(prod));
    expect(JSON.parse(round)).toEqual(JSON.parse(prod));
  });

  it('生产值本身无校验错误', () => {
    expect(validateSitesBranding(parseSitesBranding(prod), t)).toEqual([]);
  });

  it('带短横线的站点 ID 合法', () => {
    const errs = validateSitesBranding(parseSitesBranding(prod), t);
    expect(errs.some((e) => e.includes('open-late'))).toBe(false);
  });

  it('站点 ID 非法字符被判错', () => {
    const rows = parseSitesBranding('{"bad site": {"name": "X"}}');
    expect(validateSitesBranding(rows, t).some((e) => e.startsWith('settings.sites_branding_err_key_format'))).toBe(true);
  });

  it('blog_chrome 写坏时被判错且不写进配置', () => {
    const rows = parseSitesBranding('{"ink": {"name": "Essevin"}}');
    const [row] = rows;
    if (!row) throw new Error('expected parsed branding row');
    row.blogChrome = '{ not json';
    expect(validateSitesBranding(rows, t).some((e) => e.startsWith('settings.sites_branding_err_chrome'))).toBe(true);
    const out = JSON.parse(serializeSitesBranding(rows)) as Record<string, Record<string, unknown>>;
    expect(out['ink']).not.toHaveProperty('blog_chrome');
  });

  it('缺品牌名被判错', () => {
    const rows = parseSitesBranding('{"ink": {"logo": "https://x/y.svg"}}');
    expect(validateSitesBranding(rows, t).some((e) => e.startsWith('settings.sites_branding_err_name'))).toBe(true);
  });

  it('原始 JSON 结构错误会阻塞保存', () => {
    expect(isSitesBrandingShapeValid('{"ink":null}')).toBe(false);
    expect(validateSitesBranding([], t, true)).toContain('settings.sites_branding_invalid');
  });

  it('保留单站未知扩展字段', () => {
    const raw = '{"ink":{"name":"Essevin","future_option":{"enabled":true}}}';
    expect(JSON.parse(serializeSitesBranding(parseSitesBranding(raw)))).toEqual(JSON.parse(raw));
  });
});

describe('blog_sites 圆环往返', () => {
  const prod = '[{"key":"essevin","label":"Essevin 主站"},{"key":"kite","label":"KITE"}]';

  it('parse → serialize 语义等价', () => {
    expect(JSON.parse(serializeBlogSites(parseBlogSites(prod)))).toEqual(JSON.parse(prod));
  });

  it('生产值本身无校验错误', () => {
    expect(validateBlogSites(parseBlogSites(prod), t)).toEqual([]);
  });

  it('重复 key 被判错', () => {
    const rows = parseBlogSites('[{"key":"a","label":"A"},{"key":"a","label":"B"}]');
    expect(validateBlogSites(rows, t).some((e) => e.startsWith('settings.blog_sites_err_key_dup'))).toBe(true);
  });

  it('全空行视为待填，不报错也不写出', () => {
    const rows = parseBlogSites('[{"key":"a","label":"A"}]');
    rows.push({ uid: 999, key: '', label: '' });
    expect(validateBlogSites(rows, t)).toEqual([]);
    expect(JSON.parse(serializeBlogSites(rows))).toHaveLength(1);
  });

  it('只填名称缺 key 被判错', () => {
    const rows = parseBlogSites('[]');
    rows.push({ uid: 1, key: '', label: '有名字没 key' });
    expect(validateBlogSites(rows, t).some((e) => e === 'settings.blog_sites_err_key_empty')).toBe(true);
  });

  it('原始 JSON 结构错误会阻塞保存', () => {
    expect(isBlogSitesShapeValid('{"key":"a"}')).toBe(false);
    expect(validateBlogSites([], t, true)).toContain('settings.blog_sites_invalid');
  });

  it('保留单站未知扩展字段', () => {
    const raw = '[{"key":"essevin","label":"Essevin","future_option":true}]';
    expect(JSON.parse(serializeBlogSites(parseBlogSites(raw)))).toEqual(JSON.parse(raw));
  });
});

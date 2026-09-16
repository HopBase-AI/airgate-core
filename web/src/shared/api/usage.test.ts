import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// 后端 internal/i18n.DetectLanguage 只认这五个标签(按小写前缀匹配 en / zh-hk|zh-tw|
// zh-hant|zh-mo / zh / es / ja),其余一律回落 en。前端发出去的值必须落在这个集合内,
// 否则导出的 CSV 会悄悄退回英文表头。
const BACKEND_TAGS = ['en', 'zh', 'zh-HK', 'ja', 'es'];

function csvResponse(): Response {
  return new Response('time,model\n', {
    status: 200,
    headers: {
      'Content-Type': 'text/csv; charset=utf-8',
      'Content-Disposition': 'attachment; filename="usage-20260916.csv"',
    },
  });
}

function headersOf(fetchMock: ReturnType<typeof vi.fn>): Record<string, string> {
  const call = fetchMock.mock.calls[0];
  if (!call) throw new Error('exportCsv 没有发出请求');
  const init = call[1] as RequestInit | undefined;
  return (init?.headers ?? {}) as Record<string, string>;
}

// 每个用例都在干净的模块注册表里重新 import:i18n 实例是模块单例,
// 不重置的话上一个用例切过的语言会漏到下一个用例。
async function exportWith(language?: string) {
  const fetchMock = vi.fn().mockResolvedValue(csvResponse());
  vi.stubGlobal('fetch', fetchMock);

  const { default: i18n } = await import('../../i18n');
  if (language) await i18n.changeLanguage(language);

  const { usageApi } = await import('./usage');
  await usageApi.exportCsv({ start_time: '2026-09-01T00:00:00Z' });

  return { headers: headersOf(fetchMock), fetchMock, uiLanguage: i18n.language };
}

describe('usageApi.exportCsv Accept-Language', () => {
  beforeEach(() => {
    vi.resetModules();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('sends the console UI language instead of letting the browser decide', async () => {
    const { headers } = await exportWith();
    // 无存储无浏览器语言时界面语言为 en,导出请求就该带 en——关键是头必须存在,
    // 缺头时后端读到的是浏览器的 Accept-Language。
    expect(headers['Accept-Language']).toBe('en');
  });

  it.each([
    ['ja', 'ja'],
    ['es', 'es'],
    ['zh', 'zh'],
    // 繁体不能被压成 zh:后端对 zh 与 zh-HK 是两套文案。
    ['zh-HK', 'zh-HK'],
    ['en', 'en'],
  ])('follows the language switch to %s', async (language, expected) => {
    const { headers } = await exportWith(language);
    expect(headers['Accept-Language']).toBe(expected);
    expect(BACKEND_TAGS).toContain(headers['Accept-Language']);
  });

  it('keeps the Authorization header alongside Accept-Language', async () => {
    const fetchMock = vi.fn().mockResolvedValue(csvResponse());
    vi.stubGlobal('fetch', fetchMock);

    const client = await import('./client');
    client.setToken('header.payload.signature');
    const { default: i18n } = await import('../../i18n');
    await i18n.changeLanguage('ja');

    const { usageApi } = await import('./usage');
    const result = await usageApi.exportCsv({ start_time: '2026-09-01T00:00:00Z' });

    const headers = headersOf(fetchMock);
    expect(headers['Authorization']).toBe('Bearer header.payload.signature');
    expect(headers['Accept-Language']).toBe('ja');
    expect(result.filename).toBe('usage-20260916.csv');
  });
});

describe('acceptLanguage tag mapping', () => {
  beforeEach(() => {
    vi.resetModules();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it.each([
    ['zh', 'zh'],
    ['zh-CN', 'zh'],
    ['zh-Hans', 'zh'],
    ['zh-HK', 'zh-HK'],
    ['zh-TW', 'zh-HK'],
    ['zh-Hant', 'zh-HK'],
    ['en', 'en'],
    ['en-US', 'en'],
    ['ja', 'ja'],
    // 带地区后缀的取值退回主语言,别直接甩 ja-JP 让后端去猜。
    ['ja-JP', 'ja'],
    ['es', 'es'],
    ['es-MX', 'es'],
    // 不支持的语言回落 en,与后端网关口径一致。
    ['fr-FR', 'en'],
    ['', 'en'],
  ])('maps %s to %s', async (input, expected) => {
    const { acceptLanguage } = await import('../../i18n');
    expect(acceptLanguage(input)).toBe(expected);
    expect(BACKEND_TAGS).toContain(acceptLanguage(input));
  });
});

import { describe, expect, it } from 'vitest';
import { EXPORT_MAX_WINDOW_MS, usageFilterEndToRFC3339, usageFilterStartToRFC3339 } from './usageExportRange';

// 用固定偏移的本地时区断言会随 CI 机器时区漂，这里改为断言「与本地解析一致」的关系，
// 既覆盖时区正确性，又不绑定具体 TZ。
describe('usageFilterStartToRFC3339', () => {
  it('带时间的串按本地时区解析', () => {
    expect(usageFilterStartToRFC3339('2026-09-03T14:30:05'))
      .toBe(new Date('2026-09-03T14:30:05').toISOString());
  });

  it('纯日期按本地零点补齐（不能被当成 UTC 零点）', () => {
    expect(usageFilterStartToRFC3339('2026-09-03'))
      .toBe(new Date('2026-09-03T00:00:00').toISOString());
  });

  it('空值与非法值返回 undefined', () => {
    expect(usageFilterStartToRFC3339(undefined)).toBeUndefined();
    expect(usageFilterStartToRFC3339('')).toBeUndefined();
    expect(usageFilterStartToRFC3339('not-a-date')).toBeUndefined();
  });
});

describe('usageFilterEndToRFC3339', () => {
  it('右界推后 1 秒，抵消导出端点的左闭右开', () => {
    const end = usageFilterEndToRFC3339('2026-09-03T18:00:00');
    expect(end).toBe(new Date(new Date('2026-09-03T18:00:00').getTime() + 1000).toISOString());
    // 选到 18:00:00 时，18:00:00 那一秒的记录必须落在导出区间内
    expect(new Date('2026-09-03T18:00:00').getTime()).toBeLessThan(new Date(end!).getTime());
  });

  it('纯日期补到当日 23:59:59 再加 1 秒 = 次日零点', () => {
    expect(usageFilterEndToRFC3339('2026-09-03'))
      .toBe(new Date('2026-09-04T00:00:00').toISOString());
  });

  it('空值返回 undefined（后端缺省用「现在」）', () => {
    expect(usageFilterEndToRFC3339(undefined)).toBeUndefined();
  });
});

describe('EXPORT_MAX_WINDOW_MS', () => {
  it('与后端 exportMaxWindow(400 天) 一致', () => {
    expect(EXPORT_MAX_WINDOW_MS / (24 * 60 * 60 * 1000)).toBe(400);
  });
});

import { describe, expect, it } from 'vitest';
import {
  buildUsageVerification,
  currencySymbol,
  formatDiscount,
  formatDivisor,
  formatNativeAmount,
  hasDivisor,
  verificationFormula,
  type UsageRowWithOfficialNative,
} from './usageListPrice';
import en from '../i18n/en.json';
import es from '../i18n/es.json';
import ja from '../i18n/ja.json';
import zh from '../i18n/zh.json';
import zhHK from '../i18n/zh-HK.json';

// 验收算例取自 docs/pricing-list-verification-sop.md §7（USD 账本那一档）：
// 通义 ¥12 / 1M input、10,000 input tokens、7 折 →
//   actual_cost = 12 × 0.01 × 0.7 ÷ 6.8 = 0.012353
// 美元基准价 12 ÷ 6.8 = 1.7647，account_cost = 1.7647 × 0.01 = 0.017647。
// 本文件只测展示层：discount / divisor 由后端按账本口径算好下发，前端一分不算。
function qwenRow(): UsageRowWithOfficialNative {
  return {
    actual_cost: 0.012353,
    usage_cost_details: [
      {
        key: 'input_tokens',
        label: '输入 Token',
        account_cost: 0.017647,
        metadata: {
          unit: 'USD/1M tokens',
          unit_price: '1.7647',
          list_currency: 'CNY',
          list_unit_price: '12',
          list_fx: '6.8',
        },
      },
      {
        key: 'output_tokens',
        label: '输出 Token',
        account_cost: 0,
        metadata: {
          unit: 'USD/1M tokens',
          unit_price: '5.2941',
          list_currency: 'CNY',
          list_unit_price: '36',
          list_fx: '6.8',
        },
      },
    ],
    usage_metadata: { list_currency: 'CNY', list_fx: '6.8' },
    official_native: { currency: 'CNY', fx: 6.8, cost: 0.12, discount: 0.7, divisor: 6.8, ledger_currency: 'USD' },
  };
}

describe('官方牌价验算块：渲染与不渲染', () => {
  it('有 official_native 即给出验算数据与各档牌价单价', () => {
    const verification = buildUsageVerification(qwenRow());
    expect(verification).not.toBeNull();
    expect(verification?.official).toEqual({ currency: 'CNY', fx: 6.8, cost: 0.12, discount: 0.7, divisor: 6.8, ledger_currency: 'USD' });
    expect(verification?.showDivisor).toBe(true);
    expect(verification?.actualCost).toBe(0.012353);
    expect(verification?.unitPrices).toEqual([
      {
        rowKey: 'input_tokens',
        labelKey: 'usage.input_unit_price',
        fallbackLabel: '输入 Token',
        price: 12,
        currency: 'CNY',
        unitKey: 'usage.per_million_tokens',
      },
      {
        rowKey: 'output_tokens',
        labelKey: 'usage.output_unit_price',
        fallbackLabel: '输出 Token',
        price: 36,
        currency: 'CNY',
        unitKey: 'usage.per_million_tokens',
      },
    ]);
  });

  it('后端未下发 official_native 的行一律不渲染验算块', () => {
    // 历史行（切 USD 前没有快照）、官方价本就是美元的模型、API Key 会话都走这条路。
    const row = qwenRow();
    delete row.official_native;
    expect(buildUsageVerification(row)).toBeNull();
    expect(buildUsageVerification(undefined)).toBeNull();
  });

  it('币种或账本除数缺失时宁可不展示，也不给半截等式', () => {
    const noCurrency = { ...qwenRow(), official_native: { currency: '', fx: 6.8, cost: 0.12, discount: 0.7, divisor: 6.8, ledger_currency: 'USD' } };
    expect(buildUsageVerification(noCurrency)).toBeNull();
    // 除数是等式里唯一必需的那个数，缺了它凑不齐。
    const noDivisor = { ...qwenRow(), official_native: { currency: 'CNY', fx: 6.8, cost: 0.12, discount: 0.7, divisor: 0, ledger_currency: 'USD' } };
    expect(buildUsageVerification(noDivisor)).toBeNull();
  });

  it('折缺失按 1 记：否则会显示「折扣 0」却扣了钱', () => {
    const row = { ...qwenRow(), official_native: { currency: 'CNY', fx: 6.8, cost: 0.12, discount: 0, divisor: 6.8, ledger_currency: 'USD' } };
    expect(buildUsageVerification(row)?.official.discount).toBe(1);
  });

  it('¥ 账本（除数 1）：折算率整行隐藏，实扣按账本币种标 ¥', () => {
    // 生产当前口径：组 27 可灵 75 折，倍率 5.1，后端还原成折 0.75、除数 6.8 ÷ 6.8 = 1。
    const row: UsageRowWithOfficialNative = {
      actual_cost: 2.25,
      usage_metrics: [{
        key: 'duration', label: '视频时长', value: 5, account_cost: 0.441176,
        metadata: { price_per_sec: '0.0882353', list_currency: 'CNY', list_unit_price: '0.6', list_fx: '6.8' },
      }],
      official_native: { currency: 'CNY', fx: 6.8, cost: 3, discount: 0.75, divisor: 1, ledger_currency: 'CNY' },
    };
    const verification = buildUsageVerification(row);
    if (!verification) throw new Error('¥ 账本的行应产出验算块');
    expect(verification.showDivisor).toBe(false);
    expect(verification.official.ledger_currency).toBe('CNY');
    // 等式收缩成「官方费用 × 折」，自己就闭合：¥3.00 × 0.75 = ¥2.25。
    expect(verificationFormula(verification.official)).toBe('¥3.0000 × 0.75');
    expect(verification.official.cost * verification.official.discount).toBeCloseTo(verification.actualCost, 6);
  });

  it('账本币种缺省回落原币，不硬标 $', () => {
    const row = { ...qwenRow(), official_native: { currency: 'CNY', fx: 6.8, cost: 0.12, discount: 0.7, divisor: 1, ledger_currency: '' } };
    expect(buildUsageVerification(row)?.official.ledger_currency).toBe('CNY');
  });

  it('明细没带快照时才回退 metric，绝不双份累加', () => {
    const row: UsageRowWithOfficialNative = {
      actual_cost: 0.330882,
      // 可灵 / 万相只上报 metric，不上报 cost detail。
      usage_cost_details: [{ key: 'video', label: '视频', account_cost: 0.441176 }],
      usage_metrics: [
        {
          key: 'duration',
          label: '视频时长',
          value: 5,
          account_cost: 0.441176,
          metadata: { price_per_sec: '0.0882353', list_currency: 'CNY', list_unit_price: '0.6', list_fx: '6.8' },
        },
      ],
      official_native: { currency: 'CNY', fx: 6.8, cost: 3, discount: 0.75, divisor: 6.8, ledger_currency: 'USD' },
    };
    const verification = buildUsageVerification(row);
    expect(verification?.unitPrices).toHaveLength(1);
    expect(verification?.unitPrices[0]).toMatchObject({
      price: 0.6,
      currency: 'CNY',
      unitKey: 'usage.per_second',
      // 可灵的明细 key 不是 token 档，回落插件自带 label
      labelKey: undefined,
      fallbackLabel: '视频时长',
    });
  });

  it('量纲随明细的美元单价键：秒 / 张 / 次 / 百万 token', () => {
    const unitOf = (metadata: Record<string, string>) => buildUsageVerification({
      official_native: { currency: 'CNY', fx: 6.8, cost: 1, discount: 1, divisor: 6.8, ledger_currency: 'USD' },
      usage_cost_details: [{ key: 'x', label: 'x', account_cost: 1, metadata }],
    })?.unitPrices[0]?.unitKey;
    const snapshot = { list_currency: 'CNY', list_unit_price: '1', list_fx: '6.8' };
    expect(unitOf({ ...snapshot, price_per_second: '0.1' })).toBe('usage.per_second');
    expect(unitOf({ ...snapshot, price_per_image: '0.1' })).toBe('usage.per_image');
    expect(unitOf({ ...snapshot, price_per_call: '0.1' })).toBe('usage.per_call');
    expect(unitOf({ ...snapshot, price_per_million: '0.1' })).toBe('usage.per_million_tokens');
    // 插件没写美元单价（按次追加的服务器工具费）时按 token 口径兜底，不空着。
    expect(unitOf(snapshot)).toBe('usage.per_million_tokens');
  });

  it('一行内混币种时只保留与 official_native 同币的单价行', () => {
    const row = qwenRow();
    row.usage_cost_details?.push({
      key: 'tool',
      label: '服务器工具调用',
      account_cost: 0.01,
      metadata: { unit_price: '1', list_currency: 'USD', list_unit_price: '1', list_fx: '1' },
    });
    expect(buildUsageVerification(row)?.unitPrices.map((item) => item.currency)).toEqual(['CNY', 'CNY']);
  });

  it('快照单价缺失或为零的明细不铺单价行', () => {
    const row = qwenRow();
    row.usage_cost_details = [
      { key: 'input_tokens', label: '输入', account_cost: 0.01, metadata: { list_currency: 'CNY', list_unit_price: '0', list_fx: '6.8' } },
      { key: 'output_tokens', label: '输出', account_cost: 0.01, metadata: { unit_price: '1' } },
    ];
    expect(buildUsageVerification(row)?.unitPrices).toEqual([]);
  });
});

describe('金额格式化', () => {
  it('币种符号按 list_currency 映射，认不出的币种不硬标 $', () => {
    expect(currencySymbol('CNY')).toBe('¥');
    expect(currencySymbol('usd')).toBe('$');
    expect(currencySymbol('KRW')).toBe('KRW ');
    expect(currencySymbol(undefined)).toBe('');
  });

  it('原币金额固定四位小数，客户可与官网牌价逐位对照', () => {
    expect(formatNativeAmount(0.156, 'CNY')).toBe('¥0.1560');
    expect(formatNativeAmount(0.12, 'CNY')).toBe('¥0.1200');
    expect(formatNativeAmount(12, 'USD')).toBe('$12.0000');
    expect(formatNativeAmount(Number.NaN, 'CNY')).toBe('¥0.0000');
  });

  it('折两位、账本除数不补零', () => {
    expect(formatDiscount(0.7)).toBe('0.70');
    expect(formatDiscount(1)).toBe('1.00');
    expect(formatDivisor(6.8)).toBe('6.8');
    expect(formatDivisor(0)).toBe('');
  });

  it('除数为 1 = 不发生折算，折算率行与「÷」都省掉', () => {
    expect(hasDivisor(1)).toBe(false);
    expect(hasDivisor(6.8)).toBe(true);
    expect(hasDivisor(0)).toBe(false);
    // 日元牌价在 ¥ 账本下除数是 150 ÷ 6.8，不是 1——写死 1 的实现会在这里红。
    expect(hasDivisor(150 / 6.8)).toBe(true);
  });

  it('验算式与 SOP §4.1 模板一致（两种账本各一式）', () => {
    // USD 账本：官方费用 ¥ → 实扣 $，要除。
    expect(verificationFormula({ currency: 'CNY', fx: 6.8, cost: 0.156, discount: 0.7, divisor: 6.8, ledger_currency: 'USD' }))
      .toBe('¥0.1560 × 0.70 ÷ 6.8');
    // ¥ 账本：官方费用与实扣同币，不除。
    expect(verificationFormula({ currency: 'CNY', fx: 6.8, cost: 0.156, discount: 0.7, divisor: 1, ledger_currency: 'CNY' }))
      .toBe('¥0.1560 × 0.70');
  });
});

describe('验算块文案五语齐备', () => {
  // docs/i18n-sop.md：新增用户可见文案必须五包同批落地，不许靠回退兜底。
  const KEYS = [
    'official_list_price', 'official_cost_native', 'discount', 'actual_charged',
    'verify_formula', 'list_fx', 'per_million_tokens', 'per_second', 'per_image', 'per_call',
  ];
  const PACKS: Record<string, { usage: Record<string, string> }> = { zh, 'zh-HK': zhHK, en, ja, es };

  it.each(Object.entries(PACKS))('%s 有全部验算块 key 且非空', (_lang, pack) => {
    for (const key of KEYS) {
      expect(pack.usage[key], key).toBeTruthy();
    }
  });

  it('模型广场牌价小字模板五语齐备且带 {{price}} 占位', () => {
    for (const [lang, pack] of Object.entries(PACKS)) {
      const plaza = (pack as unknown as { model_plaza: Record<string, string> }).model_plaza;
      expect(plaza.list_price, lang).toContain('{{price}}');
      expect(plaza.list_price_title, lang).toBeTruthy();
    }
  });
});

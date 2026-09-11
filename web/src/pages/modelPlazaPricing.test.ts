import { describe, expect, it } from 'vitest';
import {
  formatModelPrice,
  hasFixedImagePricingBuckets,
  hasFixedImageTierPrices,
  isPerSecondPricing,
  priceUnitOf,
  resolveBucketDiscount,
  officialPriceSymbol,
  resolveFixedImageTierPrices,
  resolvePlazaFixedImageTiers,
  videoBucketLabel,
  videoBucketRank,
  videoPriceCopyKeys,
} from './modelPlazaPricing';
import en from '../i18n/en.json';
import zh from '../i18n/zh.json';

describe('model plaza price formatting', () => {
  it('preserves sub-cent image prices and their discounts', () => {
    expect(formatModelPrice(0.035)).toBe('$0.035');
    expect(formatModelPrice(0.035 * 4.624 / 6.8)).toBe('$0.0238');
    expect(formatModelPrice(0.04 * 4.624 / 6.8)).toBe('$0.0272');
    expect(formatModelPrice(0.045 * 4.624 / 6.8)).toBe('$0.0306');
  });

  it('keeps ordinary prices compact', () => {
    expect(formatModelPrice(1.5)).toBe('$1.5');
    expect(formatModelPrice(0.125, '¥')).toBe('¥0.125');
  });

  it('does not present invalid or zero values as prices', () => {
    expect(formatModelPrice(0)).toBe('—');
    expect(formatModelPrice(Number.NaN)).toBe('—');
  });

  it('presents an explicit zero fixed price as free instead of missing', () => {
    expect(formatModelPrice(0, '¥', true)).toBe('¥0');
  });
});

describe('fixed image prices', () => {
  it('displays all effective 1K/2K/4K prices in CNY without a token multiplier', () => {
    expect(resolveFixedImageTierPrices({
      image_price_1k: 0.08,
      image_price_2k: 0.12,
      image_price_4k: 0.15,
    }, 6.8, 'CNY')).toEqual([
      { tier: '1k', sale: 0.08, billingMode: 'fixed' },
      { tier: '2k', sale: 0.12, billingMode: 'fixed' },
      { tier: '4k', sale: 0.15, billingMode: 'fixed' },
    ]);
  });

  it('converts fixed balance prices and marks missing tiers as token fallback', () => {
    const prices = resolveFixedImageTierPrices({
      image_price_1k: 0.068,
      image_price_2k: Number.NaN,
    }, 6.8, 'USD');
    expect(prices).toEqual([
      { tier: '1k', sale: 0.01, billingMode: 'fixed' },
      { tier: '2k', sale: null, billingMode: 'token' },
      { tier: '4k', sale: null, billingMode: 'token' },
    ]);
  });

  it('treats zero as a configured fixed tier and rejects non-finite tiers', () => {
    expect(hasFixedImageTierPrices({ image_price_1k: 0 })).toBe(true);
    expect(hasFixedImageTierPrices({
      image_price_1k: Number.NaN,
      image_price_2k: Number.POSITIVE_INFINITY,
    })).toBe(false);
    expect(resolveFixedImageTierPrices({ image_price_1k: 0 }, 6.8, 'CNY')[0]).toEqual({
      tier: '1k', sale: 0, billingMode: 'fixed',
    });
  });

  it('never derives a token discount for fixed image pricing', () => {
    expect(resolveBucketDiscount(0.6, 6.8, true)).toBeNull();
    expect(resolveBucketDiscount(0.6, 6.8, false)).toBeCloseTo(0.6 / 6.8);
  });

  it('keeps the discount path for ordinary per-image pricing buckets', () => {
    expect(hasFixedImagePricingBuckets([
      { imageBillingMode: undefined },
      { imageBillingMode: undefined },
    ])).toBe(false);
    expect(resolveBucketDiscount(0.6, 6.8, false)).toBeCloseTo(0.6 / 6.8);
    expect(hasFixedImagePricingBuckets([{ imageBillingMode: 'fixed' }])).toBe(true);
  });
});

// 固定图价来自分组配置（groups.plugin_settings.openai.image_price_*），是某个分组的
// 成交价而不是官方牌价。ToB 逐客户单独报价，广场切「只展示官方基准价」后一旦漏铺，
// 就等于把某客户的成交价公开挂出去。
describe('官方基准价口径下的固定图价', () => {
  const model = { image_price_1k: 0.4, image_price_2k: 0.4, image_price_4k: 0.4 };

  it('展示实付价时照常铺出三档', () => {
    expect(resolvePlazaFixedImageTiers(model, 6.8, 'CNY', true)).toEqual([
      { tier: '1k', sale: 0.4, billingMode: 'fixed' },
      { tier: '2k', sale: 0.4, billingMode: 'fixed' },
      { tier: '4k', sale: 0.4, billingMode: 'fixed' },
    ]);
  });

  it('官方基准价口径下一张都不铺', () => {
    expect(resolvePlazaFixedImageTiers(model, 6.8, 'CNY', false)).toEqual([]);
    expect(resolvePlazaFixedImageTiers(model, 6.8, 'USD', false)).toEqual([]);
  });
});

// 人民币牌价模型（GLM 等，currency="CNY"）的基准价数字本身就是 ¥。
// 广场切到「只展示官方基准价」后全站都走这条路，标错币种就是把 ¥1.4 报成 $1.4。
describe('基准价币种符号', () => {
  it('人民币牌价模型用 ¥', () => {
    expect(officialPriceSymbol({ currency: 'CNY' })).toBe('¥');
  });

  it('官方美元价模型与未声明币种的模型用 $', () => {
    expect(officialPriceSymbol({ currency: 'USD' })).toBe('$');
    expect(officialPriceSymbol({})).toBe('$');
  });
});

// ──────────────────────── 视频计价单位 ────────────────────────
//
// 回归的是一条真实的客户侧错价展示：wan3.0-video 720P 的 $0.088235 其实是
// **每秒**，广场却统一标「/ 1M video tokens」——15 秒的片子实际 $1.32，
// 按 token 读会被少估两个数量级。单位由插件经 price.unit 声明、core 下发 price_unit，
// seedance 不声明则回落 token，既有展示零回归。
function translator(pack: Record<string, Record<string, string>>) {
  return (key: string): string => {
    const [namespace, leaf] = key.split('.');
    return pack[namespace ?? '']?.[leaf ?? ''] ?? key;
  };
}

const tZh = translator(zh as unknown as Record<string, Record<string, string>>);
const tEn = translator(en as unknown as Record<string, Record<string, string>>);

describe('视频计价单位', () => {
  const perSecondModel = { price_unit: 'second' };
  const tokenModel = { price_unit: 'token' };

  it('未下发 price_unit 的老后端按 token 兜底', () => {
    expect(priceUnitOf({})).toBe('token');
    expect(priceUnitOf({ price_unit: ' Second ' })).toBe('second');
    expect(isPerSecondPricing({})).toBe(false);
    expect(isPerSecondPricing(perSecondModel)).toBe(true);
  });

  it('按秒计费的模型给出「每秒」抬头与秒数脚注，绝不出现 video tokens', () => {
    const { unitKey, noteKey } = videoPriceCopyKeys(perSecondModel);
    expect(tZh(unitKey)).toBe('计费单价 · 每秒');
    expect(tZh(noteKey)).toContain('单价 × 计费秒数');
    expect(tZh(unitKey)).not.toContain('video tokens');
    expect(tEn(unitKey)).toBe('Billing rate · per second');
    expect(tEn(unitKey).toLowerCase()).not.toContain('token');
  });

  it('按 token 计费的模型（seedance）维持原文案', () => {
    const { unitKey, noteKey } = videoPriceCopyKeys(tokenModel);
    expect(tZh(unitKey)).toBe('计费单价 · / 1M video tokens');
    expect(tZh(noteKey)).toContain('video tokens');
    expect(videoPriceCopyKeys({})).toEqual(videoPriceCopyKeys(tokenModel));
  });
});

describe('视频桶标签', () => {
  it('seedance 的参考图维度维持文生 / 图生视频文案', () => {
    expect(videoBucketLabel('480p_no_ref', tZh)).toBe('480P · 文生视频');
    expect(videoBucketLabel('1080p_with_ref', tZh)).toBe('1080P · 图生视频');
  });

  it('可灵的有无声 / 有无参考视频维度不再被压成同一行', () => {
    expect(videoBucketLabel('720p_silent_noref', tZh)).toBe('720P · 无声 · 无参考视频');
    expect(videoBucketLabel('720p_audio_noref', tZh)).toBe('720P · 有声 · 无参考视频');
    expect(videoBucketLabel('1080p_silent_ref', tZh)).toBe('1080P · 无声 · 含参考视频');
    expect(videoBucketLabel('4k_voice_noref', tEn)).toBe('4K · Preset voice · No reference video');
  });

  it('特例桶与只带分辨率的桶不再硬套「文生视频」', () => {
    expect(videoBucketLabel('motion_control_1080p', tZh)).toBe('动作控制 · 1080P');
    expect(videoBucketLabel('multi_elements_720p', tZh)).toBe('多元素编辑 · 720P');
    expect(videoBucketLabel('avatar_720p', tZh)).toBe('数字人 · 720P');
    expect(videoBucketLabel('lip_sync', tZh)).toBe('对口型');
    expect(videoBucketLabel('reference_image', tZh)).toBe('参考图 / 张');
    // 海螺 / 万相 / 快乐马：桶名只有分辨率
    expect(videoBucketLabel('768p', tZh)).toBe('768P');
    expect(videoBucketLabel('480p', tZh)).toBe('480P');
    // Grok：桶名自带 per_second，单位由抬头统一说明
    expect(videoBucketLabel('720p_per_second', tZh)).toBe('720P');
    // 认不出的桶原样大写，不编造维度
    expect(videoBucketLabel('mystery_bucket', tZh)).toBe('MYSTERY_BUCKET');
  });

  it('展示序：分辨率低→高、无声→有声、无参考→有参考，特例桶垫底', () => {
    const sorted = [
      'motion_control_720p', '4k_silent_noref', '720p_audio_noref',
      '720p_silent_noref', 'lip_sync', '1080p_silent_noref', '768p',
    ].sort((a, b) => videoBucketRank(a) - videoBucketRank(b));
    expect(sorted).toEqual([
      '720p_silent_noref', '720p_audio_noref', '768p', '1080p_silent_noref',
      '4k_silent_noref', 'motion_control_720p', 'lip_sync',
    ]);
    expect(['480p_with_ref', '480p_no_ref'].sort((a, b) => videoBucketRank(a) - videoBucketRank(b)))
      .toEqual(['480p_no_ref', '480p_with_ref']);
  });
});

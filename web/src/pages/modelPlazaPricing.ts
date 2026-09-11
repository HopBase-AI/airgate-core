export function formatModelPrice(value: number, symbol: '$' | '¥' = '$', allowZero = false): string {
  if (!Number.isFinite(value) || value < 0 || (!allowZero && value === 0)) return '—';

  const rounded = Math.round((value + Number.EPSILON) * 1_000_000) / 1_000_000;
  return `${symbol}${rounded.toLocaleString(undefined, { maximumFractionDigits: 6 })}`;
}

export interface FixedImagePriceModel {
  image_price_1k?: number;
  image_price_2k?: number;
  image_price_4k?: number;
}

export interface FixedImageTierPrice {
  tier: '1k' | '2k' | '4k';
  sale: number | null;
  billingMode: 'fixed' | 'token';
}

export interface ImageBillingBucket {
  imageBillingMode?: 'fixed' | 'token';
}

function validFixedImagePrice(price: number | undefined): price is number {
  return typeof price === 'number' && Number.isFinite(price) && price >= 0;
}

export function hasFixedImageTierPrices(model: FixedImagePriceModel): boolean {
  return [model.image_price_1k, model.image_price_2k, model.image_price_4k]
    .some(validFixedImagePrice);
}

// API 返回的固定图价是余额/CNY 单位；ToC 美元视图按站点汇率换算，ToB 人民币视图直接展示。
export function resolveFixedImageTierPrices(
  model: FixedImagePriceModel,
  fx: number,
  saleCurrency: 'CNY' | 'USD',
): FixedImageTierPrice[] {
  const safeFX = Number.isFinite(fx) && fx > 0 ? fx : 6.8;
  const tiers: Array<[FixedImageTierPrice['tier'], number | undefined]> = [
    ['1k', model.image_price_1k],
    ['2k', model.image_price_2k],
    ['4k', model.image_price_4k],
  ];
  if (!tiers.some(([, price]) => validFixedImagePrice(price))) return [];
  return tiers.map(([tier, price]) => validFixedImagePrice(price)
    ? { tier, sale: saleCurrency === 'CNY' ? price : price / safeFX, billingMode: 'fixed' as const }
    : { tier, sale: null, billingMode: 'token' as const });
}

// officialPriceSymbol 基准价该用哪个币种符号。
//
// currency="CNY" 表示这批 input/output 数字本身就是人民币牌价（按 1:1 记账），
// 其余是官方美元价。硬标 $ 会把 ¥1.4 说成 $1.4，凭空虚报一个汇率的倍数。
export function officialPriceSymbol(model: { currency?: string }): '$' | '¥' {
  return model.currency === 'CNY' ? '¥' : '$';
}

// resolvePlazaFixedImageTiers 是模型广场取固定图价的唯一入口。
//
// 固定图价是「分组配置的实付价」（groups.plugin_settings 的 image_price_1k/2k/4k），
// 不是官方牌价；官方基准价口径下必须一张都不铺，否则广场会把某个分组的成交价
// 当成公开价展示出去。逐客户单独报价的部署尤其不能漏。
export function resolvePlazaFixedImageTiers(
  model: FixedImagePriceModel,
  fx: number,
  saleCurrency: 'CNY' | 'USD',
  showUserPrice: boolean,
): FixedImageTierPrice[] {
  if (!showUserPrice) return [];
  return resolveFixedImageTierPrices(model, fx, saleCurrency);
}

export function resolveBucketDiscount(
  userRate: number | undefined,
  fx: number,
  hasFixedImagePricing: boolean,
): number | null {
  if (hasFixedImagePricing || typeof userRate !== 'number' || !Number.isFinite(userRate)
    || userRate <= 0 || !Number.isFinite(fx) || fx <= 0) {
    return null;
  }
  return userRate / fx;
}

export function hasFixedImagePricingBuckets(buckets: ImageBillingBucket[] | null): boolean {
  return !!buckets?.some((bucket) => bucket.imageBillingMode != null);
}

// ──────────────────────── 视频计价单位与桶标签 ────────────────────────
//
// 视频桶价的键统一叫 video_tokens 是历史遗留，**键名不代表量纲**：
// seedance 的一份是「每百万 video_tokens」，可灵 / 海螺 / 万相 / 快乐马的一份是
// 「每秒」。core 用 price_unit 下发真实单位，展示端必须据此选文案——否则
// wan3.0-video 720P 的 $0.088235/秒 会被标成 $0.088235/1M video tokens，
// 15 秒的片子（实际 $1.32）被少估两个数量级。

/** 模型携带的计价单位；老后端不下发时按 token 兜底（与该字段上线前行为一致）。 */
export function priceUnitOf(model: { price_unit?: string }): string {
  return (model.price_unit ?? '').trim().toLowerCase() || 'token';
}

/** 按视频时长计费（$/秒）而非按 token 计量。 */
export function isPerSecondPricing(model: { price_unit?: string }): boolean {
  return priceUnitOf(model) === 'second';
}

/** 视频价格区的单位抬头 / 脚注文案 key。 */
export function videoPriceCopyKeys(model: { price_unit?: string }): { unitKey: string; noteKey: string } {
  return isPerSecondPricing(model)
    ? { unitKey: 'model_plaza.video_price_unit_second', noteKey: 'model_plaza.video_price_note_second' }
    : { unitKey: 'model_plaza.video_price_unit', noteKey: 'model_plaza.video_price_note' };
}

// 分辨率展示序：低 → 高。768p（海螺）与 2k（海螺 / 可灵）漏了会被排到特例桶之后。
const VIDEO_RESOLUTIONS = ['480p', '720p', '768p', '1080p', '2k', '4k'];

// 特例桶：官方价目表里单独成行，不落在「分辨率 × 有无声 × 有无参考」的网格内。
// 顺序即展示序，统一排在常规分辨率桶之后。
const VIDEO_SPECIAL_BUCKETS = [
  { prefix: 'motion_control', labelKey: 'model_plaza.video_bucket_motion_control' },
  { prefix: 'multi_elements', labelKey: 'model_plaza.video_bucket_multi_elements' },
  { prefix: 'avatar', labelKey: 'model_plaza.video_bucket_avatar' },
  { prefix: 'lip_sync', labelKey: 'model_plaza.video_bucket_lip_sync' },
  { prefix: 'reference_image', labelKey: 'model_plaza.video_bucket_reference_image' },
];

// 有无声维度（可灵）：silent 无声 / audio 有声 / voice 指定音色。
const VIDEO_AUDIO_LABEL_KEYS: Record<string, string> = {
  silent: 'model_plaza.video_audio_silent',
  audio: 'model_plaza.video_audio_on',
  voice: 'model_plaza.video_audio_voice',
};
const VIDEO_AUDIO_ORDER = ['silent', 'audio', 'voice'];

// 参考维度两套命名：seedance 的 no_ref/with_ref 说的是参考**图**（文生 / 图生），
// 可灵的 noref/ref 说的是参考**视频**，两者不是一回事，文案不可混用。
const VIDEO_REF_LABEL_KEYS: Record<string, string> = {
  no_ref: 'model_plaza.video_no_ref',
  with_ref: 'model_plaza.video_with_ref',
  noref: 'model_plaza.video_ref_none',
  ref: 'model_plaza.video_ref_video',
};
const VIDEO_REF_WITH = new Set(['with_ref', 'ref']);

export interface VideoBucketParts {
  /** 特例桶文案 key（动作控制 / 数字人 / 对口型 / 参考图…）。 */
  specialKey?: string;
  resolution?: string;
  audio?: string;
  ref?: string;
}

/**
 * parseVideoBucket 解析桶名的展示维度。只认桶名结构，不做模型名特判：
 *   <res>                      海螺 / 万相 / 快乐马：仅分辨率
 *   <res>_per_second           Grok：桶名自带单位，展示只留分辨率
 *   <res>_{no,with}_ref        seedance：有无参考图
 *   <res>_<audio>_<ref>        可灵：分辨率 × 有无声 × 有无参考视频
 *   <special>[_<res>]          动作控制 / 多元素编辑 / 数字人 / 对口型 / 参考图
 */
export function parseVideoBucket(bucket: string): VideoBucketParts {
  const raw = bucket.trim().toLowerCase();
  for (const special of VIDEO_SPECIAL_BUCKETS) {
    if (raw === special.prefix) return { specialKey: special.labelKey };
    if (raw.startsWith(`${special.prefix}_`)) {
      const tail = raw.slice(special.prefix.length + 1);
      return VIDEO_RESOLUTIONS.includes(tail)
        ? { specialKey: special.labelKey, resolution: tail }
        : { specialKey: special.labelKey };
    }
  }
  const segments = raw.split('_');
  const resolution = segments[0] ?? '';
  if (!VIDEO_RESOLUTIONS.includes(resolution)) return {};
  const tail = segments.slice(1).join('_');
  if (tail === '' || tail === 'per_second') return { resolution };
  if (tail === 'no_ref' || tail === 'with_ref') return { resolution, ref: tail };
  const audio = segments[1] ?? '';
  const ref = segments.slice(2).join('_');
  if (VIDEO_AUDIO_LABEL_KEYS[audio] && VIDEO_REF_LABEL_KEYS[ref]) {
    return { resolution, audio, ref };
  }
  return { resolution };
}

/** videoBucketLabel 桶名 → 展示标签；认不出的桶原样大写，不硬套「文生视频」。 */
export function videoBucketLabel(bucket: string, t: (key: string) => string): string {
  const parts = parseVideoBucket(bucket);
  const segments: string[] = [];
  if (parts.specialKey) segments.push(t(parts.specialKey));
  if (parts.resolution) segments.push(parts.resolution.toUpperCase());
  if (parts.audio) segments.push(t(VIDEO_AUDIO_LABEL_KEYS[parts.audio] ?? ''));
  if (parts.ref) segments.push(t(VIDEO_REF_LABEL_KEYS[parts.ref] ?? ''));
  return segments.length > 0 ? segments.join(' · ') : bucket.toUpperCase();
}

/** videoBucketRank 展示序：常规分辨率桶（低→高、无声→有声、无参考→有参考）在前，特例桶垫底。 */
export function videoBucketRank(bucket: string): number {
  const parts = parseVideoBucket(bucket);
  const specialRank = parts.specialKey
    ? VIDEO_SPECIAL_BUCKETS.findIndex((special) => special.labelKey === parts.specialKey) + 1
    : 0;
  const resolutionIndex = parts.resolution ? VIDEO_RESOLUTIONS.indexOf(parts.resolution) : -1;
  const resolutionRank = resolutionIndex < 0 ? VIDEO_RESOLUTIONS.length : resolutionIndex;
  const audioIndex = parts.audio ? VIDEO_AUDIO_ORDER.indexOf(parts.audio) : -1;
  const audioRank = audioIndex < 0 ? 0 : audioIndex;
  const refRank = parts.ref && VIDEO_REF_WITH.has(parts.ref) ? 1 : 0;
  return specialRank * 1000 + resolutionRank * 100 + audioRank * 10 + refRank;
}

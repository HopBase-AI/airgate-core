/**
 * 使用记录的「厂商官方牌价验算块」数据层。
 *
 * 背景见 docs/pricing-list-verification-sop.md：国内厂商模型的官网标价是 ¥，
 * 插件按固定折算率折成美元基准价计费，客户看到的数字对不上官网 ¥ 牌价。后端在
 * `UserUsageLogResp.official_native` 里给出只读计算块（原币费用 / 折 / 账本除数 / 账本币种），
 * 各明细的 metadata 里带 `list_currency` / `list_unit_price` / `list_fx` 单价快照。
 *
 * 验算式：`官方费用(原币) × 折 ÷ 账本除数 = 实扣(账本币种)`。除数为 1 时不发生折算
 * （账本币种与牌价币种相同），展示层隐藏折算率那一行——摆一个「÷1」只会让人以为漏读了什么。
 *
 * 本模块只做「读快照 → 整理成展示行」，**一分钱都不自己算**：
 * 金额一律取后端的 official_native，单价一律取明细快照。tooltip、插件渲染器、
 * 导出三处若各算各的必然漂移（SOP §4.1 明写）。
 *
 * 红线：这里出现的一切文案只提「厂商官方牌价」，不得引入上游通道 / 账号 / 供应商
 * （docs/upstream-identity-egress-sop.md）。
 */

import type { OfficialNativeCost, UsageCostDetail, UsageMetric } from './types';

export type { OfficialNativeCost };

/** 带 official_native 的行（只有普通用户视角的 DTO 才有；API Key 会话按 SOP §6.3 不下发）。 */
export interface UsageRowWithOfficialNative {
  actual_cost?: number;
  usage_cost_details?: UsageCostDetail[];
  usage_metrics?: UsageMetric[];
  usage_metadata?: Record<string, string>;
  official_native?: OfficialNativeCost;
}

/** 单档官方牌价单价行。 */
export interface ListUnitPriceRow {
  /** React key，取明细的 key/label，缺省按序号。 */
  rowKey: string;
  /** 命中已知档位时的 i18n 键（输入 / 输出单价）；未命中为空。 */
  labelKey?: string;
  /** labelKey 缺省时的兜底文案：明细自带的 label（插件原文）。 */
  fallbackLabel: string;
  /** 原币单价。 */
  price: number;
  /** 原币币种。 */
  currency: string;
  /** 量纲文案 i18n 键：usage.per_million_tokens / per_second / per_image / per_call。 */
  unitKey: string;
}

/** 验算块的完整展示数据。 */
export interface UsageVerification {
  official: OfficialNativeCost;
  /** 实扣金额（取自本行 actual_cost，不再自乘），币种是 official.ledger_currency。 */
  actualCost: number;
  /** 各档官方牌价单价；快照缺单价时为空数组，此时只展示金额行。 */
  unitPrices: ListUnitPriceRow[];
  /** 是否显示「折算率」行：除数为 1 时账本与牌价同币，不发生折算，整行隐藏。 */
  showDivisor: boolean;
}

/** 用量快照键名，与后端 internal/pkg/listprice 的常量一一对应。 */
const SNAPSHOT_CURRENCY = 'list_currency';
const SNAPSHOT_UNIT_PRICE = 'list_unit_price';

/**
 * 各插件写在同一条明细 metadata 里的「美元单价」键，顺序即优先级。
 * list_unit_price 与其中之一同量纲，所以命中哪个键就决定了单位文案。
 * 与后端 usdUnitPriceKeys 保持同一份名单（多出的 price_per_million_chars 只用于选单位文案）。
 */
const UNIT_KEY_BY_METADATA: Array<[string, string]> = [
  ['price_per_sec', 'usage.per_second'],
  ['price_per_second', 'usage.per_second'],
  ['price_per_image', 'usage.per_image'],
  ['price_per_call', 'usage.per_call'],
  ['price_per_million', 'usage.per_million_tokens'],
  ['price_per_million_chars', 'usage.per_million_tokens'],
  ['unit_price', 'usage.per_million_tokens'],
];

/** 币种符号表。认不出的币种原样回退币种代码，绝不硬标 $（会把 ¥1.4 说成 $1.4）。 */
const CURRENCY_SYMBOLS: Record<string, string> = {
  CNY: '¥',
  RMB: '¥',
  USD: '$',
  JPY: '¥',
  EUR: '€',
  HKD: 'HK$',
  GBP: '£',
};

export function currencySymbol(currency: string | undefined): string {
  const code = (currency ?? '').trim().toUpperCase();
  if (!code) return '';
  return CURRENCY_SYMBOLS[code] ?? `${code} `;
}

/** 原币金额文案。decimals 缺省 4 位：¥0.1560 这种量级再少就看不出差异。 */
export function formatNativeAmount(value: number, currency: string, decimals = 4): string {
  const amount = Number.isFinite(value) ? value : 0;
  return `${currencySymbol(currency)}${amount.toFixed(decimals)}`;
}

/** 折文案：0.75 → "0.75"。倍率缺失时后端已按原价（折 1）记，这里不再兜底。 */
export function formatDiscount(discount: number): string {
  return (Number.isFinite(discount) ? discount : 1).toFixed(2);
}

/** 账本除数文案：6.8 → "6.8"（末尾零不补，客户对照的是「除以 6.8」这个数）。 */
export function formatDivisor(divisor: number): string {
  if (!Number.isFinite(divisor) || divisor <= 0) return '';
  return String(Number(divisor.toFixed(6)));
}

/** 除数为 1 = 账本币种与牌价币种相同，不发生折算，折算率行与验算式里的「÷」都省掉。 */
export function hasDivisor(divisor: number): boolean {
  return Number.isFinite(divisor) && divisor > 0 && divisor !== 1;
}

/**
 * 验算式文案：`¥0.1560 × 0.75 ÷ 6.8`；除数为 1 时是 `¥0.1560 × 0.75`。
 * 刻意不带 "= x"——结果就在紧邻的「实扣」行里，重复三遍反而看不清。
 */
export function verificationFormula(official: OfficialNativeCost): string {
  const head = `${formatNativeAmount(official.cost, official.currency)} × ${formatDiscount(official.discount)}`;
  return hasDivisor(official.divisor) ? `${head} ÷ ${formatDivisor(official.divisor)}` : head;
}

/** 明细 key → 已知档位的 i18n 标签键。认不出的档位回落明细自带 label。 */
function unitPriceLabelKey(rawKey: string): string | undefined {
  const key = rawKey.trim().toLowerCase();
  switch (key) {
    case 'input':
    case 'input_tokens':
    case 'input_token':
    case 'prompt_tokens':
    case 'prompt_token':
      return 'usage.input_unit_price';
    case 'output':
    case 'output_tokens':
    case 'output_token':
    case 'completion_tokens':
    case 'completion_token':
      return 'usage.output_unit_price';
    default:
      return undefined;
  }
}

function parseSnapshotFloat(metadata: Record<string, string> | undefined, key: string): number {
  const raw = (metadata?.[key] ?? '').trim();
  if (!raw) return 0;
  const value = Number(raw);
  return Number.isFinite(value) ? value : 0;
}

function snapshotUnitKey(metadata: Record<string, string> | undefined): string {
  for (const [metadataKey, unitKey] of UNIT_KEY_BY_METADATA) {
    if (parseSnapshotFloat(metadata, metadataKey) > 0) return unitKey;
  }
  // 插件没写美元单价（如按次追加的服务器工具费）时按 token 口径兜底：
  // 后端同样按行级 fx 折算，量纲与 token 档一致。
  return 'usage.per_million_tokens';
}

interface SnapshotItem {
  key?: string;
  label?: string;
  metadata?: Record<string, string>;
}

function collectUnitPrices(items: SnapshotItem[]): ListUnitPriceRow[] {
  const rows: ListUnitPriceRow[] = [];
  for (const [index, item] of items.entries()) {
    const currency = (item.metadata?.[SNAPSHOT_CURRENCY] ?? '').trim().toUpperCase();
    if (!currency) continue;
    const price = parseSnapshotFloat(item.metadata, SNAPSHOT_UNIT_PRICE);
    if (price <= 0) continue;
    const rawKey = item.key ?? '';
    rows.push({
      rowKey: rawKey || item.label || String(index),
      labelKey: unitPriceLabelKey(rawKey),
      fallbackLabel: (item.label ?? '').trim() || rawKey,
      price,
      currency,
      unitKey: snapshotUnitKey(item.metadata),
    });
  }
  return rows;
}

/**
 * buildUsageVerification 组装本行的验算块；没有 official_native 就返回 null（不渲染）。
 *
 * 不渲染的三种来源：历史行（切 USD 前没有快照）、官方价本就是美元的模型、
 * API Key 会话视角（后端根本不下发该字段）。
 */
export function buildUsageVerification(row: UsageRowWithOfficialNative | undefined): UsageVerification | null {
  const official = row?.official_native;
  if (!official) return null;
  const currency = (official.currency ?? '').trim().toUpperCase();
  // 除数是验算式里唯一必需的那个数：缺了它等式凑不齐，宁可整块不渲染。
  if (!currency || !(official.divisor > 0)) return null;

  // 单价快照与后端同口径：明细优先，一条都没带快照时才回退 metric——
  // 两者常共用同一份 metadata（airgate-openai 就是），都取会出现重复行。
  const detailRows = collectUnitPrices(row?.usage_cost_details ?? []);
  const unitPrices = detailRows.length > 0 ? detailRows : collectUnitPrices(row?.usage_metrics ?? []);

  return {
    official: {
      currency,
      fx: official.fx,
      cost: Number.isFinite(official.cost) ? official.cost : 0,
      discount: official.discount > 0 ? official.discount : 1,
      divisor: official.divisor,
      // 账本币种缺省回落原币：¥ 账本下两者本来就相同，标错币种比不标更糟。
      ledger_currency: (official.ledger_currency ?? '').trim().toUpperCase() || currency,
    },
    actualCost: row?.actual_cost ?? 0,
    unitPrices: unitPrices.filter((item) => item.currency === currency),
    showDivisor: hasDivisor(official.divisor),
  };
}

import type { MyPlatformPricing, MyPricingModel, PublicPlatformPricing } from '../shared/api/models';

export interface ModelLedgerItem extends MyPricingModel {
  platform: string;
  platforms: string[];
  // brands 保存接入渠道而非模型厂商。一个模型条目只属于一个渠道，
  // 避免 Azure Google 与 Gemini 官方直连的同名模型、价格被合并。
  brands: string[];
  capabilities: string[];
  // 以下三者是模型广场的三层导航轴，与 brands（渠道）正交：
  //   categoryKey — L1 用途大类，core 下发，缺省归 other
  //   vendorKey   — L2 模型厂商（用户视角：这是谁家的模型）
  //   seriesKey   — L3 系列，空串表示不折叠
  categoryKey: string;
  vendorKey: string;
  seriesKey: string;
}

/** L1 用途大类的展示顺序。core 下发 category，此处只定顺序与兜底桶。 */
export const CATEGORY_ORDER = ['chat', 'image', 'video', 'audio', 'embedding', 'other'] as const;

export type CategoryKey = (typeof CATEGORY_ORDER)[number];

/** categoryKeyOf 取 core 下发的大类；未下发（老后端 / 能力未标注）归 other。 */
export function categoryKeyOf(model: MyPricingModel): CategoryKey {
  const raw = (model.category ?? '').trim().toLowerCase();
  return (CATEGORY_ORDER as readonly string[]).includes(raw) ? (raw as CategoryKey) : 'other';
}

/**
 * vendorKeyOf 模型厂商（L2 轴，用户视角）。
 *
 * 刻意不复用 sourceCategory：那是接入渠道（供给视角），同一厂商可能有多条渠道
 * （Gemini 官方直连 / Azure Google），价格不同故条目不合并；但筛选时用户想的是
 * 「Google 家的模型」，两条渠道都该出现在同一个厂商下。
 */
export function vendorKeyOf(platform: string, model: MyPricingModel): string {
  return (model.vendor ?? '').trim() || platform;
}

/** seriesKeyOf 系列标识；插件未声明时返回空串（该模型不参与折叠）。 */
export function seriesKeyOf(model: MyPricingModel): string {
  return (model.series ?? '').trim();
}

export function sourceCategory(platform: string, model: MyPricingModel): string {
  if (platform === 'gemini') return 'gemini_official';
  if (platform === 'seedance') return 'byteplus';
  if (platform === 'openai' && model.vendor === 'google') return 'azure_google';
  return model.vendor || platform;
}

function modelVersion(id: string): number[] {
  const match = id.match(/\d+(?:[.-]\d+)*/);
  if (!match) return [];
  return match[0].split(/[.-]/).map(Number);
}

function compareVersionDescending(left: string, right: string): number {
  const leftVersion = modelVersion(left);
  const rightVersion = modelVersion(right);
  const length = Math.max(leftVersion.length, rightVersion.length);
  for (let index = 0; index < length; index += 1) {
    const difference = (rightVersion[index] ?? 0) - (leftVersion[index] ?? 0);
    if (difference !== 0) return difference;
  }
  return left.localeCompare(right, undefined, { numeric: true });
}

export function mergeCatalog(platforms: Array<MyPlatformPricing | PublicPlatformPricing>): ModelLedgerItem[] {
  const merged = new Map<string, ModelLedgerItem>();
  const sourceOrder = new Map<string, number>();
  for (const platform of platforms) {
    if (!platform || !Array.isArray(platform.models)) continue;
    for (const model of platform.models as MyPricingModel[]) {
      if (!model?.id) continue;
      // Kiro 与 Claude 目录高度重复，产品侧暂不在模型广场展示；
      // 网关和 API 路由保持不变，仅隐藏广场入口。
      if (platform.platform === 'kiro') continue;
      const key = `${platform.platform}:${model.id}`;
      if (merged.has(key)) continue;
      const source = sourceCategory(platform.platform, model);
      if (!sourceOrder.has(source)) sourceOrder.set(source, sourceOrder.size);
      merged.set(key, {
        ...model,
        platform: platform.platform,
        platforms: [platform.platform],
        brands: [source],
        capabilities: [...(model.capabilities ?? [])],
        categoryKey: categoryKeyOf(model),
        vendorKey: vendorKeyOf(platform.platform, model),
        seriesKey: seriesKeyOf(model),
      });
    }
  }
  return [...merged.values()].sort((left, right) => {
    const sourceDifference = (sourceOrder.get(left.brands[0] ?? '') ?? 0)
      - (sourceOrder.get(right.brands[0] ?? '') ?? 0);
    return sourceDifference || compareVersionDescending(left.id, right.id);
  });
}

/**
 * SeriesGroup 模型广场 L3 折叠单元。
 *
 * folded=false 时是「单模型行」（插件未声明系列，或该系列只有一个版本），
 * 展示上与改造前一致；folded=true 时是「系列卡」，默认收起，展开看各版本。
 */
export interface SeriesGroup {
  key: string;
  /** 系列标识；未声明系列的单模型组为空串 */
  series: string;
  vendor: string;
  category: string;
  /** 组内成员按版本降序，代表款（最新版本）在首位 */
  items: ModelLedgerItem[];
  folded: boolean;
}

/**
 * groupBySeries 把已排序的模型清单折叠成系列组，保持传入顺序（首次出现即组序）。
 *
 * 折叠键含 vendor 与 category：同名 series 若跨厂商/跨用途（如某厂商的 "3.0"
 * 同时有生图与生视频），不应被并成一组。渠道（brands）刻意不入键——同一系列
 * 经不同渠道接入的版本要落在同一张卡里，渠道差异在行内标签体现。
 */
export function groupBySeries(items: ModelLedgerItem[]): SeriesGroup[] {
  const groups: SeriesGroup[] = [];
  const index = new Map<string, number>();
  for (const item of items) {
    // 未声明系列：每个模型自成一组，用 id 保证键唯一，不与他人合并。
    const key = item.seriesKey
      ? `s:${item.categoryKey}|${item.vendorKey}|${item.seriesKey}`
      : `m:${item.platform}:${item.id}`;
    const position = index.get(key);
    if (position === undefined) {
      index.set(key, groups.length);
      groups.push({
        key,
        series: item.seriesKey,
        vendor: item.vendorKey,
        category: item.categoryKey,
        items: [item],
        folded: false,
      });
      continue;
    }
    groups[position]?.items.push(item);
  }
  for (const group of groups) {
    group.folded = Boolean(group.series) && group.items.length > 1;
  }
  return groups;
}

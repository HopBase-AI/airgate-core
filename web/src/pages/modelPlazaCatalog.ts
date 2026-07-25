import type { MyPlatformPricing, MyPricingModel, PublicPlatformPricing } from '../shared/api/models';

export interface ModelLedgerItem extends MyPricingModel {
  platform: string;
  platforms: string[];
  // brands 保存接入渠道而非模型厂商。一个模型条目只属于一个渠道，
  // 避免 Azure Google 与 Gemini 官方直连的同名模型、价格被合并。
  brands: string[];
  capabilities: string[];
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
      });
    }
  }
  return [...merged.values()].sort((left, right) => {
    const sourceDifference = (sourceOrder.get(left.brands[0] ?? '') ?? 0)
      - (sourceOrder.get(right.brands[0] ?? '') ?? 0);
    return sourceDifference || compareVersionDescending(left.id, right.id);
  });
}

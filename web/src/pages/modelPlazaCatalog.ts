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

export function mergeCatalog(platforms: Array<MyPlatformPricing | PublicPlatformPricing>): ModelLedgerItem[] {
  const merged = new Map<string, ModelLedgerItem>();
  for (const platform of platforms) {
    if (!platform || !Array.isArray(platform.models)) continue;
    for (const model of platform.models as MyPricingModel[]) {
      if (!model?.id) continue;
      const key = `${platform.platform}:${model.id}`;
      if (merged.has(key)) continue;
      merged.set(key, {
        ...model,
        platform: platform.platform,
        platforms: [platform.platform],
        brands: [sourceCategory(platform.platform, model)],
        capabilities: [...(model.capabilities ?? [])],
      });
    }
  }
  return [...merged.values()];
}

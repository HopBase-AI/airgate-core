import { get } from './client';

// 各网关平台当前生效的内置模型目录（后台「模型目录」编辑器种子数据）。
// metadata 里的 price.* / long_context.* 键是插件编码的内置基础价提示。

export interface BuiltinModel {
  id: string;
  name: string;
  context_window: number;
  max_output_tokens: number;
  metadata?: Record<string, string>;
}

export interface BuiltinPlatformModels {
  platform: string;
  models: BuiltinModel[];
}

export interface PublicModelLongContext {
  threshold: number;
  input_multiplier?: number;
  cached_multiplier?: number;
  output_multiplier?: number;
}

export interface PublicPricingModel {
  id: string;
  name?: string;
  context_window?: number;
  capabilities?: string[];
  input: number;
  cached_input?: number;
  output: number;
  long_context?: PublicModelLongContext;
}

export interface PublicPlatformPricing {
  platform: string;
  models: PublicPricingModel[];
}

export const modelsApi = {
  builtin: () => get<BuiltinPlatformModels[]>('/api/v1/admin/models/builtin'),
  pricing: () => get<PublicPlatformPricing[]>('/api/v1/models/pricing'),
};

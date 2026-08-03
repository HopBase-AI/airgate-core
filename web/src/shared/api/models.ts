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
  // 厂商标识(如 google/openai):平台是接入协议,vendor 是模型出品方;缺省=展示端回退平台名。
  vendor?: string;
  // input/cached_input/output 是计费基准价（余额单位 / 1M tokens，¥1=$1 平价；常规模型即官方美元价）。
  input: number;
  cached_input?: number;
  output: number;
  // currency="CNY"：基准价是官方人民币牌价按 1:1 记账，展示须按 official（官方美元参考价）换算。
  currency?: string;
  official?: { input: number; cached_input?: number; output: number };
  long_context?: PublicModelLongContext;
  // 视频生成模型的桶价：bucket（<分辨率>_{no,with}_ref）→ $/1M video_tokens。
  // 有值时按桶铺价，无 input/output。
  video_tokens?: Record<string, number>;
  // 图片生成模型的按张价：像素档位（le_236w / gt_236w）→ $/张。
  image?: Record<string, number>;
}

export interface PublicPlatformPricing {
  platform: string;
  models: PublicPricingModel[];
}

// 用户实付价视图（/models/pricing/me）：公开定价 + 最优可用分组的实付倍率。
export interface MyPricingModel extends PublicPricingModel {
  // 实付倍率（计费口径，含用户专属调价）；缺省 = 无可用分组能路由到该模型。
  user_rate?: number;
  group_id?: number;
  group_name?: string;
  // 分组名多语言覆盖(键=语言码 en / zh-HK / ja;zh 基准即 group_name)
  group_name_i18n?: Record<string, string>;
  // 纯图片接口命中用户/分组覆盖后的最终固定价（余额单位 / 张）。
  image_price_1k?: number;
  image_price_2k?: number;
  image_price_4k?: number;
}

export interface MyPlatformPricing {
  platform: string;
  models: MyPricingModel[];
}

// usd_multiplier：相对官方美元价的有效倍率（输入价口径），折扣 = usd_multiplier / 汇率。
export interface MyGroupQuote {
  id: number;
  name: string;
  platform: string;
  group_rate: number;
  effective_rate: number;
  usd_multiplier?: number;
}

export interface MyModelPricing {
  platforms: MyPlatformPricing[];
  groups: MyGroupQuote[];
}

export const modelsApi = {
  builtin: () => get<BuiltinPlatformModels[]>('/api/v1/admin/models/builtin'),
  pricing: () => get<PublicPlatformPricing[]>('/api/v1/models/pricing'),
  myPricing: () => get<MyModelPricing>('/api/v1/models/pricing/me'),
};

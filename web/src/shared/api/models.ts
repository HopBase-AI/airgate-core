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
  // 系列标识(如 gpt-5/claude-opus/kling-3):模型广场据此把同系列多版本折叠成一张卡。
  // 与调度侧 family(账号家族冷却)无关;缺省=不折叠、单独成行。
  series?: string;
  // 一级大类:video/image/audio/embedding/chat。由 core 按能力统一推导下发，
  // 控制台/主站/ToC 站群共用同一口径，展示端不要自行再推一遍;缺省=归"其他"。
  category?: string;
  // input/cached_input/output 是计费基准价（余额单位 / 1M tokens，¥1=$1 平价；常规模型即官方美元价）。
  input: number;
  cached_input?: number;
  output: number;
  // currency="CNY"：基准价是官方人民币牌价按 1:1 记账，展示须按 official（官方美元参考价）换算。
  currency?: string;
  official?: { input: number; cached_input?: number; output: number };
  long_context?: PublicModelLongContext;
  // 视频生成模型的桶价：bucket（如 <分辨率>_{no,with}_ref、<分辨率>_<有无声>_<有无参考>）
  // → $ / price_unit 指定的一份。有值时按桶铺价，无 input/output。
  //
  // ⚠️ 键名里的 video_tokens 是历史遗留，**不代表量纲**：seedance 是每百万 video_tokens，
  // 可灵 / 海螺 / 万相 / 快乐马是每秒，必须看 price_unit。
  video_tokens?: Record<string, number>;
  // 图片生成模型的按张价：像素档位及参考图（如 le_261w / gt_261w / input_reference）→ $/张。
  image?: Record<string, number>;
  // 价格的计量单位（core 下发，缺省 "token"）："token" = $/1M token（视频模型即
  // $/1M video_tokens）；"second" = $/秒（按视频时长计费）；生图模型可为 "image"。
  // 老后端不下发该字段，展示端按 "token" 兜底（与改动前行为一致）。
  price_unit?: string;
  // 按次计价能力（$ / 次，键 = 能力名，如可灵人脸识别 face_detect）。
  call?: Record<string, number>;
  // 厂商官方牌价（原币）。国内厂商模型（可灵 / 万相 / 海螺 H3 / 国内 Seedance /
  // 覆盖层登记的千问、Kimi）官网标价是 ¥，插件按 fx 折成上面的美元基准价计费，
  // 客户只看 $ 对不上官网 ¥。缺省 = 基准价本身就是官方美元价，无需换算。
  // 只作展示，不参与计费；键与同名基准价字段一一对应（docs/pricing-list-verification-sop.md §1.1）。
  list_price?: PublicModelListPrice;
}

// PublicModelListPrice 官方牌价（原币）。恒等式 <价> ÷ fx ≈ 同名基准价字段。
// 只含币种 / 折算率 / 单价，不含任何上游通道、账号或供应商信息。
export interface PublicModelListPrice {
  // 原币币种（如 "CNY"）。
  currency: string;
  // 折算率：1 USD = fx 原币（如 6.8）。
  fx: number;
  input?: number;
  cached_input?: number;
  output?: number;
  // 键与同名基准价 map 一致；video_tokens 的「一份」量纲同样由 price_unit 决定。
  video_tokens?: Record<string, number>;
  image?: Record<string, number>;
  call?: Record<string, number>;
}

export interface PublicPlatformPricing {
  platform: string;
  models: PublicPricingModel[];
}

// 用户实付价视图（/models/pricing/me）：公开定价 + 最优可用分组的 token 实付倍率。
export interface MyPricingModel extends PublicPricingModel {
  // 部分图片尺寸有固定价时，实付倍率用于未配置尺寸的 token 回退。缺省表示
  // 无可用 token 报价，或三个图片尺寸均使用固定价。
  user_rate?: number;
  // 缓存读那一档的实付倍率，仅当它与 user_rate 不同（分组的缓存读按厂商官方牌价原价计、
  // 不吃折扣）时下发；缺省 = 缓存读与输入/输出同倍率。
  cached_user_rate?: number;
  // 分组字段只归属 user_rate 的 token 报价，不代表固定图片档位的来源。
  group_id?: number;
  group_name?: string;
  // 分组名多语言覆盖(键=语言码 en / zh-HK / ja;zh 基准即 group_name)
  group_name_i18n?: Record<string, string>;
  // 纯图片接口命中用户/分组覆盖后的最终固定价（余额/CNY 单位 / 张）。
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
  // 该分组的缓存读按厂商官方牌价原价计，不吃折扣；缺省/false = 缓存读与输入/输出同折。
  cached_input_full_price?: boolean;
}

export interface MyModelPricing {
  platforms: MyPlatformPricing[];
  // Legacy Core versions omitted this field. When present, even an empty list
  // or usd_multiplier=0 is authoritative and must not be reconstructed client-side.
  groups?: MyGroupQuote[];
  // 定价展示模式：quote=报价客户，响应已按报价口径裁剪（模型不带分组来源、
  // group_rate=effective_rate），前端须隐藏划线原价/折扣徽章/公开牌价回退。
  pricing_mode?: 'standard' | 'quote';
}

export const modelsApi = {
  builtin: () => get<BuiltinPlatformModels[]>('/api/v1/admin/models/builtin'),
  pricing: () => get<PublicPlatformPricing[]>('/api/v1/models/pricing'),
  myPricing: () => get<MyModelPricing>('/api/v1/models/pricing/me'),
};

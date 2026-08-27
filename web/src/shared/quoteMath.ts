// 报价单的折/倍率换算：全站统一语义「倍率 = 每官方 $1 扣 ¥，折 = 倍率 ÷ 汇率」。
// 报价 UI 只让人填「折数」（如 7.5 = 7.5 折），倍率一律由系统换算——
// 历史上让人手填倍率导致过静默错价事故（6.5 折被配成 9.6 折），此处把入口焊死。
// 折→倍率的换算规则全站只此一份，任何页面不得内联复制。

export const DEFAULT_QUOTE_FX = 6.8;

export function parseQuoteFx(rawTocPricing: string | undefined): number {
  try {
    const config = JSON.parse(rawTocPricing ?? '{}') as { fx?: number };
    return typeof config.fx === 'number' && config.fx > 0 ? config.fx : DEFAULT_QUOTE_FX;
  } catch {
    return DEFAULT_QUOTE_FX;
  }
}

// 倍率 → 折数（5.5 折口径的 5.5），保留两位。
export function zheOfRate(rate: number, fx: number): number {
  if (!(rate > 0) || !(fx > 0)) return 0;
  return Math.round((rate / fx) * 1000) / 100;
}

// 折数 → 倍率，保留四位（7.5 折 × 6.8 = 5.1）。
export function rateOfZhe(zhe: number, fx: number): number {
  if (!(zhe > 0) || !(fx > 0)) return 0;
  return Math.round((zhe / 10) * fx * 10000) / 10000;
}

// 在某分组默认倍率的折数基础上加 N 个点（销售点差），返回目标折数；无效返回 0。
// 报价单批量工具与新建用户快捷报价共用，保证同一「+N 点」得到同一倍率。
export function zheWithPoints(defaultRate: number, points: number, fx: number): number {
  const base = zheOfRate(defaultRate, fx);
  if (!(base > 0) || !Number.isFinite(points)) return 0;
  return Math.round((base + points) * 100) / 100;
}

// 解析折数输入框文本：合法（0 < 折 ≤ 100）返回数值，空串/非法返回 null。
// 校验、落库、展示三处共用同一判定，避免「保存按钮拦了但状态已写入」的分叉。
export function parseZheInput(text: string | undefined): number | null {
  const trimmed = (text ?? '').trim();
  if (trimmed === '') return null;
  const value = Number(trimmed);
  if (!Number.isFinite(value) || value <= 0 || value > 100) return null;
  return value;
}

// 折数展示：去掉多余尾零（5.50 → 5.5）。
export function formatZhe(zhe: number): string {
  return String(Math.round(zhe * 100) / 100);
}

export function formatRate(rate: number): string {
  return String(Math.round(rate * 10000) / 10000);
}

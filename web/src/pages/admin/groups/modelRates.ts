// 分组「按模型倍率」编辑行 ↔ 后端 model_rates（模型 ID → 倍率）的换算与校验。
// 语义与后端 billing.ResolveBillingRateForGroupModel 一致：值 > 0 的条目对该模型覆盖分组倍率；
// 全站统一口径「倍率 = 每官方 $1 扣 ¥，折 = 倍率 ÷ 汇率」，折数展示走 shared/quoteMath。

export interface ModelRateRow {
  id: number;
  model: string;
  rate: string;
}

export type ModelRates = Record<string, number>;

let nextRowId = 1;

export function newModelRateRow(model = '', rate = ''): ModelRateRow {
  return { id: nextRowId++, model, rate };
}

// 后端 map → 编辑行（按模型名排序，回显稳定）。
export function modelRateRowsFromMap(rates: ModelRates | undefined): ModelRateRow[] {
  return Object.entries(rates ?? {})
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([model, rate]) => newModelRateRow(model, String(rate)));
}

// 解析倍率输入：合法（有限且 > 0）返回数值，否则 null。
export function parseModelRateInput(text: string | undefined): number | null {
  const trimmed = (text ?? '').trim();
  if (trimmed === '') return null;
  const value = Number(trimmed);
  if (!Number.isFinite(value) || value <= 0) return null;
  return value;
}

export interface BuildModelRatesResult {
  rates: ModelRates;
  // 有任一行不完整/非法/重复时为 true，调用方应拦截提交。
  invalid: boolean;
  invalidRowIds: number[];
}

// 编辑行 → 后端 map：模型名去首尾空白；模型与倍率都为空的行视为未填直接跳过；
// 只填一半、倍率非法、模型名（忽略大小写）重复的行标记为非法。
export function buildModelRates(rows: ModelRateRow[]): BuildModelRatesResult {
  const rates: ModelRates = {};
  const seen = new Map<string, number>();
  const invalidRowIds: number[] = [];
  for (const row of rows) {
    const model = row.model.trim();
    const rateText = row.rate.trim();
    if (model === '' && rateText === '') continue;
    const rate = parseModelRateInput(rateText);
    const folded = model.toLowerCase();
    if (model === '' || rate == null || seen.has(folded)) {
      invalidRowIds.push(row.id);
      const dup = seen.get(folded);
      if (dup != null && !invalidRowIds.includes(dup)) invalidRowIds.push(dup);
      continue;
    }
    seen.set(folded, row.id);
    rates[model] = rate;
  }
  return { rates, invalid: invalidRowIds.length > 0, invalidRowIds };
}

export function modelRateCount(rates: ModelRates | undefined): number {
  return Object.keys(rates ?? {}).length;
}

import i18n from '../../i18n';

/** 格式化过期时间，未设置则显示"永不过期" */
export function formatExpiry(date?: string, neverLabel?: string): string {
  if (!date) return neverLabel ?? i18n.t('common.never_expire');
  return new Date(date).toLocaleDateString('zh-CN');
}

/** 格式化日期时间 (yyyy/M/d HH:mm) */
export function formatDateTime(date: string): string {
  return new Date(date).toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
}

/** 格式化日期 (yyyy/M/d) */
export function formatDate(date: string): string {
  return new Date(date).toLocaleDateString('zh-CN');
}

/**
 * 格式化余额:整额显示 2 位小数,存在更细小数位时完整展示(最多 8 位,对齐 DB numeric(20,8))。
 * 余额扣费常在 $0.000x 量级,固定 toFixed(2) 会把小额消费四舍五入掩盖成"余额没动"。
 */
export function formatBalance(value: number | null | undefined): string {
  const amount = value ?? 0;
  const [int, dec = ''] = amount.toFixed(8).replace(/0+$/, '').split('.');
  return `${int}.${dec.padEnd(2, '0')}`;
}

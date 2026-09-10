import type { ReactNode } from 'react';

/**
 * 侧栏底部账户区的身份徽记(企业主 / 团队成员;后续「已认证 / 未认证」等状态也挂这里)。
 * tone 只管颜色语义,文案由调用方传入;样式在 brand-hopbase-shell.css 的 .ag-account-badge。
 *   neutral — 身份标签(默认):发丝线 + 弱色
 *   success — 正向状态(如已认证)
 *   warning — 待处理状态(如未认证)
 */
export type AccountBadgeTone = 'neutral' | 'success' | 'warning';

interface AccountBadgeProps {
  children: ReactNode;
  tone?: AccountBadgeTone;
  className?: string;
}

export function AccountBadge({ children, tone = 'neutral', className }: AccountBadgeProps) {
  return (
    <span className={`ag-account-badge${className ? ` ${className}` : ''}`} data-tone={tone}>
      {children}
    </span>
  );
}

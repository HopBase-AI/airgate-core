export interface MemberForm {
  name: string;
  email: string;
  note: string;
  /** 额度（USD）。空字符串或 "0" 表示不限 */
  quota_usd: string;
  quota_period: 'none' | 'monthly';
}

export const emptyMemberForm: MemberForm = {
  name: '',
  email: '',
  note: '',
  quota_usd: '',
  quota_period: 'monthly',
};

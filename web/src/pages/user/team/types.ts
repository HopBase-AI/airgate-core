export interface MemberForm {
  name: string;
  /** 成员登录邮箱（新建时必填，作为登录账号） */
  email: string;
  /** 新建时必填；编辑时留空 = 不改，填了 = 重置密码 */
  password: string;
  note: string;
  /** 额度（USD）。有登录账号的成员必填(>0)；仅老模型无账号成员允许空字符串或 "0" = 不限 */
  quota_usd: string;
  quota_period: 'none' | 'monthly';
  /** 分组白名单；空 = 继承企业主全部可见分组 */
  allowed_group_ids: number[];
}

export const emptyMemberForm: MemberForm = {
  name: '',
  email: '',
  password: '',
  note: '',
  quota_usd: '',
  quota_period: 'monthly',
  allowed_group_ids: [],
};

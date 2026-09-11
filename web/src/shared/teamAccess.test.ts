import { describe, expect, it } from 'vitest';
import { canEditMemberRow, resolveTeamAccess } from './teamAccess';
import type { UserResp } from './types';

const base: UserResp = {
  id: 1,
  email: 'x@example.com',
  username: 'x',
  balance: 0,
  role: 'user',
  max_concurrency: 0,
  balance_alert_threshold: 0,
  status: 'active',
  created_at: '',
  updated_at: '',
} as unknown as UserResp;

const owner: UserResp = { ...base, id: 7, is_enterprise_owner: true };
const manager: UserResp = { ...base, id: 20, member_id: 15, managed_department_id: 4, managed_department_name: '研发部' };
const plainMember: UserResp = { ...base, id: 21, member_id: 16 };
const plainUser: UserResp = { ...base, id: 22 };
const apiKeySession: UserResp = { ...base, role: 'api_key', api_key_id: 3 };

describe('resolveTeamAccess', () => {
  it('管理员：全企业 + 组织结构 + 审计', () => {
    const access = resolveTeamAccess(plainUser, 'admin');
    expect(access).toMatchObject({ canOpenTeam: true, canManageDepartments: true, canViewAudit: true, isDepartmentManager: false });
  });

  it('企业主：全企业 + 组织结构 + 审计', () => {
    const access = resolveTeamAccess(owner);
    expect(access).toMatchObject({ canOpenTeam: true, canManageDepartments: true, canViewAudit: true, canSeeEnterpriseBalance: true });
  });

  it('部门负责人：进得去团队页，但没有组织结构 / 审计 / 企业余额', () => {
    const access = resolveTeamAccess(manager);
    expect(access).toMatchObject({
      canOpenTeam: true,
      isDepartmentManager: true,
      managedDepartmentId: 4,
      managedDepartmentName: '研发部',
      canManageDepartments: false,
      canViewAudit: false,
      canSeeEnterpriseBalance: false,
    });
  });

  it('普通成员 / 普通用户 / 未登录：进不去', () => {
    expect(resolveTeamAccess(plainMember).canOpenTeam).toBe(false);
    expect(resolveTeamAccess(plainUser).canOpenTeam).toBe(false);
    expect(resolveTeamAccess(null).canOpenTeam).toBe(false);
    expect(resolveTeamAccess(undefined).canOpenTeam).toBe(false);
  });

  it('API Key 会话一律进不去（即便 token 角色是管理员）', () => {
    expect(resolveTeamAccess(apiKeySession, 'admin').canOpenTeam).toBe(false);
  });

  it('成员账号即便被误置 is_enterprise_owner 也只按负责人算', () => {
    const hybrid: UserResp = { ...manager, is_enterprise_owner: true };
    const access = resolveTeamAccess(hybrid);
    expect(access.isDepartmentManager).toBe(true);
    expect(access.canManageDepartments).toBe(false);
    // 不是负责人的成员则直接没有权限
    expect(resolveTeamAccess({ ...plainMember, is_enterprise_owner: true }).canOpenTeam).toBe(false);
  });
});

describe('canEditMemberRow', () => {
  it('负责人改不了自己那条成员记录，别人可以', () => {
    const access = resolveTeamAccess(manager);
    expect(canEditMemberRow(access, manager, 15)).toBe(false);
    expect(canEditMemberRow(access, manager, 16)).toBe(true);
  });

  it('企业主对谁都能改（含负责人本人）', () => {
    const access = resolveTeamAccess(owner);
    expect(canEditMemberRow(access, owner, 15)).toBe(true);
  });

  it('没有团队权限的人一律不可改', () => {
    expect(canEditMemberRow(resolveTeamAccess(plainUser), plainUser, 1)).toBe(false);
  });
});

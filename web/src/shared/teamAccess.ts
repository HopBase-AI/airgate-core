import type { UserResp } from './types';

/**
 * 团队页的权限口径（与后端 middleware.RequireTeamScope / RequireEnterpriseOwner 一一对应）。
 *
 * 三种身份：
 *   - 管理员 / 企业主：全企业，组织结构与账期都能改；
 *   - 部门负责人（某部门的 department_manager，以成员账号登录）：只管本部门成员；
 *   - 其他人：进不去团队页。
 *
 * 这里只负责"界面别把按钮摆出来"，真正的拦截在服务端——前端判断永远不是边界。
 */
export interface TeamAccess {
  /** 能否进入「团队」页 */
  canOpenTeam: boolean;
  /** 是否部门负责人视角（页面收敛到一个部门） */
  isDepartmentManager: boolean;
  /** 负责的部门 id / 名称（不是负责人时为 0 / 空） */
  managedDepartmentId: number;
  managedDepartmentName: string;
  /** 能否管理组织结构（建 / 改 / 删部门、重置部门本期、改企业账期） */
  canManageDepartments: boolean;
  /** 能否查看团队操作审计（/team/audit） */
  canViewAudit: boolean;
  /** 能否看到企业余额等企业层数字 */
  canSeeEnterpriseBalance: boolean;
}

const NO_ACCESS: TeamAccess = {
  canOpenTeam: false,
  isDepartmentManager: false,
  managedDepartmentId: 0,
  managedDepartmentName: '',
  canManageDepartments: false,
  canViewAudit: false,
  canSeeEnterpriseBalance: false,
};

/**
 * resolveTeamAccess 按 /users/me 的投影算出团队页能力位。
 * tokenRole 传 JWT 里的 role（getTokenRole()），用于 user 尚未加载时先认管理员。
 */
export function resolveTeamAccess(user?: UserResp | null, tokenRole?: string | null): TeamAccess {
  const isAPIKeySession = user?.role === 'api_key' || !!(user?.api_key_id && user.api_key_id > 0);
  if (isAPIKeySession) return NO_ACCESS;

  if (tokenRole === 'admin' || user?.role === 'admin') {
    return {
      canOpenTeam: true,
      isDepartmentManager: false,
      managedDepartmentId: 0,
      managedDepartmentName: '',
      canManageDepartments: true,
      canViewAudit: true,
      canSeeEnterpriseBalance: true,
    };
  }
  if (!user) return NO_ACCESS;

  // 成员账号：只有部门负责人进得来，且只能管自己那个部门。
  // 判定在成员分支里先做——成员账号即便被误置 is_enterprise_owner 也不该拿到全企业视图。
  const memberId = user.member_id ?? 0;
  if (memberId > 0) {
    const departmentId = user.managed_department_id ?? 0;
    if (departmentId <= 0) return NO_ACCESS;
    return {
      canOpenTeam: true,
      isDepartmentManager: true,
      managedDepartmentId: departmentId,
      managedDepartmentName: user.managed_department_name ?? '',
      canManageDepartments: false,
      canViewAudit: false,
      canSeeEnterpriseBalance: false,
    };
  }

  if (user.is_enterprise_owner) {
    return {
      canOpenTeam: true,
      isDepartmentManager: false,
      managedDepartmentId: 0,
      managedDepartmentName: '',
      canManageDepartments: true,
      canViewAudit: true,
      canSeeEnterpriseBalance: true,
    };
  }
  return NO_ACCESS;
}

/**
 * canEditMemberRow 某条成员记录是否可改 / 删。
 * 部门负责人不能动自己那条：抬自己的额度、或把自己停用锁死，都必须回到企业主手里。
 */
export function canEditMemberRow(access: TeamAccess, user: UserResp | null | undefined, memberId: number): boolean {
  if (!access.canOpenTeam) return false;
  if (!access.isDepartmentManager) return true;
  return memberId !== (user?.member_id ?? 0);
}

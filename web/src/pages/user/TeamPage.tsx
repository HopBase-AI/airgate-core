import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from '@tanstack/react-router';
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertDialog, Button, Dropdown, EmptyState, ListBox, Select, Spinner, Tabs } from '@heroui/react';
import { DialogTriggerShim } from '../../shared/components/DialogTriggerShim';
import { membersApi } from '../../shared/api/members';
import { departmentsApi } from '../../shared/api/departments';
import { FETCH_ALL_PARAMS } from '../../shared/constants';
import { usePagination } from '../../shared/hooks/usePagination';
import { useCrudMutation } from '../../shared/hooks/useCrudMutation';
import { useToast, StatusChip } from '../../shared/ui';
import { queryKeys } from '../../shared/queryKeys';
import { DEFAULT_PAGE_SIZE } from '../../shared/constants';
import { getTotalPages } from '../../shared/utils/pagination';
import { TablePaginationFooter } from '../../shared/components/TablePaginationFooter';
import { TableLoadingRow } from '../../shared/components/TableLoadingRow';
import { CommonTable } from '../../shared/components/CommonTable';
import {
  Ban,
  Building2,
  CheckCircle,
  KeyRound,
  MoreHorizontal,
  Pencil,
  Plus,
  ReceiptText,
  RefreshCw,
  RotateCcw,
  Trash2,
  UsersRound,
} from 'lucide-react';
import type { CreateMemberReq, MemberResp, UpdateMemberReq } from '../../shared/types';
import { useAuth } from '../../app/providers/AuthProvider';
import { getTokenRole } from '../../shared/api/client';
import { canEditMemberRow, resolveTeamAccess } from '../../shared/teamAccess';
import { EditMemberModal } from './team/EditMemberModal';
import { DepartmentsTab } from './team/DepartmentsTab';
import { TeamOverviewBar } from './team/TeamOverviewBar';
import { type MemberForm, emptyMemberForm } from './team/types';

type TeamTab = 'members' | 'departments';
const UNASSIGNED_DEPARTMENT_FILTER = '__unassigned__';

// 团队管理：「组织 + 成员」二级结构——企业主充值 → 划额度给部门 → 部门划给成员，
// 三层的额度 / 已用 / 剩余 / 使用率 / 周期全部可见可下钻；组织调整、成员变更、额度调整、
// 密钥操作全程进操作记录。成员用自己的账号正常登录、功能与普通用户一致，只是消耗从企业主余额扣、
// 用量归属到成员与部门。
//
// 部门负责人（成员账号且是某部门的 department_manager）进的是同一个页面的**收敛版**：
// 只有成员页签、只看得到本部门、改不了组织结构与账期、也动不了自己那条成员记录。
// 这里的隐藏只是别把按钮摆出来——真正的拦截在服务端（RequireTeamScope + service 层范围校验）。
export default function TeamPage() {
  const { t, i18n } = useTranslation();
  const { toast } = useToast();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const { user } = useAuth();
  const access = resolveTeamAccess(user, getTokenRole());
  const isManager = access.isDepartmentManager;

  const [tab, setTab] = useState<TeamTab>('members');
  const [departmentFilter, setDepartmentFilter] = useState('');
  const [deptCreateOpen, setDeptCreateOpen] = useState(false);
  const { page, setPage, pageSize, setPageSize } = usePagination(DEFAULT_PAGE_SIZE, 'user.team');
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<MemberResp | null>(null);
  const [form, setForm] = useState<MemberForm>(emptyMemberForm);
  const [deleteTarget, setDeleteTarget] = useState<MemberResp | null>(null);
  const [resetTarget, setResetTarget] = useState<MemberResp | null>(null);

  const departmentFilterID = departmentFilter === UNASSIGNED_DEPARTMENT_FILTER ? 0 : departmentFilter ? Number(departmentFilter) : undefined;
  const { data, isLoading, refetch } = useQuery({
    queryKey: queryKeys.members(page, pageSize, departmentFilter),
    queryFn: () => membersApi.list({ page, page_size: pageSize, department_id: departmentFilterID }),
    placeholderData: keepPreviousData,
  });
  // 部门下拉：成员表单归属 + 成员列表筛选共用；没建过部门的企业主看不到这些控件。
  // 负责人只有一个部门、且成员归属被钉死，这份下拉对他毫无用处，干脆不请求。
  const { data: departmentsData } = useQuery({
    queryKey: queryKeys.departmentsAll(),
    queryFn: () => departmentsApi.list(FETCH_ALL_PARAMS),
    enabled: !isManager,
    staleTime: 60_000,
  });
  const departmentOptions = useMemo(
    () => (departmentsData?.list ?? []).map((dept) => ({ id: String(dept.id), label: dept.name })),
    [departmentsData?.list],
  );
  // 负责人只管一个部门：部门列 / 部门筛选 / 表单里的部门选择一律不出现（所有人都在本部门），
  // 归属由 departmentPayload 钉死，不给"落到未分配"留口子。
  const hasDepartments = !isManager && departmentOptions.length > 0;
  const departmentFilterOptions = useMemo(() => ([
    { id: '', label: t('common.all') },
    ...departmentOptions,
    { id: UNASSIGNED_DEPARTMENT_FILTER, label: t('team.department_none') },
  ]), [departmentOptions, t]);

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: queryKeys.members() });
    queryClient.invalidateQueries({ queryKey: queryKeys.membersForKeys() });
    queryClient.invalidateQueries({ queryKey: queryKeys.departments() });
    queryClient.invalidateQueries({ queryKey: queryKeys.departmentsAll() });
    queryClient.invalidateQueries({ queryKey: queryKeys.teamOverview() });
    queryClient.invalidateQueries({ queryKey: queryKeys.teamAuditLogs() });
  };
  const showMembersOfDepartment = (departmentId: number) => {
    setDepartmentFilter(String(departmentId));
    setPage(1);
    setTab('members');
  };

  const createMutation = useCrudMutation<MemberResp, CreateMemberReq>({
    mutationFn: (payload) => membersApi.create(payload),
    successMessage: t('team.create_success'),
    queryKey: queryKeys.members(),
    onSuccess: () => {
      closeModal();
      invalidate();
    },
  });
  const updateMutation = useCrudMutation<MemberResp, { id: number; data: UpdateMemberReq }>({
    mutationFn: ({ id, data: payload }) => membersApi.update(id, payload),
    successMessage: t('team.update_success'),
    queryKey: queryKeys.members(),
    onSuccess: () => {
      closeModal();
      invalidate();
    },
  });
  const deleteMutation = useCrudMutation<unknown, number>({
    mutationFn: (id) => membersApi.delete(id),
    successMessage: t('team.delete_success'),
    queryKey: queryKeys.members(),
    onSuccess: () => {
      setDeleteTarget(null);
      invalidate();
      queryClient.invalidateQueries({ queryKey: queryKeys.userKeys() });
    },
  });
  const resetMutation = useCrudMutation<MemberResp, number>({
    mutationFn: (id) => membersApi.resetPeriod(id),
    successMessage: t('team.reset_success'),
    queryKey: queryKeys.members(),
    onSuccess: () => setResetTarget(null),
  });
  const toggleStatusMutation = useMutation({
    mutationFn: ({ id, status }: { id: number; status: 'active' | 'disabled' }) =>
      membersApi.update(id, { status }),
    onSuccess: (_resp, variables) => {
      toast('success', variables.status === 'active' ? t('team.enable_success') : t('team.disable_success'));
      invalidate();
    },
    onError: (err: Error) => toast('error', err.message),
  });

  function openCreate() {
    setEditing(null);
    // 正在按某个部门筛选时，新建成员默认归到该部门
    setForm({ ...emptyMemberForm, department_id: departmentFilterID && departmentFilterID > 0 ? String(departmentFilterID) : '' });
    setModalOpen(true);
  }

  function openEdit(member: MemberResp) {
    setEditing(member);
    setForm({
      name: member.name,
      email: member.email,
      password: '',
      note: member.note,
      quota_usd: member.quota_usd > 0 ? String(member.quota_usd) : '',
      quota_period: member.quota_period,
      allowed_group_ids: member.allowed_group_ids ?? [],
      department_id: member.department_id > 0 ? String(member.department_id) : '',
    });
    setModalOpen(true);
  }

  function closeModal() {
    setModalOpen(false);
    setEditing(null);
    setForm(emptyMemberForm);
  }

  function handleSubmit() {
    const name = form.name.trim();
    if (!name) {
      toast('error', t('team.name_placeholder'));
      return;
    }
    const email = form.email.trim();
    const password = form.password;
    const quota = form.quota_usd.trim() ? Number(form.quota_usd) : 0;
    if (!Number.isFinite(quota) || quota < 0) {
      toast('error', t('team.quota_hint'));
      return;
    }
    // 新建成员 = 开登录账号：邮箱与密码必填；编辑时密码留空表示不改。
    // 有登录账号的成员额度必填(成员控制台的"余额"就是本期剩余额度,0 = 不限没有意义);
    // 老模型无账号成员(has_account === false)沿用 0 = 不限。
    if (!editing || editing.has_account) {
      if (!email && !(editing && isManager)) {
        toast('error', t('team.email_required'));
        return;
      }
      if (quota <= 0) {
        toast('error', t('team.quota_required'));
        return;
      }
    }
    if (!editing && password.length < 6) {
      toast('error', t('team.password_hint'));
      return;
    }
    if (editing && !isManager && password && password.length < 6) {
      toast('error', t('team.password_hint'));
      return;
    }
    // 0 = 调出部门（未分配）；只有建过部门时表单才出现这一项。
    // 负责人一律钉死本部门：跨部门调岗是企业主的动作，服务端也会拒。
    const departmentPayload = isManager
      ? { department_id: access.managedDepartmentId }
      : hasDepartments
        ? { department_id: form.department_id ? Number(form.department_id) : 0 }
        : {};
    if (editing) {
      // 负责人改不了他人的登录凭证（邮箱是全站唯一身份、密码等于可冒用其账号），两项都不提交。
      const identityPayload = isManager ? {} : { email, ...(password ? { password } : {}) };
      updateMutation.mutate({
        id: editing.id,
        data: {
          name,
          ...identityPayload,
          note: form.note.trim(),
          quota_usd: quota,
          quota_period: form.quota_period,
          allowed_group_ids: form.allowed_group_ids,
          ...departmentPayload,
        },
      });
    } else {
      createMutation.mutate({
        name,
        email,
        password,
        note: form.note.trim(),
        quota_usd: quota,
        quota_period: form.quota_period,
        allowed_group_ids: form.allowed_group_ids,
        ...departmentPayload,
      });
    }
  }

  const saving = createMutation.isPending || updateMutation.isPending;
  const rows = data?.list ?? [];
  const total = data?.total ?? 0;
  const totalPages = getTotalPages(total, pageSize);
  const formatDate = (value?: string) => (value ? new Date(value).toLocaleDateString(i18n.language) : '');
  const tabs: Array<{ key: TeamTab; label: string; icon: typeof UsersRound }> = [
    { key: 'members', label: t('team.members_tab'), icon: UsersRound },
    ...(access.canManageDepartments ? [{ key: 'departments' as const, label: t('team.departments_tab'), icon: Building2 }] : []),
  ];
  const membersColSpan = hasDepartments ? 10 : 9;

  return (
    <div className="p-6">
      {/* 标题下一行弱化说明，替代原来的整条提示横幅；完整引导只在成员为空时出现 */}
      <p className="mb-4 text-[13px] leading-5 text-text-tertiary">
        {isManager
          ? t('team.manager_intro', { name: access.managedDepartmentName })
          : t('team.intro')}
      </p>

      <TeamOverviewBar isDepartmentManager={isManager} departmentName={access.managedDepartmentName} />

      {/* Tabs 只做页签切换：HeroUI 的 Select 放进 Tabs 内会拿不到 listbox 状态而崩，
          筛选控件与各面板内容一律放在 Tabs 之外按 tab 条件渲染 */}
      <div className="ag-page-toolbar mb-5">
        <Tabs className="ag-team-tabs" selectedKey={tab} onSelectionChange={(key) => setTab(key as TeamTab)}>
          <Tabs.ListContainer className="ag-page-tabs w-full sm:w-auto">
            <Tabs.List>
              {tabs.map((item, index) => {
                const Icon = item.icon;
                return (
                  <Tabs.Tab key={item.key} id={item.key}>
                    {index > 0 ? <Tabs.Separator /> : null}
                    <Tabs.Indicator />
                    <Icon className="w-4 h-4" />
                    {item.label}
                  </Tabs.Tab>
                );
              })}
            </Tabs.List>
          </Tabs.ListContainer>
        </Tabs>
        <div className="flex items-center gap-2 sm:ml-auto">
            {tab === 'members' && hasDepartments ? (
              <div className="w-44">
                <Select
                  aria-label={t('team.filter_department')}
                  fullWidth
                  selectedKey={departmentFilter}
                  onSelectionChange={(key) => { setDepartmentFilter(key == null ? '' : String(key)); setPage(1); }}
                >
                  <Select.Trigger>
                    <Select.Value>
                      {departmentFilter
                        ? departmentFilterOptions.find((item) => item.id === departmentFilter)?.label ?? departmentFilter
                        : <span className="text-text-tertiary">{t('team.filter_department')}</span>}
                    </Select.Value>
                    <Select.Indicator />
                  </Select.Trigger>
                  <Select.Popover className="w-[var(--trigger-width)]">
                    <ListBox items={departmentFilterOptions}>
                      {(item) => (
                        <ListBox.Item id={item.id} textValue={item.label}>
                          <span className="block truncate">{item.label}</span>
                        </ListBox.Item>
                      )}
                    </ListBox>
                  </Select.Popover>
                </Select>
              </div>
            ) : null}
            <Button
              isIconOnly
              aria-label={t('common.refresh', 'Refresh')}
              size="md"
              variant="ghost"
              onPress={() => { refetch(); invalidate(); }}
            >
              <RefreshCw className="h-4 w-4" />
            </Button>
            {tab === 'members' ? (
              <Button variant="primary" onPress={openCreate}>
                <Plus className="h-4 w-4" />
                {t('team.create')}
              </Button>
            ) : tab === 'departments' && access.canManageDepartments ? (
              <Button variant="primary" onPress={() => setDeptCreateOpen(true)}>
                <Plus className="h-4 w-4" />
                {t('team.dept_create')}
              </Button>
            ) : null}
        </div>
      </div>

      {tab === 'departments' && access.canManageDepartments ? (
        <DepartmentsTab createOpen={deptCreateOpen} onCreateClose={() => setDeptCreateOpen(false)} onViewMembers={showMembersOfDepartment} />
      ) : null}
      {tab === 'members' ? (
      <CommonTable
        ariaLabel={t('team.title')}
        footer={(
          <TablePaginationFooter
            page={page}
            pageSize={pageSize}
            setPage={setPage}
            setPageSize={setPageSize}
            total={total}
            totalPages={totalPages}
          />
        )}
        minWidth={960}
      >
        <CommonTable.Header>
          <CommonTable.Column id="name">{t('team.name')}</CommonTable.Column>
          {...(hasDepartments ? [<CommonTable.Column id="department" key="department" style={{ width: '8rem' }}>{t('team.department')}</CommonTable.Column>] : [])}
          <CommonTable.Column id="status">{t('common.status')}</CommonTable.Column>
          <CommonTable.Column id="account" style={{ width: '7rem' }}>{t('team.account_col', '登录账号')}</CommonTable.Column>
          <CommonTable.Column id="quota" style={{ width: '15rem' }}>{t('team.quota_label')}</CommonTable.Column>
          <CommonTable.Column id="period" style={{ width: '9rem' }}>{t('team.period_col', '周期')}</CommonTable.Column>
          <CommonTable.Column id="groups" style={{ width: '8rem' }}>{t('team.groups')}</CommonTable.Column>
          <CommonTable.Column id="usage" style={{ width: '11.5rem' }}>{t('api_keys.usage')}</CommonTable.Column>
          <CommonTable.Column id="keys" style={{ width: '9rem' }}>{t('team.keys')}</CommonTable.Column>
          <CommonTable.Column id="actions" style={{ width: 132 }}>{t('common.actions')}</CommonTable.Column>
        </CommonTable.Header>
        <CommonTable.Body>
          {isLoading ? (
            <TableLoadingRow colSpan={membersColSpan} />
          ) : rows.length === 0 ? (
            <CommonTable.Row id="empty">
              <CommonTable.Cell colSpan={membersColSpan}>
                <EmptyState>
                  <div className="text-sm text-default-500">{t('team.empty_hint')}</div>
                </EmptyState>
              </CommonTable.Cell>
            </CommonTable.Row>
          ) : (
            rows.map((row) => {
              const unlimited = row.quota_usd <= 0;
              const pct = unlimited ? 0 : Math.min((row.period_used / row.quota_usd) * 100, 100);
              return (
                <CommonTable.Row id={String(row.id)} key={row.id}>
                  <CommonTable.Cell>
                    <div className="min-w-0">
                      <div className="ag-cell-2line font-medium text-text">{row.name}</div>
                      {row.email ? (
                        <div className="truncate text-xs text-text-tertiary" title={row.email}>{row.email}</div>
                      ) : null}
                      {row.note ? (
                        <div className="truncate text-xs text-text-tertiary" title={row.note}>{row.note}</div>
                      ) : null}
                    </div>
                  </CommonTable.Cell>
                  {...(hasDepartments ? [(
                    <CommonTable.Cell key="department">
                      <span className="truncate text-xs text-text-secondary" title={row.department_name}>
                        {row.department_id > 0 ? row.department_name || t('team.department_deleted') : t('team.department_none')}
                      </span>
                    </CommonTable.Cell>
                  )] : [])}
                  <CommonTable.Cell>
                    <StatusChip status={row.status} />
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    {row.has_account ? (
                      <span className="text-xs text-text-secondary">{t('team.account_opened', '已开通')}</span>
                    ) : (
                      <span className="text-xs text-warning" title={t('team.no_account_hint')}>{t('team.no_account')}</span>
                    )}
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    {/* 额度:一行「已用 / 总额」+ 细进度条 + 周期说明,替代两枚彩色徽记 */}
                    <div className="ag-quota-cell">
                      <div className="ag-quota-cell-line">
                        <b>${row.period_used.toFixed(2)}</b>
                        <span>/ {unlimited ? '∞' : `$${row.quota_usd.toFixed(2)}`}</span>
                      </div>
                      {!unlimited ? (
                        <div className="ag-quota-bar" aria-hidden="true">
                          <i data-tone={pct >= 90 ? 'danger' : pct >= 70 ? 'warning' : undefined} style={{ width: `${pct}%` }} />
                        </div>
                      ) : null}
                    </div>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <span className="text-xs text-text-secondary">
                      {row.quota_period === 'monthly'
                        ? t('team.period_ends', { date: formatDate(row.period_end) })
                        : t('team.period_none')}
                    </span>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <span className="text-sm text-text-secondary">
                      {row.allowed_group_ids.length > 0
                        ? t('team.groups_count', { count: row.allowed_group_ids.length })
                        : t('team.groups_all')}
                    </span>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    {/* 用量:一行三段,零值弱化;累计取 used_quota_actual(主账号实付,与今日 / 30 天同基准),
                        与额度列的「本期已用」(账面口径,受 sell_rate 影响)刻意不混 */}
                    <span className="ag-usage-line">
                      <span>{t('team.usage_today')}</span><b data-zero={row.today_cost === 0}>${row.today_cost.toFixed(2)}</b>
                      <span>{t('team.usage_30d')}</span><b data-zero={row.thirty_day_cost === 0}>${row.thirty_day_cost.toFixed(2)}</b>
                      <span>{t('team.cumulative')}</span><b data-zero={row.used_quota_actual === 0}>${row.used_quota_actual.toFixed(2)}</b>
                    </span>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    {/* 密钥 / 用量的下钻页都是「看自己」的用户页：负责人点过去只会看到自己的数据，
                        与其给个会误导的入口，不如只显示数字（跨成员的用量视图仍是企业主能力）。 */}
                    {isManager ? (
                      <span className="ag-usage-line">
                        <KeyRound className="h-3.5 w-3.5" />
                        <b data-zero={row.key_count === 0}>{t('team.keys_count', { count: row.key_count })}</b>
                      </span>
                    ) : (
                      <Button
                        size="sm"
                        variant="ghost"
                        onPress={() => navigate({ to: '/keys', search: { member_id: row.id } })}
                      >
                        <KeyRound className="h-3.5 w-3.5" />
                        {t('team.keys_count', { count: row.key_count })}
                      </Button>
                    )}
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <div className="flex items-center gap-1">
                      {/* 负责人自己那条记录只读：额度与停用都得回到企业主手里（服务端同样拒绝） */}
                      {canEditMemberRow(access, user, row.id) ? (
                        <Button isIconOnly aria-label={t('team.edit')} size="sm" variant="ghost" onPress={() => openEdit(row)}>
                          <Pencil className="h-4 w-4" />
                        </Button>
                      ) : null}
                      {!isManager ? (
                        <Button
                          isIconOnly
                          aria-label={t('team.view_usage')}
                          size="sm"
                          variant="ghost"
                          onPress={() => navigate({ to: '/usage', search: { member_id: row.id } })}
                        >
                          <ReceiptText className="h-4 w-4" />
                        </Button>
                      ) : null}
                      {!canEditMemberRow(access, user, row.id) ? (
                        <span className="text-xs text-text-tertiary" title={t('team.self_row_readonly_hint')}>
                          {t('team.self_row_readonly')}
                        </span>
                      ) : (
                      <Dropdown>
                        <Button isIconOnly aria-label={t('common.more')} size="sm" variant="ghost">
                          <MoreHorizontal className="h-4 w-4" />
                        </Button>
                        <Dropdown.Popover placement="bottom end">
                          <Dropdown.Menu
                            aria-label={t('common.actions')}
                            onAction={(key) => {
                              if (key === 'reset') setResetTarget(row);
                              else if (key === 'toggle') toggleStatusMutation.mutate({ id: row.id, status: row.status === 'active' ? 'disabled' : 'active' });
                              else if (key === 'delete') setDeleteTarget(row);
                            }}
                          >
                            <Dropdown.Item id="reset" textValue={t('team.reset_period')}>
                              <span className="flex items-center gap-2">
                                <RotateCcw className="w-3.5 h-3.5" />
                                {t('team.reset_period')}
                              </span>
                            </Dropdown.Item>
                            <Dropdown.Item id="toggle" textValue={row.status === 'active' ? t('common.disable') : t('common.enable')}>
                              <span className="flex items-center gap-2">
                                {row.status === 'active' ? <Ban className="w-3.5 h-3.5" /> : <CheckCircle className="w-3.5 h-3.5" />}
                                {row.status === 'active' ? t('common.disable') : t('common.enable')}
                              </span>
                            </Dropdown.Item>
                            <Dropdown.Item id="delete" className="text-danger" textValue={t('team.delete_member')}>
                              <span className="flex items-center gap-2">
                                <Trash2 className="w-3.5 h-3.5" />
                                {t('team.delete_member')}
                              </span>
                            </Dropdown.Item>
                          </Dropdown.Menu>
                        </Dropdown.Popover>
                      </Dropdown>
                      )}
                    </div>
                  </CommonTable.Cell>
                </CommonTable.Row>
              );
            })
          )}
        </CommonTable.Body>
      </CommonTable>
      ) : null}

      <EditMemberModal
        open={modalOpen}
        isEdit={!!editing}
        hasAccount={!!editing?.has_account}
        canEditIdentity={!isManager || !editing}
        form={form}
        setForm={setForm}
        onClose={closeModal}
        onSubmit={handleSubmit}
        loading={saving}
        departmentOptions={hasDepartments ? departmentOptions : []}
      />

      {/* 重置本期确认 */}
      <AlertDialog
        isOpen={!!resetTarget}
        onOpenChange={(open) => {
          if (!open) setResetTarget(null);
        }}
      >
        <DialogTriggerShim />
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="sm">
            <AlertDialog.Dialog className="ag-elevation-modal">
              <AlertDialog.Header>
                <AlertDialog.Icon status="warning" />
                <AlertDialog.Heading>{t('team.reset_period')}</AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>{t('team.reset_confirm', { name: resetTarget?.name })}</AlertDialog.Body>
              <AlertDialog.Footer>
                <Button variant="secondary" onPress={() => setResetTarget(null)}>
                  {t('common.cancel')}
                </Button>
                <Button
                  aria-busy={resetMutation.isPending}
                  isDisabled={resetMutation.isPending}
                  variant="primary"
                  onPress={() => resetTarget && resetMutation.mutate(resetTarget.id)}
                >
                  {resetMutation.isPending ? <Spinner size="sm" /> : null}
                  {t('common.confirm')}
                </Button>
              </AlertDialog.Footer>
            </AlertDialog.Dialog>
          </AlertDialog.Container>
        </AlertDialog.Backdrop>
      </AlertDialog>

      {/* 删除确认 */}
      <AlertDialog
        isOpen={!!deleteTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null);
        }}
      >
        <DialogTriggerShim />
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="sm">
            <AlertDialog.Dialog className="ag-elevation-modal">
              <AlertDialog.Header>
                <AlertDialog.Icon status="danger" />
                <AlertDialog.Heading>{t('team.delete_member')}</AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>{t('team.delete_confirm', { name: deleteTarget?.name })}</AlertDialog.Body>
              <AlertDialog.Footer>
                <Button variant="secondary" onPress={() => setDeleteTarget(null)}>
                  {t('common.cancel')}
                </Button>
                <Button
                  aria-busy={deleteMutation.isPending}
                  isDisabled={deleteMutation.isPending}
                  variant="danger"
                  onPress={() => deleteTarget && deleteMutation.mutate(deleteTarget.id)}
                >
                  {deleteMutation.isPending ? <Spinner size="sm" /> : null}
                  {t('common.confirm')}
                </Button>
              </AlertDialog.Footer>
            </AlertDialog.Dialog>
          </AlertDialog.Container>
        </AlertDialog.Backdrop>
      </AlertDialog>
    </div>
  );
}

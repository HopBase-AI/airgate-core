import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, EmptyState, ListBox, Select } from '@heroui/react';
import { RefreshCw } from 'lucide-react';
import { CommonTable } from '../../shared/components/CommonTable';
import { TableLoadingRow } from '../../shared/components/TableLoadingRow';
import { TablePaginationFooter } from '../../shared/components/TablePaginationFooter';
import { UsageDateRangeFilter } from '../../shared/components/UsageDateRangeFilter';
import { departmentsApi } from '../../shared/api/departments';
import { membersApi } from '../../shared/api/members';
import { groupsApi } from '../../shared/api/groups';
import { localizedGroupText } from '../../shared/groupText';
import { queryKeys } from '../../shared/queryKeys';
import { FETCH_ALL_PARAMS } from '../../shared/constants';
import { usePagination } from '../../shared/hooks/usePagination';
import { getTotalPages } from '../../shared/utils/pagination';
import type { TeamAuditLogResp } from '../../shared/types';

const TARGET_TYPES = ['department', 'member', 'apikey', 'team'] as const;

// 变更前后快照里值得给企业主看的字段（其余是内部字段），按 key 做 i18n；顺序即展示顺序。
const DIFF_FIELDS = ['name', 'email', 'quota_usd', 'quota_period', 'status', 'department_id', 'manager_member_id', 'member_id', 'group_id', 'allowed_group_ids', 'sell_rate', 'max_concurrency', 'expires_at', 'billing_day', 'password_reset', 'note'] as const;
type DiffField = (typeof DIFF_FIELDS)[number];
// 新建只列「是什么」：名称、额度、周期、归属；状态 / 邮箱 / 备注这类快照字段是噪音
const CREATE_FIELDS = new Set<DiffField>(['name', 'quota_usd', 'quota_period', 'department_id', 'manager_member_id', 'member_id', 'group_id', 'expires_at']);
const MONEY_FIELDS = new Set<DiffField>(['quota_usd']);
// 一行内最多展示两条变更，其余折成「+N 项」，完整列表进 title
const INLINE_LINES = 2;

type Translate = (key: string, options?: Record<string, unknown>) => string;

interface ValueResolver {
  department: (id: number) => string;
  member: (id: number) => string;
  manager: (id: number) => string;
  group: (id: number) => string;
  groups: (ids: number[]) => string;
}

function isEmpty(value: unknown): boolean {
  if (value == null || value === '' || value === 0 || value === false) return true;
  return Array.isArray(value) && value.length === 0;
}

function toID(value: unknown): number {
  const n = typeof value === 'number' ? value : Number(value);
  return Number.isFinite(n) ? n : 0;
}

// 把快照原值翻成人话：枚举走 i18n、外键换成名称、金额带 $、时间按本地格式
function fmtValue(field: DiffField, value: unknown, t: Translate, lang: string, resolve: ValueResolver): string {
  if (value == null || value === '') return '—';
  switch (field) {
    case 'quota_period':
      return value === 'monthly' ? t('team.period_monthly') : value === 'none' ? t('team.period_none') : String(value);
    case 'status':
      return value === 'active' ? t('status.active') : value === 'disabled' ? t('status.disabled') : String(value);
    case 'department_id':
      return resolve.department(toID(value));
    case 'manager_member_id':
      return resolve.manager(toID(value));
    case 'member_id':
      return resolve.member(toID(value));
    case 'group_id':
      return resolve.group(toID(value));
    case 'allowed_group_ids':
      return Array.isArray(value) ? resolve.groups(value.map(toID)) : String(value);
    case 'expires_at': {
      const date = new Date(String(value));
      return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString(lang);
    }
    case 'billing_day':
      return t('team.overview_billing_day', { day: value });
    default:
      break;
  }
  if (typeof value === 'boolean') return value ? t('common.yes') : t('common.no');
  if (typeof value === 'number') {
    if (MONEY_FIELDS.has(field)) return `$${value.toFixed(2)}`;
    return Number.isInteger(value) ? String(value) : value.toFixed(2);
  }
  if (Array.isArray(value)) return value.length === 0 ? '—' : value.join(', ');
  if (typeof value === 'object') return JSON.stringify(value);
  return String(value);
}

// 只列出前后有差异的字段：创建 = 关键字段的 after；删除 = 全部 before；更新 = 变了的那几项。
// 前后都是空 / 零的字段（「备注：—」）不占行。
function diffLines(entry: TeamAuditLogResp, t: Translate, lang: string, resolve: ValueResolver): string[] {
  const before = entry.before ?? {};
  const after = entry.after ?? {};
  const isCreate = entry.action.endsWith('.create');
  const sep = lang === 'zh' || lang === 'zh-HK' || lang === 'ja' ? '：' : ': ';
  const lines: string[] = [];
  for (const field of DIFF_FIELDS) {
    if (isCreate && !CREATE_FIELDS.has(field)) continue;
    const b = before[field];
    const a = after[field];
    if (b === undefined && a === undefined) continue;
    if (isEmpty(b) && isEmpty(a)) continue;
    if (JSON.stringify(b) === JSON.stringify(a)) continue;
    const label = t(`team.audit_field_${field}`);
    if (b === undefined) lines.push(`${label}${sep}${fmtValue(field, a, t, lang, resolve)}`);
    else if (a === undefined) lines.push(`${label}${sep}${fmtValue(field, b, t, lang, resolve)}`);
    else lines.push(`${label}${sep}${fmtValue(field, b, t, lang, resolve)} → ${fmtValue(field, a, t, lang, resolve)}`);
  }
  return lines;
}

// 操作记录：组织调整 / 成员变更 / 权限与额度调整 / 密钥操作 / 账期改动全程可查、可追溯。
// 独立成页挂在侧栏「团队」段下：与「团队管理」的额度经营是两件事，页签里挤着反而找不到。
export default function TeamAuditPage() {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const { page, setPage, pageSize, setPageSize } = usePagination(20, 'user.team.audit');
  const [targetType, setTargetType] = useState<'' | (typeof TARGET_TYPES)[number]>('');
  const [startDate, setStartDate] = useState<string | undefined>();
  const [endDate, setEndDate] = useState<string | undefined>();

  const params = {
    page,
    page_size: pageSize,
    target_type: targetType || undefined,
    start_date: startDate,
    end_date: endDate,
    tz: Intl.DateTimeFormat().resolvedOptions().timeZone,
  };
  const { data, isLoading, refetch } = useQuery({
    queryKey: queryKeys.teamAuditLogs(params),
    queryFn: ({ signal }) => departmentsApi.auditLogs(params, { signal }),
    placeholderData: keepPreviousData,
  });
  // 快照里只有外键 ID，展示时换成当前的部门 / 成员 / 分组名称；已删除的对象按「已删除」标注
  const { data: departmentsData } = useQuery({
    queryKey: queryKeys.departmentsAll(),
    queryFn: () => departmentsApi.list(FETCH_ALL_PARAMS),
    staleTime: 60_000,
  });
  const { data: membersData } = useQuery({
    queryKey: queryKeys.membersForKeys(),
    queryFn: () => membersApi.list(FETCH_ALL_PARAMS),
    staleTime: 60_000,
  });
  const { data: groupsData } = useQuery({
    queryKey: queryKeys.groupsForKeys(),
    queryFn: () => groupsApi.listAvailable(FETCH_ALL_PARAMS),
    staleTime: 60_000,
  });
  const lang = i18n.language;
  const resolve = useMemo<ValueResolver>(() => {
    const departments = new Map((departmentsData?.list ?? []).map((dept) => [dept.id, dept.name]));
    const members = new Map((membersData?.list ?? []).map((member) => [member.id, member.name]));
    const groups = new Map((groupsData?.list ?? []).map((group) => [group.id, localizedGroupText(group.name, group.name_i18n, lang)]));
    const groupName = (id: number) => groups.get(id) ?? `#${id}`;
    return {
      department: (id) => (id === 0 ? t('team.department_none') : departments.get(id) ?? t('team.department_deleted')),
      member: (id) => (id === 0 ? t('team.no_member') : members.get(id) ?? t('team.member_deleted')),
      manager: (id) => (id === 0 ? t('team.dept_manager_none') : members.get(id) ?? t('team.member_deleted')),
      group: groupName,
      groups: (ids) => (ids.length === 0 ? t('team.groups_all') : ids.map(groupName).join(', ')),
    };
  }, [departmentsData?.list, groupsData?.list, lang, membersData?.list, t]);

  // 刷新要连名称映射一起刷:三张 lookup 的 staleTime 是 60s,刚改完部门名再点刷新否则仍显示旧名
  const refreshLookups = () => {
    void queryClient.invalidateQueries({ queryKey: queryKeys.departmentsAll() });
    void queryClient.invalidateQueries({ queryKey: queryKeys.membersForKeys() });
    void queryClient.invalidateQueries({ queryKey: queryKeys.groupsForKeys() });
  };

  const rows = data?.list ?? [];
  const total = data?.total ?? 0;
  const typeOptions = [
    { id: '', label: t('common.all') },
    ...TARGET_TYPES.map((type) => ({ id: type, label: t(`team.audit_target_${type}`) })),
  ];

  return (
    <div className="p-6">
      {/* 标题由 AppShell 按侧栏项渲染；这里只留一行弱化说明 */}
      <p className="mb-4 text-[13px] leading-5 text-text-tertiary">{t('team.audit_intro')}</p>

      {/* 筛选条不做盒子：分区靠间距，刷新按钮并进同一行末尾，避免多出一条横线 */}
      <div className="ag-filter-bar flex flex-col sm:flex-row items-stretch sm:items-center gap-3 mb-5 flex-wrap">
        <div className="w-full sm:w-auto">
          <UsageDateRangeFilter
            clearLabel={t('common.clear')}
            endDate={endDate}
            endTimeLabel={t('usage.end_time')}
            label={t('usage.time_range')}
            startDate={startDate}
            startTimeLabel={t('usage.start_time')}
            onChange={(start, end) => {
              setPage(1);
              setStartDate(start);
              setEndDate(end);
            }}
          />
        </div>
        <div className="w-full sm:w-44">
          <Select
            aria-label={t('team.audit_target')}
            fullWidth
            selectedKey={targetType}
            onSelectionChange={(key) => { setTargetType(key == null ? '' : (String(key) as typeof targetType)); setPage(1); }}
          >
            <Select.Trigger>
              <Select.Value>
                {targetType ? typeOptions.find((item) => item.id === targetType)?.label : <span className="text-text-tertiary">{t('team.audit_target')}</span>}
              </Select.Value>
              <Select.Indicator />
            </Select.Trigger>
            <Select.Popover className="w-[var(--trigger-width)]">
              <ListBox items={typeOptions}>
                {(item) => (
                  <ListBox.Item id={item.id} textValue={item.label}>{item.label}</ListBox.Item>
                )}
              </ListBox>
            </Select.Popover>
          </Select>
        </div>
        {/* 刷新按钮靠右成组:与用户管理 / 分组等筛选条一致(sm:ml-auto),不跟筛选控件挤在一起 */}
        <div className="flex items-center gap-2 sm:ml-auto">
          <Button
            isIconOnly
            aria-label={t('common.refresh', 'Refresh')}
            size="sm"
            variant="ghost"
            onPress={() => { void refetch(); refreshLookups(); }}
          >
            <RefreshCw className="h-4 w-4" />
          </Button>
        </div>
      </div>

      <CommonTable
        ariaLabel={t('nav.my_team_audit')}
        footer={(
          <TablePaginationFooter page={page} pageSize={pageSize} setPage={setPage} setPageSize={setPageSize} total={total} totalPages={getTotalPages(total, pageSize)} />
        )}
        minWidth={880}
      >
        <CommonTable.Header>
          <CommonTable.Column id="time" style={{ width: '9rem' }}>{t('team.audit_time')}</CommonTable.Column>
          <CommonTable.Column id="actor" style={{ width: '12rem' }}>{t('team.audit_actor')}</CommonTable.Column>
          <CommonTable.Column id="action" style={{ width: '7rem' }}>{t('team.audit_action')}</CommonTable.Column>
          <CommonTable.Column id="target" style={{ width: '10rem' }}>{t('team.audit_target')}</CommonTable.Column>
          <CommonTable.Column id="detail">{t('team.audit_detail')}</CommonTable.Column>
        </CommonTable.Header>
        <CommonTable.Body>
          {isLoading ? (
            <TableLoadingRow colSpan={5} />
          ) : rows.length === 0 ? (
            <CommonTable.Row id="empty">
              <CommonTable.Cell colSpan={5}>
                <EmptyState>
                  <div className="text-sm text-default-500">{t('team.audit_empty')}</div>
                </EmptyState>
              </CommonTable.Cell>
            </CommonTable.Row>
          ) : (
            rows.map((row) => {
              const lines = diffLines(row, t, lang, resolve);
              const hidden = lines.length - INLINE_LINES;
              return (
                <CommonTable.Row id={String(row.id)} key={row.id}>
                  <CommonTable.Cell>
                    <span className="text-xs text-text-secondary">{new Date(row.created_at).toLocaleString(lang)}</span>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <div className="min-w-0">
                      <div className="truncate text-sm text-text" title={row.actor_email}>{row.actor_email || `#${row.actor_user_id}`}</div>
                      {row.ip ? <div className="truncate text-[11px] leading-4 text-text-tertiary">{row.ip}</div> : null}
                    </div>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <span className="text-sm text-text">{t(`team.audit_action_${row.action.replace('.', '_')}`)}</span>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <div className="min-w-0">
                      <div className="truncate text-sm text-text" title={row.target_name}>{row.target_name || `#${row.target_id}`}</div>
                      <div className="text-xs text-text-tertiary">{t(`team.audit_target_${row.target_type}`)}</div>
                    </div>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    {lines.length > 0 ? (
                      <ul className="space-y-0.5 text-xs text-text-secondary" title={lines.join('\n')}>
                        {lines.slice(0, INLINE_LINES).map((line) => <li key={line} className="truncate">{line}</li>)}
                        {hidden > 0 ? <li className="text-text-tertiary">{t('team.audit_more', { count: hidden })}</li> : null}
                      </ul>
                    ) : (
                      <span className="text-xs text-text-tertiary">—</span>
                    )}
                  </CommonTable.Cell>
                </CommonTable.Row>
              );
            })
          )}
        </CommonTable.Body>
      </CommonTable>
    </div>
  );
}

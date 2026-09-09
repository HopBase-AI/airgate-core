import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { EmptyState, ListBox, Select } from '@heroui/react';
import { CommonTable } from '../../../shared/components/CommonTable';
import { TableLoadingRow } from '../../../shared/components/TableLoadingRow';
import { TablePaginationFooter } from '../../../shared/components/TablePaginationFooter';
import { UsageDateRangeFilter } from '../../../shared/components/UsageDateRangeFilter';
import { departmentsApi } from '../../../shared/api/departments';
import { queryKeys } from '../../../shared/queryKeys';
import { usePagination } from '../../../shared/hooks/usePagination';
import { getTotalPages } from '../../../shared/utils/pagination';
import type { TeamAuditLogResp } from '../../../shared/types';

const TARGET_TYPES = ['department', 'member', 'apikey', 'team'] as const;

// 变更前后快照里值得给企业主看的字段（其余是内部字段），按 key 做 i18n。
const DIFF_FIELDS = ['name', 'email', 'quota_usd', 'quota_period', 'status', 'department_id', 'member_id', 'group_id', 'allowed_group_ids', 'sell_rate', 'max_concurrency', 'expires_at', 'billing_day', 'password_reset', 'note'] as const;

function fmtValue(value: unknown): string {
  if (value == null || value === '') return '—';
  if (Array.isArray(value)) return value.length === 0 ? '—' : value.join(', ');
  if (typeof value === 'boolean') return value ? '✓' : '—';
  if (typeof value === 'number') return Number.isInteger(value) ? String(value) : value.toFixed(2);
  if (typeof value === 'object') return JSON.stringify(value);
  return String(value);
}

// 只列出前后有差异的字段：创建 = 全部 after；删除 = 全部 before；更新 = 变了的那几项。
function diffLines(entry: TeamAuditLogResp, t: (key: string) => string): string[] {
  const before = entry.before ?? {};
  const after = entry.after ?? {};
  const lines: string[] = [];
  for (const field of DIFF_FIELDS) {
    const b = before[field];
    const a = after[field];
    if (b === undefined && a === undefined) continue;
    if (JSON.stringify(b) === JSON.stringify(a)) continue;
    const label = t(`team.audit_field_${field}`);
    if (b === undefined) lines.push(`${label}: ${fmtValue(a)}`);
    else if (a === undefined) lines.push(`${label}: ${fmtValue(b)}`);
    else lines.push(`${label}: ${fmtValue(b)} → ${fmtValue(a)}`);
  }
  return lines;
}

// 操作记录：组织调整 / 成员变更 / 权限与额度调整 / 密钥操作 / 账期改动全程可查、可追溯。
export function AuditTab() {
  const { t, i18n } = useTranslation();
  const { page, setPage, pageSize, setPageSize } = usePagination(20, 'user.team.audit');
  const [targetType, setTargetType] = useState('');
  const [startDate, setStartDate] = useState<string | undefined>();
  const [endDate, setEndDate] = useState<string | undefined>();

  const params = {
    page,
    page_size: pageSize,
    target_type: (targetType || undefined) as (typeof TARGET_TYPES)[number] | undefined,
    start_date: startDate,
    end_date: endDate,
    tz: Intl.DateTimeFormat().resolvedOptions().timeZone,
  };
  const { data, isLoading } = useQuery({
    queryKey: queryKeys.teamAuditLogs(params),
    queryFn: ({ signal }) => departmentsApi.auditLogs(params, { signal }),
    placeholderData: keepPreviousData,
  });
  const rows = data?.list ?? [];
  const total = data?.total ?? 0;
  const typeOptions = [
    { id: '', label: t('common.all') },
    ...TARGET_TYPES.map((type) => ({ id: type, label: t(`team.audit_target_${type}`) })),
  ];

  return (
    <>
      <div className="ag-filter-bar mb-4 flex flex-col flex-wrap items-stretch gap-3 sm:flex-row sm:items-center">
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
            onSelectionChange={(key) => { setTargetType(key == null ? '' : String(key)); setPage(1); }}
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
      </div>

      <CommonTable
        ariaLabel={t('team.audit_tab')}
        footer={(
          <TablePaginationFooter page={page} pageSize={pageSize} setPage={setPage} setPageSize={setPageSize} total={total} totalPages={getTotalPages(total, pageSize)} />
        )}
        minWidth={880}
      >
        <CommonTable.Header>
          <CommonTable.Column id="time" style={{ width: '11rem' }}>{t('team.audit_time')}</CommonTable.Column>
          <CommonTable.Column id="actor" style={{ width: '14rem' }}>{t('team.audit_actor')}</CommonTable.Column>
          <CommonTable.Column id="action" style={{ width: '10rem' }}>{t('team.audit_action')}</CommonTable.Column>
          <CommonTable.Column id="target" style={{ width: '12rem' }}>{t('team.audit_target')}</CommonTable.Column>
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
              const lines = diffLines(row, t);
              return (
                <CommonTable.Row id={String(row.id)} key={row.id}>
                  <CommonTable.Cell>
                    <span className="text-xs text-text-secondary">{new Date(row.created_at).toLocaleString(i18n.language)}</span>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <div className="min-w-0">
                      <div className="truncate text-sm text-text" title={row.actor_email}>{row.actor_email || `#${row.actor_user_id}`}</div>
                      {row.ip ? <div className="truncate text-xs text-text-tertiary">{row.ip}</div> : null}
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
                      <ul className="space-y-0.5 text-xs text-text-secondary">
                        {lines.map((line) => <li key={line} className="truncate" title={line}>{line}</li>)}
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
    </>
  );
}

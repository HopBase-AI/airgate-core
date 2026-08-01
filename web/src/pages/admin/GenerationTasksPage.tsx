import { useCallback, useMemo, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query';
import { Card, Chip, EmptyState, Label, ListBox, Select } from '@heroui/react';
import { CircleCheck, CircleX, Clock3, LoaderCircle } from 'lucide-react';
import { generationTasksApi } from '../../shared/api/generationTasks';
import { queryKeys } from '../../shared/queryKeys';
import { usePagination } from '../../shared/hooks/usePagination';
import { ADMIN_AUTO_REFRESH_OPTIONS, usePersistentAutoRefresh } from '../../shared/hooks/usePersistentAutoRefresh';
import { AutoRefreshControl } from '../../shared/components/AutoRefreshControl';
import { CommonTable } from '../../shared/components/CommonTable';
import { TableLoadingRow } from '../../shared/components/TableLoadingRow';
import { TablePaginationFooter } from '../../shared/components/TablePaginationFooter';
import { StatusChip } from '../../shared/ui/display/StatusChip';
import { DEFAULT_PAGE_SIZE } from '../../shared/constants';
import { getTotalPages } from '../../shared/utils/pagination';
import type { GenerationTaskResp, GenerationTaskStatus } from '../../shared/types';

const AUTO_REFRESH_STORAGE_KEY = 'airgate.admin.generation_tasks.auto_refresh';

const TASK_STATUSES: GenerationTaskStatus[] = [
  'pending',
  'processing',
  'retrying',
  'failed',
  'completed',
  'cancelling',
  'cancelled',
];

const ACTIVE_STATUSES = new Set<GenerationTaskStatus>(['pending', 'processing', 'retrying', 'cancelling']);

function formatTime(value: string, locale: string) {
  return new Date(value).toLocaleString(locale, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

function formatDuration(seconds: number) {
  const safeSeconds = Math.max(0, Math.floor(seconds));
  if (safeSeconds < 60) return `${safeSeconds}s`;
  const minutes = Math.floor(safeSeconds / 60);
  if (minutes < 60) return `${minutes}m ${safeSeconds % 60}s`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ${minutes % 60}m`;
}

function elapsedSeconds(from?: string, to?: string) {
  if (!from) return 0;
  const start = Date.parse(from);
  const end = to ? Date.parse(to) : Date.now();
  if (!Number.isFinite(start) || !Number.isFinite(end)) return 0;
  return (end - start) / 1000;
}

function taskTypeLabel(taskType: string, t: ReturnType<typeof useTranslation>['t']) {
  const key = `generation_tasks.type_${taskType.replace(/\./g, '_')}`;
  return t(key, { defaultValue: taskType });
}

function MetricCard({
  accent,
  detail,
  icon,
  label,
  value,
}: {
  accent: string;
  detail: ReactNode;
  icon: ReactNode;
  label: string;
  value: ReactNode;
}) {
  return (
    <Card className="ag-dashboard-metric min-h-[92px]">
      <Card.Content className="ag-dashboard-metric-content p-3.5">
        <div className="min-w-0">
          <div className="truncate text-sm font-semibold text-text-tertiary">{label}</div>
          <div className="mt-1 font-mono text-2xl font-semibold leading-none text-text">{value}</div>
          <div className="mt-2 truncate text-xs text-text-secondary">{detail}</div>
        </div>
        <div
          className="flex h-10 w-10 shrink-0 items-center justify-center rounded-[var(--field-radius)] ring-1"
          style={{
            background: `color-mix(in srgb, ${accent} 14%, transparent)`,
            borderColor: `color-mix(in srgb, ${accent} 24%, transparent)`,
            color: accent,
          }}
        >
          {icon}
        </div>
      </Card.Content>
    </Card>
  );
}

function TaskStatusCell({ task }: { task: GenerationTaskResp }) {
  const { t } = useTranslation();
  const active = ACTIVE_STATUSES.has(task.status);
  const determinate = active && task.progress > 0;
  return (
    <div className="flex min-w-[8.5rem] flex-col gap-1.5">
      <div className="flex items-center gap-2">
        <StatusChip status={task.status} />
        {active ? <span className="font-mono text-[11px] text-text-tertiary">{task.progress}%</span> : null}
      </div>
      {active ? (
        <div
          aria-label={t('generation_tasks.progress_aria', { progress: task.progress })}
          aria-valuemax={100}
          aria-valuemin={0}
          aria-valuenow={determinate ? task.progress : undefined}
          className="h-1.5 w-28 overflow-hidden rounded-full bg-default-100"
          role="progressbar"
        >
          <span
            className={`block h-full rounded-full bg-accent ${determinate ? '' : 'w-1/3 animate-pulse'}`}
            style={determinate ? { width: `${Math.max(2, Math.min(100, task.progress))}%` } : undefined}
          />
        </div>
      ) : null}
      {task.stage ? <span className="max-w-36 truncate text-[11px] text-text-tertiary" title={task.stage}>{task.stage}</span> : null}
    </div>
  );
}

function TimingCell({ task }: { task: GenerationTaskResp }) {
  const { t } = useTranslation();
  const queueEnd = task.started_at || task.completed_at || (ACTIVE_STATUSES.has(task.status) ? undefined : task.updated_at);
  const runEnd = task.completed_at || (ACTIVE_STATUSES.has(task.status) ? undefined : task.updated_at);
  const queueSeconds = elapsedSeconds(task.created_at, queueEnd);
  const runSeconds = task.started_at ? elapsedSeconds(task.started_at, runEnd) : 0;
  return (
    <div className="flex min-w-0 flex-col gap-0.5 font-mono text-[11px] tabular-nums">
      <span title={t('generation_tasks.queue_time')}>{t('generation_tasks.queue_short')} {formatDuration(queueSeconds)}</span>
      <span className="text-text-tertiary" title={t('generation_tasks.run_time')}>
        {t('generation_tasks.run_short')} {task.started_at ? formatDuration(runSeconds) : '-'}
      </span>
    </div>
  );
}

function ErrorCell({ task, staleThresholdSeconds }: { task: GenerationTaskResp; staleThresholdSeconds: number }) {
  const { t } = useTranslation();
  const stale = task.status === 'processing' && elapsedSeconds(task.updated_at) >= staleThresholdSeconds;
  const labels = [task.error_type, task.error_code].filter(Boolean);
  const message = task.error_message || (stale ? t('generation_tasks.stale_task') : '');
  if (!message && labels.length === 0) {
    return <span className="text-text-tertiary">-</span>;
  }
  return (
    <div className="flex min-w-0 flex-col gap-1" title={message || labels.join(' · ')}>
      {labels.length > 0 ? (
        <div className="flex min-w-0 gap-1 overflow-hidden">
          {labels.map((label) => (
            <Chip key={label} color="danger" size="sm" variant="soft" className="max-w-40 shrink truncate font-mono">
              {label}
            </Chip>
          ))}
        </div>
      ) : null}
      {message ? <span className={`line-clamp-2 ${stale ? 'text-warning' : 'text-text'}`}>{message}</span> : null}
    </div>
  );
}

export default function GenerationTasksPage() {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const { page, pageSize, setPage, setPageSize } = usePagination(DEFAULT_PAGE_SIZE, 'admin.generation_tasks');
  const [statusFilter, setStatusFilter] = useState('');
  const [kindFilter, setKindFilter] = useState('');
  const [typeFilter, setTypeFilter] = useState('');
  const [pluginFilter, setPluginFilter] = useState('');
  const [autoRefresh, setAutoRefresh] = usePersistentAutoRefresh(
    AUTO_REFRESH_STORAGE_KEY,
    5,
    ADMIN_AUTO_REFRESH_OPTIONS,
  );

  const listQuery = useQuery({
    queryKey: queryKeys.generationTasks('list', page, pageSize, statusFilter, kindFilter, typeFilter, pluginFilter),
    queryFn: () => generationTasksApi.list({
      page,
      page_size: pageSize,
      status: statusFilter ? statusFilter as GenerationTaskStatus : undefined,
      kind: kindFilter || undefined,
      task_type: typeFilter || undefined,
      plugin_id: pluginFilter || undefined,
    }),
    meta: { globalLoading: false },
    placeholderData: keepPreviousData,
  });
  const summaryQuery = useQuery({
    queryKey: queryKeys.generationTasks('summary'),
    queryFn: generationTasksApi.summary,
    meta: { globalLoading: false },
  });

  const refreshTasks = useCallback(() => (
    queryClient.invalidateQueries({ queryKey: queryKeys.generationTasks() }, { cancelRefetch: false })
  ), [queryClient]);

  const summary = summaryQuery.data;
  const rows = listQuery.data?.list ?? [];
  const total = listQuery.data?.total ?? 0;
  const totalPages = getTotalPages(total, pageSize);
  const isFetching = listQuery.isFetching || summaryQuery.isFetching;
  const thresholdMinutes = Math.round((summary?.backlog_threshold_seconds ?? 300) / 60);
  const staleMinutes = Math.round((summary?.stale_threshold_seconds ?? 900) / 60);
  const oldestWait = summary?.oldest_queued_at ? formatDuration(elapsedSeconds(summary.oldest_queued_at)) : '-';

  const kindOptions = useMemo(() => [
    { id: '', label: t('generation_tasks.all_kinds') },
    { id: 'image', label: t('generation_tasks.kind_image') },
    { id: 'video', label: t('generation_tasks.kind_video') },
    { id: 'audio', label: t('generation_tasks.kind_audio') },
  ], [t]);
  const statusOptions = useMemo(() => [
    { id: '', label: t('generation_tasks.all_statuses') },
    ...TASK_STATUSES.map((status) => ({ id: status, label: t(`status.${status}`) })),
  ], [t]);
  const typeOptions = useMemo(() => [
    { id: '', label: t('generation_tasks.all_types') },
    ...(summary?.task_types ?? []).map((taskType) => ({ id: taskType, label: taskTypeLabel(taskType, t) })),
  ], [summary?.task_types, t]);
  const pluginOptions = useMemo(() => [
    { id: '', label: t('generation_tasks.all_plugins') },
    ...(summary?.plugins ?? []).map((plugin) => ({ id: plugin, label: plugin })),
  ], [summary?.plugins, t]);

  const filters = [
    { key: 'status', label: t('generation_tasks.status'), value: statusFilter, options: statusOptions, setValue: setStatusFilter },
    { key: 'kind', label: t('generation_tasks.kind'), value: kindFilter, options: kindOptions, setValue: setKindFilter },
    { key: 'type', label: t('generation_tasks.task_type'), value: typeFilter, options: typeOptions, setValue: setTypeFilter },
    { key: 'plugin', label: t('generation_tasks.plugin'), value: pluginFilter, options: pluginOptions, setValue: setPluginFilter },
  ];

  const healthy = (summary?.backlog ?? 0) === 0 && (summary?.stale_processing ?? 0) === 0;

  return (
    <div className="space-y-5">
      <div className="flex min-h-8 items-center justify-between gap-3">
        <h1 className="text-xl font-semibold text-text">{t('generation_tasks.title')}</h1>
        <Chip color={healthy ? 'success' : 'warning'} size="sm" variant="soft">
          {t(healthy ? 'generation_tasks.health_normal' : 'generation_tasks.health_attention')}
        </Chip>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <MetricCard
          accent={healthy ? 'var(--ag-success)' : 'var(--ag-warning)'}
          detail={summary?.queued
            ? t('generation_tasks.queue_detail', { count: summary.backlog, duration: oldestWait, minutes: thresholdMinutes })
            : t('generation_tasks.queue_empty')}
          icon={<Clock3 className="h-5 w-5" />}
          label={t('generation_tasks.queued')}
          value={summary?.queued ?? '-'}
        />
        <MetricCard
          accent={(summary?.stale_processing ?? 0) > 0 ? 'var(--ag-warning)' : 'var(--ag-primary)'}
          detail={t('generation_tasks.stale_detail', { count: summary?.stale_processing ?? 0, minutes: staleMinutes })}
          icon={<LoaderCircle className="h-5 w-5" />}
          label={t('generation_tasks.processing')}
          value={summary?.processing ?? '-'}
        />
        <MetricCard
          accent="var(--ag-danger)"
          detail={t('generation_tasks.failure_rate', { rate: ((summary?.failure_rate_recent ?? 0) * 100).toFixed(1) })}
          icon={<CircleX className="h-5 w-5" />}
          label={t('generation_tasks.failed_recent')}
          value={summary?.failed_recent ?? '-'}
        />
        <MetricCard
          accent="var(--ag-success)"
          detail={t('generation_tasks.cancelled_recent_detail', { count: summary?.cancelled_recent ?? 0 })}
          icon={<CircleCheck className="h-5 w-5" />}
          label={t('generation_tasks.completed_recent')}
          value={summary?.completed_recent ?? '-'}
        />
      </div>

      <div className="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center">
        {filters.map((filter) => (
          <div key={filter.key} className="w-full sm:w-44">
            <Select
              aria-label={filter.label}
              fullWidth
              selectedKey={filter.value}
              onSelectionChange={(key) => {
                filter.setValue(key == null ? '' : String(key));
                setPage(1);
              }}
            >
              <Label className="sr-only">{filter.label}</Label>
              <Select.Trigger>
                <Select.Value>
                  {filter.options.find((item) => item.id === filter.value)?.label ?? filter.options[0]?.label}
                </Select.Value>
                <Select.Indicator />
              </Select.Trigger>
              <Select.Popover>
                <ListBox items={filter.options}>
                  {(item) => <ListBox.Item id={item.id} textValue={item.label}>{item.label}</ListBox.Item>}
                </ListBox>
              </Select.Popover>
            </Select>
          </div>
        ))}
        <div className="flex items-center gap-2 sm:ml-auto">
          <AutoRefreshControl
            value={autoRefresh}
            options={ADMIN_AUTO_REFRESH_OPTIONS}
            label={t('accounts.auto_refresh')}
            offLabel={t('accounts.auto_refresh_off')}
            ariaLabel={t('accounts.auto_refresh')}
            refreshAriaLabel={t('common.refresh')}
            onChange={setAutoRefresh}
            onRefresh={refreshTasks}
            isRefreshing={isFetching}
          />
        </div>
      </div>

      <CommonTable
        ariaLabel={t('generation_tasks.title')}
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
        minWidth={1280}
      >
        <CommonTable.Header>
          <CommonTable.Column id="created_at" style={{ width: 150 }}>{t('generation_tasks.created_at')}</CommonTable.Column>
          <CommonTable.Column id="id" style={{ width: 205 }}>{t('generation_tasks.task')}</CommonTable.Column>
          <CommonTable.Column id="model" style={{ width: 190 }}>{t('generation_tasks.model_plugin')}</CommonTable.Column>
          <CommonTable.Column id="user" style={{ width: 180 }}>{t('generation_tasks.user')}</CommonTable.Column>
          <CommonTable.Column id="status" style={{ width: 160 }}>{t('generation_tasks.status')}</CommonTable.Column>
          <CommonTable.Column id="timing" style={{ width: 125 }}>{t('generation_tasks.timing')}</CommonTable.Column>
          <CommonTable.Column id="attempts" style={{ width: 82 }}>{t('generation_tasks.attempts')}</CommonTable.Column>
          <CommonTable.Column id="error">{t('generation_tasks.error')}</CommonTable.Column>
        </CommonTable.Header>
        <CommonTable.Body>
          {listQuery.isLoading ? (
            <TableLoadingRow colSpan={8} />
          ) : rows.length === 0 ? (
            <CommonTable.Row id="empty">
              <CommonTable.Cell colSpan={8}>
                <EmptyState>
                  <div className="text-sm text-default-500">{t('generation_tasks.empty')}</div>
                </EmptyState>
              </CommonTable.Cell>
            </CommonTable.Row>
          ) : rows.map((task) => (
            <CommonTable.Row id={task.id} key={task.id}>
              <CommonTable.Cell>
                <span className="whitespace-nowrap font-mono tabular-nums">{formatTime(task.created_at, i18n.language)}</span>
              </CommonTable.Cell>
              <CommonTable.Cell>
                <div className="flex min-w-0 flex-col gap-0.5">
                  <span className="truncate font-mono font-medium text-text" title={task.public_task_id || String(task.id)}>
                    {task.public_task_id || `#${task.id}`}
                  </span>
                  <span className="truncate text-[11px] text-text-tertiary" title={task.task_type}>{taskTypeLabel(task.task_type, t)}</span>
                </div>
              </CommonTable.Cell>
              <CommonTable.Cell>
                <div className="flex min-w-0 flex-col gap-0.5">
                  <span className="truncate font-mono text-text" title={task.model || undefined}>{task.model || '-'}</span>
                  <span className="truncate text-[11px] text-text-tertiary" title={task.plugin_id}>{task.plugin_id}</span>
                </div>
              </CommonTable.Cell>
              <CommonTable.Cell>
                <div className="flex min-w-0 flex-col gap-0.5" title={task.user_email || `#${task.user_id}`}>
                  <span className="truncate text-text">{task.user_email || `#${task.user_id}`}</span>
                  <span className="font-mono text-[11px] text-text-tertiary">#{task.user_id}</span>
                </div>
              </CommonTable.Cell>
              <CommonTable.Cell><TaskStatusCell task={task} /></CommonTable.Cell>
              <CommonTable.Cell><TimingCell task={task} /></CommonTable.Cell>
              <CommonTable.Cell>
                <span className={`font-mono ${task.attempts >= task.max_attempts ? 'text-danger' : 'text-text'}`}>
                  {task.attempts}/{task.max_attempts}
                </span>
              </CommonTable.Cell>
              <CommonTable.Cell className="max-w-0">
                <ErrorCell task={task} staleThresholdSeconds={summary?.stale_threshold_seconds ?? 900} />
              </CommonTable.Cell>
            </CommonTable.Row>
          ))}
        </CommonTable.Body>
      </CommonTable>
    </div>
  );
}

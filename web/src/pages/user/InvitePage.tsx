import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { Button, Chip, EmptyState } from '@heroui/react';
import { Copy, Gift, Users, Wallet } from 'lucide-react';
import { referralApi } from '../../shared/api/referral';
import { queryKeys } from '../../shared/queryKeys';
import { usePagination } from '../../shared/hooks/usePagination';
import { DEFAULT_PAGE_SIZE } from '../../shared/constants';
import { getTotalPages } from '../../shared/utils/pagination';
import { CommonTable } from '../../shared/components/CommonTable';
import { TableLoadingRow } from '../../shared/components/TableLoadingRow';
import { TablePaginationFooter } from '../../shared/components/TablePaginationFooter';
import { useToast } from '../../shared/ui';

function formatTime(date: string): string {
  return new Date(date).toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
}

function StatCard({ icon, label, value }: { icon: React.ReactNode; label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-center gap-3 rounded-[var(--radius)] border border-border bg-surface px-4 py-3.5">
      <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-[var(--field-radius)] bg-accent/10 text-accent">
        {icon}
      </span>
      <div className="min-w-0">
        <div className="text-xs" style={{ color: 'var(--ag-text-tertiary)' }}>{label}</div>
        <div className="truncate text-lg font-semibold tabular-nums" style={{ color: 'var(--ag-text)' }}>{value}</div>
      </div>
    </div>
  );
}

export default function InvitePage() {
  const { t } = useTranslation();
  const { toast } = useToast();
  const { page, setPage, pageSize, setPageSize } = usePagination(DEFAULT_PAGE_SIZE, 'user.referral');

  const { data: me, isLoading: meLoading } = useQuery({
    queryKey: queryKeys.referralMe(),
    queryFn: () => referralApi.me(),
  });

  const { data: commissions, isLoading: listLoading } = useQuery({
    queryKey: queryKeys.referralMyCommissions(page, pageSize),
    queryFn: () => referralApi.myCommissions({ page, page_size: pageSize }),
    meta: { globalLoading: false },
    placeholderData: keepPreviousData,
  });

  // 邀请链接：后台可配前缀（如指向落地页），未配置则用当前控制台域名。
  const inviteLink = useMemo(() => {
    if (!me?.invite_code) return '';
    const base = me.link_base_url?.trim() || window.location.origin;
    return `${base.replace(/\/+$/, '')}/?inv=${me.invite_code}`;
  }, [me?.invite_code, me?.link_base_url]);

  const copyLink = async () => {
    try {
      await navigator.clipboard.writeText(inviteLink);
      toast('success', t('referral.link_copied'));
    } catch {
      toast('error', t('referral.copy_failed'));
    }
  };

  const rows = commissions?.list ?? [];
  const total = commissions?.total ?? 0;
  const totalPages = getTotalPages(total, pageSize);

  return (
    <div className="space-y-5">
      {/* 邀请链接卡片 */}
      <div className="rounded-[var(--radius)] border border-border bg-surface p-5">
        <div className="mb-1 text-base font-semibold" style={{ color: 'var(--ag-text)' }}>
          {t('referral.title')}
        </div>
        <p className="mb-4 text-sm" style={{ color: 'var(--ag-text-secondary)' }}>
          {t('referral.subtitle')}
        </p>
        {me && !me.enabled && (
          <div className="mb-4 rounded-[var(--field-radius)] border border-warning/30 bg-warning/10 px-3 py-2 text-sm text-warning">
            {t('referral.disabled_hint')}
          </div>
        )}
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
          <code
            className="min-w-0 flex-1 truncate rounded-[var(--field-radius)] border border-border bg-background px-3 py-2 font-mono text-sm"
            style={{ color: 'var(--ag-text)' }}
            title={inviteLink}
          >
            {meLoading ? '…' : inviteLink || '-'}
          </code>
          <Button variant="primary" className="shrink-0 gap-1.5" onPress={copyLink} isDisabled={!inviteLink}>
            <Copy className="h-4 w-4" />
            {t('referral.copy_link')}
          </Button>
        </div>
        {me?.invite_code ? (
          <div className="mt-2 text-xs" style={{ color: 'var(--ag-text-tertiary)' }}>
            {t('referral.my_code')}: <span className="font-mono">{me.invite_code}</span>
          </div>
        ) : null}
      </div>

      {/* 统计 */}
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <StatCard icon={<Users className="h-5 w-5" />} label={t('referral.invitee_count')} value={me?.invitee_count ?? 0} />
        <StatCard icon={<Wallet className="h-5 w-5" />} label={t('referral.total_rebate')} value={`$${(me?.total_rebate ?? 0).toFixed(2)}`} />
        <StatCard icon={<Gift className="h-5 w-5" />} label={t('referral.total_reversed')} value={`$${(me?.total_reversed ?? 0).toFixed(2)}`} />
      </div>

      {/* 返利流水 */}
      <CommonTable
        ariaLabel={t('referral.records')}
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
        minWidth={640}
      >
        <CommonTable.Header>
          <CommonTable.Column id="time" style={{ width: 160 }}>{t('referral.col_time')}</CommonTable.Column>
          <CommonTable.Column id="invitee">{t('referral.col_invitee')}</CommonTable.Column>
          <CommonTable.Column id="paid" style={{ width: 120 }}>{t('referral.col_paid')}</CommonTable.Column>
          <CommonTable.Column id="rate" style={{ width: 88 }}>{t('referral.col_rate')}</CommonTable.Column>
          <CommonTable.Column id="amount" style={{ width: 120 }}>{t('referral.col_amount')}</CommonTable.Column>
          <CommonTable.Column id="status" style={{ width: 96 }}>{t('referral.col_status')}</CommonTable.Column>
        </CommonTable.Header>
        <CommonTable.Body>
          {listLoading ? (
            <TableLoadingRow colSpan={6} />
          ) : rows.length === 0 ? (
            <CommonTable.Row id="empty">
              <CommonTable.Cell colSpan={6}>
                <EmptyState>
                  <div className="text-sm text-default-500">{t('referral.empty')}</div>
                </EmptyState>
              </CommonTable.Cell>
            </CommonTable.Row>
          ) : (
            rows.map((row) => (
              <CommonTable.Row id={String(row.id)} key={row.id}>
                <CommonTable.Cell>
                  <span className="font-mono tabular-nums whitespace-nowrap">{formatTime(row.created_at)}</span>
                </CommonTable.Cell>
                <CommonTable.Cell>
                  <span className="truncate" style={{ color: 'var(--ag-text)' }}>{row.invitee_email}</span>
                </CommonTable.Cell>
                <CommonTable.Cell>
                  <span className="font-mono tabular-nums">${row.paid_amount.toFixed(2)}</span>
                </CommonTable.Cell>
                <CommonTable.Cell>
                  <span className="font-mono tabular-nums">{(row.rate * 100).toFixed(1)}%</span>
                </CommonTable.Cell>
                <CommonTable.Cell>
                  <span className="font-mono tabular-nums font-medium" style={{ color: 'var(--ag-success)' }}>
                    +${row.amount.toFixed(4)}
                  </span>
                </CommonTable.Cell>
                <CommonTable.Cell>
                  <Chip color={row.status === 'settled' ? 'success' : 'default'} size="sm" variant="soft">
                    {row.status === 'settled' ? t('referral.status_settled') : t('referral.status_reversed')}
                  </Chip>
                </CommonTable.Cell>
              </CommonTable.Row>
            ))
          )}
        </CommonTable.Body>
      </CommonTable>
    </div>
  );
}

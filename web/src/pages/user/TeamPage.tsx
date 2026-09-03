import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from '@tanstack/react-router';
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, AlertDialog, Button, Dropdown, EmptyState, Spinner } from '@heroui/react';
import { DialogTriggerShim } from '../../shared/components/DialogTriggerShim';
import { membersApi } from '../../shared/api/members';
import { usePagination } from '../../shared/hooks/usePagination';
import { useCrudMutation } from '../../shared/hooks/useCrudMutation';
import { useToast, StatusChip } from '../../shared/ui';
import { queryKeys } from '../../shared/queryKeys';
import { DEFAULT_PAGE_SIZE } from '../../shared/constants';
import { getTotalPages } from '../../shared/utils/pagination';
import { TablePaginationFooter } from '../../shared/components/TablePaginationFooter';
import { TableLoadingRow } from '../../shared/components/TableLoadingRow';
import { CommonTable } from '../../shared/components/CommonTable';
import { MetricChips } from '../../shared/components/MetricChips';
import {
  Ban,
  CheckCircle,
  Info,
  KeyRound,
  MoreHorizontal,
  Pencil,
  Plus,
  ReceiptText,
  RefreshCw,
  RotateCcw,
  Trash2,
} from 'lucide-react';
import type { CreateMemberReq, MemberResp, UpdateMemberReq } from '../../shared/types';
import { EditMemberModal } from './team/EditMemberModal';
import { type MemberForm, emptyMemberForm } from './team/types';

// 团队成员（企业子账号）：主账号侧的花名册——分配额度、看本期用量、管理密钥、停用/删除。
// 成员没有余额，所有消耗都从主账号扣；这里只呈现"额度闸门"与"用量归属"。
export default function TeamPage() {
  const { t, i18n } = useTranslation();
  const { toast } = useToast();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const { page, setPage, pageSize, setPageSize } = usePagination(DEFAULT_PAGE_SIZE, 'user.team');
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<MemberResp | null>(null);
  const [form, setForm] = useState<MemberForm>(emptyMemberForm);
  const [deleteTarget, setDeleteTarget] = useState<MemberResp | null>(null);
  const [resetTarget, setResetTarget] = useState<MemberResp | null>(null);

  const { data, isLoading, refetch } = useQuery({
    queryKey: queryKeys.members(page, pageSize),
    queryFn: () => membersApi.list({ page, page_size: pageSize }),
    placeholderData: keepPreviousData,
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: queryKeys.members() });
    queryClient.invalidateQueries({ queryKey: queryKeys.membersForKeys() });
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
    setForm(emptyMemberForm);
    setModalOpen(true);
  }

  function openEdit(member: MemberResp) {
    setEditing(member);
    setForm({
      name: member.name,
      email: member.email,
      note: member.note,
      quota_usd: member.quota_usd > 0 ? String(member.quota_usd) : '',
      quota_period: member.quota_period,
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
    const quota = form.quota_usd.trim() ? Number(form.quota_usd) : 0;
    if (!Number.isFinite(quota) || quota < 0) {
      toast('error', t('team.quota_hint'));
      return;
    }
    if (editing) {
      updateMutation.mutate({
        id: editing.id,
        data: {
          name,
          email: form.email.trim(),
          note: form.note.trim(),
          quota_usd: quota,
          quota_period: form.quota_period,
        },
      });
    } else {
      createMutation.mutate({
        name,
        email: form.email.trim(),
        note: form.note.trim(),
        quota_usd: quota,
        quota_period: form.quota_period,
      });
    }
  }

  const saving = createMutation.isPending || updateMutation.isPending;
  const rows = data?.list ?? [];
  const total = data?.total ?? 0;
  const totalPages = getTotalPages(total, pageSize);
  const formatDate = (value?: string) => (value ? new Date(value).toLocaleDateString(i18n.language) : '');

  return (
    <div className="p-6">
      <Alert className="mb-5" status="accent">
        <Alert.Indicator>
          <Info className="h-4 w-4" />
        </Alert.Indicator>
        <Alert.Content>
          <Alert.Description>
            {t('team.description')}
            {' '}
            {t('team.login_hint')}
          </Alert.Description>
        </Alert.Content>
      </Alert>

      <div className="mb-5 flex justify-end">
        <div className="ml-auto flex items-center gap-2">
          <Button
            isIconOnly
            aria-label={t('common.refresh', 'Refresh')}
            size="md"
            variant="ghost"
            onPress={() => refetch()}
          >
            <RefreshCw className="h-4 w-4" />
          </Button>
          <Button variant="primary" onPress={openCreate}>
            <Plus className="h-4 w-4" />
            {t('team.create')}
          </Button>
        </div>
      </div>

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
          <CommonTable.Column id="status">{t('common.status')}</CommonTable.Column>
          <CommonTable.Column id="quota" style={{ width: '18rem' }}>{t('team.quota_label')}</CommonTable.Column>
          <CommonTable.Column id="usage" style={{ width: '11.5rem' }}>{t('api_keys.usage')}</CommonTable.Column>
          <CommonTable.Column id="keys" style={{ width: '9rem' }}>{t('team.keys')}</CommonTable.Column>
          <CommonTable.Column id="actions" style={{ width: 132 }}>{t('common.actions')}</CommonTable.Column>
        </CommonTable.Header>
        <CommonTable.Body>
          {isLoading ? (
            <TableLoadingRow colSpan={6} />
          ) : rows.length === 0 ? (
            <CommonTable.Row id="empty">
              <CommonTable.Cell colSpan={6}>
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
                      <div className="truncate font-medium text-text">{row.name}</div>
                      {row.email ? (
                        <div className="truncate text-xs text-text-tertiary" title={row.email}>{row.email}</div>
                      ) : null}
                      {row.note ? (
                        <div className="truncate text-xs text-text-tertiary" title={row.note}>{row.note}</div>
                      ) : null}
                    </div>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <StatusChip status={row.status} />
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <div className="space-y-1">
                      <MetricChips
                        className="ag-metric-chips--quota"
                        items={[
                          {
                            amount: row.period_used,
                            color: pct >= 90 ? 'danger' : 'warning',
                            highlightDollar: true,
                            label: t('team.period_used'),
                          },
                          {
                            amount: unlimited ? undefined : row.quota_usd,
                            color: 'success',
                            label: t('user_keys.quota_total_short', 'Total'),
                            value: '∞',
                          },
                        ]}
                      />
                      <div className="text-xs text-text-tertiary">
                        {row.quota_period === 'monthly'
                          ? t('team.period_ends', { date: formatDate(row.period_end) })
                          : t('team.period_none')}
                      </div>
                    </div>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <MetricChips
                      className="ag-metric-chips--stack"
                      items={[
                        { amount: row.today_cost, color: 'default', label: t('team.usage_today'), mutedWhenZero: true },
                        { amount: row.thirty_day_cost, color: 'default', label: t('team.usage_30d'), mutedWhenZero: true },
                        // 累计取 used_quota_actual：主账号为该成员实际付出的金额，与今日/30 天同基准；
                        // 上面额度列的「本期已用」是账面口径(受 sell_rate 影响)，两者刻意不混。
                        { amount: row.used_quota_actual, color: 'default', label: t('team.cumulative'), mutedWhenZero: true },
                      ]}
                    />
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <Button
                      size="sm"
                      variant="outline"
                      onPress={() => navigate({ to: '/keys', search: { member_id: row.id } })}
                    >
                      <KeyRound className="h-3.5 w-3.5" />
                      {t('team.keys_count', { count: row.key_count })}
                    </Button>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <div className="flex items-center gap-1">
                      <Button isIconOnly aria-label={t('team.edit')} size="sm" variant="ghost" onPress={() => openEdit(row)}>
                        <Pencil className="h-4 w-4" />
                      </Button>
                      <Button
                        isIconOnly
                        aria-label={t('team.view_usage')}
                        size="sm"
                        variant="ghost"
                        onPress={() => navigate({ to: '/usage', search: { member_id: row.id } })}
                      >
                        <ReceiptText className="h-4 w-4" />
                      </Button>
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
                    </div>
                  </CommonTable.Cell>
                </CommonTable.Row>
              );
            })
          )}
        </CommonTable.Body>
      </CommonTable>

      <EditMemberModal
        open={modalOpen}
        isEdit={!!editing}
        form={form}
        setForm={setForm}
        onClose={closeModal}
        onSubmit={handleSubmit}
        loading={saving}
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

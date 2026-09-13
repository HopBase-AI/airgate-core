import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from '@tanstack/react-router';
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertDialog, Button, Dropdown, EmptyState, Spinner } from '@heroui/react';
import { MoreHorizontal, Pencil, ReceiptText, RotateCcw, Trash2, UsersRound } from 'lucide-react';
import { DialogTriggerShim } from '../../../shared/components/DialogTriggerShim';
import { CommonTable } from '../../../shared/components/CommonTable';
import { TableLoadingRow } from '../../../shared/components/TableLoadingRow';
import { TablePaginationFooter } from '../../../shared/components/TablePaginationFooter';
import { departmentsApi } from '../../../shared/api/departments';
import { queryKeys } from '../../../shared/queryKeys';
import { usePagination } from '../../../shared/hooks/usePagination';
import { useCrudMutation } from '../../../shared/hooks/useCrudMutation';
import { DEFAULT_PAGE_SIZE } from '../../../shared/constants';
import { getTotalPages } from '../../../shared/utils/pagination';
import type { CreateDepartmentReq, DepartmentResp, UpdateDepartmentReq } from '../../../shared/types';
import { EditDepartmentModal } from './EditDepartmentModal';
import { type DepartmentForm, emptyDepartmentForm } from './types';

// 部门 Tab：每行 = 额度 / 本期已用 / 剩余 / 使用率 / 周期截止 / 成员数 / 密钥数 / 已分配给成员；
// 点「成员」切到成员 Tab 并按部门筛选，点「用量」跳用量页按部门筛选——这就是客户要的分层下钻入口。
export function DepartmentsTab({
  createOpen,
  onCreateClose,
  onViewMembers,
}: {
  createOpen: boolean;
  onCreateClose: () => void;
  onViewMembers: (departmentId: number) => void;
}) {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { page, setPage, pageSize, setPageSize } = usePagination(DEFAULT_PAGE_SIZE, 'user.team.departments');
  const [editing, setEditing] = useState<DepartmentResp | null>(null);
  const [editOpen, setEditOpen] = useState(false);
  const [form, setForm] = useState<DepartmentForm>(emptyDepartmentForm);
  const [deleteTarget, setDeleteTarget] = useState<DepartmentResp | null>(null);
  const [resetTarget, setResetTarget] = useState<DepartmentResp | null>(null);

  const { data, isLoading } = useQuery({
    queryKey: queryKeys.departments(page, pageSize),
    queryFn: () => departmentsApi.list({ page, page_size: pageSize }),
    placeholderData: keepPreviousData,
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: queryKeys.departments() });
    queryClient.invalidateQueries({ queryKey: queryKeys.departmentsAll() });
    queryClient.invalidateQueries({ queryKey: queryKeys.teamOverview() });
    queryClient.invalidateQueries({ queryKey: queryKeys.members() });
    queryClient.invalidateQueries({ queryKey: queryKeys.teamAuditLogs() });
  };
  const closeModal = () => {
    setEditOpen(false);
    setEditing(null);
    setForm(emptyDepartmentForm);
    onCreateClose();
  };
  const createMutation = useCrudMutation<DepartmentResp, CreateDepartmentReq>({
    mutationFn: (payload) => departmentsApi.create(payload),
    successMessage: t('team.dept_create_success'),
    queryKey: queryKeys.departments(),
    onSuccess: () => {
      closeModal();
      invalidate();
    },
  });
  const updateMutation = useCrudMutation<DepartmentResp, { id: number; data: UpdateDepartmentReq }>({
    mutationFn: ({ id, data: payload }) => departmentsApi.update(id, payload),
    successMessage: t('team.dept_update_success'),
    queryKey: queryKeys.departments(),
    onSuccess: () => {
      closeModal();
      invalidate();
    },
  });
  const deleteMutation = useCrudMutation<unknown, number>({
    mutationFn: (id) => departmentsApi.delete(id),
    successMessage: t('team.dept_delete_success'),
    queryKey: queryKeys.departments(),
    onSuccess: () => {
      setDeleteTarget(null);
      invalidate();
      queryClient.invalidateQueries({ queryKey: queryKeys.userKeys() });
    },
  });
  const resetMutation = useCrudMutation<DepartmentResp, number>({
    mutationFn: (id) => departmentsApi.resetPeriod(id),
    successMessage: t('team.reset_success'),
    queryKey: queryKeys.departments(),
    onSuccess: () => {
      setResetTarget(null);
      invalidate();
    },
  });

  function openEdit(dept: DepartmentResp) {
    setEditing(dept);
    setForm({
      name: dept.name,
      note: dept.note,
      quota_usd: dept.quota_usd > 0 ? String(dept.quota_usd) : '',
      quota_period: dept.quota_period,
      manager_member_id: dept.manager_member_id > 0 ? String(dept.manager_member_id) : '',
    });
    setEditOpen(true);
  }

  function handleSubmit() {
    const name = form.name.trim();
    if (!name) return;
    const quota = form.quota_usd.trim() ? Number(form.quota_usd) : 0;
    if (!Number.isFinite(quota) || quota < 0) return;
    const payload = { name, note: form.note.trim(), quota_usd: quota, quota_period: form.quota_period };
    // 负责人只在编辑时可设：空 = 清空（传 0）；创建时新部门还没有成员，不传。
    if (editing) {
      const managerMemberId = form.manager_member_id ? Number(form.manager_member_id) : 0;
      updateMutation.mutate({ id: editing.id, data: { ...payload, manager_member_id: managerMemberId } });
    } else {
      createMutation.mutate(payload);
    }
  }

  const rows = data?.list ?? [];
  const total = data?.total ?? 0;
  const totalPages = getTotalPages(total, pageSize);
  const formatDate = (value?: string) => (value ? new Date(value).toLocaleDateString(i18n.language) : '');
  const modalOpen = editOpen || createOpen;

  return (
    <>
      <CommonTable
        ariaLabel={t('team.departments_tab')}
        footer={(
          <TablePaginationFooter page={page} pageSize={pageSize} setPage={setPage} setPageSize={setPageSize} total={total} totalPages={totalPages} />
        )}
        minWidth={960}
      >
        <CommonTable.Header>
          <CommonTable.Column id="name">{t('team.dept_name')}</CommonTable.Column>
          <CommonTable.Column id="quota" style={{ width: '16rem' }}>{t('team.dept_quota_col')}</CommonTable.Column>
          <CommonTable.Column id="period" style={{ width: '9rem' }}>{t('team.period_col')}</CommonTable.Column>
          <CommonTable.Column id="allocated" style={{ width: '11rem' }}>{t('team.dept_allocated_col')}</CommonTable.Column>
          <CommonTable.Column id="members" style={{ width: '8rem' }}>{t('team.dept_members_col')}</CommonTable.Column>
          <CommonTable.Column id="usage" style={{ width: '11.5rem' }}>{t('api_keys.usage')}</CommonTable.Column>
          <CommonTable.Column id="actions" style={{ width: 132 }}>{t('common.actions')}</CommonTable.Column>
        </CommonTable.Header>
        <CommonTable.Body>
          {isLoading ? (
            <TableLoadingRow colSpan={7} />
          ) : rows.length === 0 ? (
            <CommonTable.Row id="empty">
              <CommonTable.Cell colSpan={7}>
                <EmptyState>
                  <div className="text-sm text-default-500">{t('team.dept_empty_hint')}</div>
                </EmptyState>
              </CommonTable.Cell>
            </CommonTable.Row>
          ) : (
            rows.map((row) => {
              const unlimited = row.quota_usd <= 0;
              const pct = unlimited ? 0 : Math.min((row.period_used / row.quota_usd) * 100, 100);
              const overAllocated = !unlimited && row.member_quota_total > row.quota_usd;
              return (
                <CommonTable.Row id={String(row.id)} key={row.id}>
                  <CommonTable.Cell>
                    <div className="min-w-0">
                      <div className="ag-cell-2line font-medium text-text">{row.name}</div>
                      {row.manager_member_id > 0 && row.manager_name ? (
                        <div className="truncate text-xs text-text-secondary">{t('team.dept_manager_label', { name: row.manager_name })}</div>
                      ) : null}
                      {row.note ? <div className="truncate text-xs text-text-tertiary" title={row.note}>{row.note}</div> : null}
                    </div>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
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
                      {row.quota_period === 'monthly' ? t('team.period_ends', { date: formatDate(row.period_end) }) : t('team.period_none')}
                    </span>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <span className={overAllocated ? 'text-xs text-warning' : 'text-xs text-text-secondary'} title={overAllocated ? t('team.dept_over_allocated_hint') : undefined}>
                      ${row.member_quota_total.toFixed(2)}
                      {!unlimited ? ` / $${row.quota_usd.toFixed(2)}` : ''}
                      {overAllocated ? ' ⚠' : ''}
                    </span>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <Button size="sm" variant="ghost" onPress={() => onViewMembers(row.id)}>
                      <UsersRound className="h-3.5 w-3.5" />
                      {t('team.dept_members_count', { count: row.member_count })}
                    </Button>
                    <div className="text-xs text-text-tertiary">{t('team.keys_count', { count: row.key_count })}</div>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <span className="ag-usage-line">
                      <span>{t('team.usage_today')}</span><b data-zero={row.today_cost === 0}>${row.today_cost.toFixed(2)}</b>
                      <span>{t('team.usage_30d')}</span><b data-zero={row.thirty_day_cost === 0}>${row.thirty_day_cost.toFixed(2)}</b>
                      <span>{t('team.cumulative')}</span><b data-zero={row.used_quota_actual === 0}>${row.used_quota_actual.toFixed(2)}</b>
                    </span>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <div className="flex items-center gap-1">
                      <Button isIconOnly aria-label={t('team.dept_edit')} size="sm" variant="ghost" onPress={() => openEdit(row)}>
                        <Pencil className="h-4 w-4" />
                      </Button>
                      <Button
                        isIconOnly
                        aria-label={t('team.view_usage')}
                        size="sm"
                        variant="ghost"
                        onPress={() => navigate({ to: '/usage', search: { department_id: row.id } })}
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
                              else if (key === 'delete') setDeleteTarget(row);
                            }}
                          >
                            <Dropdown.Item id="reset" textValue={t('team.reset_period')}>
                              <span className="flex items-center gap-2"><RotateCcw className="w-3.5 h-3.5" />{t('team.reset_period')}</span>
                            </Dropdown.Item>
                            <Dropdown.Item id="delete" className="text-danger" textValue={t('team.dept_delete')}>
                              <span className="flex items-center gap-2"><Trash2 className="w-3.5 h-3.5" />{t('team.dept_delete')}</span>
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

      <EditDepartmentModal
        open={modalOpen}
        isEdit={!!editing}
        departmentId={editing?.id}
        form={form}
        setForm={setForm}
        onClose={closeModal}
        onSubmit={handleSubmit}
        loading={createMutation.isPending || updateMutation.isPending}
      />

      <AlertDialog isOpen={!!resetTarget} onOpenChange={(open) => { if (!open) setResetTarget(null); }}>
        <DialogTriggerShim />
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="sm">
            <AlertDialog.Dialog className="ag-elevation-modal">
              <AlertDialog.Header>
                <AlertDialog.Icon status="warning" />
                <AlertDialog.Heading>{t('team.reset_period')}</AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>{t('team.dept_reset_confirm', { name: resetTarget?.name })}</AlertDialog.Body>
              <AlertDialog.Footer>
                <Button variant="secondary" onPress={() => setResetTarget(null)}>{t('common.cancel')}</Button>
                <Button aria-busy={resetMutation.isPending} isDisabled={resetMutation.isPending} variant="primary" onPress={() => resetTarget && resetMutation.mutate(resetTarget.id)}>
                  {resetMutation.isPending ? <Spinner size="sm" /> : null}
                  {t('common.confirm')}
                </Button>
              </AlertDialog.Footer>
            </AlertDialog.Dialog>
          </AlertDialog.Container>
        </AlertDialog.Backdrop>
      </AlertDialog>

      <AlertDialog isOpen={!!deleteTarget} onOpenChange={(open) => { if (!open) setDeleteTarget(null); }}>
        <DialogTriggerShim />
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="sm">
            <AlertDialog.Dialog className="ag-elevation-modal">
              <AlertDialog.Header>
                <AlertDialog.Icon status="danger" />
                <AlertDialog.Heading>{t('team.dept_delete')}</AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>{t('team.dept_delete_confirm', { name: deleteTarget?.name })}</AlertDialog.Body>
              <AlertDialog.Footer>
                <Button variant="secondary" onPress={() => setDeleteTarget(null)}>{t('common.cancel')}</Button>
                <Button aria-busy={deleteMutation.isPending} isDisabled={deleteMutation.isPending} variant="danger" onPress={() => deleteTarget && deleteMutation.mutate(deleteTarget.id)}>
                  {deleteMutation.isPending ? <Spinner size="sm" /> : null}
                  {t('common.confirm')}
                </Button>
              </AlertDialog.Footer>
            </AlertDialog.Dialog>
          </AlertDialog.Container>
        </AlertDialog.Backdrop>
      </AlertDialog>
    </>
  );
}

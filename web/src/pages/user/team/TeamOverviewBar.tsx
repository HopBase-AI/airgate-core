import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Input, Label, Spinner, TextField as HeroTextField, useOverlayState } from '@heroui/react';
import { departmentsApi } from '../../../shared/api/departments';
import { queryKeys } from '../../../shared/queryKeys';
import { CommonModal } from '../../../shared/components/CommonModal';
import { useToast } from '../../../shared/ui';

// 企业层账本条：一块发丝线面板，一行五项（余额 / 已分配给部门 / 已分配给成员 / 本期消耗 / 账期），
// 项与项之间用发丝线分隔。企业余额是唯一真实扣费点；分配给部门 / 成员的额度是限额之和，允许超发只提示。
// 三层额度的「剩余」口径在后端统一，这里只展示；账期日的修改入口就放在账期项里。
//
// 部门负责人视角（isDepartmentManager）：后端返回的是**部门口径**的同形投影——不含企业余额，
// 消耗按本部门聚合。此时隐去余额项与账期修改入口（账期是全企业口径，只有企业主能改），
// 「已分配给部门」改叫「本部门额度」。
export function TeamOverviewBar({
  isDepartmentManager = false,
  departmentName = '',
}: {
  isDepartmentManager?: boolean;
  departmentName?: string;
} = {}) {
  const { t, i18n } = useTranslation();
  const { toast } = useToast();
  const queryClient = useQueryClient();
  const [billingOpen, setBillingOpen] = useState(false);
  const [billingDay, setBillingDay] = useState('');

  const { data } = useQuery({
    queryKey: queryKeys.teamOverview(),
    queryFn: ({ signal }) => departmentsApi.overview({ signal }),
    staleTime: 15_000,
  });
  const billingMutation = useMutation({
    mutationFn: (day: number) => departmentsApi.updateBillingPeriod(day),
    onSuccess: () => {
      toast('success', t('team.billing_day_updated'));
      setBillingOpen(false);
      queryClient.invalidateQueries({ queryKey: queryKeys.teamOverview() });
      queryClient.invalidateQueries({ queryKey: queryKeys.departments() });
      queryClient.invalidateQueries({ queryKey: queryKeys.members() });
    },
    onError: (err: Error) => toast('error', err.message),
  });
  const modalState = useOverlayState({
    isOpen: billingOpen,
    onOpenChange: (open) => {
      if (!open) setBillingOpen(false);
    },
  });

  if (!data) return null;
  // 超发提示要拿企业余额比，负责人看不到余额也就无从提示。
  const overAllocated = !isDepartmentManager && data.department_quota_total > data.balance;
  const fmt = (value: number) => `$${value.toFixed(2)}`;
  const fmtDate = (value: string) => new Date(value).toLocaleDateString(i18n.language);
  const openBilling = () => {
    setBillingDay(String(data.billing_day || 1));
    setBillingOpen(true);
  };
  const submitBilling = () => {
    const day = Number(billingDay);
    if (!Number.isInteger(day) || day < 1 || day > 28) {
      toast('error', t('team.billing_day_hint'));
      return;
    }
    billingMutation.mutate(day);
  };

  return (
    <>
      <dl className="ag-team-overview mb-5">
        {!isDepartmentManager ? (
          <div className="ag-team-overview-item">
            <dt className="ag-team-overview-label">{t('team.overview_balance')}</dt>
            <dd className="ag-team-overview-value">{fmt(data.balance)}</dd>
          </div>
        ) : null}
        <div className="ag-team-overview-item">
          <dt className="ag-team-overview-label">
            {isDepartmentManager
              ? t('team.overview_my_department_quota', { name: departmentName })
              : t('team.overview_department_quota')}
          </dt>
          <dd className="ag-team-overview-value">{fmt(data.department_quota_total)}</dd>
          {overAllocated ? (
            <dd className="ag-team-overview-hint" data-tone="warning" title={t('team.overview_over_allocated_hint')}>
              {t('team.overview_over_allocated', { amount: fmt(data.department_quota_total - data.balance) })}
            </dd>
          ) : null}
        </div>
        <div className="ag-team-overview-item">
          <dt className="ag-team-overview-label">{t('team.overview_member_quota')}</dt>
          <dd className="ag-team-overview-value">{fmt(data.member_quota_total)}</dd>
          {data.unassigned_member_quota > 0 ? (
            <dd className="ag-team-overview-hint">
              {t('team.overview_unassigned_member_quota', { amount: fmt(data.unassigned_member_quota) })}
            </dd>
          ) : null}
        </div>
        <div className="ag-team-overview-item">
          <dt className="ag-team-overview-label">{t('team.overview_period_used')}</dt>
          <dd className="ag-team-overview-value">{fmt(data.period_used_actual)}</dd>
        </div>
        <div className="ag-team-overview-item">
          <dt className="ag-team-overview-label">{t('team.overview_period')}</dt>
          <dd className="ag-team-overview-value" data-kind="date">{`${fmtDate(data.period_start)} – ${fmtDate(data.period_end)}`}</dd>
          <dd className="ag-team-overview-hint">
            <span>{t('team.overview_billing_day', { day: data.billing_day })}</span>
            {!isDepartmentManager ? (
              <button type="button" className="ag-team-overview-link" onClick={openBilling}>
                {t('team.billing_day_edit')}
              </button>
            ) : null}
          </dd>
        </div>
      </dl>

      <CommonModal
        footer={(
          <div className="flex w-full justify-end gap-2">
            <Button variant="secondary" onPress={() => setBillingOpen(false)}>{t('common.cancel')}</Button>
            <Button variant="primary" isDisabled={billingMutation.isPending} onPress={submitBilling}>
              {billingMutation.isPending ? <Spinner size="sm" /> : null}
              {t('common.save')}
            </Button>
          </div>
        )}
        state={modalState}
        title={t('team.billing_day_edit')}
      >
        <div className="space-y-3">
          <HeroTextField fullWidth isRequired>
            <Label>{t('team.billing_day_label')}</Label>
            <Input type="number" min={1} max={28} value={billingDay} onChange={(e) => setBillingDay(e.target.value)} />
          </HeroTextField>
          <p className="text-xs leading-5 text-text-tertiary">{t('team.billing_day_hint')}</p>
          <p className="text-xs leading-5 text-text-tertiary">{t('team.billing_day_effect')}</p>
        </div>
      </CommonModal>
    </>
  );
}

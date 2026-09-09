import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Input, Label, Spinner, TextField as HeroTextField, useOverlayState } from '@heroui/react';
import { CalendarClock } from 'lucide-react';
import { departmentsApi } from '../../../shared/api/departments';
import { queryKeys } from '../../../shared/queryKeys';
import { CommonModal } from '../../../shared/components/CommonModal';
import { useToast } from '../../../shared/ui';

// 企业层总览：企业余额（唯一真实扣费点）、已分配给部门 / 成员的额度（限额之和，允许超发只提示）、
// 本期消耗与账期。三层额度的「剩余」口径在后端统一，这里只展示。
export function TeamOverviewBar() {
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
  const overAllocated = data.department_quota_total > data.balance;
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

  const items: Array<{ key: string; label: string; value: string; tone?: 'warning' | 'muted'; hint?: string }> = [
    { key: 'balance', label: t('team.overview_balance'), value: fmt(data.balance) },
    {
      key: 'departments',
      label: t('team.overview_department_quota'),
      value: fmt(data.department_quota_total),
      tone: overAllocated ? 'warning' : undefined,
      hint: overAllocated ? t('team.overview_over_allocated', { amount: fmt(data.department_quota_total - data.balance) }) : undefined,
    },
    {
      key: 'members',
      label: t('team.overview_member_quota'),
      value: fmt(data.member_quota_total),
      hint: data.unassigned_member_quota > 0 ? t('team.overview_unassigned_member_quota', { amount: fmt(data.unassigned_member_quota) }) : undefined,
    },
    { key: 'period_used', label: t('team.overview_period_used'), value: fmt(data.period_used_actual) },
    {
      key: 'period',
      label: t('team.overview_period'),
      value: `${fmtDate(data.period_start)} – ${fmtDate(data.period_end)}`,
      tone: 'muted',
      hint: t('team.overview_billing_day', { day: data.billing_day }),
    },
  ];

  return (
    <div className="ag-team-overview mb-5">
      <dl className="ag-team-overview-grid">
        {items.map((item) => (
          <div key={item.key} className="ag-team-overview-item" data-tone={item.tone}>
            <dt>{item.label}</dt>
            <dd>{item.value}</dd>
            {item.hint ? <p>{item.hint}</p> : null}
          </div>
        ))}
        <div className="ag-team-overview-item ag-team-overview-action">
          <Button size="sm" variant="ghost" onPress={openBilling}>
            <CalendarClock className="h-3.5 w-3.5" />
            {t('team.billing_day_edit')}
          </Button>
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
    </div>
  );
}

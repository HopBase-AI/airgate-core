import { useTranslation } from 'react-i18next';
import { Button, Description, Input, Label, ListBox, Select, Spinner, TextField as HeroTextField, useOverlayState } from '@heroui/react';
import { CommonModal } from '../../../shared/components/CommonModal';
import type { DepartmentForm } from './types';

// 部门表单：名称 + 额度 + 周期 + 备注。额度是「限额」不是预扣——各部门额度之和可以超过企业余额，
// 页面只提示超发不拦截；0 = 不限。
export function EditDepartmentModal({
  open,
  isEdit,
  form,
  setForm,
  onClose,
  onSubmit,
  loading,
}: {
  open: boolean;
  isEdit: boolean;
  form: DepartmentForm;
  setForm: (form: DepartmentForm) => void;
  onClose: () => void;
  onSubmit: () => void;
  loading: boolean;
}) {
  const { t } = useTranslation();
  const modalState = useOverlayState({
    isOpen: open,
    onOpenChange: (nextOpen) => {
      if (!nextOpen) onClose();
    },
  });
  const monthlyItem = { id: 'monthly', label: t('team.period_monthly'), hint: t('team.dept_period_monthly_hint') };
  const noneItem = { id: 'none', label: t('team.period_none'), hint: t('team.period_none_hint') };
  const periodItems = [monthlyItem, noneItem];
  const selectedPeriod = form.quota_period === 'none' ? noneItem : monthlyItem;

  return (
    <CommonModal
      footer={(
        <div className="flex w-full justify-end gap-2">
          <Button variant="secondary" onPress={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" isDisabled={loading} onPress={onSubmit}>
            {loading ? <Spinner size="sm" /> : null}
            {isEdit ? t('common.save') : t('common.create')}
          </Button>
        </div>
      )}
      state={modalState}
      title={isEdit ? t('team.dept_edit') : t('team.dept_create')}
    >
      <div className="space-y-4">
        <HeroTextField fullWidth isRequired>
          <Label>{t('team.dept_name')}</Label>
          <Input
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            placeholder={t('team.dept_name_placeholder')}
            maxLength={64}
            required
          />
        </HeroTextField>
        <HeroTextField fullWidth>
          <Label>{t('team.dept_quota_label')}</Label>
          <Input
            type="number"
            min={0}
            value={form.quota_usd}
            onChange={(e) => setForm({ ...form, quota_usd: e.target.value })}
            placeholder={t('team.unlimited')}
          />
          <Description>{t('team.dept_quota_hint')}</Description>
        </HeroTextField>
        <Select
          fullWidth
          selectedKey={form.quota_period}
          onSelectionChange={(key) => setForm({ ...form, quota_period: key === 'none' ? 'none' : 'monthly' })}
        >
          <Label>{t('team.quota_period')}</Label>
          <Select.Trigger>
            <Select.Value>{selectedPeriod.label}</Select.Value>
            <Select.Indicator />
          </Select.Trigger>
          <Select.Popover className="w-[var(--trigger-width)]">
            <ListBox items={periodItems}>
              {(item) => (
                <ListBox.Item id={item.id} textValue={item.label}>
                  <div className="min-w-0">
                    <div className="truncate">{item.label}</div>
                    <div className="mt-0.5 truncate text-xs font-normal leading-5 text-text-tertiary">{item.hint}</div>
                  </div>
                </ListBox.Item>
              )}
            </ListBox>
          </Select.Popover>
        </Select>
        <p className="-mt-2 text-xs leading-5 text-text-tertiary">{selectedPeriod.hint}</p>
        <HeroTextField fullWidth>
          <Label>{t('team.note')}</Label>
          <Input
            value={form.note}
            onChange={(e) => setForm({ ...form, note: e.target.value })}
            placeholder={t('team.note_placeholder')}
            maxLength={255}
          />
        </HeroTextField>
      </div>
    </CommonModal>
  );
}

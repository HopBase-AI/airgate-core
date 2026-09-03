import { useTranslation } from 'react-i18next';
import { Button, Description, Input, Label, ListBox, Select, Spinner, TextField as HeroTextField, useOverlayState } from '@heroui/react';
import { CommonModal } from '../../../shared/components/CommonModal';
import type { MemberForm } from './types';

export function EditMemberModal({
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
  form: MemberForm;
  setForm: (form: MemberForm) => void;
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
  const monthlyItem = { id: 'monthly', label: t('team.period_monthly'), hint: t('team.period_monthly_hint') };
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
      title={isEdit ? t('team.edit') : t('team.create')}
    >
      <div className="space-y-4">
        <HeroTextField fullWidth isRequired>
          <Label>{t('team.name')}</Label>
          <Input
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            placeholder={t('team.name_placeholder')}
            maxLength={64}
            required
          />
        </HeroTextField>
        <HeroTextField fullWidth>
          <Label>{t('team.email')}</Label>
          <Input
            type="email"
            value={form.email}
            onChange={(e) => setForm({ ...form, email: e.target.value })}
            placeholder={t('team.email_placeholder')}
            maxLength={255}
          />
        </HeroTextField>
        <HeroTextField fullWidth>
          <Label>{t('team.quota_label')}</Label>
          <Input
            type="number"
            min={0}
            value={form.quota_usd}
            onChange={(e) => setForm({ ...form, quota_usd: e.target.value })}
            placeholder={t('team.unlimited')}
          />
          <Description>{t('team.quota_hint')}</Description>
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

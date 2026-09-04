import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { Button, Checkbox, Description, Input, Label, ListBox, Select, Spinner, TextField as HeroTextField, useOverlayState } from '@heroui/react';
import { CommonModal } from '../../../shared/components/CommonModal';
import { groupsApi } from '../../../shared/api/groups';
import { queryKeys } from '../../../shared/queryKeys';
import { FETCH_ALL_PARAMS } from '../../../shared/constants';
import { localizedGroupText } from '../../../shared/groupText';
import type { MemberForm } from './types';

// 成员表单：成员是真实登录账号——新建时邮箱+密码必填；编辑时邮箱可改、密码留空不动。
// 分组白名单从企业主自己可见的分组里勾选，一个都不勾 = 继承全部。
export function EditMemberModal({
  open,
  isEdit,
  hasAccount,
  form,
  setForm,
  onClose,
  onSubmit,
  loading,
}: {
  open: boolean;
  isEdit: boolean;
  /** 编辑的成员是否有登录账号（老模型成员没有，此时不展示密码栏） */
  hasAccount: boolean;
  form: MemberForm;
  setForm: (form: MemberForm) => void;
  onClose: () => void;
  onSubmit: () => void;
  loading: boolean;
}) {
  const { t, i18n } = useTranslation();
  const modalState = useOverlayState({
    isOpen: open,
    onOpenChange: (nextOpen) => {
      if (!nextOpen) onClose();
    },
  });
  const { data: groupsData, isLoading: groupsLoading } = useQuery({
    queryKey: queryKeys.groupsForKeys(),
    queryFn: () => groupsApi.listAvailable(FETCH_ALL_PARAMS),
    enabled: open,
    staleTime: 60_000,
  });
  const groups = groupsData?.list ?? [];
  const monthlyItem = { id: 'monthly', label: t('team.period_monthly'), hint: t('team.period_monthly_hint') };
  const noneItem = { id: 'none', label: t('team.period_none'), hint: t('team.period_none_hint') };
  const periodItems = [monthlyItem, noneItem];
  const selectedPeriod = form.quota_period === 'none' ? noneItem : monthlyItem;
  const showPassword = !isEdit || hasAccount;
  const quotaRequired = !isEdit || hasAccount;

  const toggleGroup = (groupId: number, selected: boolean) => {
    const next = selected
      ? [...new Set([...form.allowed_group_ids, groupId])]
      : form.allowed_group_ids.filter((id) => id !== groupId);
    setForm({ ...form, allowed_group_ids: next });
  };

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
        <HeroTextField fullWidth isRequired={!isEdit || hasAccount}>
          <Label>{t('team.email')}</Label>
          <Input
            type="email"
            value={form.email}
            onChange={(e) => setForm({ ...form, email: e.target.value })}
            placeholder={t('team.email_placeholder')}
            maxLength={255}
            autoComplete="off"
          />
          <Description>{t('team.email_hint')}</Description>
        </HeroTextField>
        {showPassword ? (
          <HeroTextField fullWidth isRequired={!isEdit}>
            <Label>{isEdit ? t('team.password_reset') : t('team.password')}</Label>
            <Input
              type="password"
              value={form.password}
              onChange={(e) => setForm({ ...form, password: e.target.value })}
              placeholder={isEdit ? t('team.password_reset_placeholder') : t('team.password_placeholder')}
              minLength={6}
              maxLength={72}
              autoComplete="new-password"
            />
          </HeroTextField>
        ) : null}
        {/* 有登录账号的成员额度必填(成员余额 = 本期剩余额度);仅老模型无账号成员保留 0 = 不限 */}
        <HeroTextField fullWidth isRequired={quotaRequired}>
          <Label>{t('team.quota_label')}</Label>
          <Input
            type="number"
            min={0}
            value={form.quota_usd}
            onChange={(e) => setForm({ ...form, quota_usd: e.target.value })}
            placeholder={quotaRequired ? undefined : t('team.unlimited')}
            required={quotaRequired}
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

        <div>
          <p className="mb-1 text-sm font-medium text-text">{t('team.groups')}</p>
          <p className="mb-2 text-xs leading-5 text-text-tertiary">{t('team.groups_hint')}</p>
          {groupsLoading ? (
            <p className="py-3 text-center text-xs text-text-tertiary">{t('common.loading')}</p>
          ) : groups.length === 0 ? (
            <p className="py-3 text-center text-xs text-text-tertiary">{t('common.no_data')}</p>
          ) : (
            <div className="max-h-48 space-y-0.5 overflow-y-auto rounded-lg border border-border p-1">
              {groups.map((group) => (
                <div key={group.id} className="flex items-center gap-2.5 rounded-md px-2 py-1.5 text-sm">
                  <Checkbox
                    isSelected={form.allowed_group_ids.includes(group.id)}
                    onChange={(selected) => toggleGroup(group.id, selected)}
                  >
                    <Checkbox.Control>
                      <Checkbox.Indicator />
                    </Checkbox.Control>
                    <span className="text-text">{localizedGroupText(group.name, group.name_i18n, i18n.language)}</span>
                  </Checkbox>
                  <span className="text-[10px] text-text-tertiary">{group.platform}</span>
                </div>
              ))}
            </div>
          )}
        </div>

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

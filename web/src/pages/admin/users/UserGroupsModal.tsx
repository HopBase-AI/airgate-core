import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { Button, Checkbox, Modal, Spinner, useOverlayState } from '@heroui/react';
import { DialogTriggerShim } from '../../../shared/components/DialogTriggerShim';
import { usersApi } from '../../../shared/api/users';
import { groupsApi } from '../../../shared/api/groups';
import { useCrudMutation } from '../../../shared/hooks/useCrudMutation';
import { queryKeys } from '../../../shared/queryKeys';
import { FETCH_ALL_PARAMS } from '../../../shared/constants';
import type { UserResp, GroupResp, UpdateUserReq } from '../../../shared/types';

interface UserGroupsModalProps {
  open: boolean;
  user: UserResp;
  onClose: () => void;
  onSaved: () => void;
}

// 用户分组弹窗只管「专属分组授权」。价格（分组折数/专属倍率/固定图价）
// 已迁入报价单（QuoteSheetModal），两个职能不再混在一个面板里。
export function UserGroupsModal({ open, user, onClose, onSaved }: UserGroupsModalProps) {
  const { t } = useTranslation();
  const [selectedIds, setSelectedIds] = useState<number[]>(user.allowed_group_ids ?? []);

  const { data: groupsData, isLoading: groupsLoading } = useQuery({
    queryKey: queryKeys.groupsAll(),
    queryFn: () => groupsApi.list(FETCH_ALL_PARAMS),
    enabled: open,
  });

  const allGroups: GroupResp[] = groupsData?.list ?? [];
  const exclusiveGroups = allGroups.filter((group) => group.is_exclusive);
  const normalGroups = allGroups.filter((group) => !group.is_exclusive);

  // 只提交授权字段：group_rates / group_plugin_settings 缺省即「不修改」，
  // 避免这里保存把报价单配置整包清掉。
  const buildPayload = (): UpdateUserReq => ({ allowed_group_ids: selectedIds });

  const updateMutation = useCrudMutation({
    mutationFn: (_?: void) => usersApi.update(user.id, buildPayload()),
    successMessage: t('users.update_success'),
    queryKey: queryKeys.users(),
    onSuccess: () => onSaved(),
  });

  const toggleExclusiveGroup = (groupId: number, isSelected: boolean) => {
    setSelectedIds((current) =>
      isSelected
        ? [...new Set([...current, groupId])]
        : current.filter((value) => value !== groupId),
    );
  };

  const renderGroupRow = (group: GroupResp, selected: boolean, locked: boolean) => (
    <div
      key={group.id}
      className="flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm text-text-secondary"
    >
      <Checkbox
        isDisabled={locked}
        isSelected={selected}
        onChange={(nextSelected) => toggleExclusiveGroup(group.id, nextSelected)}
      >
        <Checkbox.Control>
          <Checkbox.Indicator />
        </Checkbox.Control>
        <span className="text-text">{group.name}</span>
      </Checkbox>
      <span className="text-[10px] text-text-tertiary">{group.platform}</span>
    </div>
  );
  const modalState = useOverlayState({
    isOpen: open,
    onOpenChange: (nextOpen) => {
      if (!nextOpen) onClose();
    },
  });

  return (
    <Modal state={modalState}>
      <DialogTriggerShim />
      <Modal.Backdrop>
        <Modal.Container placement="center" scroll="inside" size="md">
          <Modal.Dialog
            className="ag-elevation-modal"
            style={{ maxWidth: '560px', width: 'min(100%, calc(100vw - 2rem))' }}
          >
            <Modal.Header>
              <Modal.Heading>{`${t('users.groups')} - ${user.email}`}</Modal.Heading>
              <Modal.CloseTrigger />
            </Modal.Header>
            <Modal.Body>
              <p className="mb-3 text-xs text-text-tertiary">{t('users.groups_pricing_moved_hint')}</p>
              {groupsLoading ? (
                <p className="py-8 text-center text-sm text-text-tertiary">{t('common.loading')}</p>
              ) : allGroups.length === 0 ? (
                <p className="py-8 text-center text-sm text-text-tertiary">{t('common.no_data')}</p>
              ) : (
                <div className="max-h-[26rem] space-y-4 overflow-y-auto">
                  {normalGroups.length > 0 ? (
                    <div>
                      <p className="mb-2 text-xs font-medium uppercaser text-text-tertiary">
                        {t('users.normal_groups')}
                      </p>
                      <div className="space-y-0.5">
                        {normalGroups.map((group) => renderGroupRow(group, true, true))}
                      </div>
                    </div>
                  ) : null}

                  {exclusiveGroups.length > 0 ? (
                    <div>
                      <p className="mb-2 text-xs font-medium uppercaser text-text-tertiary">
                        {t('users.exclusive_groups')}
                      </p>
                      <div className="space-y-0.5">
                        {exclusiveGroups.map((group) =>
                          renderGroupRow(group, selectedIds.includes(group.id), false),
                        )}
                      </div>
                    </div>
                  ) : null}
                </div>
              )}
            </Modal.Body>
            <Modal.Footer>
              <Button variant="secondary" onPress={onClose}>
                {t('common.cancel')}
              </Button>
              <Button
                variant="primary"
                isDisabled={updateMutation.isPending}
                onPress={() => updateMutation.mutate()}
              >
                {updateMutation.isPending ? <Spinner size="sm" /> : null}
                {t('common.save')}
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}

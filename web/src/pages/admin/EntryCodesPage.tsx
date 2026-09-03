import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import {
  AlertDialog, Button, Chip, EmptyState, Form, Input, Label, Modal, Spinner,
  TextField as HeroTextField, useOverlayState,
} from '@heroui/react';
import { NativeSwitch } from '../../shared/components/NativeSwitch';
import { Plus, Pencil, Trash2, Copy, RefreshCw } from 'lucide-react';
import { entryCodesApi } from '../../shared/api/entryCodes';
import { queryKeys } from '../../shared/queryKeys';
import { useCrudMutation } from '../../shared/hooks/useCrudMutation';
import { useToast } from '../../shared/ui';
import { DialogTriggerShim } from '../../shared/components/DialogTriggerShim';
import { CommonTable } from '../../shared/components/CommonTable';
import { TableLoadingRow } from '../../shared/components/TableLoadingRow';
import type { EntryCodeResp } from '../../shared/types';

interface EntryCodeForm {
  note: string;
  userId: string;
  enabled: boolean;
}

const emptyForm: EntryCodeForm = { note: '', userId: '', enabled: true };

export default function EntryCodesPage() {
  const { t } = useTranslation();
  const { toast } = useToast();
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<EntryCodeResp | null>(null);
  const [form, setForm] = useState<EntryCodeForm>(emptyForm);
  const [deleteTarget, setDeleteTarget] = useState<EntryCodeResp | null>(null);

  const { data, isLoading, refetch } = useQuery({
    queryKey: queryKeys.entryCodes(),
    queryFn: () => entryCodesApi.list(),
  });

  const createMutation = useCrudMutation({
    mutationFn: (payload: { note?: string; user_id?: number }) => entryCodesApi.create(payload),
    successMessage: t('entryCodes.create_success'),
    queryKey: queryKeys.entryCodes(),
    onSuccess: () => closeModal(),
  });
  const updateMutation = useCrudMutation({
    mutationFn: ({ code, data: payload }: { code: string; data: { note?: string; enabled?: boolean; user_id?: number } }) =>
      entryCodesApi.update(code, payload),
    successMessage: t('entryCodes.update_success'),
    queryKey: queryKeys.entryCodes(),
    onSuccess: () => closeModal(),
  });
  const deleteMutation = useCrudMutation({
    mutationFn: (code: string) => entryCodesApi.delete(code),
    successMessage: t('entryCodes.delete_success'),
    queryKey: queryKeys.entryCodes(),
    onSuccess: () => setDeleteTarget(null),
  });

  function openCreate() {
    setEditing(null);
    setForm(emptyForm);
    setModalOpen(true);
  }
  function openEdit(row: EntryCodeResp) {
    setEditing(row);
    setForm({ note: row.note, userId: row.user_id ? String(row.user_id) : '', enabled: row.enabled });
    setModalOpen(true);
  }
  function closeModal() {
    setModalOpen(false);
    setEditing(null);
    setForm(emptyForm);
  }
  function handleSubmit(event?: FormEvent<HTMLFormElement>) {
    event?.preventDefault();
    const userId = form.userId.trim() ? Number(form.userId.trim()) : 0;
    if (form.userId.trim() && Number.isNaN(userId)) {
      toast('error', t('entryCodes.invalid_user_id'));
      return;
    }
    if (editing) {
      updateMutation.mutate({ code: editing.code, data: { note: form.note, enabled: form.enabled, user_id: userId } });
    } else {
      createMutation.mutate({ note: form.note, user_id: userId });
    }
  }
  async function copyBaseURL(url: string) {
    try {
      await navigator.clipboard.writeText(url);
      toast('success', t('entryCodes.copied'));
    } catch {
      toast('error', t('entryCodes.copy_failed'));
    }
  }

  const rows = data ?? [];
  const saving = createMutation.isPending || updateMutation.isPending;
  const dialogState = useOverlayState({
    isOpen: modalOpen,
    onOpenChange: (open) => { if (!open) closeModal(); },
  });

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <p className="text-sm text-text-secondary">{t('entryCodes.subtitle')}</p>
        <div className="flex items-center gap-2">
          <Button isIconOnly aria-label={t('common.refresh', 'Refresh')} size="md" variant="ghost" onPress={() => { void refetch(); }}>
            <RefreshCw className="w-4 h-4" />
          </Button>
          <Button variant="primary" onPress={openCreate}>
            <Plus className="w-4 h-4" />
            {t('entryCodes.create')}
          </Button>
        </div>
      </div>
      <CommonTable ariaLabel={t('entryCodes.title')} minWidth={960}>
        <CommonTable.Header>
          <CommonTable.Column id="note">{t('entryCodes.customer')}</CommonTable.Column>
          <CommonTable.Column id="base_url">{t('entryCodes.base_url')}</CommonTable.Column>
          <CommonTable.Column id="status">{t('common.status')}</CommonTable.Column>
          <CommonTable.Column id="count">{t('entryCodes.request_count')}</CommonTable.Column>
          <CommonTable.Column id="last_used">{t('entryCodes.last_used')}</CommonTable.Column>
          <CommonTable.Column id="actions">{t('common.actions')}</CommonTable.Column>
        </CommonTable.Header>
        <CommonTable.Body>
          {isLoading ? (
            <TableLoadingRow colSpan={6} />
          ) : rows.length === 0 ? (
            <CommonTable.Row id="empty">
              <CommonTable.Cell colSpan={6}>
                <EmptyState>
                  <div className="text-sm text-default-500">{t('common.no_data')}</div>
                </EmptyState>
              </CommonTable.Cell>
            </CommonTable.Row>
          ) : (
            rows.map((row) => (
              <CommonTable.Row id={row.code} key={row.code}>
                <CommonTable.Cell>
                  <div className="flex flex-col">
                    <span className="text-text">{row.note || <span className="text-text-tertiary">{t('entryCodes.no_note')}</span>}</span>
                    <span className="text-xs text-text-secondary">
                      {row.user_id ? `#${row.user_id} ${row.user_email}` : t('entryCodes.unbound')}
                    </span>
                  </div>
                </CommonTable.Cell>
                <CommonTable.Cell>
                  <div className="flex items-center gap-2">
                    <span className="font-mono text-xs text-text-secondary break-all">{row.base_url}</span>
                    <Button isIconOnly size="sm" variant="ghost" aria-label={t('entryCodes.copy')} onPress={() => { void copyBaseURL(row.base_url); }}>
                      <Copy className="w-3.5 h-3.5" />
                    </Button>
                  </div>
                </CommonTable.Cell>
                <CommonTable.Cell>
                  <Chip color={row.enabled ? 'success' : 'default'} size="sm" variant="soft">
                    {row.enabled ? t('entryCodes.enabled') : t('entryCodes.disabled')}
                  </Chip>
                </CommonTable.Cell>
                <CommonTable.Cell>
                  <span className="font-mono">{row.request_count}</span>
                </CommonTable.Cell>
                <CommonTable.Cell>
                  <span className="text-text-secondary text-xs">{row.last_used_at ? new Date(row.last_used_at).toLocaleString() : '-'}</span>
                </CommonTable.Cell>
                <CommonTable.Cell>
                  <div className="ag-table-row-actions flex justify-center gap-1">
                    <Button size="sm" variant="secondary" onPress={() => openEdit(row)}>
                      <Pencil className="w-3.5 h-3.5" />
                      {t('common.edit')}
                    </Button>
                    <Button size="sm" variant="danger-soft" className="text-danger" onPress={() => setDeleteTarget(row)}>
                      <Trash2 className="w-3.5 h-3.5" />
                      {t('common.delete')}
                    </Button>
                  </div>
                </CommonTable.Cell>
              </CommonTable.Row>
            ))
          )}
        </CommonTable.Body>
      </CommonTable>

      <Modal state={dialogState}>
        <DialogTriggerShim />
        <Modal.Backdrop>
          <Modal.Container placement="center" scroll="inside" size="md">
            <Modal.Dialog className="ag-elevation-modal">
              <Modal.Header>
                <Modal.Heading>{editing ? t('entryCodes.edit') : t('entryCodes.create')}</Modal.Heading>
                <Modal.CloseTrigger />
              </Modal.Header>
              <Modal.Body>
                <Form id="entry-code-form" className="space-y-4" onSubmit={handleSubmit}>
                  <HeroTextField fullWidth>
                    <Label>{t('entryCodes.note')}</Label>
                    <Input
                      name="note"
                      autoComplete="off"
                      value={form.note}
                      onChange={(e) => setForm({ ...form, note: e.target.value })}
                      placeholder={t('entryCodes.note_placeholder')}
                    />
                  </HeroTextField>
                  <HeroTextField fullWidth>
                    <Label>{t('entryCodes.user_id')}</Label>
                    <Input
                      name="user_id"
                      autoComplete="off"
                      value={form.userId}
                      onChange={(e) => setForm({ ...form, userId: e.target.value })}
                      placeholder={t('entryCodes.user_id_placeholder')}
                    />
                  </HeroTextField>
                  {editing && (
                    <NativeSwitch
                      isSelected={form.enabled}
                      onChange={(v) => setForm({ ...form, enabled: v })}
                      label={<span className="text-sm font-medium text-text">{t('entryCodes.enabled')}</span>}
                    />
                  )}
                  {editing && (
                    <div className="flex items-center gap-2 rounded-md bg-surface-secondary px-3 py-2">
                      <span className="font-mono text-xs break-all">{editing.base_url}</span>
                      <Button isIconOnly size="sm" variant="ghost" aria-label={t('entryCodes.copy')} onPress={() => { void copyBaseURL(editing.base_url); }}>
                        <Copy className="w-3.5 h-3.5" />
                      </Button>
                    </div>
                  )}
                </Form>
              </Modal.Body>
              <Modal.Footer>
                <Button variant="secondary" onPress={closeModal}>{t('common.cancel')}</Button>
                <Button variant="primary" isDisabled={saving} onPress={() => handleSubmit()}>
                  {saving ? <Spinner size="sm" /> : null}
                  {editing ? t('common.save') : t('common.create')}
                </Button>
              </Modal.Footer>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>

      <AlertDialog isOpen={!!deleteTarget} onOpenChange={(open) => { if (!open) setDeleteTarget(null); }}>
        <DialogTriggerShim />
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="sm">
            <AlertDialog.Dialog className="ag-elevation-modal">
              <AlertDialog.Header>
                <AlertDialog.Icon status="danger" />
                <AlertDialog.Heading>{t('entryCodes.delete')}</AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>{t('entryCodes.delete_confirm', { code: deleteTarget?.code })}</AlertDialog.Body>
              <AlertDialog.Footer>
                <Button variant="secondary" onPress={() => setDeleteTarget(null)}>{t('common.cancel')}</Button>
                <Button
                  aria-busy={deleteMutation.isPending}
                  isDisabled={deleteMutation.isPending}
                  variant="danger"
                  onPress={() => deleteTarget && deleteMutation.mutate(deleteTarget.code)}
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

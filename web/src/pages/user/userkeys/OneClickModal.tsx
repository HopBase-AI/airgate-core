import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { Button, Modal, useOverlayState } from '@heroui/react';
import { DialogTriggerShim } from '../../../shared/components/DialogTriggerShim';
import { CheckCircle, Copy, Download, Loader2, TimerOff } from 'lucide-react';
import { useToast } from '../../../shared/ui';
import { useClipboard } from '../../../shared/hooks/useClipboard';
import { oneclickApi, type OneClickIssueResp, type OneClickStatus } from '../../../shared/api/oneclick';
import { queryKeys } from '../../../shared/queryKeys';
import type { APIKeyResp } from '../../../shared/types';

const CCS_RELEASES_URL = 'https://github.com/farion1231/cc-switch/releases';

// 一键接入弹窗：签发一次性令牌 → 展示按 OS 拼好的整条命令 →
// 轮询令牌状态（pending → exchanged → verified），成功后变绿。
export function useOneClickModal() {
  const { toast } = useToast();
  const { t } = useTranslation();

  const [oneClickTarget, setOneClickTarget] = useState<APIKeyResp | null>(null);
  const [oneClickIssue, setOneClickIssue] = useState<OneClickIssueResp | null>(null);

  const openOneClickModal = useCallback(
    async (row: APIKeyResp) => {
      setOneClickTarget(row);
      setOneClickIssue(null);
      try {
        const resp = await oneclickApi.issue(row.id);
        setOneClickIssue(resp);
      } catch {
        toast('error', t('user_keys.one_click_issue_failed'));
        setOneClickTarget(null);
      }
    },
    [toast, t],
  );

  const closeOneClickModal = useCallback(() => {
    setOneClickTarget(null);
    setOneClickIssue(null);
  }, []);

  return { oneClickTarget, oneClickIssue, openOneClickModal, closeOneClickModal };
}

function StatusStrip({ status }: { status: OneClickStatus | undefined }) {
  const { t } = useTranslation();
  if (status === 'verified') {
    return (
      <div className="flex items-center gap-2 rounded-md border border-success/40 bg-success/10 px-3 py-2.5 text-sm text-success">
        <CheckCircle className="h-4 w-4 shrink-0" />
        {t('user_keys.one_click_status_verified')}
      </div>
    );
  }
  if (status === 'expired') {
    return (
      <div className="flex items-center gap-2 rounded-md border border-warning/40 bg-warning/10 px-3 py-2.5 text-sm text-warning">
        <TimerOff className="h-4 w-4 shrink-0" />
        {t('user_keys.one_click_status_expired')}
      </div>
    );
  }
  return (
    <div className="flex items-center gap-2 rounded-md border border-glass-border bg-surface px-3 py-2.5 text-sm text-text-secondary">
      <Loader2 className="h-4 w-4 shrink-0 animate-spin" />
      {status === 'exchanged'
        ? t('user_keys.one_click_status_exchanged')
        : t('user_keys.one_click_status_pending')}
    </div>
  );
}

// detectOs 按浏览器 UA 猜默认系统 tab；猜错也无妨,弹窗里可手动切换。
function detectOs(): 'unix' | 'powershell' {
  if (typeof navigator === 'undefined') return 'unix';
  const ua = navigator.userAgent || '';
  const platform = navigator.platform || '';
  return /windows/i.test(ua) || /^win/i.test(platform) ? 'powershell' : 'unix';
}

export function OneClickModal({
  target,
  issue,
  onRegenerate,
  onClose,
}: {
  target: APIKeyResp | null;
  issue: OneClickIssueResp | null;
  onRegenerate: () => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const copy = useClipboard();
  const [os, setOs] = useState<'unix' | 'powershell'>(detectOs);

  const modalState = useOverlayState({
    isOpen: !!target,
    onOpenChange: (nextOpen) => {
      if (!nextOpen) onClose();
    },
  });

  const { data: statusData } = useQuery({
    queryKey: queryKeys.oneclickStatus(issue?.token ?? ''),
    queryFn: () => oneclickApi.status(issue!.token),
    enabled: !!target && !!issue,
    refetchInterval: (query) => {
      const s = query.state.data?.status;
      return s === 'verified' || s === 'expired' ? false : 2000;
    },
  });
  const status = statusData?.status;

  const command = issue
    ? os === 'unix'
      ? issue.command_bash
      : issue.command_powershell
    : '';

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
              <Modal.Heading>{t('user_keys.one_click_title')}</Modal.Heading>
              <Modal.CloseTrigger />
            </Modal.Header>
            <Modal.Body>
              {issue ? (
                <div className="space-y-4">
                  <p className="text-sm text-text-secondary">{t('user_keys.one_click_desc')}</p>

                  {/* OS 切换 */}
                  <div className="flex gap-1">
                    <Button
                      fullWidth
                      size="sm"
                      variant={os === 'unix' ? 'primary' : 'secondary'}
                      onPress={() => setOs('unix')}
                    >
                      macOS / Linux
                    </Button>
                    <Button
                      fullWidth
                      size="sm"
                      variant={os === 'powershell' ? 'primary' : 'secondary'}
                      onPress={() => setOs('powershell')}
                    >
                      Windows PowerShell
                    </Button>
                  </div>

                  {/* 命令块 */}
                  <div className="rounded-md overflow-hidden border border-glass-border">
                    <div className="flex items-center justify-between px-3 py-1.5 bg-bg-hover border-b border-glass-border">
                      <span className="text-xs text-text-tertiary font-mono">
                        {os === 'unix' ? 'Terminal' : 'PowerShell'}
                      </span>
                      <Button
                        size="sm"
                        variant="ghost"
                        onPress={() => copy(command, t('user_keys.copied'))}
                      >
                        <Copy className="w-3 h-3" />
                        {t('user_keys.copy')}
                      </Button>
                    </div>
                    <pre className="p-3 text-sm font-mono text-text bg-surface overflow-x-auto whitespace-pre-wrap break-all">
                      {command}
                    </pre>
                  </div>

                  <p className="text-xs text-text-tertiary">{t('user_keys.one_click_expires_hint')}</p>

                  {/* 接入状态 */}
                  <StatusStrip status={status} />
                  {status === 'expired' && (
                    <Button size="sm" variant="secondary" onPress={onRegenerate}>
                      {t('user_keys.one_click_regenerate')}
                    </Button>
                  )}

                  {/* 进阶：CC Switch */}
                  <div className="rounded-md border border-glass-border bg-surface px-3 py-2.5">
                    <p className="text-xs text-text-tertiary">
                      {t('user_keys.one_click_ccs_hint')}{' '}
                      <a
                        href={CCS_RELEASES_URL}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex items-center gap-1 text-primary underline"
                      >
                        <Download className="h-3 w-3" />
                        {t('user_keys.one_click_ccs_download')}
                      </a>
                    </p>
                  </div>
                </div>
              ) : (
                <div className="flex items-center justify-center py-8 text-text-tertiary text-sm">
                  {t('common.loading')}
                </div>
              )}
            </Modal.Body>
            <Modal.Footer>
              <Button variant="primary" onPress={onClose}>
                {t('common.close')}
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}

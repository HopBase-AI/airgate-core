import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { Button, Modal, useOverlayState } from '@heroui/react';
import { DialogTriggerShim } from '../../../shared/components/DialogTriggerShim';
import { ArrowLeftRight, CheckCircle, Copy, Download, Loader2, Rocket, TimerOff } from 'lucide-react';
import { useToast } from '../../../shared/ui';
import { useClipboard } from '../../../shared/hooks/useClipboard';
import { oneclickApi, type OneClickIssueResp, type OneClickStatus } from '../../../shared/api/oneclick';
import { queryKeys } from '../../../shared/queryKeys';
import type { APIKeyResp, GroupResp } from '../../../shared/types';

const CCS_RELEASES_URL = 'https://github.com/farion1231/cc-switch/releases';

// 一键接入弹窗。流程刻意分两步:打开只展示说明,用户点「生成接入命令」这个明确的
// 确认动作后才签发一次性令牌并开始轮询——避免"一打开就 loading"造成的误解,
// 也不为误点的弹窗浪费令牌。令牌与客户端无关,Claude Code / Codex 命令共用。
export function useOneClickModal(groupMap: Map<number, GroupResp>) {
  const { toast } = useToast();
  const { t } = useTranslation();

  const [oneClickTarget, setOneClickTarget] = useState<APIKeyResp | null>(null);
  const [oneClickIssue, setOneClickIssue] = useState<OneClickIssueResp | null>(null);
  const [oneClickGenerating, setOneClickGenerating] = useState(false);

  const openOneClickModal = useCallback((row: APIKeyResp) => {
    setOneClickTarget(row);
    setOneClickIssue(null);
    setOneClickGenerating(false);
  }, []);

  const generateOneClick = useCallback(async () => {
    if (!oneClickTarget) return;
    setOneClickGenerating(true);
    try {
      const resp = await oneclickApi.issue(oneClickTarget.id);
      setOneClickIssue(resp);
    } catch {
      toast('error', t('user_keys.one_click_issue_failed'));
    } finally {
      setOneClickGenerating(false);
    }
  }, [oneClickTarget, toast, t]);

  const closeOneClickModal = useCallback(() => {
    setOneClickTarget(null);
    setOneClickIssue(null);
    setOneClickGenerating(false);
  }, []);

  const oneClickPlatform = oneClickTarget?.group_id != null
    ? groupMap.get(Number(oneClickTarget.group_id))?.platform || ''
    : '';

  return {
    oneClickTarget,
    oneClickIssue,
    oneClickGenerating,
    oneClickPlatform,
    openOneClickModal,
    generateOneClick,
    closeOneClickModal,
  };
}

// detectOs 按浏览器 UA 猜默认系统 tab；猜错也无妨,弹窗里可手动切换。
function detectOs(): 'unix' | 'powershell' {
  if (typeof navigator === 'undefined') return 'unix';
  const ua = navigator.userAgent || '';
  const platform = navigator.platform || '';
  return /windows/i.test(ua) || /^win/i.test(platform) ? 'powershell' : 'unix';
}

function StatusStrip({ status, verifiedText }: { status: OneClickStatus | undefined; verifiedText: string }) {
  const { t } = useTranslation();
  if (status === 'verified') {
    return (
      <div className="flex items-center gap-2 rounded-md border border-success/40 bg-success/10 px-3 py-2.5 text-sm text-success">
        <CheckCircle className="h-4 w-4 shrink-0" />
        {verifiedText}
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

export function OneClickModal({
  target,
  issue,
  generating,
  platform,
  onGenerate,
  onClose,
}: {
  target: APIKeyResp | null;
  issue: OneClickIssueResp | null;
  generating: boolean;
  platform: string;
  onGenerate: () => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const copy = useClipboard();
  const [os, setOs] = useState<'unix' | 'powershell'>(detectOs);
  const [client, setClient] = useState<'claude' | 'codex'>('claude');

  const modalState = useOverlayState({
    isOpen: !!target,
    onOpenChange: (nextOpen) => {
      if (!nextOpen) onClose();
    },
  });

  // Codex CLI 仅 openai 平台分组的密钥可用;其余平台强制回落 Claude Code。
  const showClientTabs = platform === 'openai';
  const effectiveClient = showClientTabs ? client : 'claude';

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
    ? effectiveClient === 'codex'
      ? (os === 'unix' ? issue.command_codex_bash : issue.command_codex_powershell)
      : (os === 'unix' ? issue.command_bash : issue.command_powershell)
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
              <Modal.Heading>
                {effectiveClient === 'codex'
                  ? t('user_keys.one_click_title_codex')
                  : t('user_keys.one_click_title')}
              </Modal.Heading>
              <Modal.CloseTrigger />
            </Modal.Header>
            <Modal.Body>
              <div className="space-y-4">
                <p className="text-sm text-text-secondary">
                  {effectiveClient === 'codex'
                    ? t('user_keys.one_click_desc_codex')
                    : t('user_keys.one_click_desc')}
                </p>

                {/* 客户端切换（仅 openai 平台密钥） */}
                {showClientTabs && (
                  <div className="flex gap-1">
                    <Button
                      fullWidth
                      size="sm"
                      variant={effectiveClient === 'claude' ? 'primary' : 'secondary'}
                      onPress={() => setClient('claude')}
                    >
                      Claude Code
                    </Button>
                    <Button
                      fullWidth
                      size="sm"
                      variant={effectiveClient === 'codex' ? 'primary' : 'secondary'}
                      onPress={() => setClient('codex')}
                    >
                      Codex CLI
                    </Button>
                  </div>
                )}

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

                {!issue ? (
                  <>
                    {/* 确认动作:点击才签发令牌,不会动用户本机任何东西 */}
                    <Button
                      fullWidth
                      variant="primary"
                      isDisabled={generating}
                      onPress={onGenerate}
                    >
                      {generating
                        ? <Loader2 className="h-4 w-4 animate-spin" />
                        : <Rocket className="h-4 w-4" />}
                      {t('user_keys.one_click_generate')}
                    </Button>
                    <p className="text-xs text-text-tertiary">{t('user_keys.one_click_pregen_hint')}</p>
                  </>
                ) : (
                  <>
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
                    <StatusStrip
                      status={status}
                      verifiedText={effectiveClient === 'codex'
                        ? t('user_keys.one_click_status_verified_codex')
                        : t('user_keys.one_click_status_verified')}
                    />
                    {status === 'expired' && (
                      <Button size="sm" variant="secondary" isDisabled={generating} onPress={onGenerate}>
                        {t('user_keys.one_click_regenerate')}
                      </Button>
                    )}
                  </>
                )}

                {/* 进阶：CC Switch（重点样式） */}
                <div
                  className="rounded-md border px-3 py-2.5"
                  style={{
                    borderColor: 'color-mix(in srgb, var(--ag-primary) 40%, transparent)',
                    background: 'color-mix(in srgb, var(--ag-primary) 8%, transparent)',
                  }}
                >
                  <p className="flex items-center gap-1.5 text-[13px] font-semibold text-text">
                    <ArrowLeftRight className="h-3.5 w-3.5 shrink-0" style={{ color: 'var(--ag-primary)' }} />
                    {t('user_keys.one_click_ccs_title')}
                  </p>
                  <p className="mt-1 text-xs leading-5 text-text-secondary">
                    {t('user_keys.one_click_ccs_hint')}
                  </p>
                  <a
                    href={CCS_RELEASES_URL}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="mt-1.5 inline-flex items-center gap-1 text-xs font-medium underline"
                    style={{ color: 'var(--ag-primary)' }}
                  >
                    <Download className="h-3.5 w-3.5" />
                    {t('user_keys.one_click_ccs_download')}
                  </a>
                </div>
              </div>
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

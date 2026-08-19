import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { Button, Input, Label, Modal, Spinner, TextField as HeroTextField, useOverlayState } from '@heroui/react';
import { DialogTriggerShim } from '../../../shared/components/DialogTriggerShim';
import { Eye, EyeOff, RefreshCw } from 'lucide-react';
import { useClipboard } from '../../../shared/hooks/useClipboard';
import { groupsApi } from '../../../shared/api/groups';
import { settingsApi } from '../../../shared/api/settings';
import { queryKeys } from '../../../shared/queryKeys';
import { FETCH_ALL_PARAMS } from '../../../shared/constants';
import type { CreateUserReq } from '../../../shared/types';
import { formatZhe, parseQuoteFx, parseZheInput, rateOfZhe, zheOfRate, zheWithPoints } from '../../../shared/quoteMath';

// 建号即配价的快捷报价：standard=标准牌价；plus=各分组默认折 + N 点；uniform=统一 N 折。
// 逐分组微调走建号后的报价单面板。
type QuoteChoice = 'standard' | 'plus' | 'uniform';

interface CreateUserModalProps {
  open: boolean;
  onClose: () => void;
  onSubmit: (data: CreateUserReq) => void;
  loading: boolean;
  defaultMaxConcurrency: number;
}

function createDefaultForm(defaultMaxConcurrency: number): CreateUserReq {
  return {
    email: '',
    display_badge: '',
    max_concurrency: defaultMaxConcurrency,
    password: '',
    role: 'user',
    username: '',
  };
}

export function CreateUserModal({ open, onClose, onSubmit, loading, defaultMaxConcurrency }: CreateUserModalProps) {
  const { t } = useTranslation();
  const copy = useClipboard();
  const [form, setForm] = useState<CreateUserReq>(() => createDefaultForm(defaultMaxConcurrency));
  const [showPassword, setShowPassword] = useState(false);
  const [maxConcurrencyTouched, setMaxConcurrencyTouched] = useState(false);
  const [quoteChoice, setQuoteChoice] = useState<QuoteChoice>('standard');
  const [quotePoints, setQuotePoints] = useState('2');
  const [quoteZhe, setQuoteZhe] = useState('');

  const { data: groupsData } = useQuery({
    queryKey: queryKeys.groupsAll(),
    queryFn: () => groupsApi.list(FETCH_ALL_PARAMS),
    enabled: open,
  });
  const { data: publicSettings } = useQuery({
    queryKey: queryKeys.siteSettings(),
    queryFn: settingsApi.getPublic,
    staleTime: 5 * 60_000,
    retry: false,
    enabled: open,
  });
  const fx = useMemo(() => parseQuoteFx(publicSettings?.toc_landing_pricing), [publicSettings?.toc_landing_pricing]);
  // 新用户尚无专属分组授权，快捷报价只覆盖普通分组里有 token 倍率的
  const tokenGroups = useMemo(
    () => (groupsData?.list ?? []).filter((g) => !g.delisted && !g.is_exclusive && g.rate_multiplier > 0),
    [groupsData?.list],
  );

  // 表单与快捷报价共用一个重置入口：关闭回调与 open=false 的 effect 各自调用，
  // 新增状态只需登记一处，避免两份重置清单漂移导致跨次打开泄漏。
  const resetAll = () => {
    setForm(createDefaultForm(defaultMaxConcurrency));
    setMaxConcurrencyTouched(false);
    setQuoteChoice('standard');
    setQuotePoints('2');
    setQuoteZhe('');
  };

  useEffect(() => {
    if (!open) {
      resetAll();
      return;
    }
    if (!maxConcurrencyTouched) {
      setForm((prev) => ({ ...prev, max_concurrency: defaultMaxConcurrency }));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- resetAll 仅依赖 setter 与 defaultMaxConcurrency
  }, [defaultMaxConcurrency, maxConcurrencyTouched, open]);

  const handleClose = () => {
    resetAll();
    onClose();
  };

  // 快捷报价输入无效时禁止提交：静默建出一个「报价模式但没有报价单」的用户
  // 等于按标准价卖给报价客户，界面上看不出任何异常，只会在对账时爆雷。
  const quoteInputInvalid =
    (quoteChoice === 'plus' && (quotePoints.trim() === '' || !Number.isFinite(Number(quotePoints)) || Number(quotePoints) === 0)) ||
    (quoteChoice === 'uniform' && parseZheInput(quoteZhe) == null);

  // 快捷报价 → group_rates（倍率换算统一走 quoteMath，与报价单面板同一规则）
  const buildQuoteFields = (): Pick<CreateUserReq, 'pricing_mode' | 'group_rates'> => {
    if (quoteChoice === 'standard') return {};
    const group_rates: Record<number, number> = {};
    if (quoteChoice === 'plus') {
      const points = Number(quotePoints);
      if (quoteInputInvalid) return {}; // 提交闸兜底：无效输入宁可回落标准牌价
      for (const g of tokenGroups) {
        const zhe = zheWithPoints(g.rate_multiplier, points, fx);
        if (zhe > 0) group_rates[g.id] = rateOfZhe(zhe, fx);
      }
    } else {
      const zhe = parseZheInput(quoteZhe);
      if (zhe == null) return {};
      for (const g of tokenGroups) {
        group_rates[g.id] = rateOfZhe(zhe, fx);
      }
    }
    return { pricing_mode: 'quote', group_rates };
  };

  const generatePassword = () => {
    const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%&*';
    const arr = new Uint8Array(16);
    crypto.getRandomValues(arr);
    const pwd = Array.from(arr, (b) => chars[b % chars.length]).join('');
    setForm({ ...form, password: pwd });
    copy(pwd);
  };

  const handleSubmit = () => {
    if (!form.email || !form.password || quoteInputInvalid) return;
    onSubmit({ ...form, ...buildQuoteFields() });
  };
  const modalState = useOverlayState({
    isOpen: open,
    onOpenChange: (nextOpen) => {
      if (!nextOpen) handleClose();
    },
  });

  return (
    <Modal state={modalState}>
      <DialogTriggerShim />
      <Modal.Backdrop>
        <Modal.Container placement="center" scroll="inside" size="md">
          <Modal.Dialog className="ag-elevation-modal">
            <Modal.Header>
              <Modal.Heading>{t('users.create')}</Modal.Heading>
              <Modal.CloseTrigger />
            </Modal.Header>
            <Modal.Body>
              <div className="space-y-4">
                <HeroTextField fullWidth isRequired>
                  <Label>{t('users.email')}</Label>
                  <Input
                    name="email"
                    type="email"
                    value={form.email}
                    onChange={(e) => setForm({ ...form, email: e.target.value })}
                    autoComplete="email"
                    required
                  />
                </HeroTextField>
                <div className="space-y-1.5">
                  <HeroTextField fullWidth isRequired>
                    <Label>{t('users.password')}</Label>
                    <div className="relative">
                      <Input
                        className="pr-10"
                        name="new-password"
                        type={showPassword ? 'text' : 'password'}
                        value={form.password}
                        onChange={(e) => setForm({ ...form, password: e.target.value })}
                        autoComplete="new-password"
                        required
                      />
                      <Button
                        isIconOnly
                        aria-label={showPassword ? 'Hide password' : 'Show password'}
                        className="absolute right-1 top-1/2 z-10 -translate-y-1/2"
                        size="sm"
                        type="button"
                        variant="ghost"
                        onPress={() => setShowPassword((value) => !value)}
                      >
                        {showPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                      </Button>
                    </div>
                  </HeroTextField>
                  <Button size="sm" variant="ghost" onPress={generatePassword}>
                    <RefreshCw className="h-3 w-3" />
                    {t('users.generate_password')}
                  </Button>
                </div>
                <HeroTextField fullWidth>
                  <Label>{t('users.username')}</Label>
                  <Input
                    name="username"
                    value={form.username}
                    onChange={(e) => setForm({ ...form, username: e.target.value })}
                    autoComplete="username"
                  />
                </HeroTextField>
                <HeroTextField fullWidth>
                  <Label>{t('users.display_badge')}</Label>
                  <Input
                    name="display_badge"
                    value={form.display_badge ?? ''}
                    maxLength={32}
                    placeholder={t('users.display_badge_placeholder')}
                    onChange={(e) => setForm({ ...form, display_badge: e.target.value })}
                  />
                </HeroTextField>
                <HeroTextField fullWidth>
                  <Label>{t('users.max_concurrency')}</Label>
                  <Input
                    type="number"
                    min="0"
                    value={String(form.max_concurrency ?? 0)}
                    onChange={(e) => {
                      setMaxConcurrencyTouched(true);
                      setForm({ ...form, max_concurrency: Number(e.target.value) });
                    }}
                  />
                </HeroTextField>

                <div className="space-y-2 border-t border-glass-border pt-3">
                  <p className="text-sm font-medium text-text">{t('users.quote')}</p>
                  <div className="flex flex-wrap items-center gap-2">
                    <Button
                      aria-pressed={quoteChoice === 'standard'}
                      size="sm"
                      variant={quoteChoice === 'standard' ? 'primary' : 'secondary'}
                      onPress={() => setQuoteChoice('standard')}
                    >
                      {t('users.quote_mode_standard')}
                    </Button>
                    <Button
                      aria-pressed={quoteChoice === 'plus'}
                      size="sm"
                      variant={quoteChoice === 'plus' ? 'primary' : 'secondary'}
                      onPress={() => setQuoteChoice('plus')}
                    >
                      {t('users.quote_create_plus')}
                    </Button>
                    <Button
                      aria-pressed={quoteChoice === 'uniform'}
                      size="sm"
                      variant={quoteChoice === 'uniform' ? 'primary' : 'secondary'}
                      onPress={() => setQuoteChoice('uniform')}
                    >
                      {t('users.quote_create_uniform')}
                    </Button>
                    {quoteChoice === 'plus' ? (
                      <span className="flex items-center gap-1 text-xs text-text-secondary">
                        <span>{t('users.quote_bulk_points_prefix')}</span>
                        <span className="w-16">
                          <HeroTextField fullWidth>
                            <Input
                              aria-label={t('users.quote_bulk_points_prefix')}
                              type="number"
                              step="0.1"
                              value={quotePoints}
                              onChange={(e) => setQuotePoints(e.target.value)}
                            />
                          </HeroTextField>
                        </span>
                        <span>{t('users.quote_bulk_points_unit')}</span>
                      </span>
                    ) : null}
                    {quoteChoice === 'uniform' ? (
                      <span className="flex items-center gap-1 text-xs text-text-secondary">
                        <span className="w-16">
                          <HeroTextField fullWidth>
                            <Input
                              aria-label={t('users.quote_create_uniform')}
                              type="number"
                              min="0"
                              step="0.1"
                              value={quoteZhe}
                              onChange={(e) => setQuoteZhe(e.target.value)}
                            />
                          </HeroTextField>
                        </span>
                        <span>{t('users.quote_zhe_unit')}</span>
                      </span>
                    ) : null}
                  </div>
                  <p className="text-xs text-text-tertiary">
                    {quoteChoice === 'standard'
                      ? t('users.quote_create_standard_hint')
                      : t('users.quote_create_note', {
                          n: tokenGroups.length,
                          example: tokenGroups[0]
                            ? t('users.quote_create_example', {
                                group: tokenGroups[0].name,
                                zhe: formatZhe(zheOfRate(tokenGroups[0].rate_multiplier, fx)),
                              })
                            : '',
                        })}
                  </p>
                </div>
              </div>
            </Modal.Body>
            <Modal.Footer>
              <Button variant="secondary" onPress={handleClose}>
                {t('common.cancel')}
              </Button>
              <Button variant="primary" isDisabled={loading || quoteInputInvalid} onPress={handleSubmit}>
                {loading ? <Spinner size="sm" /> : null}
                {t('common.create')}
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}

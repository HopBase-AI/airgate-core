import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { queryKeys } from '../../../shared/queryKeys';
import { Button, Checkbox, Chip, ComboBox, Description, Input, Label, ListBox, Modal, Select, Spinner, TextArea, TextField as HeroTextField, useOverlayState } from '@heroui/react';
import { DialogTriggerShim } from '../../../shared/components/DialogTriggerShim';
import { ArrowUpDown, Layers, Search, X } from 'lucide-react';
import { groupsApi } from '../../../shared/api/groups';
import { accountsApi } from '../../../shared/api/accounts';
import { usersApi } from '../../../shared/api/users';
import { useDebouncedValue } from '../../../shared/hooks/useDebouncedValue';
import { NativeSwitch } from '../../../shared/components/NativeSwitch';
import type { GroupResp, GroupAllowedUser, CreateGroupReq, UpdateGroupReq } from '../../../shared/types';

// 分组可见性：公开（所有用户）/ 指定用户可见 / 仅管理员可见。
// 后端实际只有 is_exclusive + allowed_users 两个原语，这里在 UI 层归并成三态：
//   public   → is_exclusive=false
//   specific → is_exclusive=true 且 allowed_user_ids 非空
//   admin    → is_exclusive=true 且 allowed_user_ids 为空
type Visibility = 'public' | 'specific' | 'admin';

function initialVisibility(group?: GroupResp): Visibility {
  if (!group?.is_exclusive) return 'public';
  return (group.allowed_users?.length ?? 0) > 0 ? 'specific' : 'admin';
}

function parseQuotas(quotas?: Record<string, unknown>): { daily: string; weekly: string; monthly: string } {
  return {
    daily: quotas?.daily ? String(quotas.daily) : '',
    monthly: quotas?.monthly ? String(quotas.monthly) : '',
    weekly: quotas?.weekly ? String(quotas.weekly) : '',
  };
}

function buildQuotas(q: { daily: string; weekly: string; monthly: string }): Record<string, unknown> | undefined {
  const result: Record<string, number> = {};
  if (q.daily && Number(q.daily) > 0) result.daily = Number(q.daily);
  if (q.weekly && Number(q.weekly) > 0) result.weekly = Number(q.weekly);
  if (q.monthly && Number(q.monthly) > 0) result.monthly = Number(q.monthly);
  return Object.keys(result).length > 0 ? result : undefined;
}

type ImagePrices = {
  oneK: string;
  twoK: string;
  fourK: string;
};

const IMAGE_PRICE_FIELDS: Array<{ key: keyof ImagePrices; setting: string; label: string }> = [
  { key: 'oneK', setting: 'image_price_1k', label: '1K' },
  { key: 'twoK', setting: 'image_price_2k', label: '2K' },
  { key: 'fourK', setting: 'image_price_4k', label: '4K' },
];

function parseImagePrices(settings?: Record<string, Record<string, string>>): ImagePrices {
  const openai = settings?.openai ?? {};
  return {
    oneK: openai.image_price_1k ?? '',
    twoK: openai.image_price_2k ?? '',
    fourK: openai.image_price_4k ?? '',
  };
}

function clonePluginSettings(
  settings?: Record<string, Record<string, string>>,
): Record<string, Record<string, string>> {
  const result: Record<string, Record<string, string>> = {};
  for (const [plugin, values] of Object.entries(settings ?? {})) {
    result[plugin] = { ...values };
  }
  return result;
}

function buildOpenAISettings(
  current: Record<string, string> | undefined,
  imageEnabled: boolean,
  prices: ImagePrices,
): Record<string, string> {
  const settings: Record<string, string> = { ...(current ?? {}), image_enabled: imageEnabled ? 'true' : 'false' };

  for (const field of IMAGE_PRICE_FIELDS) {
    delete settings[field.setting];
    const raw = prices[field.key].trim();
    if (raw === '') continue;
    const value = Number(raw);
    if (Number.isFinite(value) && value >= 0) {
      settings[field.setting] = raw;
    }
  }
  return settings;
}

export function GroupFormModal({
  open,
  title,
  group,
  onClose,
  onSubmit,
  loading,
  platforms,
  instructionPresets,
}: {
  open: boolean;
  title: string;
  group?: GroupResp;
  onClose: () => void;
  onSubmit: (data: CreateGroupReq | UpdateGroupReq) => void;
  loading: boolean;
  platforms: string[];
  instructionPresets: (platform: string) => string[];
}) {
  const { t } = useTranslation();
  const isEdit = !!group;

  const [form, setForm] = useState({
    force_instructions: group?.force_instructions ?? '',
    name: group?.name ?? '',
    note: group?.note ?? '',
    platform: group?.platform ?? '',
    rate_multiplier: group?.rate_multiplier ?? 1,
    sort_weight: group?.sort_weight ?? 0,
    status_visible: group?.status_visible ?? true,
    subscription_type: group?.subscription_type ?? 'standard' as const,
  });
  // 可见性三态 + 指定用户清单（提交时映射为 is_exclusive / allowed_user_ids）。
  const [visibility, setVisibility] = useState<Visibility>(() => initialVisibility(group));
  const [allowedUsers, setAllowedUsers] = useState<GroupAllowedUser[]>(() => group?.allowed_users ?? []);
  const [userQuery, setUserQuery] = useState('');
  const debouncedUserQuery = useDebouncedValue(userQuery.trim(), 250);
  const [quotas, setQuotas] = useState(parseQuotas(group?.quotas as Record<string, unknown> | undefined));
  const [claudeCodeOnly, setClaudeCodeOnly] = useState(group?.plugin_settings?.claude?.claude_code_only === 'true');
  const [imageEnabled, setImageEnabled] = useState(group?.plugin_settings?.openai?.image_enabled === 'true');
  const [imagePrices, setImagePrices] = useState<ImagePrices>(() => parseImagePrices(group?.plugin_settings));
  const [copyFromGroupIds, setCopyFromGroupIds] = useState<number[]>([]);

  // 支持模型：编辑 model_routing（模型ID → 账号ID[]）。一个分组支持哪些模型 = 该 map 的 key 集合。
  // 始终从原值出发、只按勾选增删 key，保证候选加载失败时提交的仍是原值，不会误清。
  const [modelRouting, setModelRouting] = useState<Record<string, number[]>>(group?.model_routing ?? {});
  const routedAccountIds = Array.from(new Set(Object.values(modelRouting).flat()));
  const primaryAccountId = routedAccountIds[0];
  // 候选模型取自分组现有账号（从 model_routing 的账号ID里取代表账号查其模型目录）。
  const { data: candidateModels = [] } = useQuery({
    queryKey: queryKeys.accountModels(primaryAccountId),
    queryFn: () => accountsApi.models(primaryAccountId as number),
    enabled: open && isEdit && primaryAccountId != null,
  });
  const modelNameById = new Map(candidateModels.map((m) => [m.id, m.name]));
  const supportedModelIds = Array.from(
    new Set([...candidateModels.map((m) => m.id), ...Object.keys(modelRouting)]),
  ).sort();
  const toggleModel = (modelId: string, checked: boolean) => {
    setModelRouting((prev) => {
      const next = { ...prev };
      if (checked) {
        if (!next[modelId]) next[modelId] = routedAccountIds;
      } else {
        delete next[modelId];
      }
      return next;
    });
  };

  const { data: copySourceData } = useQuery({
    queryKey: queryKeys.groupsForCopy(form.platform),
    queryFn: () => groupsApi.list({ page: 1, page_size: 100, platform: form.platform }),
    enabled: !isEdit && !!form.platform && open,
  });
  const copySourceGroups: GroupResp[] = copySourceData?.list ?? [];

  // 指定用户可见：按邮箱/用户名服务端搜索；已选用户存于 allowedUsers，从候选里剔除。
  const { data: userSearchData } = useQuery({
    queryKey: queryKeys.users('group-allowed-users-search', debouncedUserQuery),
    queryFn: () => usersApi.list({ page: 1, page_size: 20, keyword: debouncedUserQuery || undefined }),
    enabled: open && visibility === 'specific',
  });
  const allowedUserIdSet = useMemo(() => new Set(allowedUsers.map((u) => u.user_id)), [allowedUsers]);
  const userSearchOptions = useMemo(
    () => (userSearchData?.list ?? [])
      .filter((u) => !allowedUserIdSet.has(u.id))
      .map((u) => ({ id: String(u.id), label: u.email, description: u.username, textValue: `${u.email} ${u.username ?? ''}` })),
    [userSearchData?.list, allowedUserIdSet],
  );
  const visibilityOptions = [
    { id: 'public', label: t('groups.visibility_public') },
    { id: 'specific', label: t('groups.visibility_specific') },
    { id: 'admin', label: t('groups.visibility_admin') },
  ];
  const visibilityLabel = visibilityOptions.find((o) => o.id === visibility)?.label ?? '';
  const visibilityHint = t(`groups.visibility_${visibility}_hint`);

  const platformOptions = [
    { id: '', label: t('groups.select_platform') },
    ...platforms.map((platform) => ({ id: platform, label: platform })),
  ];
  const selectedPlatformLabel = platformOptions.find((item) => item.id === form.platform)?.label ?? t('groups.select_platform');
  const copyAccountOptions = [
    {
      id: '',
      label: !form.platform
        ? t('groups.copy_accounts_select_platform_first')
        : copySourceGroups.length === 0
          ? t('groups.copy_accounts_empty')
          : t('groups.copy_accounts_placeholder'),
    },
    ...copySourceGroups
      .filter((copyGroup) => !copyFromGroupIds.includes(copyGroup.id))
      .map((copyGroup) => ({
        id: String(copyGroup.id),
        label: `${copyGroup.name} (${t('groups.copy_accounts_count', { count: copyGroup.account_total })})`,
      })),
  ];
  const subscriptionTypeOptions = [
    { id: 'standard', label: t('groups.type_standard') },
    { id: 'subscription', label: t('groups.type_subscription') },
  ];
  const selectedSubscriptionTypeLabel =
    subscriptionTypeOptions.find((item) => item.id === form.subscription_type)?.label ?? t('groups.type_standard');

  const handleSubmit = () => {
    if (!isEdit && (!form.name || !form.platform)) return;

    const pluginSettings = clonePluginSettings(group?.plugin_settings);
    if (form.platform === 'claude') {
      pluginSettings.claude = {
        ...(pluginSettings.claude ?? {}),
        claude_code_only: claudeCodeOnly ? 'true' : 'false',
      };
    }
    if (form.platform === 'openai') {
      pluginSettings.openai = buildOpenAISettings(pluginSettings.openai, imageEnabled, imagePrices);
    }

    onSubmit({
      ...form,
      force_instructions: form.force_instructions ?? '',
      note: form.note,
      is_exclusive: visibility !== 'public',
      allowed_user_ids: visibility === 'specific' ? allowedUsers.map((u) => u.user_id) : [],
      plugin_settings: Object.keys(pluginSettings).length > 0 ? pluginSettings : undefined,
      quotas: form.subscription_type === 'subscription' ? buildQuotas(quotas) : undefined,
      subscription_type: form.subscription_type as 'standard' | 'subscription',
      ...(isEdit ? { model_routing: modelRouting } : {}),
      ...(!isEdit && copyFromGroupIds.length > 0
        ? { copy_accounts_from_group_ids: copyFromGroupIds }
        : {}),
    });
  };

  const presets = instructionPresets(form.platform);
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
              <Modal.Heading>{title}</Modal.Heading>
              <Modal.CloseTrigger />
            </Modal.Header>
            <Modal.Body>
      <div className="space-y-4">
        <HeroTextField fullWidth isRequired>
          <Label>{t('common.name')}</Label>
          <div className="relative">
            <Layers className="pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-text-tertiary" />
            <Input
              className="pl-9"
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              required
            />
          </div>
        </HeroTextField>

        {isEdit ? (
          <HeroTextField fullWidth isDisabled>
            <Label>{t('groups.platform')}</Label>
            <Input value={form.platform} disabled />
          </HeroTextField>
        ) : (
          <Select
            fullWidth
            isRequired
            selectedKey={form.platform}
            onSelectionChange={(key) => {
              setForm({ ...form, platform: key == null ? '' : String(key) });
              setCopyFromGroupIds([]);
            }}
          >
            <Label>{t('groups.platform')}</Label>
            <Select.Trigger>
              <Select.Value>{selectedPlatformLabel}</Select.Value>
              <Select.Indicator />
            </Select.Trigger>
            <Select.Popover>
              <ListBox items={platformOptions}>
                {(item) => (
                  <ListBox.Item id={item.id} textValue={item.label}>
                    {item.label}
                  </ListBox.Item>
                )}
              </ListBox>
            </Select.Popover>
          </Select>
        )}

        {isEdit ? (
          <div>
            <p className="mb-1.5 text-xs font-medium uppercaser text-text-secondary">支持模型</p>
            {routedAccountIds.length === 0 ? (
              <p className="text-[11px] text-text-tertiary">请先为该分组分配账号后再配置支持模型。</p>
            ) : (
              <>
                <p className="mb-2 text-[11px] text-text-tertiary">
                  仅勾选的模型会对本分组开放；取消勾选即从本分组下架该模型。
                </p>
                <div className="flex flex-col gap-2">
                  {supportedModelIds.map((modelId) => (
                    <Checkbox
                      key={modelId}
                      isSelected={!!modelRouting[modelId]}
                      onChange={(selected) => toggleModel(modelId, selected)}
                    >
                      <Checkbox.Control>
                        <Checkbox.Indicator />
                      </Checkbox.Control>
                      <span className="text-sm text-text">
                        {modelNameById.get(modelId) ?? modelId}
                        {modelNameById.get(modelId) ? (
                          <span className="ml-1 text-[11px] text-text-tertiary">{modelId}</span>
                        ) : null}
                      </span>
                    </Checkbox>
                  ))}
                </div>
              </>
            )}
          </div>
        ) : null}

        {!isEdit ? (
          <div>
            <p className="mb-1.5 text-xs font-medium uppercaser text-text-secondary">
              {t('groups.copy_accounts_title')}
            </p>
            {copyFromGroupIds.length > 0 ? (
              <div className="mb-2 flex flex-wrap gap-1.5">
                {copyFromGroupIds.map((groupId) => {
                  const sourceGroup = copySourceGroups.find((item) => item.id === groupId);
                  return (
                    <Chip key={groupId} color="accent" size="sm" variant="soft">
                      {sourceGroup ? sourceGroup.name : `#${groupId}`}
                      <Button
                        isIconOnly
                        aria-label="remove"
                        size="sm"
                        variant="ghost"
                        onPress={() => setCopyFromGroupIds(copyFromGroupIds.filter((id) => id !== groupId))}
                      >
                        <X className="h-3 w-3" />
                      </Button>
                    </Chip>
                  );
                })}
              </div>
            ) : null}
            <Select
              fullWidth
              isDisabled={!form.platform}
              selectedKey=""
              onSelectionChange={(key) => {
                const value = Number(key);
                if (value && !copyFromGroupIds.includes(value)) {
                  setCopyFromGroupIds([...copyFromGroupIds, value]);
                }
              }}
            >
              <Select.Trigger>
                <Select.Value>{copyAccountOptions[0]?.label}</Select.Value>
                <Select.Indicator />
              </Select.Trigger>
              <Select.Popover>
                <ListBox items={copyAccountOptions}>
                  {(item) => (
                    <ListBox.Item id={item.id} textValue={item.label}>
                      {item.label}
                    </ListBox.Item>
                  )}
                </ListBox>
              </Select.Popover>
            </Select>
            <p className="mt-1 text-[11px] text-text-tertiary">{t('groups.copy_accounts_hint')}</p>
          </div>
        ) : null}

        <HeroTextField fullWidth>
          <Label>{t('groups.rate_multiplier')}</Label>
          <Input
            type="number"
            step="0.1"
            value={String(form.rate_multiplier)}
            onChange={(e) => setForm({ ...form, rate_multiplier: Number(e.target.value) })}
          />
        </HeroTextField>

        <div className="space-y-3 rounded-lg border border-glass-border p-3">
          <Select
            fullWidth
            selectedKey={visibility}
            onSelectionChange={(key) => setVisibility((key ?? 'public') as Visibility)}
          >
            <Label>{t('groups.visibility')}</Label>
            <Select.Trigger>
              <Select.Value>{visibilityLabel}</Select.Value>
              <Select.Indicator />
            </Select.Trigger>
            <Select.Popover>
              <ListBox items={visibilityOptions}>
                {(item) => (
                  <ListBox.Item id={item.id} textValue={item.label}>
                    {item.label}
                  </ListBox.Item>
                )}
              </ListBox>
            </Select.Popover>
          </Select>
          <p className="text-[11px] text-text-tertiary">{visibilityHint}</p>

          {visibility === 'specific' ? (
            <div>
              {allowedUsers.length > 0 ? (
                <div className="mb-2 flex flex-wrap gap-1.5">
                  {allowedUsers.map((u) => (
                    <Chip key={u.user_id} color="accent" size="sm" variant="soft">
                      {u.email}
                      <Button
                        isIconOnly
                        aria-label="remove"
                        size="sm"
                        variant="ghost"
                        onPress={() => setAllowedUsers((cur) => cur.filter((x) => x.user_id !== u.user_id))}
                      >
                        <X className="h-3 w-3" />
                      </Button>
                    </Chip>
                  ))}
                </div>
              ) : (
                <p className="mb-2 text-[11px] text-warning">{t('groups.visibility_specific_empty')}</p>
              )}
              <ComboBox
                aria-label={t('groups.visibility_search_placeholder')}
                allowsEmptyCollection
                fullWidth
                inputValue={userQuery}
                items={userSearchOptions}
                menuTrigger="focus"
                selectedKey={null}
                onInputChange={setUserQuery}
                onSelectionChange={(key) => {
                  const value = key == null ? '' : String(key);
                  if (!value) return;
                  const picked = (userSearchData?.list ?? []).find((item) => String(item.id) === value);
                  if (picked) {
                    setAllowedUsers((cur) =>
                      cur.some((x) => x.user_id === picked.id)
                        ? cur
                        : [...cur, { user_id: picked.id, email: picked.email, username: picked.username }],
                    );
                  }
                  setUserQuery('');
                }}
              >
                <ComboBox.InputGroup className="relative">
                  <Search className="pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-text-tertiary" />
                  <Input className="pl-9 pr-10" placeholder={t('groups.visibility_search_placeholder') ?? ''} />
                  <ComboBox.Trigger className="ag-combobox-preview-trigger absolute right-1 top-1/2 z-10 h-7 w-7 min-w-0 -translate-y-1/2 p-0 text-text-tertiary hover:text-text" />
                </ComboBox.InputGroup>
                <ComboBox.Popover>
                  <ListBox
                    items={userSearchOptions}
                    renderEmptyState={() => (
                      <div className="px-3 py-6 text-center text-xs text-text-tertiary">
                        {debouncedUserQuery ? t('common.no_data') : t('users.search_placeholder')}
                      </div>
                    )}
                  >
                    {(item) => (
                      <ListBox.Item id={item.id} textValue={item.textValue}>
                        <div className="min-w-0">
                          <div className="truncate text-sm text-text">{item.label}</div>
                          {item.description ? (
                            <div className="truncate text-xs text-text-tertiary">{item.description}</div>
                          ) : null}
                        </div>
                      </ListBox.Item>
                    )}
                  </ListBox>
                </ComboBox.Popover>
              </ComboBox>
            </div>
          ) : null}

          <NativeSwitch
            isSelected={form.status_visible}
            label={<span className="text-sm text-text">{t('groups.status_visible_hint')}</span>}
            onChange={(selected) => setForm({ ...form, status_visible: selected })}
          />
        </div>

        <Select
          fullWidth
          selectedKey={form.subscription_type}
          onSelectionChange={(key) =>
            setForm({ ...form, subscription_type: (key ?? 'standard') as 'standard' | 'subscription' })
          }
        >
          <Label>{t('groups.subscription_type')}</Label>
          <Select.Trigger>
            <Select.Value>{selectedSubscriptionTypeLabel}</Select.Value>
            <Select.Indicator />
          </Select.Trigger>
          <Select.Popover>
            <ListBox items={subscriptionTypeOptions}>
              {(item) => (
                <ListBox.Item id={item.id} textValue={item.label}>
                  {item.label}
                </ListBox.Item>
              )}
            </ListBox>
          </Select.Popover>
        </Select>

        <HeroTextField fullWidth>
          <Label>{t('groups.sort_weight')}</Label>
          <div className="relative">
            <ArrowUpDown className="pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-text-tertiary" />
            <Input
              className="pl-9"
              type="number"
              value={String(form.sort_weight)}
              onChange={(e) => setForm({ ...form, sort_weight: Number(e.target.value) })}
            />
          </div>
          <Description>{t('groups.sort_weight_hint')}</Description>
        </HeroTextField>

        <HeroTextField fullWidth>
          <Label>{t('groups.note')}</Label>
          <Input
            value={form.note}
            onChange={(e) => setForm({ ...form, note: e.target.value })}
            placeholder={t('groups.note_placeholder')}
          />
          <Description>{t('groups.note_hint')}</Description>
        </HeroTextField>

        {presets.length > 0 ? (
          <div>
            <p className="mb-1.5 text-xs font-medium uppercaser text-text-secondary">
              {t('groups.force_instructions')}
            </p>
            <p className="mb-2 text-[11px] text-text-tertiary">{t('groups.force_instructions_hint')}</p>
            <div className="mb-2 flex flex-wrap gap-2">
              {['', ...presets].map((preset) => (
                <Button
                  key={preset}
                  size="sm"
                  variant={form.force_instructions === preset ? 'primary' : 'secondary'}
                  onPress={() => setForm({ ...form, force_instructions: preset })}
                >
                  {preset || t('groups.instructions_none')}
                </Button>
              ))}
            </div>
            {form.force_instructions && !presets.includes(form.force_instructions) ? (
              <HeroTextField fullWidth>
                <TextArea
                  rows={4}
                  value={form.force_instructions}
                  onChange={(e) => setForm({ ...form, force_instructions: e.target.value })}
                  placeholder={t('groups.instructions_custom_placeholder')}
                />
              </HeroTextField>
            ) : null}
          </div>
        ) : null}

        {form.platform === 'claude' ? (
          <NativeSwitch
            isSelected={claudeCodeOnly}
            label={(
              <span>
                <span className="block text-sm">仅 Claude Code 客户端</span>
                <span className="mt-1 block text-[11px] text-text-tertiary">
                  开启后，本分组的账号只接受官方 Claude CLI 发起的流量；非 CLI 请求返回 403。
                </span>
              </span>
            )}
            onChange={setClaudeCodeOnly}
          />
        ) : null}

        {form.platform === 'openai' ? (
          <div className="space-y-3">
            <NativeSwitch
              isSelected={imageEnabled}
              label={<span className="text-sm text-text">{t('groups.image_generation')}</span>}
              onChange={setImageEnabled}
            />

            {imageEnabled ? (
              <div>
                <p className="mb-1.5 text-xs font-medium uppercaser text-text-secondary">
                  {t('groups.image_pricing')}
                </p>
                <p className="mb-2 text-[11px] text-text-tertiary">{t('groups.image_pricing_hint')}</p>
                <div className="grid grid-cols-3 gap-3">
                  {IMAGE_PRICE_FIELDS.map((field) => (
                    <HeroTextField key={field.key} fullWidth>
                      <Label>{field.label}</Label>
                      <Input
                        type="number"
                        min="0"
                        step="0.000001"
                        value={imagePrices[field.key]}
                        onChange={(e) =>
                          setImagePrices((current) => ({ ...current, [field.key]: e.target.value }))
                        }
                        placeholder={t('groups.image_price_fallback')}
                      />
                    </HeroTextField>
                  ))}
                </div>
              </div>
            ) : null}
          </div>
        ) : null}

        {form.subscription_type === 'subscription' ? (
          <div>
            <p className="mb-1.5 text-xs font-medium uppercaser text-text-secondary">
              {t('groups.quotas')}
            </p>
            <p className="mb-2 text-[11px] text-text-tertiary">{t('groups.quota_hint')}</p>
            <div className="grid grid-cols-3 gap-3">
              <HeroTextField fullWidth>
                <Label>{t('groups.quota_daily')}</Label>
                <Input
                  type="number"
                  min="0"
                  value={quotas.daily}
                  onChange={(e) => setQuotas({ ...quotas, daily: e.target.value })}
                />
              </HeroTextField>
              <HeroTextField fullWidth>
                <Label>{t('groups.quota_weekly')}</Label>
                <Input
                  type="number"
                  min="0"
                  value={quotas.weekly}
                  onChange={(e) => setQuotas({ ...quotas, weekly: e.target.value })}
                />
              </HeroTextField>
              <HeroTextField fullWidth>
                <Label>{t('groups.quota_monthly')}</Label>
                <Input
                  type="number"
                  min="0"
                  value={quotas.monthly}
                  onChange={(e) => setQuotas({ ...quotas, monthly: e.target.value })}
                />
              </HeroTextField>
            </div>
          </div>
        ) : null}
      </div>
            </Modal.Body>
            <Modal.Footer>
              <Button variant="secondary" onPress={onClose}>
                {t('common.cancel')}
              </Button>
              <Button variant="primary" isDisabled={loading} onPress={handleSubmit}>
                {loading ? <Spinner size="sm" /> : null}
                {isEdit ? t('common.save') : t('common.create')}
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}

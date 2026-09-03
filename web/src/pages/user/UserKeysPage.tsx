import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate, useSearch } from '@tanstack/react-router';
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apikeysApi } from '../../shared/api/apikeys';
import { membersApi } from '../../shared/api/members';
import { usePagination } from '../../shared/hooks/usePagination';
import { groupsApi } from '../../shared/api/groups';
import { modelsApi } from '../../shared/api/models';
import { settingsApi } from '../../shared/api/settings';
import { parseQuoteFx } from '../../shared/quoteMath';
import { useToast } from '../../shared/ui';
import { useAuth } from '../../app/providers/AuthProvider';
import { AlertDialog, Alert, Button, Dropdown, EmptyState, Input, ListBox, Modal, Select, Spinner, TextField as HeroTextField, useOverlayState } from '@heroui/react';
import { DialogTriggerShim } from '../../shared/components/DialogTriggerShim';
import {
  StatusChip,
} from '../../shared/ui';
import { useCrudMutation } from '../../shared/hooks/useCrudMutation';
import { queryKeys } from '../../shared/queryKeys';
import { DEFAULT_PAGE_SIZE, FETCH_ALL_PARAMS } from '../../shared/constants';
import { getTotalPages } from '../../shared/utils/pagination';
import { TablePaginationFooter } from '../../shared/components/TablePaginationFooter';
import { TableLoadingRow } from '../../shared/components/TableLoadingRow';
import { CommonTable } from '../../shared/components/CommonTable';
import { MetricChips } from '../../shared/components/MetricChips';
import { GROUP_CHIP_STYLE } from '../../shared/components/groupChipStyle';
import { localizedGroupText } from '../../shared/groupText';
import { useClipboard } from '../../shared/hooks/useClipboard';
import { useDebouncedValue } from '../../shared/hooks/useDebouncedValue';
import { useCopyFeedback } from '../../shared/hooks/useCopyFeedback';
import {
  AlertTriangle,
  Check,
  Copy,
  Plus,
  Pencil,
  Trash2,
  Key,
  Eye,
  Ban,
  CheckCircle,
  Terminal,
  Upload,
  MoreHorizontal,
  RefreshCw,
  Rocket,
  UsersRound,
  Search,
  X,
} from 'lucide-react';
import type { APIKeyResp, CreateAPIKeyReq, UpdateAPIKeyReq, UserGroupResp } from '../../shared/types';
import { EditKeyModal } from './userkeys/EditKeyModal';
import { CreateKeyModal } from './userkeys/CreateKeyModal';
import { UseKeyModal, useUseKeyModal } from './userkeys/UseKeyModal';
import { CcsImportModal, useCcsImportModal } from './userkeys/CcsImportModal';
import { OneClickModal, useOneClickModal } from './userkeys/OneClickModal';
import { type KeyForm, emptyForm } from './userkeys/types';
import { GroupQuoteSuffix } from './userkeys/GroupQuoteSuffix';

// 「未归属成员」筛选哨兵：与真实成员 ID 区分开，语义同账号页的 UNGROUPED_GROUP_FILTER。
const UNASSIGNED_MEMBER_FILTER = '__unassigned__';

export default function UserKeysPage() {
  const { t, i18n } = useTranslation();
  // 当前界面语言：分组名多语言覆盖按此精确匹配(en / zh-HK / ja),miss 回退基准文案
  const uiLang = i18n.language;
  const { toast } = useToast();
  const { user } = useAuth();
  const copy = useClipboard();
  const queryClient = useQueryClient();

  const navigate = useNavigate();
  // 团队成员页「管理密钥」经 ?member_id= 跳入，只是把成员筛选预置好；筛选栏常驻，随时可改。
  const search: { member_id?: number | string } = useSearch({ strict: false });
  const searchMemberID = search.member_id;

  const { page, setPage, pageSize, setPageSize } = usePagination(DEFAULT_PAGE_SIZE, 'user.keys');
  const [keyword, setKeyword] = useState('');
  const debouncedKeyword = useDebouncedValue(keyword.trim(), 250);
  const [groupFilter, setGroupFilter] = useState('');
  const [memberFilterValue, setMemberFilterValue] = useState(
    () => (searchMemberID != null && Number(searchMemberID) > 0 ? String(searchMemberID) : ''),
  );
  const [statusFilter, setStatusFilter] = useState('');

  // 同路由下 URL 变化不会重挂组件（已在本页时从团队页再点一次「管理密钥」），
  // 惰性初始化只跑一次，须显式跟随 URL 同步，否则筛选会停在上一个成员。
  // 用「渲染期修正状态」而非 useEffect：少一次带旧筛选的渲染，也不触发级联渲染。
  const [lastSearchMemberID, setLastSearchMemberID] = useState(searchMemberID);
  if (searchMemberID !== lastSearchMemberID) {
    setLastSearchMemberID(searchMemberID);
    setMemberFilterValue(searchMemberID != null && Number(searchMemberID) > 0 ? String(searchMemberID) : '');
    setPage(1);
  }

  const memberFilter = memberFilterValue && memberFilterValue !== UNASSIGNED_MEMBER_FILTER
    ? Number(memberFilterValue)
    : undefined;
  const hasActiveFilters = !!(keyword || groupFilter || memberFilterValue || statusFilter);
  // 清除筛选同时把 URL 上的 ?member_id= 一并去掉，否则同步 effect 会把成员筛选装回来。
  const clearFilters = () => {
    setKeyword('');
    setGroupFilter('');
    setMemberFilterValue('');
    setStatusFilter('');
    setPage(1);
    if (searchMemberID != null) navigate({ to: '/keys' });
  };
  const [modalOpen, setModalOpen] = useState(false);
  const [editingKey, setEditingKey] = useState<APIKeyResp | null>(null);
  const [form, setForm] = useState<KeyForm>(emptyForm);
  const [deleteTarget, setDeleteTarget] = useState<APIKeyResp | null>(null);

  // 显示新创建密钥的弹窗
  const [createdKey, setCreatedKey] = useState<string | null>(null);
  const [revealedKey, setRevealedKey] = useState<string | null>(null);
  const {
    copied: revealedKeyCopied,
    showCopied: showRevealedKeyCopied,
    resetCopied: resetRevealedKeyCopied,
  } = useCopyFeedback();

  // 密钥列表
  const { data, isLoading, refetch } = useQuery({
    queryKey: queryKeys.userKeys(page, pageSize, debouncedKeyword, groupFilter, memberFilterValue, statusFilter),
    queryFn: () => apikeysApi.list({
      page,
      page_size: pageSize,
      keyword: debouncedKeyword || undefined,
      group_id: groupFilter ? Number(groupFilter) : undefined,
      member_id: memberFilter,
      member_unassigned: memberFilterValue === UNASSIGNED_MEMBER_FILTER ? true : undefined,
      status: (statusFilter || undefined) as 'active' | 'disabled' | 'expired' | undefined,
    }),
    placeholderData: keepPreviousData,
  });

  // 团队成员（归属选择 / 名字展示）。/members 有企业主门禁，非企业主调必 403，
  // 所以这里按身份 enabled——否则每个普通用户每次进这页都会在服务端留下 403 + WARN。
  const isEnterpriseOwner = user?.role === 'admin' || !!user?.is_enterprise_owner;
  const { data: membersData } = useQuery({
    queryKey: queryKeys.membersForKeys(),
    queryFn: () => membersApi.list(FETCH_ALL_PARAMS),
    enabled: isEnterpriseOwner,
    staleTime: 60_000,
  });
  const memberList = useMemo(() => membersData?.list ?? [], [membersData?.list]);
  const memberOptions = useMemo(() => memberList.map((member) => ({ value: String(member.id), label: member.name })), [memberList]);
  const memberNameOf = (id: number | null | undefined) => (id ? memberList.find((member) => member.id === id)?.name : undefined);
  const hasMembers = memberList.length > 0;
  // 成员筛选项：全部 / 各成员 / 未归属
  const memberFilterOptions = useMemo(() => ([
    { id: '', label: t('common.all') },
    ...memberList.map((member) => ({ id: String(member.id), label: member.name })),
    { id: UNASSIGNED_MEMBER_FILTER, label: t('team.no_member') },
  ]), [memberList, t]);
  const statusFilterOptions = useMemo(() => ([
    { id: '', label: t('common.all') },
    { id: 'active', label: t('status.active') },
    { id: 'disabled', label: t('status.disabled') },
    { id: 'expired', label: t('status.expired') },
  ]), [t]);

  // 分组列表（用于选择）
  const { data: groupsData, isLoading: groupsLoading } = useQuery({
    queryKey: queryKeys.groupsForKeys(),
    queryFn: () => groupsApi.listAvailable(FETCH_ALL_PARAMS),
  });

  // 分组报价（折扣展示）：usd_multiplier ÷ fx = 对官方直付的折扣；获取失败时回退倍率文案
  const { data: myPricing } = useQuery({
    queryKey: queryKeys.myModelPricing(),
    queryFn: modelsApi.myPricing,
    staleTime: 60_000,
    retry: 1,
  });
  const { data: publicSettings } = useQuery({
    queryKey: queryKeys.siteSettings(),
    queryFn: settingsApi.getPublic,
    staleTime: 5 * 60_000,
    retry: false,
  });
  const pricingFx = useMemo(
    () => parseQuoteFx(publicSettings?.toc_landing_pricing),
    [publicSettings?.toc_landing_pricing],
  );
  const groupQuotes = useMemo(
    () => new Map((myPricing?.groups ?? []).map((quote) => [quote.id, quote])),
    [myPricing?.groups],
  );

  // 创建密钥
  const createMutation = useCrudMutation<{ key?: string }, CreateAPIKeyReq>({
    mutationFn: (data) => apikeysApi.create(data),
    successMessage: t('user_keys.create_success'),
    queryKey: queryKeys.userKeys(),
    onSuccess: (result) => {
      closeModal();
      // 显示完整密钥
      if (result.key) {
        setCreatedKey(result.key);
      }
    },
  });

  // 更新密钥
  const updateMutation = useCrudMutation<unknown, { id: number; data: UpdateAPIKeyReq }>({
    mutationFn: ({ id, data }) => apikeysApi.update(id, data),
    successMessage: t('user_keys.update_success'),
    queryKey: queryKeys.userKeys(),
    onSuccess: () => closeModal(),
  });

  // 删除密钥
  const deleteMutation = useCrudMutation<unknown, number>({
    mutationFn: (id) => apikeysApi.delete(id),
    successMessage: t('user_keys.delete_success'),
    queryKey: queryKeys.userKeys(),
    onSuccess: () => setDeleteTarget(null),
  });

  // 查看密钥
  const revealMutation = useMutation({
    mutationFn: (id: number) => apikeysApi.reveal(id),
    onSuccess: (resp) => {
      if (resp.key) {
        setRevealedKey(resp.key);
      }
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 禁用/启用密钥（动态成功消息，无法使用 useCrudMutation）
  const toggleStatusMutation = useMutation({
    mutationFn: ({ id, status }: { id: number; status: 'active' | 'disabled' }) =>
      apikeysApi.update(id, { status }),
    onSuccess: (_resp, variables) => {
      toast(
        'success',
        variables.status === 'active'
          ? t('user_keys.enable_success')
          : t('user_keys.disable_success'),
      );
      queryClient.invalidateQueries({ queryKey: queryKeys.userKeys() });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  function openCreate() {
    if (!hasAvailableGroups) {
      toast('error', t('user_keys.no_groups_available'));
      return;
    }
    setEditingKey(null);
    setForm({ ...emptyForm, member_id: memberFilter ? String(memberFilter) : '' });
    setModalOpen(true);
  }

  function openEdit(key: APIKeyResp) {
    setEditingKey(key);
    setForm({
      name: key.name,
      group_id: key.group_id == null ? '' : String(key.group_id),
      quota_usd: key.quota_usd ? String(key.quota_usd) : '',
      sell_rate: key.sell_rate ? String(key.sell_rate) : '',
      max_concurrency: key.max_concurrency ? String(key.max_concurrency) : '',
      expires_at: key.expires_at ? key.expires_at.slice(0, 10) : '',
      member_id: key.member_id ? String(key.member_id) : '',
    });
    setModalOpen(true);
  }

  function closeModal() {
    setModalOpen(false);
    setEditingKey(null);
    setForm(emptyForm);
  }

  function handleSubmit() {
    if (!form.name) {
      toast('error', t('user_keys.name_placeholder'));
      return;
    }
    if (!editingKey && !form.group_id) {
      toast('error', t('user_keys.select_group'));
      return;
    }

    // 后端要求 RFC3339 格式；空字符串表示显式清除过期时间
    const expiresAt = form.expires_at ? `${form.expires_at}T23:59:59Z` : '';

    if (editingKey) {
      const payload: UpdateAPIKeyReq = {
        name: form.name,
        group_id: form.group_id ? Number(form.group_id) : undefined,
        // 空字符串显式改为 0 = 无限配额；省略字段只表示不修改旧配额
        quota_usd: form.quota_usd.trim() ? Number(form.quota_usd) : 0,
        sell_rate: form.sell_rate ? Number(form.sell_rate) : 0,
        // 空字符串显式改为 0 = 关闭并发限制；后端看到 0 会清除旧值
        max_concurrency: form.max_concurrency ? Number(form.max_concurrency) : 0,
        expires_at: expiresAt,
        // 0 = 解除成员归属；只有主账号有成员时表单才会出现这一项
        ...(memberOptions.length > 0 ? { member_id: form.member_id ? Number(form.member_id) : 0 } : {}),
      };
      updateMutation.mutate({ id: editingKey.id, data: payload });
    } else {
      const payload: CreateAPIKeyReq = {
        name: form.name,
        group_id: Number(form.group_id),
        quota_usd: form.quota_usd ? Number(form.quota_usd) : undefined,
        sell_rate: form.sell_rate ? Number(form.sell_rate) : undefined,
        max_concurrency: form.max_concurrency ? Number(form.max_concurrency) : undefined,
        expires_at: expiresAt,
        member_id: form.member_id ? Number(form.member_id) : undefined,
      };
      createMutation.mutate(payload);
    }
  }

  // 查找分组
  const groupList = useMemo(() => groupsData?.list ?? [], [groupsData?.list]);
  const groupMap = useMemo(() => new Map<number, UserGroupResp>(groupList.map((g) => [g.id, g])), [groupList]);

  const hasAvailableGroups = groupList.length > 0;
  // 分组筛选项取「我可用的分组」——与新建密钥同源；已下架分组上的存量密钥仍会出现在不筛选的列表里。
  const groupFilterOptions = useMemo(() => ([
    { id: '', label: t('common.all') },
    ...groupList.map((g) => ({ id: String(g.id), label: localizedGroupText(g.name, g.name_i18n, uiLang) })),
  ]), [groupList, t, uiLang]);

  // 报价客户模式：只展示报价单换算出的价格，不渲染任何牌价对比/折扣锚点。
  const quoteMode = myPricing?.pricing_mode === 'quote';

  // 分组选项：右侧按统一口径展示报价与折扣。
  // （倍率语义 = 每消耗官方 $1 扣多少 ¥ 余额；折 = 倍率 ÷ fx，全站同一定义）。
  // 价格数据一律来自 /models/pricing/me 的分组摘要（权威口径，/groups 瘦投影不再带倍率），
  // 摘要请求失败时不展示价格后缀（宁缺勿错）。固定图价哨兵分组（标准倍率 0）不展示
  // token 倍率——后端摘要的 effective_rate 对这类分组是 billing 的 1.0 兜底值，不是真实报价。
  const formatGroupZhe = (zhe: number) => {
    const value = zhe * 10;
    return value < 1 ? value.toFixed(2) : value.toFixed(1);
  };
  const groupOptions = useMemo(() => groupList.map((g) => {
    const quote = groupQuotes.get(g.id);
    const effectiveRate = quote?.effective_rate ?? 0;
    const standardRate = quote?.group_rate ?? 0;
    const usdMult = quote != null && effectiveRate > 0
      && typeof quote.usd_multiplier === 'number' && Number.isFinite(quote.usd_multiplier) && quote.usd_multiplier > 0
      ? quote.usd_multiplier
      : null;
    // 报价客户的摘要里 group_rate 已被后端改写为 effective_rate，天然无「标准 vs 专属」差值
    const hasOverride = standardRate > 0 && effectiveRate > 0 && effectiveRate !== standardRate;
    const standardMult = usdMult != null && hasOverride
      ? usdMult * (standardRate / effectiveRate)
      : null;
    const rateTooltip = t('user_keys.rate_tooltip', { rate: effectiveRate });
    let suffix;
    if (quoteMode) {
      // 报价客户：只显示「¥X.XX / $1」；无 token 报价（固定图价组）则不加后缀
      suffix = usdMult != null ? (
        <GroupQuoteSuffix
          data={{
            multiplier: usdMult,
            discountZhe: '',
            discountPercent: 0,
            hasOfficialDiscount: false,
            quoteOnly: true,
          }}
        />
      ) : undefined;
    } else if (usdMult != null && usdMult / pricingFx < 1 && standardRate > 0) {
      suffix = (
        <GroupQuoteSuffix
          data={{
            multiplier: usdMult,
            discountZhe: formatGroupZhe(usdMult / pricingFx),
            discountPercent: Math.round((1 - usdMult / pricingFx) * 100),
            standardMultiplier: standardMult ?? undefined,
            hasOfficialDiscount: true,
          }}
          title={rateTooltip}
        />
      );
    } else if (effectiveRate > 0 && standardRate > 0) {
      suffix = (
        <GroupQuoteSuffix
          data={{
            multiplier: effectiveRate,
            discountZhe: '',
            discountPercent: 0,
            standardMultiplier: hasOverride ? standardRate : undefined,
            hasOfficialDiscount: false,
          }}
          title={rateTooltip}
        />
      );
    }
    return {
      value: String(g.id),
      label: localizedGroupText(g.name, g.name_i18n, uiLang),
      description: localizedGroupText(g.note ?? '', g.note_i18n, uiLang).trim() || undefined,
      suffix,
    };
  }), [groupList, groupQuotes, pricingFx, quoteMode, t, uiLang]);

  // 使用配置弹窗
  const {
    useKeyTarget,
    useKeyValue,
    useKeyTab,
    setUseKeyTab,
    useKeyShell,
    setUseKeyShell,
    useKeyPlatform,
    showClientTabs,
    openUseKeyModal,
    closeUseKeyModal,
  } = useUseKeyModal(groupMap);

  // CCS 导入弹窗
  const {
    ccsTarget,
    ccsKeyValue,
    ccsPlatform,
    openCcsModal,
    closeCcsModal,
  } = useCcsImportModal(groupMap);

  // 一键接入弹窗
  const {
    oneClickTarget,
    oneClickIssue,
    oneClickGenerating,
    oneClickPlatform,
    openOneClickModal,
    generateOneClick,
    closeOneClickModal,
  } = useOneClickModal(groupMap);

  const saving = createMutation.isPending || updateMutation.isPending;
  const rows = data?.list ?? [];
  const total = data?.total ?? 0;
  const totalPages = getTotalPages(total, pageSize);
  const closeRevealedKeyModal = () => {
    resetRevealedKeyCopied();
    setRevealedKey(null);
  };
  const handleCopyRevealedKey = async () => {
    if (await copy(revealedKey || '')) {
      showRevealedKeyCopied();
    }
  };
  const revealedKeyModalState = useOverlayState({
    isOpen: !!revealedKey,
    onOpenChange: (open) => {
      if (!open) closeRevealedKeyModal();
    },
  });

  return (
    <div className="p-6">
      <div className="mb-5 flex min-h-12 flex-col gap-3 xl:flex-row xl:items-start">
        <div className="min-w-0 flex-1">
          <div className="flex min-h-12 flex-col flex-wrap items-stretch gap-3 sm:flex-row sm:items-center">
            <div className="w-full sm:w-52">
              <HeroTextField fullWidth aria-label={t('user_keys.search_placeholder')}>
                <div className="relative">
                  <Search className="pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-text-tertiary" />
                  <Input
                    className="pl-9"
                    value={keyword}
                    onChange={(e) => { setKeyword(e.target.value); setPage(1); }}
                    placeholder={t('user_keys.search_placeholder')}
                  />
                </div>
              </HeroTextField>
            </div>
            <div className="w-full sm:w-44">
              <Select
                aria-label={t('user_keys.group')}
                fullWidth
                selectedKey={groupFilter}
                onSelectionChange={(key) => { setGroupFilter(key == null ? '' : String(key)); setPage(1); }}
              >
                <Select.Trigger>
                  <Select.Value>
                    {groupFilter
                      ? groupFilterOptions.find((item) => item.id === groupFilter)?.label ?? groupFilter
                      : <span className="text-text-tertiary">{t('user_keys.group')}</span>}
                  </Select.Value>
                  <Select.Indicator />
                </Select.Trigger>
                <Select.Popover className="w-[var(--trigger-width)]">
                  <ListBox items={groupFilterOptions}>
                    {(item) => (
                      <ListBox.Item id={item.id} textValue={item.label}>
                        <span className="block truncate">{item.label}</span>
                      </ListBox.Item>
                    )}
                  </ListBox>
                </Select.Popover>
              </Select>
            </div>
            {hasMembers ? (
              <div className="w-full sm:w-44">
                <Select
                  aria-label={t('team.filter_member')}
                  fullWidth
                  selectedKey={memberFilterValue}
                  onSelectionChange={(key) => { setMemberFilterValue(key == null ? '' : String(key)); setPage(1); }}
                >
                    <Select.Trigger>
                    <Select.Value>
                      {memberFilterValue
                        ? memberFilterOptions.find((item) => item.id === memberFilterValue)?.label ?? memberFilterValue
                        : <span className="text-text-tertiary">{t('team.filter_member')}</span>}
                    </Select.Value>
                    <Select.Indicator />
                  </Select.Trigger>
                  <Select.Popover className="w-[var(--trigger-width)]">
                    <ListBox items={memberFilterOptions}>
                      {(item) => (
                        <ListBox.Item id={item.id} textValue={item.label}>
                          <span className="block truncate">{item.label}</span>
                        </ListBox.Item>
                      )}
                    </ListBox>
                  </Select.Popover>
                </Select>
              </div>
            ) : null}
            <div className="w-full sm:w-36">
              <Select
                aria-label={t('common.status')}
                fullWidth
                selectedKey={statusFilter}
                onSelectionChange={(key) => { setStatusFilter(key == null ? '' : String(key)); setPage(1); }}
              >
                <Select.Trigger>
                  <Select.Value>
                    {statusFilter
                      ? statusFilterOptions.find((item) => item.id === statusFilter)?.label ?? statusFilter
                      : <span className="text-text-tertiary">{t('common.status')}</span>}
                  </Select.Value>
                  <Select.Indicator />
                </Select.Trigger>
                <Select.Popover className="w-[var(--trigger-width)]">
                  <ListBox items={statusFilterOptions}>
                    {(item) => (
                      <ListBox.Item id={item.id} textValue={item.label}>
                        {item.label}
                      </ListBox.Item>
                    )}
                  </ListBox>
                </Select.Popover>
              </Select>
            </div>
            {hasActiveFilters ? (
              <Button size="sm" variant="ghost" onPress={clearFilters}>
                <X className="h-3.5 w-3.5" />
                {t('user_keys.clear_filter')}
              </Button>
            ) : null}
          </div>
        </div>

        <div className="flex shrink-0 items-center gap-2 xl:ml-auto">
          <Button
            isIconOnly
            aria-label={t('common.refresh', 'Refresh')}
            size="md"
            variant="ghost"
            onPress={() => refetch()}
          >
            <RefreshCw className="w-4 h-4" />
          </Button>
          <Button
            data-onboarding-target="keys-create"
            data-onboarding-available={groupsLoading ? 'loading' : String(hasAvailableGroups)}
            isDisabled={!hasAvailableGroups}
            variant="primary"
            onPress={openCreate}
          >
            <Plus className="w-4 h-4" />
            {hasAvailableGroups ? t('user_keys.create') : t('user_keys.create_disabled_no_groups')}
          </Button>
        </div>
      </div>

      <CommonTable
        ariaLabel={t('user_keys.title', 'API keys')}
        className="ag-api-keys-table"
        footer={(
          <TablePaginationFooter
            page={page}
            pageSize={pageSize}
            setPage={setPage}
            setPageSize={setPageSize}
            total={total}
            totalPages={totalPages}
          />
        )}
        minWidth={1040}
      >
        <CommonTable.Header>
          <CommonTable.Column id="name">{t('common.name')}</CommonTable.Column>
          <CommonTable.Column id="key_prefix">{t('user_keys.title')}</CommonTable.Column>
          <CommonTable.Column id="group_id">{t('user_keys.group')}</CommonTable.Column>
          <CommonTable.Column id="status">{t('common.status')}</CommonTable.Column>
          <CommonTable.Column id="quota" style={{ width: '17.5rem' }}>{t('user_keys.quota_label')}</CommonTable.Column>
          <CommonTable.Column id="markup" style={{ width: '10.75rem' }}>{t('user_keys.markup_title', 'Sales/Cost')}</CommonTable.Column>
          <CommonTable.Column id="usage" style={{ width: '10.75rem' }}>{t('api_keys.usage')}</CommonTable.Column>
          <CommonTable.Column id="expires_at">{t('user_keys.expires_at')}</CommonTable.Column>
          <CommonTable.Column id="actions" style={{ width: 132 }}>
            {t('common.actions')}
          </CommonTable.Column>
        </CommonTable.Header>
        <CommonTable.Body>
          {isLoading ? (
            <TableLoadingRow colSpan={9} />
          ) : rows.length === 0 ? (
            <CommonTable.Row id="empty">
              <CommonTable.Cell colSpan={9}>
                <EmptyState>
                  <div className="text-sm text-default-500">{t('common.no_data')}</div>
                </EmptyState>
              </CommonTable.Cell>
            </CommonTable.Row>
          ) : (
            rows.map((row) => {
              const group = row.group_id == null ? null : groupMap.get(row.group_id);
              const isGroupUnbound = row.group_id == null;
              const groupName = isGroupUnbound
                ? t('user_keys.group_unbound')
                : group
                  ? localizedGroupText(group.name, group.name_i18n, uiLang)
                  : `#${row.group_id}`;
              const hasSellRate = row.sell_rate != null && row.sell_rate > 0;
              // 倍率展示以 /models/pricing/me 分组摘要为准（/groups 瘦投影不再带倍率）。
              // 报价客户摘要里 group_rate=effective_rate，天然不出现「专属」差值标记。
              const rowQuote = row.group_id == null ? undefined : groupQuotes.get(row.group_id);
              const rowEffectiveRate = rowQuote?.effective_rate ?? 0;
              const rowStandardRate = rowQuote?.group_rate ?? 0;
              const hasOverride = !quoteMode
                && rowEffectiveRate > 0
                && rowStandardRate > 0
                && rowEffectiveRate !== rowStandardRate;
              const rowUsdMult = rowQuote != null
                && typeof rowQuote.usd_multiplier === 'number' && rowQuote.usd_multiplier > 0
                ? rowQuote.usd_multiplier
                : null;
              const profit = (row.used_quota || 0) - (row.used_quota_actual || 0);
              const isExpired = row.expires_at && new Date(row.expires_at) < new Date();
              const displayStatus = isExpired ? 'expired' : row.status;

              return (
                <CommonTable.Row id={String(row.id)} key={row.id}>
                  <CommonTable.Cell>
                    <div className="min-w-0">
                      <span className="font-medium text-text">{row.name}</span>
                      {row.member_id ? (
                        <div className="mt-0.5 flex items-center gap-1 text-xs text-text-tertiary" title={row.member_name || memberNameOf(row.member_id)}>
                          <UsersRound className="h-3 w-3 shrink-0" />
                          <span className="truncate">{row.member_name || memberNameOf(row.member_id) || t('team.member_deleted')}</span>
                        </div>
                      ) : null}
                    </div>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <button
                      type="button"
                      className="inline-flex items-center gap-1.5 text-xs px-2 py-0.5 rounded-sm border border-glass-border bg-surface text-text-secondary font-mono cursor-pointer transition-colors hover:border-accent hover:text-accent"
                      title={t('common.copy')}
                      onClick={async () => {
                        const resp = await apikeysApi.reveal(row.id);
                        if (resp.key) await copy(resp.key);
                      }}
                    >
                      <Key className="w-3 h-3 text-text-tertiary" />
                      {row.key_prefix}...
                      <Copy className="w-3 h-3 text-text-tertiary" />
                    </button>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <div className="space-y-0.5 text-center">
                      <div className="flex justify-center">
                        <span
                          className="inline-flex h-6 min-w-0 max-w-full items-center justify-center gap-1 rounded-[var(--radius)] px-1.5 text-[13px] font-medium leading-none text-text-secondary"
                          style={GROUP_CHIP_STYLE}
                          title={groupName}
                        >
                          {isGroupUnbound ? <AlertTriangle className="h-3 w-3 shrink-0 text-warning" /> : null}
                          <span className="min-w-0 truncate">{groupName}</span>
                        </span>
                      </div>
                      {((quoteMode ? rowUsdMult != null : rowEffectiveRate > 0 && rowStandardRate > 0) || hasSellRate) && (
                        <MetricChips
                          className="ag-metric-chips--stack ag-metric-chips--markup"
                          items={[
                            ...(quoteMode && rowUsdMult != null ? [{
                              color: 'default' as const,
                              label: t('user_keys.quote_price_short', 'Quote'),
                              value: t('user_keys.group_quote_price', { m: rowUsdMult.toFixed(2) }),
                            }] : []),
                            ...(!quoteMode && rowEffectiveRate > 0 && rowStandardRate > 0 ? [{
                              color: 'default' as const,
                              label: t('user_keys.group_rate_short', 'Group Rate'),
                              value: hasOverride
                                ? `${rowEffectiveRate.toFixed(2)} ${t('user_keys.user_override_tag', 'override')}`
                                : rowEffectiveRate.toFixed(2),
                            }] : []),
                            ...(hasSellRate ? [{
                              color: 'default' as const,
                              label: t('user_keys.sell_rate_short', 'Sell Rate'),
                              value: row.sell_rate.toFixed(2),
                            }] : []),
                          ]}
                        />
                      )}
                    </div>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <StatusChip status={displayStatus} />
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <MetricChips
                      className="ag-metric-chips--quota"
                      items={[
                        {
                          amount: row.used_quota,
                          color: 'warning',
                          highlightDollar: true,
                          label: t('user_keys.quota_used_short', 'Used'),
                        },
                        {
                          amount: row.quota_usd > 0 ? row.quota_usd : undefined,
                          color: 'success',
                          label: t('user_keys.quota_total_short', 'Total'),
                          value: '∞',
                        },
                      ]}
                    />
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <MetricChips
                      className="ag-metric-chips--stack ag-metric-chips--markup"
                      items={[
                        {
                          color: 'default',
                          label: t('user_keys.sell_rate_short', 'Sell Rate'),
                          value: hasSellRate ? row.sell_rate.toFixed(2) : '—',
                        },
                        {
                          amount: row.used_quota_actual || 0,
                          color: 'default',
                          dollarTone: 'warning',
                          label: t('user_keys.cost_actual', 'Cost'),
                        },
                        {
                          amount: profit,
                          color: 'default',
                          dollarTone: 'success',
                          label: t('user_keys.profit', 'Profit'),
                        },
                      ]}
                    />
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <MetricChips
                      className="ag-metric-chips--stack ag-metric-chips--usage"
                      items={[
                        {
                          amount: row.today_cost,
                          color: 'warning',
                          dollarTone: 'warning',
                          label: t('api_keys.today', 'Today'),
                          mutedWhenZero: true,
                        },
                        {
                          amount: row.thirty_day_cost,
                          color: 'warning',
                          dollarTone: 'warning',
                          label: t('api_keys.thirty_days', '30 Days'),
                          mutedWhenZero: true,
                        },
                      ]}
                    />
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    {row.expires_at
                      ? new Date(row.expires_at).toLocaleDateString('zh-CN')
                      : t('user_keys.never_expire')}
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <div className="ag-table-row-actions flex items-center justify-center gap-0.5">
                      <Button
                        isIconOnly
                        size="sm"
                        variant="secondary"
                        aria-label={t('api_keys.reveal')}
                        onPress={() => revealMutation.mutate(row.id)}
                      >
                        <Eye className="w-3.5 h-3.5" />
                      </Button>
                      <Button
                        isIconOnly
                        size="sm"
                        variant="secondary"
                        aria-label={t('user_keys.one_click')}
                        onPress={() => openOneClickModal(row)}
                      >
                        <Rocket className="w-3.5 h-3.5" style={{ color: 'var(--ag-primary)' }} />
                      </Button>
                      <Dropdown>
                        <Dropdown.Trigger
                          data-onboarding-target="key-actions"
                          aria-label={t('common.more')}
                          className="ag-table-row-more-trigger button button--icon-only button--sm button--secondary"
                        >
                          <MoreHorizontal className="w-3.5 h-3.5" />
                        </Dropdown.Trigger>
                        <Dropdown.Popover placement="bottom end">
                          <Dropdown.Menu
                            aria-label={t('common.actions')}
                            onAction={(key) => {
                              switch (String(key)) {
                                case 'use_key':
                                  openUseKeyModal(row);
                                  break;
                                case 'import_ccs':
                                  openCcsModal(row);
                                  break;
                                case 'toggle':
                                  toggleStatusMutation.mutate({
                                    id: row.id,
                                    status: row.status === 'active' ? 'disabled' : 'active',
                                  });
                                  break;
                                case 'edit':
                                  openEdit(row);
                                  break;
                                case 'delete':
                                  setDeleteTarget(row);
                                  break;
                              }
                            }}
                          >
                            <Dropdown.Item id="use_key" textValue={t('user_keys.use_key')}>
                              <span className="flex items-center gap-2">
                                <Terminal className="w-3.5 h-3.5" style={{ color: 'var(--ag-text-tertiary)' }} />
                                {t('user_keys.use_key')}
                              </span>
                            </Dropdown.Item>
                            <Dropdown.Item id="import_ccs" textValue={t('user_keys.import_ccs')}>
                              <span data-onboarding-target="ccs-import" className="flex items-center gap-2">
                                <Upload className="w-3.5 h-3.5" style={{ color: 'var(--ag-text-tertiary)' }} />
                                {t('user_keys.import_ccs')}
                              </span>
                            </Dropdown.Item>
                            <Dropdown.Item
                              id="toggle"
                              textValue={row.status === 'active' ? t('user_keys.disable') : t('user_keys.enable')}
                            >
                              <span className="flex items-center gap-2">
                                {row.status === 'active'
                                  ? <Ban className="w-3.5 h-3.5" />
                                  : <CheckCircle className="w-3.5 h-3.5" />}
                                {row.status === 'active' ? t('user_keys.disable') : t('user_keys.enable')}
                              </span>
                            </Dropdown.Item>
                            <Dropdown.Item id="edit" textValue={t('common.edit')}>
                              <span className="flex items-center gap-2">
                                <Pencil className="w-3.5 h-3.5" />
                                {t('common.edit')}
                              </span>
                            </Dropdown.Item>
                            <Dropdown.Item id="delete" className="text-danger" textValue={t('common.delete')}>
                              <span className="flex items-center gap-2">
                                <Trash2 className="w-3.5 h-3.5" />
                                {t('common.delete')}
                              </span>
                            </Dropdown.Item>
                          </Dropdown.Menu>
                        </Dropdown.Popover>
                      </Dropdown>
                    </div>
                  </CommonTable.Cell>
                </CommonTable.Row>
              );
            })
          )}
        </CommonTable.Body>
      </CommonTable>

      {/* 创建/编辑弹窗 */}
      <EditKeyModal
        open={modalOpen}
        isEdit={!!editingKey}
        form={form}
        setForm={setForm}
        groupOptions={groupOptions}
        memberOptions={memberOptions}
        onClose={closeModal}
        onSubmit={handleSubmit}
        loading={saving}
      />

      {/* 新建密钥后显示完整密钥 */}
      <CreateKeyModal
        open={!!createdKey}
        createdKey={createdKey}
        onClose={() => setCreatedKey(null)}
      />

      {/* 查看密钥弹窗 */}
      <Modal state={revealedKeyModalState}>
        <DialogTriggerShim />
        <Modal.Backdrop>
          <Modal.Container placement="center" scroll="inside" size="md">
            <Modal.Dialog className="ag-elevation-modal">
              <Modal.Header>
                <Modal.Heading>{t('api_keys.reveal')}</Modal.Heading>
                <Modal.CloseTrigger />
              </Modal.Header>
              <Modal.Body>
                <div className="space-y-4">
                  <Alert status="warning">
                    <Alert.Indicator>
                      <AlertTriangle className="h-4 w-4" />
                    </Alert.Indicator>
                    <Alert.Content>
                      <Alert.Description>{t('api_keys.key_reveal_warning')}</Alert.Description>
                    </Alert.Content>
                  </Alert>
                  <div className="flex items-center gap-2">
                    <code className="flex-1 break-all rounded-md border border-glass-border bg-surface px-3 py-2 font-mono text-sm text-text">
                      {revealedKey || ''}
                    </code>
                    <Button size="sm" variant="secondary" onPress={handleCopyRevealedKey}>
                      {revealedKeyCopied
                        ? <Check className="h-3.5 w-3.5 text-success" />
                        : <Copy className="h-3.5 w-3.5" />}
                      <span className={revealedKeyCopied ? 'text-success' : undefined}>
                        {t('common.copy')}
                      </span>
                    </Button>
                  </div>
                </div>
              </Modal.Body>
              <Modal.Footer>
                <Button variant="primary" onPress={closeRevealedKeyModal}>
                  {t('common.close')}
                </Button>
              </Modal.Footer>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>

      {/* 使用 API 密钥配置弹窗 */}
      <UseKeyModal
        useKeyTarget={useKeyTarget}
        useKeyValue={useKeyValue}
        useKeyPlatform={useKeyPlatform}
        showClientTabs={showClientTabs}
        useKeyTab={useKeyTab}
        setUseKeyTab={setUseKeyTab}
        useKeyShell={useKeyShell}
        setUseKeyShell={setUseKeyShell}
        onClose={closeUseKeyModal}
      />

      {/* CCS 导入弹窗 */}
      <CcsImportModal
        open={!!ccsTarget}
        ccsKeyValue={ccsKeyValue}
        ccsPlatform={ccsPlatform}
        onClose={closeCcsModal}
      />

      {/* 一键接入弹窗 */}
      <OneClickModal
        target={oneClickTarget}
        issue={oneClickIssue}
        generating={oneClickGenerating}
        platform={oneClickPlatform}
        onGenerate={generateOneClick}
        onClose={closeOneClickModal}
      />

      {/* 删除确认 */}
      <AlertDialog
        isOpen={!!deleteTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null);
        }}
      >
        <DialogTriggerShim />
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="sm">
            <AlertDialog.Dialog className="ag-elevation-modal">
              <AlertDialog.Header>
                <AlertDialog.Icon status="danger" />
                <AlertDialog.Heading>{t('user_keys.delete_key')}</AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>{t('user_keys.delete_confirm', { name: deleteTarget?.name })}</AlertDialog.Body>
              <AlertDialog.Footer>
                <Button variant="secondary" onPress={() => setDeleteTarget(null)}>
                  {t('common.cancel')}
                </Button>
                <Button
                  aria-busy={deleteMutation.isPending}
                  isDisabled={deleteMutation.isPending}
                  variant="danger"
                  onPress={() => deleteTarget && deleteMutation.mutate(deleteTarget.id)}
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

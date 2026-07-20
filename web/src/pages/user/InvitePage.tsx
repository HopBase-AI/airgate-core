import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { Button, Chip, EmptyState } from '@heroui/react';
import { BadgeCheck, Copy, FileText, Gift, Percent, Users, Wallet } from 'lucide-react';
import { referralApi } from '../../shared/api/referral';
import { blogApi } from '../../shared/api/blog';
import { queryKeys } from '../../shared/queryKeys';
import { usePagination } from '../../shared/hooks/usePagination';
import { DEFAULT_PAGE_SIZE } from '../../shared/constants';
import { getTotalPages } from '../../shared/utils/pagination';
import { CommonTable } from '../../shared/components/CommonTable';
import { TableLoadingRow } from '../../shared/components/TableLoadingRow';
import { TablePaginationFooter } from '../../shared/components/TablePaginationFooter';
import { useToast } from '../../shared/ui';
import { buildBlogShareURL, publicBlogBase } from '../../shared/blogShare';
import type { BlogLanguage } from '../../shared/types';

function formatTime(date: string): string {
  return new Date(date).toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
}

function StatCard({ icon, label, value }: { icon: React.ReactNode; label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-center gap-3 rounded-[var(--radius)] border border-border bg-surface px-4 py-3.5">
      <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-[var(--field-radius)] bg-accent/10 text-accent">
        {icon}
      </span>
      <div className="min-w-0">
        <div className="text-xs" style={{ color: 'var(--ag-text-tertiary)' }}>{label}</div>
        <div className="truncate text-lg font-semibold tabular-nums" style={{ color: 'var(--ag-text)' }}>{value}</div>
      </div>
    </div>
  );
}

export default function InvitePage() {
  const { t } = useTranslation();
  const { toast } = useToast();
  const { page, setPage, pageSize, setPageSize } = usePagination(DEFAULT_PAGE_SIZE, 'user.referral');

  const { data: me, isLoading: meLoading } = useQuery({
    queryKey: queryKeys.referralMe(),
    queryFn: () => referralApi.me(),
  });

  const { data: commissions, isLoading: listLoading } = useQuery({
    queryKey: queryKeys.referralMyCommissions(page, pageSize),
    queryFn: () => referralApi.myCommissions({ page, page_size: pageSize }),
    meta: { globalLoading: false },
    placeholderData: keepPreviousData,
  });

  // 博客分享基址:优先后台配的邀请链接前缀(通常指落地页域),否则把
  // console./api. 控制台域名归一为公开落地页根域。
  // (博客经反代挂在落地页域 /blog,与文章编辑页 copyShare 口径一致)。
  const blogBase = useMemo(() => {
    const configured = me?.link_base_url?.trim();
    if (configured) return configured.replace(/\/+$/, '');
    return publicBlogBase(window.location.origin);
  }, [me?.link_base_url]);

  // 邀请链接：后台可配前缀（如指向落地页），未配置则用当前控制台域名。
  const inviteLink = useMemo(() => {
    if (!me?.invite_code) return '';
    const base = me.link_base_url?.trim() || window.location.origin;
    return `${base.replace(/\/+$/, '')}/?inv=${me.invite_code}`;
  }, [me?.invite_code, me?.link_base_url]);

  // 官方推广官身份:博客能力(编辑 + 「分享文章」复制博客链接)为官方推广官专属,
  // 普通用户不显示分享文章入口(裸邀请链接仍可用)。
  const isOfficial = me?.tier === 'official';

  // 「分享文章」软入口:列出已发布文章,拼带邀请码与文章语言的链接供分发。
  // 仅官方推广官请求与展示(后端 /blog/articles 亦按 official 校验,前后端一致)。
  const { data: articles, isLoading: articlesLoading } = useQuery({
    queryKey: queryKeys.blogPublishedArticles(),
    queryFn: () => blogApi.publishedArticles(),
    meta: { globalLoading: false },
    enabled: isOfficial && !!me?.invite_code,
  });

  const shareArticleUrl = (slug: string, lang: BlogLanguage) =>
    buildBlogShareURL(blogBase, slug, lang, me?.invite_code);

  const copyText = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      toast('success', t('referral.link_copied'));
    } catch {
      toast('error', t('referral.copy_failed'));
    }
  };

  const copyLink = () => copyText(inviteLink);

  const rows = commissions?.list ?? [];
  const total = commissions?.total ?? 0;
  const totalPages = getTotalPages(total, pageSize);

  return (
    <div className="space-y-5">
      {/* 官方推广官身份条:普通用户不显示,把「官方团队推广」与「随手拿码的普通用户」在视觉上分开 */}
      {isOfficial ? (
        <div
          className="flex items-center gap-3.5 rounded-[var(--radius)] border px-4 py-3.5"
          style={{
            borderColor: 'rgba(202,138,4,0.38)',
            background: 'linear-gradient(100deg, rgba(202,138,4,0.14), rgba(202,138,4,0.04) 60%, transparent)',
          }}
        >
          <span
            className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full"
            style={{ background: 'rgba(202,138,4,0.16)', color: '#b8860b' }}
          >
            <BadgeCheck className="h-5 w-5" />
          </span>
          <div className="min-w-0">
            <div className="flex items-center gap-2 text-sm font-semibold" style={{ color: 'var(--ag-text)' }}>
              {t('referral.official_promoter')}
              <span
                className="rounded-full px-2 py-0.5 text-[11px] font-medium"
                style={{ background: 'rgba(202,138,4,0.16)', color: '#b8860b' }}
              >
                {t('referral.official_badge')}
              </span>
            </div>
            <div className="truncate text-xs" style={{ color: 'var(--ag-text-secondary)' }}>
              {me?.display_name
                ? t('referral.official_promoter_signed', { name: me.display_name })
                : t('referral.official_promoter_hint')}
            </div>
          </div>
        </div>
      ) : null}

      {/* 邀请链接卡片:官方推广官加一圈品牌金描边强化身份 */}
      <div
        className={`rounded-[var(--radius)] border bg-surface p-5 ${isOfficial ? '' : 'border-border'}`}
        style={isOfficial ? { borderColor: 'rgba(202,138,4,0.32)' } : undefined}
      >
        <div className="mb-1 text-base font-semibold" style={{ color: 'var(--ag-text)' }}>
          {t('referral.title')}
        </div>
        <p className="mb-4 text-sm" style={{ color: 'var(--ag-text-secondary)' }}>
          {t('referral.subtitle')}
        </p>
        {me && !me.enabled && (
          <div className="mb-4 rounded-[var(--field-radius)] border border-warning/30 bg-warning/10 px-3 py-2 text-sm text-warning">
            {t('referral.disabled_hint')}
          </div>
        )}
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
          <code
            className="min-w-0 flex-1 truncate rounded-[var(--field-radius)] border border-border bg-background px-3 py-2 font-mono text-sm"
            style={{ color: 'var(--ag-text)' }}
            title={inviteLink}
          >
            {meLoading ? '…' : inviteLink || '-'}
          </code>
          <Button variant="primary" className="shrink-0 gap-1.5" onPress={copyLink} isDisabled={!inviteLink}>
            <Copy className="h-4 w-4" />
            {t('referral.copy_link')}
          </Button>
        </div>
        {me?.invite_code ? (
          <div className="mt-2 text-xs" style={{ color: 'var(--ag-text-tertiary)' }}>
            {t('referral.my_code')}: <span className="font-mono">{me.invite_code}</span>
          </div>
        ) : null}
      </div>

      {/* 分享文章(软化推荐):官方推广官专属,分享一篇文章代替裸邀请链接,邀请码内置进链接 */}
      {isOfficial && me?.invite_code ? (
        <div className="rounded-[var(--radius)] border border-border bg-surface p-5">
          <div className="mb-1 text-base font-semibold" style={{ color: 'var(--ag-text)' }}>
            {t('referral.share_articles')}
          </div>
          <p className="mb-4 text-sm" style={{ color: 'var(--ag-text-secondary)' }}>
            {t('referral.share_articles_hint')}
          </p>
          {articlesLoading ? (
            <div className="text-sm" style={{ color: 'var(--ag-text-tertiary)' }}>…</div>
          ) : articles && articles.length > 0 ? (
            <ul className="flex flex-col divide-y divide-border">
              {articles.map((a) => (
                <li key={a.slug} className="flex items-center gap-3 py-2.5">
                  <FileText className="h-4 w-4 shrink-0 text-accent" />
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm font-medium" style={{ color: 'var(--ag-text)' }}>
                      {a.title}
                    </div>
                    {a.summary ? (
                      <div className="truncate text-xs" style={{ color: 'var(--ag-text-tertiary)' }}>
                        {a.summary}
                      </div>
                    ) : null}
                  </div>
                  <Button
                    size="sm"
                    variant="secondary"
                    className="shrink-0 gap-1.5"
                    onPress={() => copyText(shareArticleUrl(a.slug, a.lang))}
                  >
                    <Copy className="h-3.5 w-3.5" />
                    {t('referral.copy_share_link')}
                  </Button>
                </li>
              ))}
            </ul>
          ) : (
            <div className="text-sm" style={{ color: 'var(--ag-text-tertiary)' }}>
              {t('referral.no_articles')}
            </div>
          )}
        </div>
      ) : null}

      {/* 统计 */}
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard icon={<Percent className="h-5 w-5" />} label={t('referral.my_rate')} value={`${((me?.referral_rate ?? 0) * 100).toFixed(1)}%`} />
        <StatCard icon={<Users className="h-5 w-5" />} label={t('referral.invitee_count')} value={me?.invitee_count ?? 0} />
        <StatCard icon={<Wallet className="h-5 w-5" />} label={t('referral.total_rebate')} value={`$${(me?.total_rebate ?? 0).toFixed(2)}`} />
        <StatCard icon={<Gift className="h-5 w-5" />} label={t('referral.total_reversed')} value={`$${(me?.total_reversed ?? 0).toFixed(2)}`} />
      </div>

      {/* 返利流水 */}
      <CommonTable
        ariaLabel={t('referral.records')}
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
        minWidth={640}
      >
        <CommonTable.Header>
          <CommonTable.Column id="time" style={{ width: 160 }}>{t('referral.col_time')}</CommonTable.Column>
          <CommonTable.Column id="invitee">{t('referral.col_invitee')}</CommonTable.Column>
          <CommonTable.Column id="paid" style={{ width: 120 }}>{t('referral.col_paid')}</CommonTable.Column>
          <CommonTable.Column id="rate" style={{ width: 88 }}>{t('referral.col_rate')}</CommonTable.Column>
          <CommonTable.Column id="amount" style={{ width: 120 }}>{t('referral.col_amount')}</CommonTable.Column>
          <CommonTable.Column id="status" style={{ width: 96 }}>{t('referral.col_status')}</CommonTable.Column>
        </CommonTable.Header>
        <CommonTable.Body>
          {listLoading ? (
            <TableLoadingRow colSpan={6} />
          ) : rows.length === 0 ? (
            <CommonTable.Row id="empty">
              <CommonTable.Cell colSpan={6}>
                <EmptyState>
                  <div className="text-sm text-default-500">{t('referral.empty')}</div>
                </EmptyState>
              </CommonTable.Cell>
            </CommonTable.Row>
          ) : (
            rows.map((row) => (
              <CommonTable.Row id={String(row.id)} key={row.id}>
                <CommonTable.Cell>
                  <span className="font-mono tabular-nums whitespace-nowrap">{formatTime(row.created_at)}</span>
                </CommonTable.Cell>
                <CommonTable.Cell>
                  <span className="truncate" style={{ color: 'var(--ag-text)' }}>{row.invitee_email}</span>
                </CommonTable.Cell>
                <CommonTable.Cell>
                  <span className="font-mono tabular-nums">${row.paid_amount.toFixed(2)}</span>
                </CommonTable.Cell>
                <CommonTable.Cell>
                  <span className="font-mono tabular-nums">{(row.rate * 100).toFixed(1)}%</span>
                </CommonTable.Cell>
                <CommonTable.Cell>
                  <span className="font-mono tabular-nums font-medium" style={{ color: 'var(--ag-success)' }}>
                    +${row.amount.toFixed(4)}
                  </span>
                </CommonTable.Cell>
                <CommonTable.Cell>
                  <Chip color={row.status === 'settled' ? 'success' : 'default'} size="sm" variant="soft">
                    {row.status === 'settled' ? t('referral.status_settled') : t('referral.status_reversed')}
                  </Chip>
                </CommonTable.Cell>
              </CommonTable.Row>
            ))
          )}
        </CommonTable.Body>
      </CommonTable>
    </div>
  );
}

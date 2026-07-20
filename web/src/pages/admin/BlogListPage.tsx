import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from '@tanstack/react-router';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { blogApi } from '../../shared/api/blog';
import { useCrudMutation } from '../../shared/hooks/useCrudMutation';
import { queryKeys } from '../../shared/queryKeys';
import { usePagination } from '../../shared/hooks/usePagination';
import { AlertDialog, Button, Chip, EmptyState, Label, ListBox, Select, Spinner } from '@heroui/react';
import { DialogTriggerShim } from '../../shared/components/DialogTriggerShim';
import { Plus, Pencil, Trash2, ExternalLink } from 'lucide-react';
import type { BlogPostResp } from '../../shared/types';
import { getTotalPages } from '../../shared/utils/pagination';
import { TablePaginationFooter } from '../../shared/components/TablePaginationFooter';
import { TableLoadingRow } from '../../shared/components/TableLoadingRow';
import { CommonTable } from '../../shared/components/CommonTable';

function formatDate(raw?: string | null): string {
  if (!raw) return '-';
  const d = new Date(raw);
  if (Number.isNaN(d.getTime())) return '-';
  return d.toLocaleDateString();
}

function languageKey(lang: BlogPostResp['lang']): string {
  if (lang === 'zh-Hant') return 'blog.lang_zh_hant';
  if (lang === 'en') return 'blog.lang_en';
  return 'blog.lang_zh';
}

export default function BlogListPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { page, setPage, pageSize, setPageSize } = usePagination(20, 'admin.blog');
  const [deleteTarget, setDeleteTarget] = useState<BlogPostResp | null>(null);
  const [langFilter, setLangFilter] = useState('');

  const languageOptions = [
    { id: '', label: t('blog.all_languages', '全部语言') },
    { id: 'zh-Hant', label: t('blog.lang_zh_hant', '繁體中文') },
    { id: 'en', label: t('blog.lang_en', 'English') },
    { id: 'zh', label: t('blog.lang_zh', '简体中文') },
  ];
  const selectedLanguageLabel = languageOptions.find((item) => item.id === langFilter)?.label
    ?? languageOptions[0]?.label
    ?? t('blog.all_languages', '全部语言');

  const { data, isLoading } = useQuery({
    queryKey: queryKeys.blog(page, pageSize, langFilter),
    queryFn: () => blogApi.list({ page, page_size: pageSize, lang: langFilter || undefined }),
    placeholderData: keepPreviousData,
  });

  const deleteMutation = useCrudMutation({
    mutationFn: (id: number) => blogApi.delete(id),
    successMessage: t('blog.delete_success', '文章已删除'),
    queryKey: queryKeys.blog(),
    onSuccess: () => setDeleteTarget(null),
  });

  const rows = data?.list ?? [];
  const total = data?.total ?? 0;
  const totalPages = getTotalPages(total, pageSize);

  const openCreate = () => navigate({ to: '/admin/blog/edit' });
  const openEdit = (id: number) => navigate({ to: '/admin/blog/edit', search: { id } });

  return (
    <div>
      <div className="mb-5 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="w-full sm:w-52">
          <Select
            fullWidth
            selectedKey={langFilter}
            onSelectionChange={(key) => {
              setLangFilter(key == null ? '' : String(key));
              setPage(1);
            }}
          >
            <Label className="sr-only">{t('common.language', '语言')}</Label>
            <Select.Trigger>
              <Select.Value>{selectedLanguageLabel}</Select.Value>
              <Select.Indicator />
            </Select.Trigger>
            <Select.Popover>
              <ListBox items={languageOptions}>
                {(item) => (
                  <ListBox.Item id={item.id} textValue={item.label}>
                    {item.label}
                  </ListBox.Item>
                )}
              </ListBox>
            </Select.Popover>
          </Select>
        </div>
        <Button variant="primary" onPress={openCreate}>
          <Plus className="w-4 h-4" />
          {t('blog.create', '写文章')}
        </Button>
      </div>

      <CommonTable
        ariaLabel={t('blog.title', 'Blog')}
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
        minWidth={960}
      >
        <CommonTable.Header>
          <CommonTable.Column id="title">{t('blog.col_title', '标题')}</CommonTable.Column>
          <CommonTable.Column id="slug">{t('blog.col_slug', '短链')}</CommonTable.Column>
          <CommonTable.Column id="language">{t('blog.col_language', '语言')}</CommonTable.Column>
          <CommonTable.Column id="status">{t('common.status', '状态')}</CommonTable.Column>
          <CommonTable.Column id="views">{t('blog.col_views', '阅读')}</CommonTable.Column>
          <CommonTable.Column id="updated">{t('blog.col_updated', '更新时间')}</CommonTable.Column>
          <CommonTable.Column id="actions">{t('common.actions', '操作')}</CommonTable.Column>
        </CommonTable.Header>
        <CommonTable.Body>
          {isLoading ? (
            <TableLoadingRow colSpan={7} />
          ) : rows.length === 0 ? (
            <CommonTable.Row id="empty">
              <CommonTable.Cell colSpan={7}>
                <EmptyState>
                  <div className="text-sm text-default-500">{t('common.no_data', '暂无数据')}</div>
                </EmptyState>
              </CommonTable.Cell>
            </CommonTable.Row>
          ) : (
            rows.map((row) => (
              <CommonTable.Row id={String(row.id)} key={row.id}>
                <CommonTable.Cell>
                  <span className="text-text">{row.title}</span>
                </CommonTable.Cell>
                <CommonTable.Cell>
                  <span className="font-mono text-xs text-text-tertiary">{row.slug}</span>
                </CommonTable.Cell>
                <CommonTable.Cell>
                  <Chip size="sm" variant="soft">{t(languageKey(row.lang))}</Chip>
                </CommonTable.Cell>
                <CommonTable.Cell>
                  <Chip color={row.status === 'published' ? 'success' : 'default'} size="sm" variant="soft">
                    {row.status === 'published' ? t('blog.status_published', '已发布') : t('blog.status_draft', '草稿')}
                  </Chip>
                </CommonTable.Cell>
                <CommonTable.Cell>
                  <span className="font-mono text-text-tertiary">{row.view_count}</span>
                </CommonTable.Cell>
                <CommonTable.Cell>
                  <span className="text-text-secondary text-sm">{formatDate(row.updated_at)}</span>
                </CommonTable.Cell>
                <CommonTable.Cell>
                  <div className="ag-table-row-actions flex justify-center gap-1">
                    {row.status === 'published' && (
                      <a href={`/blog/${row.slug}`} target="_blank" rel="noreferrer">
                        <Button size="sm" variant="secondary">
                          <ExternalLink className="w-3.5 h-3.5" />
                          {t('blog.preview', '预览')}
                        </Button>
                      </a>
                    )}
                    <Button size="sm" variant="secondary" onPress={() => openEdit(row.id)}>
                      <Pencil className="w-3.5 h-3.5" />
                      {t('common.edit', '编辑')}
                    </Button>
                    <Button size="sm" variant="danger-soft" className="text-danger" onPress={() => setDeleteTarget(row)}>
                      <Trash2 className="w-3.5 h-3.5" />
                      {t('common.delete', '删除')}
                    </Button>
                  </div>
                </CommonTable.Cell>
              </CommonTable.Row>
            ))
          )}
        </CommonTable.Body>
      </CommonTable>

      <AlertDialog
        isOpen={!!deleteTarget}
        onOpenChange={(open) => { if (!open) setDeleteTarget(null); }}
      >
        <DialogTriggerShim />
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="sm">
            <AlertDialog.Dialog className="ag-elevation-modal">
              <AlertDialog.Header>
                <AlertDialog.Icon status="danger" />
                <AlertDialog.Heading>{t('blog.delete_title', '删除文章')}</AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>{t('blog.delete_confirm', { title: deleteTarget?.title, defaultValue: '确定删除《{{title}}》吗?' })}</AlertDialog.Body>
              <AlertDialog.Footer>
                <Button variant="secondary" onPress={() => setDeleteTarget(null)}>
                  {t('common.cancel', '取消')}
                </Button>
                <Button
                  aria-busy={deleteMutation.isPending}
                  isDisabled={deleteMutation.isPending}
                  variant="danger"
                  onPress={() => deleteTarget && deleteMutation.mutate(deleteTarget.id)}
                >
                  {deleteMutation.isPending ? <Spinner size="sm" /> : null}
                  {t('common.confirm', '确定')}
                </Button>
              </AlertDialog.Footer>
            </AlertDialog.Dialog>
          </AlertDialog.Container>
        </AlertDialog.Backdrop>
      </AlertDialog>
    </div>
  );
}

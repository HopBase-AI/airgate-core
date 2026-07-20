import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate, useSearch } from '@tanstack/react-router';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Button, Input, Label, ListBox, Select, Spinner, TextArea,
  TextField as HeroTextField,
} from '@heroui/react';
import { NativeSwitch } from '../../shared/components/NativeSwitch';
import { RichTextEditor } from '../../shared/components/RichTextEditor';
import { useSiteSettings } from '../../app/providers/SiteSettingsProvider';
import { blogApi } from '../../shared/api/blog';
import { useCrudMutation } from '../../shared/hooks/useCrudMutation';
import { queryKeys } from '../../shared/queryKeys';
import { useToast } from '../../shared/ui';
import { buildBlogShareURL, publicBlogBase } from '../../shared/blogShare';
import type { BlogLanguage, BlogPostResp, BlogStatus, CreateBlogPostReq } from '../../shared/types';
import { ArrowLeft, ImagePlus, X, Copy, ExternalLink } from 'lucide-react';

interface BlogForm {
  title: string;
  slug: string;
  summary: string;
  cover_image: string;
  content_html: string;
  status: BlogStatus;
  lang: BlogLanguage;
  invite_code: string;
  gate_enabled: boolean;
  gate_position: number;
  tags: string;
  sites: string[];
  seo_title: string;
  seo_description: string;
  og_image: string;
}

const emptyForm: BlogForm = {
  title: '', slug: '', summary: '', cover_image: '', content_html: '',
  status: 'draft', lang: 'zh-Hant', invite_code: '', gate_enabled: false, gate_position: 50,
  tags: '', sites: [], seo_title: '', seo_description: '', og_image: '',
};

function fromPost(p: BlogPostResp): BlogForm {
  return {
    title: p.title, slug: p.slug, summary: p.summary, cover_image: p.cover_image,
    content_html: p.content_html, status: p.status, lang: p.lang || 'zh-Hant', invite_code: p.invite_code,
    gate_enabled: p.gate_enabled, gate_position: p.gate_position || 50,
    tags: (p.tags ?? []).join(', '), sites: p.sites ?? [], seo_title: p.seo_title,
    seo_description: p.seo_description, og_image: p.og_image,
  };
}

function toPayload(f: BlogForm): CreateBlogPostReq {
  return {
    title: f.title.trim(),
    slug: f.slug.trim() || undefined,
    summary: f.summary,
    cover_image: f.cover_image,
    content_html: f.content_html,
    status: f.status,
    lang: f.lang,
    invite_code: f.invite_code.trim(),
    gate_enabled: f.gate_enabled,
    gate_position: f.gate_position,
    tags: f.tags.split(',').map((s) => s.trim()).filter(Boolean),
    sites: f.sites,
    seo_title: f.seo_title,
    seo_description: f.seo_description,
    og_image: f.og_image,
  };
}

export default function BlogEditorPage() {
  const { t } = useTranslation();
  const { toast } = useToast();
  const { blog_sites } = useSiteSettings();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const search = useSearch({ strict: false }) as { id?: number };
  const editId = typeof search.id === 'number' ? search.id : undefined;

  const [form, setForm] = useState<BlogForm>(emptyForm);
  const initializedRef = useRef<number | 'new' | null>(null);

  const { data: post, isLoading } = useQuery({
    queryKey: queryKeys.blogPost(editId),
    queryFn: () => blogApi.get(editId as number),
    enabled: editId !== undefined,
  });

  useEffect(() => {
    if (editId === undefined) {
      if (initializedRef.current !== 'new') {
        setForm(emptyForm);
        initializedRef.current = 'new';
      }
      return;
    }
    if (post && initializedRef.current !== post.id) {
      setForm(fromPost(post));
      initializedRef.current = post.id;
    }
  }, [editId, post]);

  const createMutation = useCrudMutation({
    mutationFn: (data: CreateBlogPostReq) => blogApi.create(data),
    successMessage: t('blog.save_success', '已保存'),
    queryKey: queryKeys.blog(),
    onSuccess: (created) => {
      queryClient.setQueryData(queryKeys.blogPost(created.id), created);
      setForm(fromPost(created));
      initializedRef.current = created.id;
      navigate({ to: '/admin/blog/edit', search: { id: created.id } });
    },
  });

  const updateMutation = useCrudMutation({
    mutationFn: ({ id, data }: { id: number; data: CreateBlogPostReq }) => blogApi.update(id, data),
    successMessage: t('blog.save_success', '已保存'),
    queryKey: queryKeys.blog(),
    onSuccess: (updated) => {
      queryClient.setQueryData(queryKeys.blogPost(updated.id), updated);
      setForm(fromPost(updated));
      initializedRef.current = updated.id;
    },
  });

  const uploading = useRef(false);
  const uploadImage = async (file: File): Promise<string> => {
    try {
      const r = await blogApi.upload(file);
      return r.url;
    } catch (err) {
      toast('error', (err as Error).message);
      throw err;
    }
  };

  const handleCoverUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = '';
    if (!file || uploading.current) return;
    uploading.current = true;
    try {
      const url = await uploadImage(file);
      setForm((f) => ({ ...f, cover_image: url }));
    } catch {
      /* toasted */
    } finally {
      uploading.current = false;
    }
  };

  const save = () => {
    if (!form.title.trim()) {
      toast('error', t('blog.title_required', '请填写标题'));
      return;
    }
    const payload = toPayload(form);
    if (editId !== undefined) {
      updateMutation.mutate({ id: editId, data: payload });
    } else {
      createMutation.mutate(payload);
    }
  };

  const saving = createMutation.isPending || updateMutation.isPending;

  const statusOptions = [
    { id: 'draft', label: t('blog.status_draft', '草稿') },
    { id: 'published', label: t('blog.status_published', '已发布') },
  ];
  const selectedStatusLabel = statusOptions.find((o) => o.id === form.status)?.label ?? '';
  const languageOptions = [
    { id: 'zh-Hant', label: t('blog.lang_zh_hant', '繁體中文') },
    { id: 'en', label: t('blog.lang_en', 'English') },
    { id: 'zh', label: t('blog.lang_zh', '简体中文') },
  ];
  const selectedLanguageLabel = languageOptions.find((o) => o.id === form.lang)?.label ?? '';

  const coverInputRef = useRef<HTMLInputElement>(null);
  const shareURL = post?.status === 'published'
    ? buildBlogShareURL(publicBlogBase(window.location.origin), post.slug, post.lang)
    : '';

  const copyShare = () => {
    if (!shareURL) return;
    navigator.clipboard?.writeText(shareURL).then(
      () => toast('success', t('blog.copied', '已复制')),
      () => toast('error', t('blog.copy_failed', '复制失败')),
    );
  };

  if (editId !== undefined && isLoading) {
    return <div className="flex justify-center py-20"><Spinner /></div>;
  }

  return (
    <div className="mx-auto max-w-4xl">
      <div className="mb-5 flex items-center justify-between">
        <Button variant="ghost" onPress={() => navigate({ to: '/admin/blog' })}>
          <ArrowLeft className="h-4 w-4" />
          {t('blog.back', '返回列表')}
        </Button>
        <div className="flex items-center gap-2">
          {shareURL && (
            <a href={shareURL} target="_blank" rel="noreferrer">
              <Button variant="secondary">
                <ExternalLink className="h-4 w-4" />
                {t('blog.preview', '预览')}
              </Button>
            </a>
          )}
          <Button variant="primary" isDisabled={saving} onPress={save}>
            {saving ? <Spinner size="sm" /> : null}
            {t('common.save', '保存')}
          </Button>
        </div>
      </div>

      <div className="space-y-5">
        {/* 标题 */}
        <HeroTextField fullWidth isRequired>
          <Label>{t('blog.field_title', '标题')}</Label>
          <Input
            value={form.title}
            onChange={(e) => setForm({ ...form, title: e.target.value })}
            placeholder={t('blog.title_placeholder', '文章标题')}
          />
        </HeroTextField>

        {/* Slug */}
        <HeroTextField fullWidth>
          <Label>{t('blog.field_slug', '短链 slug(留空自动从标题生成)')}</Label>
          <Input
            value={form.slug}
            onChange={(e) => setForm({ ...form, slug: e.target.value })}
            placeholder="my-first-post"
            className="font-mono"
          />
        </HeroTextField>

        {/* 摘要 */}
        <div>
          <Label>{t('blog.field_summary', '摘要(列表展示 + SEO 描述兜底)')}</Label>
          <TextArea
            aria-label={t('blog.field_summary', '摘要')}
            value={form.summary}
            onChange={(e) => setForm({ ...form, summary: e.target.value })}
            className="mt-1.5 h-20 w-full"
            placeholder={t('blog.summary_placeholder', '一两句话概括本文')}
          />
        </div>

        {/* 封面 */}
        <div>
          <Label>{t('blog.field_cover', '封面图')}</Label>
          <div className="mt-1.5 flex items-center gap-3">
            {form.cover_image ? (
              <div className="relative">
                <img src={form.cover_image} alt="cover" className="h-20 w-32 rounded-md object-cover border border-border" />
                <button
                  type="button"
                  onClick={() => setForm({ ...form, cover_image: '' })}
                  className="absolute -right-2 -top-2 rounded-full bg-danger p-0.5 text-white"
                  aria-label={t('common.delete', '删除')}
                >
                  <X className="h-3 w-3" />
                </button>
              </div>
            ) : null}
            <Button variant="secondary" onPress={() => coverInputRef.current?.click()}>
              <ImagePlus className="h-4 w-4" />
              {t('blog.upload_cover', '上传封面')}
            </Button>
            <input ref={coverInputRef} type="file" accept="image/*" className="hidden" onChange={handleCoverUpload} />
          </div>
        </div>

        {/* 正文富文本编辑器 */}
        <div>
          <Label>{t('blog.field_content', '正文')}</Label>
          <div className="mt-1.5">
            <RichTextEditor
              value={form.content_html}
              onChange={(html) => setForm((f) => ({ ...f, content_html: html }))}
              onImageUpload={uploadImage}
              placeholder={t('blog.content_placeholder', '开始撰写……')}
            />
          </div>
        </div>

        {/* 发布与转化设置 */}
        <div className="rounded-[var(--radius)] border border-border p-4 space-y-4">
          <div className="text-sm font-semibold text-text">{t('blog.section_publish', '发布与转化')}</div>

          <Select
            fullWidth
            selectedKey={form.lang}
            onSelectionChange={(key) => setForm({ ...form, lang: (key ?? 'zh-Hant') as BlogLanguage })}
          >
            <Label>{t('blog.field_language', '文章语言')}</Label>
            <Select.Trigger>
              <Select.Value>{selectedLanguageLabel}</Select.Value>
              <Select.Indicator />
            </Select.Trigger>
            <Select.Popover>
              <ListBox items={languageOptions}>
                {(item) => (
                  <ListBox.Item id={item.id} textValue={item.label}>{item.label}</ListBox.Item>
                )}
              </ListBox>
            </Select.Popover>
          </Select>

          <Select
            fullWidth
            selectedKey={form.status}
            onSelectionChange={(key) => setForm({ ...form, status: (key ?? 'draft') as BlogStatus })}
          >
            <Label>{t('common.status', '状态')}</Label>
            <Select.Trigger>
              <Select.Value>{selectedStatusLabel}</Select.Value>
              <Select.Indicator />
            </Select.Trigger>
            <Select.Popover>
              <ListBox items={statusOptions}>
                {(item) => (
                  <ListBox.Item id={item.id} textValue={item.label}>{item.label}</ListBox.Item>
                )}
              </ListBox>
            </Select.Popover>
          </Select>

          <HeroTextField fullWidth>
            <Label>{t('blog.field_invite', '邀请码(注册/登录 CTA 自动带上 ?inv=)')}</Label>
            <Input
              value={form.invite_code}
              onChange={(e) => setForm({ ...form, invite_code: e.target.value })}
              placeholder="abc123"
              className="font-mono"
            />
            {/* 归因优先级容易误解:文章码只是裸链接访问的兜底,红字明示避免疑问 */}
            <p className="mt-1.5 text-[11px] text-danger">
              {t('blog.invite_warn', '注意:此码只在读者「不带参数」打开本文时兜底归因;读者从带 ?inv= 的链接进来(如推广人在「邀请好友」页复制的分享链接)时,以链接上的码为准,此码会被覆盖。')}
            </p>
          </HeroTextField>

          <div className="space-y-3">
            <NativeSwitch
              isSelected={form.gate_enabled}
              label={<span className="text-sm font-medium text-text">{t('blog.field_gate', '注册墙:读到指定位置弹注册引导')}</span>}
              onChange={(v) => setForm({ ...form, gate_enabled: v })}
            />
            {form.gate_enabled && (
              <HeroTextField>
                <Label>{t('blog.field_gate_pos', '触发位置(正文百分比 1~99)')}</Label>
                <Input
                  type="number"
                  min={1}
                  max={99}
                  value={String(form.gate_position)}
                  onChange={(e) => setForm({ ...form, gate_position: Number(e.target.value) || 0 })}
                  className="w-28"
                />
              </HeroTextField>
            )}
          </div>

          <HeroTextField fullWidth>
            <Label>{t('blog.field_tags', '标签(英文逗号分隔)')}</Label>
            <Input
              value={form.tags}
              onChange={(e) => setForm({ ...form, tags: e.target.value })}
              placeholder="AI, Claude, 教程"
            />
          </HeroTextField>

          {blog_sites.length > 0 && (
            <div>
              <Label>{t('blog.field_sites', '发布站点(不选=所有站点可见)')}</Label>
              <div className="mt-1.5 flex flex-wrap gap-2">
                {blog_sites.map((s) => {
                  const active = form.sites.includes(s.key);
                  return (
                    <button
                      key={s.key}
                      type="button"
                      aria-pressed={active}
                      onClick={() => setForm((f) => ({
                        ...f,
                        sites: active ? f.sites.filter((k) => k !== s.key) : [...f.sites, s.key],
                      }))}
                      className={`rounded-full border px-3 py-1 text-sm transition-colors ${
                        active
                          ? 'border-accent bg-accent/10 text-accent'
                          : 'border-border text-text-secondary hover:bg-surface'
                      }`}
                    >
                      {s.label}
                    </button>
                  );
                })}
              </div>
            </div>
          )}

          {shareURL && (
            <div className="rounded-md bg-surface px-3 py-2 text-xs text-text-secondary flex items-center justify-between gap-2">
              <span className="font-mono truncate" title={shareURL}>{shareURL}</span>
              <Button size="sm" variant="ghost" onPress={copyShare}>
                <Copy className="h-3.5 w-3.5" />
                {t('blog.copy_link', '复制链接')}
              </Button>
            </div>
          )}
          <p className="text-[11px] text-text-tertiary">{t('blog.invite_note', '邀请码已内置进本文 CTA;读者带 ?inv= 访问时以其为准。')}</p>
        </div>

        {/* SEO 覆盖(可选) */}
        <details className="rounded-[var(--radius)] border border-border p-4">
          <summary className="cursor-pointer text-sm font-semibold text-text">{t('blog.section_seo', 'SEO 覆盖(可选)')}</summary>
          <div className="mt-4 space-y-4">
            <HeroTextField fullWidth>
              <Label>{t('blog.field_seo_title', 'SEO 标题(留空用文章标题)')}</Label>
              <Input value={form.seo_title} onChange={(e) => setForm({ ...form, seo_title: e.target.value })} />
            </HeroTextField>
            <div>
              <Label>{t('blog.field_seo_desc', 'SEO 描述(留空用摘要)')}</Label>
              <TextArea
                aria-label={t('blog.field_seo_desc', 'SEO 描述')}
                value={form.seo_description}
                onChange={(e) => setForm({ ...form, seo_description: e.target.value })}
                className="mt-1.5 h-16 w-full"
              />
            </div>
            <HeroTextField fullWidth>
              <Label>{t('blog.field_og', '分享图 URL(留空用封面)')}</Label>
              <Input value={form.og_image} onChange={(e) => setForm({ ...form, og_image: e.target.value })} className="font-mono" />
            </HeroTextField>
          </div>
        </details>
      </div>
    </div>
  );
}

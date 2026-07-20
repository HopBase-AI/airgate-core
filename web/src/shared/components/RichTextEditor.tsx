import { useEditor, EditorContent, type Editor } from '@tiptap/react';
import StarterKit from '@tiptap/starter-kit';
import Image from '@tiptap/extension-image';
import Link from '@tiptap/extension-link';
import Youtube from '@tiptap/extension-youtube';
import TextAlign from '@tiptap/extension-text-align';
import Placeholder from '@tiptap/extension-placeholder';
import Table from '@tiptap/extension-table';
import TableRow from '@tiptap/extension-table-row';
import TableHeader from '@tiptap/extension-table-header';
import TableCell from '@tiptap/extension-table-cell';
import { useEffect, useRef, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Bold, Italic, Strikethrough, Heading2, Heading3, List, ListOrdered,
  Quote, Code, Link2, Image as ImageIcon, Youtube as YoutubeIcon,
  AlignLeft, AlignCenter, AlignRight, Undo, Redo,
} from 'lucide-react';
import { looksLikeMarkdown, markdownToHTML, richTextInputToHTML } from '../markdown';

interface RichTextEditorProps {
  value: string;
  onChange: (html: string) => void;
  /** 返回图片可访问 URL；用于图片按钮上传后插入。 */
  onImageUpload: (file: File) => Promise<string>;
  placeholder?: string;
}

/** 工具栏按钮。 */
function ToolbarButton(props: {
  active?: boolean;
  disabled?: boolean;
  title: string;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      title={props.title}
      aria-label={props.title}
      disabled={props.disabled}
      onClick={props.onClick}
      className={`inline-flex h-8 w-8 items-center justify-center rounded-md border border-transparent text-text-secondary transition-colors hover:bg-surface disabled:opacity-40 ${
        props.active ? 'bg-surface text-text border-border' : ''
      }`}
    >
      {props.children}
    </button>
  );
}

function Divider() {
  return <span className="mx-1 h-5 w-px self-center bg-border" aria-hidden />;
}

/** 从剪贴板/拖拽数据里挑出图片文件。 */
function imageFilesFrom(dt: DataTransfer | null): File[] {
  if (!dt) return [];
  return Array.from(dt.files ?? []).filter((f) => f.type.startsWith('image/'));
}

export function RichTextEditor({ value, onChange, onImageUpload, placeholder }: RichTextEditorProps) {
  const { t } = useTranslation();
  const fileInputRef = useRef<HTMLInputElement>(null);
  // editorProps 在 useEditor 创建时定义,此时 editor 尚不存在;用 ref 让粘贴/拖拽回调拿到实例。
  const editorRef = useRef<Editor | null>(null);
  const uploadRef = useRef(onImageUpload);
  uploadRef.current = onImageUpload;

  // 上传一组图片并按顺序插入(可指定插入位置,用于拖拽落点)。
  const uploadAndInsert = async (files: File[], atPos?: number) => {
    const ed = editorRef.current;
    if (!ed) return;
    for (const file of files) {
      try {
        const url = await uploadRef.current(file);
        const chain = ed.chain().focus();
        if (typeof atPos === 'number') chain.setTextSelection(atPos);
        chain.setImage({ src: url }).run();
      } catch {
        // onImageUpload 内部已 toast 错误
      }
    }
  };

  const editor = useEditor({
    extensions: [
      StarterKit.configure({ heading: { levels: [2, 3, 4] } }),
      Image.configure({ inline: false, HTMLAttributes: { loading: 'lazy' } }),
      Link.configure({ openOnClick: false, autolink: true, HTMLAttributes: { rel: 'noopener noreferrer nofollow', target: '_blank' } }),
      Youtube.configure({ width: 640, height: 360, nocookie: true }),
      TextAlign.configure({ types: ['heading', 'paragraph'] }),
      Placeholder.configure({ placeholder: placeholder ?? '' }),
      Table.configure({ resizable: true }),
      TableRow,
      TableHeader,
      TableCell,
    ],
    content: richTextInputToHTML(value),
    onUpdate: ({ editor }) => onChange(editor.getHTML()),
    editorProps: {
      // 粘贴截图/图片:直接上传并插入,跳过默认粘贴(避免落成 base64 巨串)。
      handlePaste: (_view, event) => {
        const files = imageFilesFrom(event.clipboardData);
        if (files.length > 0) {
          event.preventDefault();
          void uploadAndInsert(files);
          return true;
        }
        const plain = event.clipboardData?.getData('text/plain') ?? '';
        const rich = event.clipboardData?.getData('text/html') ?? '';
        if (!rich && looksLikeMarkdown(plain)) {
          event.preventDefault();
          editorRef.current?.commands.insertContent(markdownToHTML(plain));
          return true;
        }
        return false;
      },
      // 拖拽图片文件到编辑区:上传并插入到落点。
      handleDrop: (view, event) => {
        const files = imageFilesFrom(event.dataTransfer);
        if (files.length === 0) return false;
        event.preventDefault();
        const pos = view.posAtCoords({ left: event.clientX, top: event.clientY })?.pos;
        void uploadAndInsert(files, pos);
        return true;
      },
    },
  });

  useEffect(() => {
    editorRef.current = editor;
  }, [editor]);

  // 外部 value 变化(如编辑态加载文章)时同步内容,避免每次 onChange 回写造成光标跳动。
  useEffect(() => {
    if (!editor) return;
    const normalized = richTextInputToHTML(value);
    if (normalized !== editor.getHTML()) {
      editor.commands.setContent(normalized || '', false);
    }
    if (normalized !== value) onChange(normalized);
  }, [value, editor, onChange]);

  if (!editor) return null;

  const insertImage = () => fileInputRef.current?.click();

  const handleFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files ?? []);
    e.target.value = '';
    if (files.length === 0) return;
    await uploadAndInsert(files);
  };

  const insertVideo = () => {
    const url = window.prompt(t('blog.editor.video_prompt', '粘贴 YouTube / Bilibili 视频链接'));
    if (!url) return;
    editor.commands.setYoutubeVideo({ src: url });
  };

  const setLink = () => {
    const prev = editor.getAttributes('link').href as string | undefined;
    const url = window.prompt(t('blog.editor.link_prompt', '链接地址'), prev ?? 'https://');
    if (url === null) return;
    if (url === '') {
      editor.chain().focus().extendMarkRange('link').unsetLink().run();
      return;
    }
    editor.chain().focus().extendMarkRange('link').setLink({ href: url }).run();
  };

  return (
    <div className="blog-editor rounded-[var(--radius)] border border-border bg-bg">
      <EditorToolbar
        editor={editor}
        onImage={insertImage}
        onVideo={insertVideo}
        onLink={setLink}
      />
      <input ref={fileInputRef} type="file" accept="image/*" multiple className="hidden" onChange={handleFile} />
      <EditorContent editor={editor} />
      <div className="border-t border-border px-3 py-1.5 text-[11px]" style={{ color: 'var(--ag-text-tertiary)' }}>
        {t('blog.editor.paste_hint', '可粘贴 Markdown / 截图，或把图片拖进来自动上传')}
      </div>
      <style>{`
        .blog-editor .ProseMirror{min-height:420px;padding:22px 24px;outline:none;line-height:1.78;font-size:17px}
        .blog-editor .ProseMirror>*:first-child{margin-top:0}
        .blog-editor .ProseMirror h2{font-size:1.5em;font-weight:700;letter-spacing:-.02em;margin:1.6em 0 .55em;line-height:1.3}
        .blog-editor .ProseMirror h3{font-size:1.22em;font-weight:600;margin:1.4em 0 .4em;line-height:1.4}
        .blog-editor .ProseMirror h4{font-size:1.05em;font-weight:600;margin:1.1em 0 .4em}
        .blog-editor .ProseMirror p{margin:.75em 0}
        .blog-editor .ProseMirror ul,.blog-editor .ProseMirror ol{padding-left:1.5em;margin:.75em 0}
        .blog-editor .ProseMirror li{margin:.4em 0}
        .blog-editor .ProseMirror li::marker{color:var(--ag-primary,#4f46e5)}
        .blog-editor .ProseMirror blockquote{border-left:3px solid var(--ag-primary,#4f46e5);padding-left:20px;margin:1.3em 0;font-size:1.12em;line-height:1.5;color:var(--ag-text)}
        .blog-editor .ProseMirror :not(pre)>code{background:color-mix(in srgb,var(--ag-primary,#4f46e5) 12%,transparent);color:var(--ag-primary,#4f46e5);border-radius:6px;padding:.12em .42em;font-size:.85em;font-weight:500}
        .blog-editor .ProseMirror pre{background:#0b1020;color:#e5e9f2;border-radius:12px;padding:16px 18px;overflow-x:auto;font-size:.86em;line-height:1.7;margin:1.4em 0}
        .blog-editor .ProseMirror pre code{background:none;color:inherit;padding:0;font-size:1em}
        .blog-editor .ProseMirror img{display:block;max-width:100%;height:auto;margin:1.8em auto;border-radius:12px;border:1px solid var(--border);box-shadow:0 1px 2px rgba(16,24,40,.06),0 12px 30px rgba(16,24,40,.09)}
        .blog-editor .ProseMirror img.ProseMirror-selectednode{outline:2px solid var(--ag-primary,#4f46e5);outline-offset:3px}
        .blog-editor .ProseMirror iframe{max-width:100%;aspect-ratio:16/9;width:100%;border:0;border-radius:12px;margin:1.6em auto;display:block}
        .blog-editor .ProseMirror table{border-collapse:collapse;width:100%;font-size:.92em;margin:1.4em 0}
        .blog-editor .ProseMirror thead th{text-align:left;font-weight:600;color:var(--ag-text-tertiary);font-size:.82em;letter-spacing:.04em;text-transform:uppercase;padding:0 12px 8px;border-bottom:1px solid var(--border)}
        .blog-editor .ProseMirror tbody td{padding:9px 12px;border-bottom:1px solid var(--border)}
        .blog-editor .ProseMirror hr{border:0;height:1px;background:var(--border);margin:1.8em 0}
        .blog-editor .ProseMirror a{color:var(--ag-primary,#4f46e5);text-decoration:underline;text-underline-offset:3px}
        .blog-editor .ProseMirror p.is-editor-empty:first-child::before{content:attr(data-placeholder);color:var(--ag-text-tertiary,#9ca3af);float:left;height:0;pointer-events:none}
      `}</style>
    </div>
  );
}

function EditorToolbar({ editor, onImage, onVideo, onLink }: {
  editor: Editor;
  onImage: () => void;
  onVideo: () => void;
  onLink: () => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-wrap items-center gap-0.5 border-b border-border px-2 py-1.5">
      <ToolbarButton title={t('blog.editor.bold', '加粗')} active={editor.isActive('bold')} onClick={() => editor.chain().focus().toggleBold().run()}>
        <Bold className="h-4 w-4" />
      </ToolbarButton>
      <ToolbarButton title={t('blog.editor.italic', '斜体')} active={editor.isActive('italic')} onClick={() => editor.chain().focus().toggleItalic().run()}>
        <Italic className="h-4 w-4" />
      </ToolbarButton>
      <ToolbarButton title={t('blog.editor.strike', '删除线')} active={editor.isActive('strike')} onClick={() => editor.chain().focus().toggleStrike().run()}>
        <Strikethrough className="h-4 w-4" />
      </ToolbarButton>
      <Divider />
      <ToolbarButton title={t('blog.editor.h2', '二级标题')} active={editor.isActive('heading', { level: 2 })} onClick={() => editor.chain().focus().toggleHeading({ level: 2 }).run()}>
        <Heading2 className="h-4 w-4" />
      </ToolbarButton>
      <ToolbarButton title={t('blog.editor.h3', '三级标题')} active={editor.isActive('heading', { level: 3 })} onClick={() => editor.chain().focus().toggleHeading({ level: 3 }).run()}>
        <Heading3 className="h-4 w-4" />
      </ToolbarButton>
      <ToolbarButton title={t('blog.editor.bullet', '无序列表')} active={editor.isActive('bulletList')} onClick={() => editor.chain().focus().toggleBulletList().run()}>
        <List className="h-4 w-4" />
      </ToolbarButton>
      <ToolbarButton title={t('blog.editor.ordered', '有序列表')} active={editor.isActive('orderedList')} onClick={() => editor.chain().focus().toggleOrderedList().run()}>
        <ListOrdered className="h-4 w-4" />
      </ToolbarButton>
      <ToolbarButton title={t('blog.editor.quote', '引用')} active={editor.isActive('blockquote')} onClick={() => editor.chain().focus().toggleBlockquote().run()}>
        <Quote className="h-4 w-4" />
      </ToolbarButton>
      <ToolbarButton title={t('blog.editor.code', '代码块')} active={editor.isActive('codeBlock')} onClick={() => editor.chain().focus().toggleCodeBlock().run()}>
        <Code className="h-4 w-4" />
      </ToolbarButton>
      <Divider />
      <ToolbarButton title={t('blog.editor.align_left', '左对齐')} active={editor.isActive({ textAlign: 'left' })} onClick={() => editor.chain().focus().setTextAlign('left').run()}>
        <AlignLeft className="h-4 w-4" />
      </ToolbarButton>
      <ToolbarButton title={t('blog.editor.align_center', '居中')} active={editor.isActive({ textAlign: 'center' })} onClick={() => editor.chain().focus().setTextAlign('center').run()}>
        <AlignCenter className="h-4 w-4" />
      </ToolbarButton>
      <ToolbarButton title={t('blog.editor.align_right', '右对齐')} active={editor.isActive({ textAlign: 'right' })} onClick={() => editor.chain().focus().setTextAlign('right').run()}>
        <AlignRight className="h-4 w-4" />
      </ToolbarButton>
      <Divider />
      <ToolbarButton title={t('blog.editor.link', '链接')} active={editor.isActive('link')} onClick={onLink}>
        <Link2 className="h-4 w-4" />
      </ToolbarButton>
      <ToolbarButton title={t('blog.editor.image', '图片')} onClick={onImage}>
        <ImageIcon className="h-4 w-4" />
      </ToolbarButton>
      <ToolbarButton title={t('blog.editor.video', '视频')} onClick={onVideo}>
        <YoutubeIcon className="h-4 w-4" />
      </ToolbarButton>
      <Divider />
      <ToolbarButton title={t('blog.editor.undo', '撤销')} disabled={!editor.can().undo()} onClick={() => editor.chain().focus().undo().run()}>
        <Undo className="h-4 w-4" />
      </ToolbarButton>
      <ToolbarButton title={t('blog.editor.redo', '重做')} disabled={!editor.can().redo()} onClick={() => editor.chain().focus().redo().run()}>
        <Redo className="h-4 w-4" />
      </ToolbarButton>
    </div>
  );
}

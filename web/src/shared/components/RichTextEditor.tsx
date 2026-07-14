import { useEditor, EditorContent, type Editor } from '@tiptap/react';
import StarterKit from '@tiptap/starter-kit';
import Image from '@tiptap/extension-image';
import Link from '@tiptap/extension-link';
import Youtube from '@tiptap/extension-youtube';
import TextAlign from '@tiptap/extension-text-align';
import Placeholder from '@tiptap/extension-placeholder';
import { useEffect, useRef, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Bold, Italic, Strikethrough, Heading2, Heading3, List, ListOrdered,
  Quote, Code, Link2, Image as ImageIcon, Youtube as YoutubeIcon,
  AlignLeft, AlignCenter, AlignRight, Undo, Redo,
} from 'lucide-react';

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

export function RichTextEditor({ value, onChange, onImageUpload, placeholder }: RichTextEditorProps) {
  const { t } = useTranslation();
  const fileInputRef = useRef<HTMLInputElement>(null);

  const editor = useEditor({
    extensions: [
      StarterKit.configure({ heading: { levels: [2, 3, 4] } }),
      Image.configure({ inline: false, HTMLAttributes: { loading: 'lazy' } }),
      Link.configure({ openOnClick: false, autolink: true, HTMLAttributes: { rel: 'noopener noreferrer nofollow', target: '_blank' } }),
      Youtube.configure({ width: 640, height: 360, nocookie: true }),
      TextAlign.configure({ types: ['heading', 'paragraph'] }),
      Placeholder.configure({ placeholder: placeholder ?? '' }),
    ],
    content: value,
    onUpdate: ({ editor }) => onChange(editor.getHTML()),
  });

  // 外部 value 变化(如编辑态加载文章)时同步内容,避免每次 onChange 回写造成光标跳动。
  useEffect(() => {
    if (!editor) return;
    if (value !== editor.getHTML()) {
      editor.commands.setContent(value || '', false);
    }
  }, [value, editor]);

  if (!editor) return null;

  const insertImage = () => fileInputRef.current?.click();

  const handleFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = '';
    if (!file) return;
    try {
      const url = await onImageUpload(file);
      editor.chain().focus().setImage({ src: url }).run();
    } catch {
      // onImageUpload 内部已 toast 错误
    }
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
      <input ref={fileInputRef} type="file" accept="image/*" className="hidden" onChange={handleFile} />
      <EditorContent editor={editor} />
      <style>{`
        .blog-editor .ProseMirror{min-height:360px;padding:16px 18px;outline:none;line-height:1.7;font-size:15px}
        .blog-editor .ProseMirror>*:first-child{margin-top:0}
        .blog-editor .ProseMirror h2{font-size:1.4em;font-weight:700;margin:1.4em 0 .5em}
        .blog-editor .ProseMirror h3{font-size:1.2em;font-weight:600;margin:1.2em 0 .4em}
        .blog-editor .ProseMirror h4{font-size:1.05em;font-weight:600;margin:1em 0 .4em}
        .blog-editor .ProseMirror p{margin:.6em 0}
        .blog-editor .ProseMirror ul,.blog-editor .ProseMirror ol{padding-left:1.4em;margin:.6em 0}
        .blog-editor .ProseMirror blockquote{border-left:3px solid var(--border);padding-left:1em;color:var(--ag-text-tertiary,#888);margin:.8em 0}
        .blog-editor .ProseMirror pre{background:var(--ag-surface,#f4f4f5);border-radius:8px;padding:12px 14px;overflow-x:auto;font-size:.9em}
        .blog-editor .ProseMirror img{max-width:100%;height:auto;border-radius:8px}
        .blog-editor .ProseMirror iframe{max-width:100%;aspect-ratio:16/9;width:100%;border:0;border-radius:8px}
        .blog-editor .ProseMirror a{color:var(--ag-primary,#2563eb);text-decoration:underline}
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

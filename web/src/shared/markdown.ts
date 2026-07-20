import { marked } from 'marked';

const BLOCK_MARKDOWN = /(^|\n)\s{0,3}(#{1,6}\s+|(?:[-+*]|\d+[.)])\s+|>\s+|`{3,}|~{3,}|(?:-{3,}|\*{3,})\s*$)/m;
const GFM_TABLE = /(^|\n)\s*\|?.+\|.+\n\s*\|?\s*:?-{3,}:?\s*\|/m;
const INLINE_MARKDOWN = /(\*\*[^*\n]+\*\*|__[^_\n]+__|\*[^*\n]+\*|_[^_\n]+_|~~[^~\n]+~~|`[^`\n]+`|!?\[[^\]\n]+\]\((?:https?:\/\/|\/|#|mailto:)[^)\s]+\)|<https?:\/\/[^>\s]+>)/;

/** Conservative detection: normal prose remains prose; recognizable Markdown is converted. */
export function looksLikeMarkdown(value: string): boolean {
  const text = value.trim();
  return text !== '' && (BLOCK_MARKDOWN.test(text) || GFM_TABLE.test(text) || INLINE_MARKDOWN.test(text));
}

/** Convert GFM Markdown to editor HTML. Backend sanitization remains the final trust boundary. */
export function markdownToHTML(value: string): string {
  const html = marked.parse(value, { async: false, gfm: true, breaks: false });
  return typeof html === 'string' ? html.trim() : '';
}

/** Raw Markdown loaded from an older draft is normalized before Tiptap parses it. */
export function richTextInputToHTML(value: string): string {
  if (!value || /<[a-z][\s\S]*>/i.test(value) || !looksLikeMarkdown(value)) return value;
  return markdownToHTML(value);
}

import { marked } from 'marked';

const BLOCK_MARKDOWN = /(^|\n)\s{0,3}(#{1,6}\s+|(?:[-+*]|\d+[.)])\s+|>\s+|`{3,}|~{3,}|(?:-{3,}|\*{3,})\s*$)/m;
const GFM_TABLE = /(^|\n)\s*\|?.+\|.+\n(?:[ \t]*\n)*\s*\|?\s*:?-{3,}:?\s*\|/m;
const INLINE_MARKDOWN = /(\*\*[^*\n]+\*\*|~~[^~\n]+~~|`[^`\n]+`|\[[^\]\n]+\]\(https?:\/\/[^)\s]+\))/;

/** Detect tables separately so a clipboard's rich HTML cannot hide GFM source. */
export function looksLikeMarkdownTable(value: string): boolean {
  return GFM_TABLE.test(value.trim());
}

/** Some editors put an empty line between every copied table row; GFM does not. */
export function normalizeMarkdownTableSpacing(value: string): string {
  let normalized = value;
  let previous = '';
  const blankBetweenPipeRows = /(^|\n)([ \t]*\|[^\n]*\|)[ \t]*\n(?:[ \t]*\n)+(?=[ \t]*\|[^\n]*\|)/g;
  while (normalized !== previous) {
    previous = normalized;
    normalized = normalized.replace(blankBetweenPipeRows, '$1$2\n');
  }
  return normalized;
}

/** Conservative detection: normal prose remains prose; recognizable Markdown is converted. */
export function looksLikeMarkdown(value: string): boolean {
  const text = value.trim();
  return text !== '' && (BLOCK_MARKDOWN.test(text) || looksLikeMarkdownTable(text) || INLINE_MARKDOWN.test(text));
}

/** Convert GFM Markdown to editor HTML. Backend sanitization remains the final trust boundary. */
export function markdownToHTML(value: string): string {
  const html = marked.parse(normalizeMarkdownTableSpacing(value), { async: false, gfm: true, breaks: false });
  return typeof html === 'string' ? html.trim() : '';
}

/** Raw Markdown loaded from an older draft is normalized before Tiptap parses it. */
export function richTextInputToHTML(value: string): string {
  if (!value || /<[a-z][\s\S]*>/i.test(value) || !looksLikeMarkdown(value)) return value;
  return markdownToHTML(value);
}

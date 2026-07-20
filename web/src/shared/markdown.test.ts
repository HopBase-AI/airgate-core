import { describe, expect, it } from 'vitest';
import { looksLikeMarkdown, markdownToHTML, richTextInputToHTML } from './markdown';

describe('Markdown compatibility', () => {
  it('recognizes block and inline Markdown without converting ordinary prose', () => {
    expect(looksLikeMarkdown('普通的一段文字，包含 - 但不是列表')).toBe(false);
    expect(looksLikeMarkdown('## Heading\n\n- first')).toBe(true);
    expect(looksLikeMarkdown('Use **bold** and `code`.')).toBe(true);
  });

  it('renders headings, lists, quote, code fence, links and GFM table', () => {
    const html = markdownToHTML([
      '## Heading', '', '**bold** and [link](https://example.com)', '',
      '- first', '- second', '', '> quoted', '',
      '```ts', 'const answer = 42', '```', '',
      '| Name | Value |', '| --- | ---: |', '| A | 1 |',
    ].join('\n'));
    expect(html).toContain('<h2>Heading</h2>');
    expect(html).toContain('<strong>bold</strong>');
    expect(html).toContain('<a href="https://example.com">link</a>');
    expect(html).toContain('<ul>');
    expect(html).toContain('<blockquote>');
    expect(html).toContain('<pre><code class="language-ts">');
    expect(html).toContain('<table>');
    expect(html).toContain('<th align="right">Value</th>');
  });

  it('normalizes raw Markdown but leaves existing rich HTML untouched', () => {
    expect(richTextInputToHTML('## Imported')).toBe('<h2>Imported</h2>');
    expect(richTextInputToHTML('<p>## Literal text</p>')).toBe('<p>## Literal text</p>');
  });
});

import { describe, expect, it } from 'vitest';
import { safeAuthReturnTo } from './authReturnTo';

describe('safeAuthReturnTo', () => {
  it('allows a public blog on the same first-party site family', () => {
    expect(safeAuthReturnTo(
      'https://essevin.com/blog/article?inv=vip8&lang=en#fragment',
      'https://api.essevin.com',
    )).toBe('https://essevin.com/blog/article?inv=vip8&lang=en');
    expect(safeAuthReturnTo(
      'https://late.essevin.com/blog/article?lang=zh-Hant',
      'https://api.essevin.com',
    )).toBe('https://late.essevin.com/blog/article?lang=zh-Hant');
  });

  it('rejects open redirects, lookalike hosts and non-blog destinations', () => {
    expect(safeAuthReturnTo('https://evil.example/blog/a', 'https://api.essevin.com')).toBe('');
    expect(safeAuthReturnTo('https://essevin.com.evil.example/blog/a', 'https://api.essevin.com')).toBe('');
    expect(safeAuthReturnTo('https://essevin.com/account', 'https://api.essevin.com')).toBe('');
    expect(safeAuthReturnTo('javascript:alert(1)', 'https://api.essevin.com')).toBe('');
  });
});

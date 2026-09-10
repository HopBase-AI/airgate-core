import { describe, expect, it } from 'vitest';
import { isMenuPathActive } from './menuActive';

// 菜单项样本:/team 与 /team/audit 是父子兄弟项,/admin/blog 的编辑页不在菜单里。
const MENU = ['/', '/usage', '/keys', '/team', '/team/audit', '/admin/blog'];

describe('isMenuPathActive', () => {
  it('lights the parent only on its own path', () => {
    expect(isMenuPathActive('/team', '/team', MENU)).toBe(true);
    expect(isMenuPathActive('/team', '/team/audit', MENU)).toBe(false);
  });

  it('lights the longer child on the child path', () => {
    expect(isMenuPathActive('/team/audit', '/team/audit', MENU)).toBe(true);
    expect(isMenuPathActive('/team/audit', '/team', MENU)).toBe(false);
  });

  it('never lights root on other pages', () => {
    expect(isMenuPathActive('/', '/', MENU)).toBe(true);
    expect(isMenuPathActive('/', '/usage', MENU)).toBe(false);
  });

  it('keeps the parent active for sub-pages that are not in the menu', () => {
    expect(isMenuPathActive('/admin/blog', '/admin/blog/edit', MENU)).toBe(true);
  });

  // router 未开 caseSensitive,/Team/Audit 照样渲染;高亮必须跟着走
  it('matches case-insensitively like the router does', () => {
    expect(isMenuPathActive('/team/audit', '/Team/Audit', MENU)).toBe(true);
    expect(isMenuPathActive('/team', '/Team/Audit', MENU)).toBe(false);
  });
});

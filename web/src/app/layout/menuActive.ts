// 侧栏高亮:fuzzy 前缀匹配会让 /team 在 /team/audit 上也亮;当有更长的菜单项命中同一路径时,
// 只让最长的那一项亮(路由里存在但不在菜单里的子页,如 /admin/blog/edit,仍由父项承接)。
export function isMenuPathActive(itemPath: string, currentPath: string, allPaths: string[]): boolean {
  // router 未开 caseSensitive,/Team 一样能渲染出页面;这里跟随它按小写比,否则高亮会掉
  const current = currentPath.toLowerCase();
  const hits = (raw: string) => {
    const path = raw.toLowerCase();
    return path === '/' ? current === '/' : current === path || current.startsWith(`${path}/`);
  };
  if (!hits(itemPath)) return false;
  return !allPaths.some((path) => path.length > itemPath.length && hits(path));
}

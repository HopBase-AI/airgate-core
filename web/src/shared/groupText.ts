// 分组展示文案本地化：分组 name/note 是后台自由文本(zh 基准),
// 多语言覆盖存于 name_i18n/note_i18n(键=语言码 en / zh-HK / ja)。
// 按当前 i18next 语言精确匹配,miss(含 zh 基准语言/空白覆盖)回退基准文案。

export function localizedGroupText(
  base: string,
  i18nMap: Record<string, string> | undefined | null,
  lang: string,
): string {
  const text = i18nMap?.[lang];
  if (text && text.trim() !== '') return text;
  // 非中文界面缺当前语言覆盖时,先回退英文覆盖再回退基准文案(基准名多为中文)
  if (lang !== 'zh' && lang !== 'zh-HK') {
    const enText = i18nMap?.['en'];
    if (enText && enText.trim() !== '') return enText;
  }
  return base;
}

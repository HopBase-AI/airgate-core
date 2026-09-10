package i18n

import "testing"

func TestTUsesLanguageThenDefaultThenKey(t *testing.T) {
	restoreTranslations := replaceTranslationsForTest(map[string]map[string]string{
		"zh": {"hello": "你好"},
		"en": {"hello": "Hello"},
	}, "zh")
	defer restoreTranslations()

	if got := T("en", "hello"); got != "Hello" {
		t.Fatalf("英文翻译 = %q，期望 Hello", got)
	}
	if got := T("fr", "hello"); got != "你好" {
		t.Fatalf("缺失语言应回退默认语言，得到 %q", got)
	}
	if got := T("en", "missing"); got != "missing" {
		t.Fatalf("缺失 key 应回退 key 本身，得到 %q", got)
	}
}

func TestLoadEmbeddedLoadsDefaultLocales(t *testing.T) {
	restoreTranslations := replaceTranslationsForTest(map[string]map[string]string{}, "zh")
	defer restoreTranslations()

	if err := LoadEmbedded(); err != nil {
		t.Fatalf("加载嵌入翻译失败: %v", err)
	}

	mu.RLock()
	defer mu.RUnlock()
	if len(translations["zh"]) == 0 || len(translations["en"]) == 0 {
		t.Fatalf("嵌入翻译未加载完整: %+v", translations)
	}
}

func replaceTranslationsForTest(next map[string]map[string]string, nextDefault string) func() {
	mu.Lock()
	oldTranslations := translations
	oldDefault := defaultLang
	translations = next
	defaultLang = nextDefault
	mu.Unlock()

	return func() {
		mu.Lock()
		translations = oldTranslations
		defaultLang = oldDefault
		mu.Unlock()
	}
}

func TestDetectLanguageAndClientLangDefaults(t *testing.T) {
	cases := map[string]string{
		"":                           "en",
		"es-MX,es;q=0.9":             "es",
		"zh-CN,zh;q=0.9":             "zh",
		"zh-HK":                      "zh-HK",
		"zh-TW,zh;q=0.8":             "zh-HK",
		"ja,en-US;q=0.7":             "ja",
		"fr-FR,fr;q=0.9":             "en",
		"*":                          "en",
		"en-GB,en;q=0.9,zh;q=0.8":    "en",
		"de-DE,es-ES;q=0.5,en;q=0.3": "es",
	}
	for header, want := range cases {
		if got := DetectLanguage(header, LangEN); got != want {
			t.Errorf("DetectLanguage(%q) = %q, want %q", header, got, want)
		}
	}
	if got := DetectLanguage("", "zh"); got != "zh" {
		t.Errorf("空头应回退 fallback，得到 %q", got)
	}
	if got := ClientLang(nil); got != LangEN {
		t.Errorf("ClientLang(nil) = %q, want en", got)
	}
}

func TestLocalesShareIdenticalKeySets(t *testing.T) {
	restoreTranslations := replaceTranslationsForTest(map[string]map[string]string{}, "zh")
	defer restoreTranslations()
	if err := LoadEmbedded(); err != nil {
		t.Fatal(err)
	}
	langs := SupportedLanguages()
	for _, want := range []string{"zh", "en", "es", "ja", "zh-HK"} {
		if !Has(want, "gw.no_available_account") {
			t.Errorf("locales/%s.json 缺失或缺少 gw.no_available_account（已加载: %v）", want, langs)
		}
	}
	mu.RLock()
	defer mu.RUnlock()
	for lang, msgs := range translations {
		for key := range translations["en"] {
			if _, ok := msgs[key]; !ok {
				t.Errorf("locales/%s.json 缺少 key %s", lang, key)
			}
		}
		for key := range msgs {
			if _, ok := translations["en"][key]; !ok {
				t.Errorf("locales/%s.json 多出 en 没有的 key %s", lang, key)
			}
		}
	}
}

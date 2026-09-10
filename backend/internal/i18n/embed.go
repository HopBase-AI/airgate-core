package i18n

import (
	"embed"
	"encoding/json"
	"log/slog"
	"path"
	"strings"
)

//go:embed locales/*.json
var localeFS embed.FS

// LoadEmbedded 从二进制内嵌的 locales/*.json 加载翻译。
// 与 Load(dir) 不同的是，调用方不需要确保运行目录下存在 locales/ 目录 —— 这
// 是裸金属 install.sh / systemd 部署能正常工作的前提。
//
// 目录下每个 <lang>.json 都会被加载，新增语言只需放文件，不必改代码。
func LoadEmbedded() error {
	entries, err := localeFS.ReadDir("locales")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, err := localeFS.ReadFile(path.Join("locales", name))
		if err != nil {
			slog.Warn("加载嵌入翻译文件失败", "file", name, "error", err)
			continue
		}
		var msgs map[string]string
		if err := json.Unmarshal(data, &msgs); err != nil {
			slog.Warn("解析嵌入翻译文件失败", "file", name, "error", err)
			continue
		}
		lang := strings.TrimSuffix(name, ".json")
		mu.Lock()
		translations[lang] = msgs
		mu.Unlock()
		slog.Info("加载嵌入翻译", "lang", lang, "keys", len(msgs))
	}
	return nil
}

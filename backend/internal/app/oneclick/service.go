package oneclick

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"text/template"

	"github.com/redis/go-redis/v9"

	appapikey "github.com/DouDOU-start/airgate-core/internal/app/apikey"
	appsettings "github.com/DouDOU-start/airgate-core/internal/app/settings"
	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// KeyRevealer 按归属还原 API Key 明文的能力，由 apikey.Service 提供。
// 收窄成接口便于测试注入假实现。
type KeyRevealer interface {
	RevealOwned(ctx context.Context, userID, id int) (appapikey.Key, error)
}

// Service 一键接入领域服务。
type Service struct {
	rdb      *redis.Client
	apikeys  KeyRevealer
	settings *appsettings.Service
}

// NewService 创建一键接入服务。rdb 可为 nil（未配置 Redis 时相关端点返回不可用）。
func NewService(rdb *redis.Client, apikeys KeyRevealer, settings *appsettings.Service) *Service {
	return &Service{rdb: rdb, apikeys: apikeys, settings: settings}
}

// Config 渲染脚本与命令所需的站点信息。
type Config struct {
	BaseURL  string // 网关对外地址（不含 /v1 后缀）；为空时由 handler 按请求 Host 推导
	SiteName string
}

// Load 读取 site 分组设置组装 Config。
//
// BaseURL 优先级：site.api_base_url（用户密钥实际指向的网关地址）→ site.site_base_url。
// 两者都未配置时返回空串，由 handler 按请求 Host 推导兜底（与 openclaw 域同款约定）。
func (s *Service) Load(ctx context.Context) (Config, error) {
	cfg := Config{SiteName: "HopBase"}
	items, err := s.settings.List(ctx, "site")
	if err != nil {
		sdk.LoggerFromContext(ctx).Error("oneclick_load_settings_failed", sdk.LogFieldError, err)
		return cfg, err
	}
	var siteBase string
	for _, it := range items {
		switch it.Key {
		case "site_name":
			if v := strings.TrimSpace(it.Value); v != "" {
				cfg.SiteName = v
			}
		case "api_base_url":
			cfg.BaseURL = strings.TrimRight(strings.TrimSpace(it.Value), "/")
		case "site_base_url":
			siteBase = strings.TrimRight(strings.TrimSpace(it.Value), "/")
		}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = siteBase
	}
	return cfg, nil
}

// tokenRecord Redis 中的令牌记录。不存 key 明文，兑换时按 user_id/key_id 即时解密。
type tokenRecord struct {
	UserID int    `json:"user_id"`
	KeyID  int    `json:"key_id"`
	Status string `json:"status"`
}

func setupTokenKey(token string) string {
	return setupTokenKeyPrefix + token
}

// IssueToken 为用户名下的指定 API Key 签发一次性接入令牌。
//
// 签发前先走一次 RevealOwned：既校验归属，也提前暴露"legacy key 不可明文还原"
// 这类问题——让用户在控制台立刻看到错误，而不是复制命令跑到终端才失败。
func (s *Service) IssueToken(ctx context.Context, userID, keyID int) (token string, err error) {
	if s.rdb == nil {
		return "", ErrRedisUnavailable
	}
	if _, err := s.apikeys.RevealOwned(ctx, userID, keyID); err != nil {
		return "", err
	}

	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token = hex.EncodeToString(buf)

	raw, err := json.Marshal(tokenRecord{UserID: userID, KeyID: keyID, Status: StatusPending})
	if err != nil {
		return "", err
	}
	if err := s.rdb.Set(ctx, setupTokenKey(token), raw, setupTokenTTL).Err(); err != nil {
		sdk.LoggerFromContext(ctx).Error("oneclick_token_store_failed", sdk.LogFieldError, err)
		return "", err
	}
	return token, nil
}

// ExchangeResult 兑换结果。
type ExchangeResult struct {
	APIKey  string
	KeyName string
}

// Exchange 用令牌兑换 API Key 明文。单次语义：GETDEL 原子摘下记录，
// 状态非 pending 或解密失败都不会把记录放回（令牌一经触碰即作废，防重放）。
func (s *Service) Exchange(ctx context.Context, token string) (ExchangeResult, error) {
	if s.rdb == nil {
		return ExchangeResult{}, ErrRedisUnavailable
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return ExchangeResult{}, ErrTokenNotFound
	}

	raw, err := s.rdb.GetDel(ctx, setupTokenKey(token)).Result()
	if err == redis.Nil {
		return ExchangeResult{}, ErrTokenNotFound
	}
	if err != nil {
		sdk.LoggerFromContext(ctx).Error("oneclick_token_load_failed", sdk.LogFieldError, err)
		return ExchangeResult{}, err
	}
	var rec tokenRecord
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		return ExchangeResult{}, ErrTokenNotFound
	}
	if rec.Status != StatusPending {
		return ExchangeResult{}, ErrTokenState
	}

	item, err := s.apikeys.RevealOwned(ctx, rec.UserID, rec.KeyID)
	if err != nil {
		return ExchangeResult{}, err
	}

	rec.Status = StatusExchanged
	if buf, err := json.Marshal(rec); err == nil {
		// 回写 exchanged 状态供控制台轮询与 verify 回执使用；写失败不影响本次兑换。
		if serr := s.rdb.Set(ctx, setupTokenKey(token), buf, setupTokenTTL).Err(); serr != nil {
			sdk.LoggerFromContext(ctx).Warn("oneclick_token_update_failed", sdk.LogFieldError, serr)
		}
	}
	return ExchangeResult{APIKey: item.PlainKey, KeyName: item.Name}, nil
}

// Verify 脚本自检通过后的回执：exchanged → verified。
func (s *Service) Verify(ctx context.Context, token string) error {
	if s.rdb == nil {
		return ErrRedisUnavailable
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrTokenNotFound
	}

	key := setupTokenKey(token)
	raw, err := s.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return ErrTokenNotFound
	}
	if err != nil {
		return err
	}
	var rec tokenRecord
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		return ErrTokenNotFound
	}
	if rec.Status != StatusExchanged {
		return ErrTokenState
	}
	rec.Status = StatusVerified
	buf, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, key, buf, setupTokenTTL).Err()
}

// Status 查询令牌状态，供签发者（控制台）轮询。
// 只有签发时的同一用户可见；不存在/越权一律返回 expired，不泄露令牌存在性。
func (s *Service) Status(ctx context.Context, userID int, token string) string {
	if s.rdb == nil || strings.TrimSpace(token) == "" {
		return StatusExpired
	}
	raw, err := s.rdb.Get(ctx, setupTokenKey(strings.TrimSpace(token))).Result()
	if err != nil {
		return StatusExpired
	}
	var rec tokenRecord
	if err := json.Unmarshal([]byte(raw), &rec); err != nil || rec.UserID != userID {
		return StatusExpired
	}
	return rec.Status
}

// scriptTemplateData 两份接入脚本模板共享的变量。
type scriptTemplateData struct {
	BaseURL  string
	SiteName string
}

// RenderSetupScript 渲染 bash 接入脚本。
func (s *Service) RenderSetupScript(cfg Config) (string, error) {
	return renderScript("setup.sh", SetupScriptTemplate(), cfg)
}

// RenderSetupScriptPowerShell 渲染 PowerShell 接入脚本。
func (s *Service) RenderSetupScriptPowerShell(cfg Config) (string, error) {
	return renderScript("setup.ps1", SetupScriptPowerShellTemplate(), cfg)
}

// RenderSetupCodexScript 渲染 Codex CLI bash 接入脚本。
func (s *Service) RenderSetupCodexScript(cfg Config) (string, error) {
	return renderScript("setup-codex.sh", SetupCodexScriptTemplate(), cfg)
}

// RenderSetupCodexScriptPowerShell 渲染 Codex CLI PowerShell 接入脚本。
func (s *Service) RenderSetupCodexScriptPowerShell(cfg Config) (string, error) {
	return renderScript("setup-codex.ps1", SetupCodexScriptPowerShellTemplate(), cfg)
}

func renderScript(name, tmpl string, cfg Config) (string, error) {
	tpl, err := template.New(name).Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, scriptTemplateData(cfg)); err != nil {
		return "", err
	}
	return buf.String(), nil
}

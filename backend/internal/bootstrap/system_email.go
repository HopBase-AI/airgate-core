package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"

	appsettings "github.com/DouDOU-start/airgate-core/internal/app/settings"
	"github.com/DouDOU-start/airgate-core/internal/infra/mailer"
	"github.com/DouDOU-start/airgate-core/internal/infra/store"
)

// loadSMTPConfig 从系统设置读 SMTP 配置；未配置 host 返回错误。
func loadSMTPConfig(ctx context.Context, settingsService *appsettings.Service) (mailer.Config, error) {
	settings, err := settingsService.List(ctx, "smtp")
	if err != nil {
		return mailer.Config{}, err
	}
	cfg := mailer.Config{}
	for _, s := range settings {
		switch s.Key {
		case "smtp_host":
			cfg.Host = s.Value
		case "smtp_port":
			cfg.Port, _ = strconv.Atoi(s.Value)
		case "smtp_username":
			cfg.Username = s.Value
		case "smtp_password":
			cfg.Password = s.Value
		case "smtp_from_email":
			cfg.FromAddr = s.Value
		case "smtp_from_name":
			cfg.FromName = s.Value
		case "smtp_use_tls":
			cfg.UseTLS = s.Value == "true"
		}
	}
	if cfg.Host == "" {
		return mailer.Config{}, fmt.Errorf("SMTP 未配置")
	}
	if cfg.Port == 0 {
		cfg.Port = 587
	}
	return cfg, nil
}

// loadSiteName 站点名称（缺省 HopBase）。
func loadSiteName(ctx context.Context, settingsService *appsettings.Service) string {
	siteName := "HopBase"
	siteSettings, _ := settingsService.List(ctx, "site")
	for _, s := range siteSettings {
		if s.Key == "site_name" && s.Value != "" {
			siteName = s.Value
		}
	}
	return siteName
}

// sendSystemEmail 发送一封系统邮件（余额预警 / 额度预警等）：从设置读 SMTP，未配置静默跳过，
// 失败只记日志。调用方负责异步（本函数同步阻塞到 SMTP 返回）。
func sendSystemEmail(settingsService *appsettings.Service, to, subject, htmlBody string) {
	if to == "" {
		return
	}
	ctx := context.Background()
	cfg, err := loadSMTPConfig(ctx, settingsService)
	if err != nil {
		slog.Warn("mail_disabled_no_config", "context", "system_email", "subject", subject, sdk.LogFieldError, err)
		return
	}
	if err := mailer.New(cfg).Send(to, subject, htmlBody); err != nil {
		slog.Error("system_email_failed", "to_hash", store.EmailHash(to), "subject", subject, sdk.LogFieldError, err)
		return
	}
	slog.Info("system_email_sent", "to_hash", store.EmailHash(to), "subject", subject)
}

package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/redis/go-redis/v9"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"

	"github.com/DouDOU-start/airgate-core/ent"
	appaccount "github.com/DouDOU-start/airgate-core/internal/app/account"
	appaccountevent "github.com/DouDOU-start/airgate-core/internal/app/accountevent"
	appapikey "github.com/DouDOU-start/airgate-core/internal/app/apikey"
	appaudit "github.com/DouDOU-start/airgate-core/internal/app/audit"
	appauth "github.com/DouDOU-start/airgate-core/internal/app/auth"
	appblog "github.com/DouDOU-start/airgate-core/internal/app/blog"
	appdashboard "github.com/DouDOU-start/airgate-core/internal/app/dashboard"
	appdepartment "github.com/DouDOU-start/airgate-core/internal/app/department"
	appentrycode "github.com/DouDOU-start/airgate-core/internal/app/entrycode"
	appgenerationtask "github.com/DouDOU-start/airgate-core/internal/app/generationtask"
	appgroup "github.com/DouDOU-start/airgate-core/internal/app/group"
	appmcp "github.com/DouDOU-start/airgate-core/internal/app/mcp"
	appmember "github.com/DouDOU-start/airgate-core/internal/app/member"
	appmodelpricing "github.com/DouDOU-start/airgate-core/internal/app/modelpricing"
	appnotification "github.com/DouDOU-start/airgate-core/internal/app/notification"
	apponeclick "github.com/DouDOU-start/airgate-core/internal/app/oneclick"
	appopenclaw "github.com/DouDOU-start/airgate-core/internal/app/openclaw"
	apppluginadmin "github.com/DouDOU-start/airgate-core/internal/app/pluginadmin"
	appproxy "github.com/DouDOU-start/airgate-core/internal/app/proxy"
	appquotaalert "github.com/DouDOU-start/airgate-core/internal/app/quotaalert"
	appreferral "github.com/DouDOU-start/airgate-core/internal/app/referral"
	apprelaydetect "github.com/DouDOU-start/airgate-core/internal/app/relaydetect"
	appsettings "github.com/DouDOU-start/airgate-core/internal/app/settings"
	appsubscription "github.com/DouDOU-start/airgate-core/internal/app/subscription"
	appupstreamalert "github.com/DouDOU-start/airgate-core/internal/app/upstreamalert"
	appusage "github.com/DouDOU-start/airgate-core/internal/app/usage"
	appuser "github.com/DouDOU-start/airgate-core/internal/app/user"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/config"
	"github.com/DouDOU-start/airgate-core/internal/infra/mailer"
	"github.com/DouDOU-start/airgate-core/internal/infra/store"
	"github.com/DouDOU-start/airgate-core/internal/plugin"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
	"github.com/DouDOU-start/airgate-core/internal/server/handler"
	"github.com/DouDOU-start/airgate-core/internal/upgrade"
)

// HTTPDependencies 描述 HTTP 处理器装配所需依赖。
type HTTPDependencies struct {
	Config      *config.Config
	DB          *ent.Client
	Redis       *redis.Client
	JWTMgr      *auth.JWTManager
	PluginMgr   *plugin.Manager
	Marketplace *plugin.Marketplace
	Concurrency *scheduler.ConcurrencyManager
	Scheduler   *scheduler.Scheduler
	// Recorder 计费记录器：额度预警引擎挂其扣费提交回调（须在 Recorder.Start 前装配）。可为 nil。
	Recorder *billing.Recorder
}

// HTTPHandlers 聚合所有 HTTP 处理器。
type HTTPHandlers struct {
	Auth           *handler.AuthHandler
	User           *handler.UserHandler
	Account        *handler.AccountHandler
	Group          *handler.GroupHandler
	APIKey         *handler.APIKeyHandler
	Member         *handler.MemberHandler
	Department     *handler.DepartmentHandler
	Notification   *handler.NotificationHandler
	Subscription   *handler.SubscriptionHandler
	Usage          *handler.UsageHandler
	Proxy          *handler.ProxyHandler
	EntryCode      *handler.EntryCodeHandler
	Settings       *handler.SettingsHandler
	Dashboard      *handler.DashboardHandler
	Plugin         *handler.PluginHandler
	OpenClaw       *handler.OpenClawHandler
	OneClick       *handler.OneClickHandler
	Version        *handler.VersionHandler
	Upgrade        *handler.UpgradeHandler
	RelayDetection *handler.RelayDetectionHandler
	AccountEvent   *handler.AccountEventHandler
	GenerationTask *handler.GenerationTaskHandler
	Referral       *handler.ReferralHandler
	ModelPricing   *handler.ModelPricingHandler
	MCP            *handler.MCPHandler
	Blog           *handler.BlogHandler
	Pricing        *handler.PricingHandler

	AccountService *appaccount.Service
	// BlogService 供公开 SSR 博客页(server 包)复用同一份博客用例。
	BlogService     *appblog.Service
	SettingsService *appsettings.Service
}

// NewHTTPHandlers 统一构造 HTTP 处理器。
func NewHTTPHandlers(dep HTTPDependencies) *HTTPHandlers {
	auditService := appaudit.NewService(store.NewTeamAuditStore(dep.DB))
	apiKeyStore := store.NewAPIKeyStore(dep.DB)
	apiKeyService := appapikey.NewService(apiKeyStore, dep.Config.APIKeySecret())
	apiKeyService.SetAudit(auditService)
	memberStore := store.NewMemberStore(dep.DB)
	memberService := appmember.NewService(memberStore, auditService)
	departmentService := appdepartment.NewService(store.NewDepartmentStore(dep.DB), auditService)
	authStore := store.NewAuthStore(dep.DB)
	auth.SetAPIKeyCacheRedis(dep.Redis)
	authService := appauth.NewService(authStore, dep.JWTMgr)
	verifyCodeStore := mailer.NewVerifyCodeStore()
	// 设置和验证码依赖延迟到 settingsService 创建后注入
	accountStore := store.NewAccountStore(dep.DB)
	accountService := appaccount.NewService(accountStore, dep.PluginMgr, dep.Concurrency, dep.Scheduler)
	accountService.SetUsageCacheRedis(dep.Redis)
	groupStore := store.NewGroupStore(dep.DB)
	groupService := appgroup.NewService(groupStore, dep.Concurrency)
	proxyStore := store.NewProxyStore(dep.DB)
	proxyService := appproxy.NewService(proxyStore)
	subscriptionStore := store.NewSubscriptionStore(dep.DB)
	subscriptionService := appsubscription.NewService(subscriptionStore)
	dashboardStore := store.NewDashboardStore(dep.DB, dep.Redis)
	dashboardService := appdashboard.NewService(dashboardStore, dep.Redis)
	pluginAdminService := apppluginadmin.NewService(dep.PluginMgr, dep.Marketplace)
	settingsStore := store.NewSettingsStore(dep.DB)
	settingsService := appsettings.NewService(settingsStore, dep.Config.APIKeySecret())
	openclawService := appopenclaw.NewService(settingsService)
	oneclickService := apponeclick.NewService(dep.Redis, apiKeyService, settingsService)
	relayDetectionService := apprelaydetect.NewService(dep.DB, dep.Config.APIKeySecret())
	accountEventStore := store.NewAccountEventStore(dep.DB)
	accountEventService := appaccountevent.NewService(accountEventStore)
	generationTaskStore := store.NewGenerationTaskStore(dep.DB)
	generationTaskService := appgenerationtask.NewService(generationTaskStore)
	blogStore := store.NewBlogStore(dep.DB)
	blogService := appblog.NewService(blogStore)

	// 公开定价投影需要读模型目录覆盖层（settings group=models 的 models.catalog.<platform>）
	pluginAdminService.SetModelOverlayReader(func(ctx context.Context, platform string) (string, error) {
		items, err := settingsService.List(ctx, "models")
		if err != nil {
			return "", err
		}
		wanted := "models.catalog." + platform
		for _, item := range items {
			if item.Key == wanted {
				return item.Value, nil
			}
		}
		return "", nil
	})

	// 注入 auth 服务的设置/验证码/邮件依赖
	authService.SetSettingsLister(&settingsAdapter{settingsService})
	authService.SetVerifyCodeStore(verifyCodeStore)
	authService.SetMailerFactory(buildMailerFactory(settingsService))

	userStore := store.NewUserStore(dep.DB)
	userService := appuser.NewService(userStore)

	// 站内通知 + 额度预警引擎：Recorder 扣费提交后复核本批成员 / 部门，达阈值投站内信 + 邮件。
	notificationService := appnotification.NewService(store.NewNotificationStore(dep.DB))
	quotaAlertService := appquotaalert.NewService(store.NewQuotaAlertStore(dep.DB), notificationService)
	quotaAlertService.SetEmailSender(func(to, subject, body string) {
		sendSystemEmail(settingsService, to, subject, body)
	})
	if dep.Recorder != nil {
		dep.Recorder.SetChargeHook(quotaAlertService.OnCharged)
	}

	// 上游欠费预警：账号因「我们欠上游钱」不可用时，给全部管理员投站内信。
	// 单独做是因为这一类兜不住——上游挂了、限流、凭证失效换个号就能绕过，欠费不会自愈，
	// 池子里的号还会接连欠。2026-09-10 组 1 的锦坤东欠了 9.5 小时没人知道，客户一直 502。
	if dep.Scheduler != nil {
		upstreamAlertService := appupstreamalert.NewService(store.NewUpstreamAlertStore(dep.DB), notificationService)
		dep.Scheduler.SetAccountEventHook(func(accountID int, reason string, upstreamStatus int) {
			upstreamAlertService.OnAccountEvent(context.Background(), accountID, reason, upstreamStatus)
		})
	}

	// 余额预警回调：发邮件（去重靠 balance_alert_notified 标记，余额回升自动重置）+ 投站内通知。
	userService.SetBalanceAlertCallback(func(userID int, email string, balance float64, threshold float64) {
		balanceAlertNotify(context.Background(), notificationService, userID, balance, threshold)
		balanceAlertSendEmail(settingsService, email, balance, threshold)
	})
	usageStore := store.NewUsageStore(dep.DB)
	usageService := appusage.NewService(usageStore, dep.Redis)

	// 分销返利：入账复用 userService（含余额预警回调），配置读 settings referral 分组
	referralService := appreferral.NewService(store.NewReferralStore(dep.DB), userService, settingsService)

	// 用户实付价视图：模型目录（含覆盖层）× 可用分组 × 用户专属倍率
	modelPricingService := appmodelpricing.NewService(pluginAdminService, groupStore, userService, apiKeyStore)
	// MCP 管理面:key 验证经 auth 包完成(app 层不 import ent),经闭包注入 DB。
	mcpService := appmcp.NewService(func(ctx context.Context, rawKey string) (*auth.APIKeyInfo, error) {
		return auth.ValidateAPIKeyForManagement(ctx, dep.DB, rawKey)
	}, modelPricingService, usageService)

	upgradeService := upgrade.NewService(upgrade.DetectMode(), dep.Redis)

	return &HTTPHandlers{
		Auth:           handler.NewAuthHandler(authService, dep.JWTMgr),
		User:           handler.NewUserHandler(userService, settingsService),
		Account:        handler.NewAccountHandler(accountService, dep.Scheduler),
		Group:          handler.NewGroupHandler(groupService),
		Pricing:        handler.NewPricingHandler(groupService, userService, accountService),
		APIKey:         handler.NewAPIKeyHandler(apiKeyService),
		Member:         handler.NewMemberHandler(memberService),
		Department:     handler.NewDepartmentHandler(departmentService, auditService),
		Notification:   handler.NewNotificationHandler(notificationService),
		Subscription:   handler.NewSubscriptionHandler(subscriptionService),
		Usage:          handler.NewUsageHandler(usageService),
		Proxy:          handler.NewProxyHandler(proxyService),
		EntryCode:      handler.NewEntryCodeHandler(appentrycode.NewService(settingsService, userService)),
		Settings:       handler.NewSettingsHandler(settingsService),
		Dashboard:      handler.NewDashboardHandler(dashboardService),
		Plugin:         handler.NewPluginHandler(pluginAdminService),
		OpenClaw:       handler.NewOpenClawHandler(openclawService),
		OneClick:       handler.NewOneClickHandler(oneclickService),
		Version:        handler.NewVersionHandler(),
		Upgrade:        handler.NewUpgradeHandler(upgradeService),
		RelayDetection: handler.NewRelayDetectionHandler(relayDetectionService),
		AccountEvent:   handler.NewAccountEventHandler(accountEventService),
		GenerationTask: handler.NewGenerationTaskHandler(generationTaskService),
		Referral:       handler.NewReferralHandler(referralService),
		ModelPricing:   handler.NewModelPricingHandler(modelPricingService),
		MCP:            handler.NewMCPHandler(mcpService),
		Blog:           handler.NewBlogHandler(blogService),
		AccountService: accountService,

		BlogService:     blogService,
		SettingsService: settingsService,
	}
}

// settingsAdapter 将 appsettings.Service 适配为 appauth.SettingsLister 接口。
type settingsAdapter struct {
	svc *appsettings.Service
}

func (a *settingsAdapter) List(ctx context.Context, group string) ([]appauth.Setting, error) {
	items, err := a.svc.List(ctx, group)
	if err != nil {
		return nil, err
	}
	result := make([]appauth.Setting, len(items))
	for i, item := range items {
		result[i] = appauth.Setting{Key: item.Key, Value: item.Value}
	}
	return result, nil
}

// buildMailerFactory 返回一个从系统设置构建邮件发送器的工厂函数。
func buildMailerFactory(settingsService *appsettings.Service) appauth.MailSenderFactory {
	return func(ctx context.Context) (appauth.MailSender, error) {
		cfg, err := loadSMTPConfig(ctx, settingsService)
		if err != nil {
			return nil, err
		}
		return mailer.New(cfg), nil
	}
}

// defaultBalanceAlertBody 余额预警邮件默认正文模板。
const defaultBalanceAlertBody = `<div style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; max-width: 420px; margin: 0 auto; background: #ffffff; border-radius: 8px; border: 1px solid #e5e7eb;">
<div style="padding: 32px 28px;">
<div style="font-size: 16px; font-weight: 600; color: #111; margin-bottom: 20px;">{{site_name}}</div>
<p style="color: #555; font-size: 14px; line-height: 1.6; margin: 0 0 16px;">您的账户余额已低于预警阈值：</p>
<div style="background: #fef3c7; border: 1px solid #fde68a; border-radius: 8px; padding: 16px; margin-bottom: 20px;">
<div style="display: flex; justify-content: space-between; margin-bottom: 8px;">
<span style="color: #92400e; font-size: 13px;">当前余额</span>
<span style="color: #92400e; font-size: 16px; font-weight: 700;">{{balance}}</span>
</div>
<div style="display: flex; justify-content: space-between;">
<span style="color: #92400e; font-size: 13px;">预警阈值</span>
<span style="color: #92400e; font-size: 13px;">{{threshold}}</span>
</div>
</div>
<p style="color: #999; font-size: 12px; line-height: 1.6; margin: 0;">请及时充值以免影响正常使用。余额回到阈值以上后，预警将自动重置。</p>
</div>
<div style="border-top: 1px solid #f0f0f0; padding: 14px 28px;">
<p style="color: #c0c0c0; font-size: 11px; margin: 0; text-align: center;">此邮件由 {{site_name}} 系统自动发送</p>
</div>
</div>`

// balanceAlertNotify 余额预警站内通知（与邮件同一触发点；去重由 users.balance_alert_notified 承担，
// 余额回升会重置标记，因此不再额外设 dedupe_key）。
func balanceAlertNotify(ctx context.Context, notifications *appnotification.Service, userID int, balance, threshold float64) {
	if notifications == nil || userID <= 0 {
		return
	}
	_, err := notifications.Create(ctx, appnotification.CreateInput{
		UserID:  userID,
		Kind:    appnotification.KindBalanceAlert,
		Level:   appnotification.LevelWarning,
		Title:   fmt.Sprintf("账户余额已低于预警阈值 $%.2f", threshold),
		Content: fmt.Sprintf("当前余额 $%.4f，预警阈值 $%.2f。请及时充值以免影响正常使用；余额回到阈值以上后预警自动重置。", balance, threshold),
		Link:    "/profile",
	})
	if err != nil {
		slog.Error("balance_alert_notify_failed", "user_id", userID, sdk.LogFieldError, err)
	}
}

// balanceAlertSendEmail 发送余额预警邮件：按设置里的自定义模板（或默认模板）渲染后经 sendSystemEmail 发出。
func balanceAlertSendEmail(settingsService *appsettings.Service, email string, balance, threshold float64) {
	ctx := context.Background()
	smtpSettings, err := settingsService.List(ctx, "smtp")
	if err != nil {
		slog.Error("balance_alert_smtp_load_failed", sdk.LogFieldError, err)
		return
	}
	var tplSubject, tplBody string
	for _, s := range smtpSettings {
		switch s.Key {
		case "balance_alert_email_subject":
			tplSubject = s.Value
		case "balance_alert_email_body":
			tplBody = s.Value
		}
	}
	if tplSubject == "" {
		tplSubject = "{{site_name}} - 余额预警"
	}
	if tplBody == "" {
		tplBody = defaultBalanceAlertBody
	}
	replacer := strings.NewReplacer(
		"{{site_name}}", loadSiteName(ctx, settingsService),
		"{{balance}}", fmt.Sprintf("$%.4f", balance),
		"{{threshold}}", fmt.Sprintf("$%.2f", threshold),
	)
	sendSystemEmail(settingsService, email, replacer.Replace(tplSubject), replacer.Replace(tplBody))
}

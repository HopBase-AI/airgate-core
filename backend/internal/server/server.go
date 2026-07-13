// Package server 提供 HTTP 服务器初始化和生命周期管理
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/DouDOU-start/airgate-core/ent"
	entapikey "github.com/DouDOU-start/airgate-core/ent/apikey"
	entuser "github.com/DouDOU-start/airgate-core/ent/user"
	appreferral "github.com/DouDOU-start/airgate-core/internal/app/referral"
	appuser "github.com/DouDOU-start/airgate-core/internal/app/user"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/bootstrap"
	"github.com/DouDOU-start/airgate-core/internal/cluster"
	"github.com/DouDOU-start/airgate-core/internal/config"
	"github.com/DouDOU-start/airgate-core/internal/infra/store"
	"github.com/DouDOU-start/airgate-core/internal/plugin"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
	"github.com/DouDOU-start/airgate-core/internal/server/middleware"
)

// Server HTTP 服务器
type Server struct {
	cfg    *config.Config
	db     *ent.Client
	rdb    *redis.Client
	jwtMgr *auth.JWTManager
	engine *gin.Engine
	srv    *http.Server

	// 插件系统组件
	pluginMgr      *plugin.Manager
	forwarder      *plugin.Forwarder
	marketplace    *plugin.Marketplace
	dynamicRouter  *DynamicRouter
	extensionProxy *plugin.ExtensionProxy

	// 核心服务组件
	scheduler   *scheduler.Scheduler
	concurrency *scheduler.ConcurrencyManager
	calculator  *billing.Calculator
	recorder    *billing.Recorder
	handlers    *bootstrap.HTTPHandlers

	// 中间件组件（需 Shutdown 时释放）
	ipRateLimiter *middleware.IPRateLimiter

	pluginStartCancel context.CancelFunc

	// leader 跨实例领导选举：仅 leader 实例运行全局单例后台循环
	// （插件后台任务、资产迁移/清理、配额刷新），蓝绿/多实例部署时不重复执行。
	leader *cluster.Leader
}

// NewServer 创建 HTTP 服务器
func NewServer(cfg *config.Config, db *ent.Client, rdb *redis.Client) *Server {
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	jwtMgr := auth.NewJWTManager(cfg.JWT.Secret, cfg.JWT.ExpireHour)

	// 核心服务组件
	sched := scheduler.NewScheduler(db, rdb)
	concurrency := scheduler.NewConcurrencyManager(rdb)
	calculator := billing.NewCalculator()
	recorder := billing.NewRecorder(db, 0)
	// 计费 WAL：队列满/落库失败/停机窗口的记录落盘暂存+回放，防丢账。
	// 目录须挂载宿主卷（compose 已配 ./data/billing-wal）。启用失败不致命，退化为旧丢弃行为。
	walDir := cfg.Billing.WALDir
	if walDir == "" {
		walDir = "data/billing-wal"
	}
	if err := recorder.EnableWAL(walDir); err != nil {
		slog.Error("billing_wal_unavailable_fallback_drop", "dir", walDir, "error", err)
	}
	// 负余额兜底：批量扣费后发现透支，立刻失效该用户全部 API Key 的验证缓存，
	// 把"负余额仍可透支"的窗口从缓存 TTL（5s）压到秒级。
	recorder.SetNegativeBalanceHook(func(userIDs []int) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hashes, err := db.APIKey.Query().
			Where(entapikey.HasUserWith(entuser.IDIn(userIDs...))).
			Select(entapikey.FieldKeyHash).
			Strings(ctx)
		if err != nil {
			slog.Error("billing_negative_balance_invalidate_failed", "user_ids", userIDs, "error", err)
			return
		}
		for _, hash := range hashes {
			auth.InvalidateAPIKeyCacheByHash(hash)
		}
		slog.Warn("billing_negative_balance_keys_invalidated", "user_ids", userIDs, "keys", len(hashes))
	})

	// 插件系统组件
	pluginDir := cfg.Plugins.Dir
	if pluginDir == "" {
		pluginDir = "data/plugins"
	}
	pluginMgr := plugin.NewManager(pluginDir, cfg.Log.Level, cfg.Database.DSN(), db)
	// 注入插件目录的模型家族查询：调度器据此优先从插件声明的 Metadata["family"]
	// 获取家族键，替代 scheduler.ModelFamily 中的硬编码 gpt-image 前缀判定。
	sched.SetModelFamilyFunc(pluginMgr.ModelFamily)
	// HostService 通过 hashicorp/go-plugin GRPCBroker 暴露给所有插件子进程，
	// 替代旧的 admin HTTP API + admin_api_key 模式。必须在加载任何插件之前注入。
	// users.update_balance 复用 app/user 服务（独立实例，不挂余额预警邮件回调——
	// 入账只会抬高余额，预警重置逻辑无需回调即可生效）。
	hostUserSvc := appuser.NewService(store.NewUserStore(db))
	// users.notify_topup（充值入账事件）→ 分销返利；settings store 直接满足
	// referral.SettingsReader（与 settings.Service.List 同签名），无需整个设置服务。
	hostReferralSvc := appreferral.NewService(store.NewReferralStore(db), hostUserSvc, store.NewSettingsStore(db))
	pluginMgr.SetHostService(plugin.NewHostService(db, pluginMgr, sched, concurrency, calculator, recorder, hostUserSvc, hostReferralSvc))
	forwarder := plugin.NewForwarder(db, pluginMgr, sched, concurrency, calculator, recorder)

	marketOpts := []plugin.MarketplaceOption{
		plugin.WithGithubToken(cfg.Plugins.Marketplace.GithubToken),
		plugin.WithRefreshInterval(cfg.Plugins.Marketplace.RefreshInterval),
	}
	if entries := convertMarketEntries(cfg.Plugins.Marketplace.Plugins); len(entries) > 0 {
		marketOpts = append(marketOpts, plugin.WithEntries(entries))
	}
	marketplace := plugin.NewMarketplace(pluginDir, marketOpts...)
	dynamicRouter := NewDynamicRouter(forwarder)
	extensionProxy := plugin.NewExtensionProxy(pluginMgr)

	s := &Server{
		cfg:    cfg,
		db:     db,
		rdb:    rdb,
		jwtMgr: jwtMgr,
		// gin.New 不挂默认 Logger/Recovery，由我们的中间件接管以便接入结构化日志
		engine:         gin.New(),
		pluginMgr:      pluginMgr,
		forwarder:      forwarder,
		marketplace:    marketplace,
		dynamicRouter:  dynamicRouter,
		extensionProxy: extensionProxy,
		scheduler:      sched,
		concurrency:    concurrency,
		calculator:     calculator,
		recorder:       recorder,
		leader:         cluster.New(rdb, 30*time.Second),
	}

	s.handlers = bootstrap.NewHTTPHandlers(bootstrap.HTTPDependencies{
		Config:      cfg,
		DB:          db,
		Redis:       rdb,
		JWTMgr:      jwtMgr,
		PluginMgr:   pluginMgr,
		Marketplace: marketplace,
		Concurrency: concurrency,
		Scheduler:   sched,
	})

	// 注册路由
	s.registerRoutes()

	s.srv = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler: s.engine,
	}

	return s
}

// convertMarketEntries 把 config 层 MarketEntry 转换为 plugin 层 MarketplacePlugin
func convertMarketEntries(entries []config.MarketEntry) []plugin.MarketplacePlugin {
	if len(entries) == 0 {
		return nil
	}
	out := make([]plugin.MarketplacePlugin, 0, len(entries))
	for _, e := range entries {
		out = append(out, plugin.MarketplacePlugin{
			Name:        e.Name,
			Description: e.Description,
			Author:      e.Author,
			Type:        e.Type,
			GithubRepo:  e.GithubRepo,
		})
	}
	return out
}

// Start 启动 HTTP 服务器（阻塞）
func (s *Server) Start() error {
	slog.Info("server_listening", "host", s.cfg.Server.Host, "port", s.cfg.Server.Port, "addr", s.srv.Addr)
	if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server_listen_failed", "addr", s.srv.Addr, "error", err)
		return err
	}
	return nil
}

// StartPlugins 启动异步记录器和插件系统
func (s *Server) StartPlugins(ctx context.Context) {
	// 启动使用量异步记录器
	s.recorder.Start()

	pluginCtx, cancel := context.WithCancel(ctx)
	s.pluginStartCancel = cancel

	// 先建立领导选举，再启动单例后台循环：仅 leader 实例真正执行，
	// 蓝绿/多实例期间不会重复轮询上游或重复计费。
	s.leader.Start(pluginCtx)
	s.pluginMgr.SetLeaderFunc(s.leader.IsLeader)

	go plugin.StartAssetMigrationLoop(pluginCtx, s.db, s.leader.IsLeader)
	go plugin.StartAssetCleanupLoop(pluginCtx, s.db, s.leader.IsLeader)
	go scheduler.StartAccountEventCleanupLoop(pluginCtx, s.db, s.leader.IsLeader)

	go func() {
		// 加载已编译的插件。后台执行，避免坏插件阻塞 core 监听端口。
		if err := s.pluginMgr.LoadAll(pluginCtx); err != nil {
			slog.Error("加载插件失败（不影响核心服务）", "error", err)
		}
		if pluginCtx.Err() != nil {
			return
		}

		// 加载开发模式插件（go run 源码）
		for _, dev := range s.cfg.Plugins.Dev {
			if pluginCtx.Err() != nil {
				return
			}
			if err := s.pluginMgr.LoadDev(pluginCtx, dev.Name, dev.Path); err != nil {
				slog.Error("加载开发插件失败", "name", dev.Name, "path", dev.Path, "error", err)
			}
		}

		// 启动统一任务分发器
		if pluginCtx.Err() == nil {
			s.pluginMgr.StartTaskDispatcher(pluginCtx)
		}

		if s.handlers != nil && s.handlers.AccountService != nil && pluginCtx.Err() == nil {
			s.handlers.AccountService.StartQuotaRefreshLoop(pluginCtx, s.leader.IsLeader)
		}

		// 启动插件市场后台同步（默认开启，配置 plugins.marketplace.disabled=true 可关闭）
		if !s.cfg.Plugins.Marketplace.Disabled && pluginCtx.Err() == nil {
			s.marketplace.Start(context.Background())
		}
	}()
}

// Shutdown 优雅关闭服务器
func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("正在关闭服务器...")

	if s.pluginStartCancel != nil {
		s.pluginStartCancel()
	}

	// 主动释放领导租约，让接班实例尽快接管单例后台循环。
	s.leader.Resign(ctx)

	// ⚠️ 顺序关键：必须先排空在途 HTTP 请求，再停插件和记录器。
	// 旧实现先 recorder.Stop() 再 srv.Shutdown()，停机窗口（≤ctx 超时）内完成的
	// 请求会往已 close 的 channel 写账 → panic + 丢账，每次蓝绿切换都会触发。
	err := s.srv.Shutdown(ctx)

	// 停止 IP 限流器后台清理
	if s.ipRateLimiter != nil {
		s.ipRateLimiter.Stop()
	}

	// 停止插件市场后台同步
	if !s.cfg.Plugins.Marketplace.Disabled {
		s.marketplace.Stop()
	}

	// 停止所有插件（在途请求已排空，任务收尾可能仍产生计费记录）
	s.pluginMgr.StopAll(ctx)

	// 最后停使用量记录器：吞掉上面各步收尾产生的记录后再退出
	s.recorder.Stop()

	return err
}

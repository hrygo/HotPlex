package main

import (
	"encoding/json"
	"net/http"
	"slices"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/hrygo/hotplex/internal/admin"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/docs"
	"github.com/hrygo/hotplex/internal/gateway"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/observability"
	"github.com/hrygo/hotplex/internal/security"
)

func setupRoutes(
	mux *http.ServeMux,
	deps *GatewayDeps,
) http.Handler {
	log := deps.Log
	cfg := deps.Config
	hub := deps.Hub
	sm := deps.SessionMgr
	auth := deps.Auth
	handler := deps.Handler
	bridge := deps.Bridge
	configWatcher := deps.ConfigWatcher

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	gatewayAPI := gateway.NewGatewayAPI(log, auth, sm, bridge, deps.ConfigStore, deps.EventStore, deps.EventStore, deps.WorkspaceStore)

	// CORS middleware reads allowed origins from config (supports hot-reload).
	corsOriginsFn := func() []string {
		return deps.ConfigStore.Load().Security.AllowedOrigins
	}
	corsMw := security.CORSMiddleware(corsOriginsFn)

	if slices.Contains(cfg.Security.AllowedOrigins, "*") {
		log.Warn("cors: allowed_origins contains '*' — all origins permitted; set security.allowed_origins in production")
	}

	mux.Handle("GET /api/sessions", corsMw(http.HandlerFunc(gatewayAPI.ListSessions)))
	mux.Handle("POST /api/sessions", corsMw(http.HandlerFunc(gatewayAPI.CreateSession)))
	mux.Handle("GET /api/sessions/{id}", corsMw(http.HandlerFunc(gatewayAPI.GetSession)))
	mux.Handle("DELETE /api/sessions/{id}", corsMw(http.HandlerFunc(gatewayAPI.DeleteSession)))
	mux.Handle("POST /api/sessions/{id}/cd", corsMw(http.HandlerFunc(gatewayAPI.SwitchWorkDir)))
	mux.Handle("GET /api/sessions/{id}/history", corsMw(http.HandlerFunc(gatewayAPI.GetHistory)))
	mux.Handle("GET /api/sessions/{id}/events", corsMw(http.HandlerFunc(gatewayAPI.GetEvents)))
	mux.Handle("OPTIONS /api/sessions", corsMw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
	mux.Handle("OPTIONS /api/sessions/", corsMw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
	mux.Handle("OPTIONS /api/sessions/{id}", corsMw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
	mux.Handle("OPTIONS /api/sessions/{id}/history", corsMw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
	mux.Handle("OPTIONS /api/sessions/{id}/events", corsMw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))

	mux.Handle("GET /ws", hub.HandleHTTP(auth, handler, bridge, deps.CookieAuth))

	sessionAdapter := &sessionManagerAdapter{sm: sm}
	hubAdapter := &hubAdapter{hub: hub}
	bridgeAdapter := &bridgeAdapter{bridge: bridge}
	configAdapter := &configAdapter{cfgStore: deps.ConfigStore}
	configWatcherAdapter := &configWatcherAdapter{watcher: configWatcher}
	turnsAdapter := &turnsStoreAdapter{es: deps.EventStore}

	var cronProvider admin.CronSchedulerProvider
	if deps.CronScheduler != nil {
		cronProvider = &cronAdminAdapter{scheduler: deps.CronScheduler, turnsStore: deps.EventStore}
	}

	adminAPI := admin.New(admin.Deps{
		Log:              log,
		Config:           configAdapter,
		SessionMgr:       sessionAdapter,
		TurnStats:        turnsAdapter,
		Hub:              hubAdapter,
		Bridge:           bridgeAdapter,
		ConfigWatcher:    configWatcherAdapter,
		Cron:             cronProvider,
		BotLister:        &botListerAdapter{registry: messaging.DefaultBotRegistry()},
		BotConfig:        newBotConfigAdapter(deps.ConfigStore, cfg.AgentConfig.ConfigDir, ""),
		Version:          versionString,
		NewSessionID:     newSessionID,
		AllowedOriginsFn: corsOriginsFn,
		Restart: func() error {
			inst, err := findRunningGateway()
			if err != nil {
				return err
			}
			return forkRestartHelper(inst, deps.ConfigPath, deps.DevMode, true)
		},
		DB:           deps.DB,
		DBResolver:   deps.DBResolver,
		KeyValidator: deps.Auth,
		APIKeyStore:  deps.APIKeyStore,
		WriteMu:      deps.WriteMu,
	})

	if cfg.Admin.RateLimitEnabled {
		limiter := admin.NewRateLimiter(cfg.Admin.RequestsPerSec, cfg.Admin.Burst)
		adminAPI.SetRateLimiter(limiter)

		deps.ConfigStore.RegisterFunc(func(prev, next *config.Config) {
			if prev.Admin.RequestsPerSec != next.Admin.RequestsPerSec || prev.Admin.Burst != next.Admin.Burst {
				limiter.UpdateRate(next.Admin.RequestsPerSec, next.Admin.Burst)
			}
		})
	}
	if cfg.Admin.IPWhitelistEnabled {
		adminAPI.SetAllowedCIDRs(cfg.Admin.AllowedCIDRs)

		deps.ConfigStore.RegisterFunc(func(prev, next *config.Config) {
			if !slices.Equal(prev.Admin.AllowedCIDRs, next.Admin.AllowedCIDRs) {
				adminAPI.SetAllowedCIDRs(next.Admin.AllowedCIDRs)
			}
		})
	}

	adminMux := adminAPI.Mux()

	adminMux.HandleFunc("GET /admin/health/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	adminMux.Handle("GET /admin/metrics", observability.MetricsHandler())

	adminMux.HandleFunc("GET /admin/stats", adminAPI.HandleStats)
	adminMux.HandleFunc("GET /admin/health/workers", adminAPI.HandleWorkerHealth)
	adminMux.HandleFunc("GET /admin/health", adminAPI.HandleHealth)
	adminMux.HandleFunc("GET /admin/logs", adminAPI.HandleLogs)
	adminMux.HandleFunc("POST /admin/config/validate", adminAPI.HandleConfigValidate)
	adminMux.HandleFunc("POST /admin/config/rollback", adminAPI.HandleConfigRollback)
	adminMux.HandleFunc("GET /admin/debug/sessions/{id}", adminAPI.HandleDebugSession)
	adminMux.HandleFunc("POST /admin/restart", adminAPI.HandleRestart)

	adminMux.HandleFunc("GET /admin/sessions", adminAPI.ListSessions)
	adminMux.HandleFunc("GET /admin/sessions/{id}", adminAPI.GetSession)
	adminMux.HandleFunc("DELETE /admin/sessions/{id}", adminAPI.DeleteSession)
	adminMux.HandleFunc("POST /admin/sessions/{id}/terminate", adminAPI.TerminateSession)
	adminMux.HandleFunc("GET /admin/sessions/{id}/stats", adminAPI.HandleSessionStats)

	// Cron API
	adminMux.HandleFunc("GET /admin/cron/jobs", adminAPI.HandleCronList)
	adminMux.HandleFunc("GET /admin/cron/jobs/{id}", adminAPI.HandleCronGet)
	adminMux.HandleFunc("POST /admin/cron/jobs", adminAPI.HandleCronCreate)
	adminMux.HandleFunc("PATCH /admin/cron/jobs/{id}", adminAPI.HandleCronUpdate)
	adminMux.HandleFunc("DELETE /admin/cron/jobs/{id}", adminAPI.HandleCronDelete)
	adminMux.HandleFunc("POST /admin/cron/jobs/{id}/run", adminAPI.HandleCronTrigger)
	adminMux.HandleFunc("GET /admin/cron/jobs/{id}/runs", adminAPI.HandleCronRunHistory)

	// Bot status API
	adminMux.HandleFunc("GET /admin/bots", adminAPI.HandleListBots)
	adminMux.HandleFunc("GET /admin/bots/{name}", adminAPI.HandleGetBot)

	// Bot config API
	adminMux.HandleFunc("GET /admin/bots/config", adminAPI.HandleListBotConfigs)
	adminMux.HandleFunc("GET /admin/bots/{name}/config", adminAPI.HandleGetBotConfig)
	adminMux.HandleFunc("GET /admin/bots/{name}/config/{file}", adminAPI.HandleGetAgentConfigFile)
	adminMux.HandleFunc("GET /admin/bots/{name}/preview", adminAPI.HandleSystemPromptPreview)
	adminMux.HandleFunc("PATCH /admin/bots/{name}", adminAPI.HandleUpdateBotConfig)
	adminMux.HandleFunc("POST /admin/bots", adminAPI.HandleCreateBot)
	adminMux.HandleFunc("DELETE /admin/bots/{name}", adminAPI.HandleDeleteBot)
	adminMux.HandleFunc("PUT /admin/bots/{name}/config/{file}", adminAPI.HandleWriteAgentConfigFile)

	// API key user management
	adminMux.HandleFunc("GET /admin/api-keys", adminAPI.HandleAPIKeyUserList)
	adminMux.HandleFunc("POST /admin/api-keys", adminAPI.HandleAPIKeyUserCreate)
	adminMux.HandleFunc("GET /admin/api-keys/{id}", adminAPI.HandleAPIKeyUserGet)
	adminMux.HandleFunc("PATCH /admin/api-keys/{id}", adminAPI.HandleAPIKeyUserUpdate)
	adminMux.HandleFunc("DELETE /admin/api-keys/{id}", adminAPI.HandleAPIKeyUserDelete)

	// Documentation
	if resolved := security.ResolveCSP(security.DefaultDocsCSP, cfg.Security.CSP); security.IsPermissiveCSP(resolved) {
		log.Warn("csp: docs policy is permissive (any http/https/ws/wss host allowed); set security.csp to restrict in production",
			"service", "docs")
	}
	mux.Handle("GET /docs/", http.StripPrefix("/docs", docs.Handler(cfg.Security.CSP)))

	// Webhook endpoint (GitHub → HotPlex event-driven triggers)
	if cfg.Webhook.Enabled && deps.CronScheduler != nil {
		if cfg.Webhook.Secret == "" {
			log.Warn("webhook enabled but secret is empty — rejecting for security")
		} else {
			webhookHandler := gateway.NewWebhookHandler(
				deps.Ctx,
				cfg.Webhook,
				deps.CronScheduler,
				log,
			)
			deps.WebhookHandler = webhookHandler
			mux.Handle(cfg.Webhook.Path, webhookHandler)
			log.Info("webhook handler registered", "path", cfg.Webhook.Path)
		}
	}

	// Global favicon fallback using docs logo
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs/assets/logo.png", http.StatusMovedPermanently)
	})

	// security.txt (RFC 9116) — contact info from config, no hardcoded domains.
	mux.Handle("GET /.well-known/security.txt", security.SecurityTxtHandler(
		func() string { return deps.ConfigStore.Load().Security.SecurityContact },
	))

	// Webchat SPA is NOT registered on the mux directly.
	// Instead, the caller wraps the mux with a fallback handler below.
	h := adminAPI.Middleware(adminMux)
	return otelhttp.NewHandler(h, "hotplex-gateway",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)
}

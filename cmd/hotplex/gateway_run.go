package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/admin"
	"github.com/hrygo/hotplex/internal/assets"
	"github.com/hrygo/hotplex/internal/brain"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/cron"
	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/gateway"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/skills"
	"github.com/hrygo/hotplex/internal/sqlutil"
	"github.com/hrygo/hotplex/internal/tracing"
	"github.com/hrygo/hotplex/internal/webchat"
	"github.com/hrygo/hotplex/internal/worker/claudecode"
	"github.com/hrygo/hotplex/internal/worker/codexcli"
	"github.com/hrygo/hotplex/internal/worker/opencodeserver"
	"github.com/hrygo/hotplex/internal/worker/proc"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// eventStoreProvider combines the query and event-store interfaces needed by all consumers.
// Both *eventstore.SQLiteStore and the internal pgEventStore satisfy it.
type eventStoreProvider interface {
	eventstore.TurnQuerier
	QueryBySession(ctx context.Context, sessionID string, cursor int64, dir eventstore.CursorDirection, limit int) (*eventstore.EventPage, error)
	DeleteExpired(ctx context.Context, cutoff time.Time) (int64, error)
	Close() error
}

// GatewayDeps holds all dependencies constructed during gateway initialization.
// These are passed to various components and registrations.
type GatewayDeps struct {
	Log             *slog.Logger
	Config          *config.Config
	ConfigStore     *config.ConfigStore
	Hub             *gateway.Hub
	SessionMgr      *session.Manager
	EventStore      eventStoreProvider
	EventCollector  *eventstore.Collector
	Auth            *security.Authenticator
	Handler         *gateway.Handler
	Bridge          *gateway.Bridge
	ConfigWatcher   *config.Watcher
	CronScheduler   *cron.Scheduler
	ChatAccessStore messaging.ChatAccessStorer
	DB              *sql.DB
	DBResolver      *security.DBResolver
	APIKeyStore     admin.APIKeyUserStorer
	WriteMu         *sqlutil.WriteMu
	ConfigPath      string
	DevMode         bool
}

const defaultConfigPath = config.DefaultConfigPath

func configFlag(cmd *cobra.Command, target *string) {
	cmd.Flags().StringVarP(target, "config", "c", defaultConfigPath, "config file path")
}

func runGateway(configPath string, devMode bool, stopCh <-chan struct{}) (err error) { //nolint:unparam // stopCh used by Windows service wrapper
	defer func() {
		if err != nil {
			removeGatewayState()
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := loadConfig(configPath, devMode)
	if err != nil {
		return err
	}

	if validationErrs := cfg.Validate(); len(validationErrs) > 0 {
		for _, ve := range validationErrs {
			fmt.Fprintf(os.Stderr, "config validation warning: %s\n", ve)
		}
	}

	// Extract embedded Python scripts to ~/.hotplex/scripts
	scriptsDir := filepath.Join(config.HotplexHome(), "scripts")
	if err := assets.InstallScripts(scriptsDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: assets: script extraction failed: %s\n", err)
	}

	log, cfgStore, levelVar := initLogging(cfg)
	pidTracker, cleanupWG := initOrphanCleanup(ctx, cfg, log)

	tracing.Init(ctx, log, "hotplex-gateway")
	log.Info("gateway: starting",
		"go", runtime.Version(),
		"addr", cfg.Gateway.Addr,
		"config", configPath,
	)

	stores, err := initStores(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer stores.close(log)

	releaseDBStatsManual(log)

	sm, err := session.NewManager(ctx, log, cfg, cfgStore, stores.session)
	if err != nil {
		return err
	}
	var cronScheduler *cron.Scheduler
	var cronDelivery *cron.Delivery
	var cronAttRouter *cronAttachedRouter

	sm.OnTerminate = func(sessionID string) {
		log.Info("gateway: session terminated", "session_id", sessionID)
		if cronScheduler != nil {
			cronScheduler.CleanupForSession(sessionID)
		}
	}

	// Wait for orphan process cleanup to finish before repairing sessions.
	cleanupWG.Wait()

	repaired, repairErr := sm.RepairRunningSessions(ctx)
	if repairErr != nil {
		log.Warn("gateway: session state repair failed", "err", repairErr)
	} else if repaired > 0 {
		log.Info("gateway: repaired orphaned sessions", "count", repaired)
	}

	hub := gateway.NewHub(log, cfgStore)
	hub.LogHandler = func(level, msg, sessionID string) {
		admin.AddLog(level, msg, sessionID)
	}

	var configWatcher *config.Watcher
	if configPath != "" {
		configWatcher = config.NewWatcher(log, configPath, cfgStore,
			func(newCfg *config.Config) {
				log.Info("config: hot reload applied",
					"gateway_addr", newCfg.Gateway.Addr,
					"pool_max_size", newCfg.Pool.MaxSize,
					"gc_scan_interval", newCfg.Session.GCScanInterval,
				)
			},
			func(field string) {
				log.Warn("config: static field changed, restart required to apply",
					"field", field,
				)
			},
		)
		configWatcher.SetInitial(cfg)
	}

	// Config hot-reload callbacks
	cfgStore.RegisterFunc(func(prev, next *config.Config) {
		if prev.Log.Level != next.Log.Level {
			var newLevel slog.Level
			if err := newLevel.UnmarshalText([]byte(next.Log.Level)); err == nil {
				levelVar.Set(newLevel)
				log.Info("config: log level updated", "old", prev.Log.Level, "new", next.Log.Level)
			}
		}
	})
	cfgStore.RegisterFunc(func(prev, next *config.Config) {
		if prev.Pool.MaxSize != next.Pool.MaxSize || prev.Pool.MaxIdlePerUser != next.Pool.MaxIdlePerUser {
			sm.Pool().UpdateLimits(next.Pool.MaxSize, next.Pool.MaxIdlePerUser)
		}
	})
	cfgStore.RegisterFunc(func(prev, next *config.Config) {
		if prev.Session.GCScanInterval != next.Session.GCScanInterval {
			sm.ResetGCInterval(next.Session.GCScanInterval)
		}
	})

	sm.StateNotifier = func(ctx context.Context, sessionID string, state events.SessionState, message string) {
		env := events.NewEnvelope(aep.NewID(), sessionID, hub.NextSeq(sessionID), events.State, events.StateData{
			State:   state,
			Message: message,
		})
		_ = hub.SendToSession(ctx, env)
	}

	auth := security.NewAuthenticator(&cfg.Security)

	// API key → user identity resolver: YAML config takes priority over DB (Admin API CRUD).
	// ChainResolver tries config map first, falls back to DB. Either source may be empty.
	dbResolver := stores.dbResolver
	if len(cfg.ResolvedAPIKeyUsers) > 0 {
		mapResolver := security.NewMapResolver(cfg.ResolvedAPIKeyUsers)
		auth.SetKeyResolver(security.NewChainResolver(mapResolver, dbResolver))
		log.Info("gateway: API key resolver: config → database",
			"mapped_config_keys", len(cfg.ResolvedAPIKeyUsers))
	} else {
		auth.SetKeyResolver(dbResolver)
		log.Info("gateway: API key resolver: database")
	}

	retryCtrl := gateway.NewLLMRetryController(cfg.Worker.AutoRetry, log)

	agentConfigDir := ""
	if cfg.AgentConfig.Enabled {
		agentConfigDir = cfg.AgentConfig.ConfigDir
		warnDeprecatedSuffixFiles(agentConfigDir, log)
		log.Debug("config: agent config resolved", "dir", agentConfigDir)
	}

	bridge := gateway.NewBridge(gateway.BridgeDeps{
		Log:                log,
		Hub:                hub,
		SM:                 sm,
		EventCollector:     stores.collector,
		TurnsQuerier:       stores.event, // SQLiteStore implements TurnQuerier
		RetryCtrl:          retryCtrl,
		AgentConfigDir:     agentConfigDir,
		TurnTimeout:        cfg.Worker.TurnTimeout,
		WorkerEnv:          buildWorkerEnv(cfg),
		WorkerEnvBlocklist: cfg.Worker.EnvBlocklist,
		CronEnv:            buildCronEnv(cfg),
		MCPConfigJSON:      buildMCPConfigJSON(cfg),
	})

	skillsLocator := skills.NewLocator(log, cfg.Skills.CacheTTL)

	handler := gateway.NewHandler(gateway.HandlerDeps{
		Log:           log,
		Hub:           hub,
		SM:            sm,
		Auth:          auth,
		Bridge:        bridge,
		SkillsLocator: skillsLocator,
	})

	if cfg.Worker.AutoRetry.Enabled {
		log.Info("gateway: LLM auto-retry enabled", "max_retries", cfg.Worker.AutoRetry.MaxRetries, "base_delay", cfg.Worker.AutoRetry.BaseDelay)
	}

	opencodeserver.InitSingleton(log, cfg.Worker.OpenCodeServer)
	claudecode.InitConfig(cfg.Worker.ClaudeCode)
	if cfg.Worker.CodexCLI.UseAppServer {
		codexcli.InitSingleton(log, cfg.Worker.CodexCLI)
	} else {
		codexcli.InitConfig(cfg.Worker.CodexCLI)
	}

	cfgStore.RegisterFunc(func(prev, next *config.Config) {
		if !reflect.DeepEqual(prev.Worker.AutoRetry, next.Worker.AutoRetry) {
			retryCtrl.UpdateConfig(next.Worker.AutoRetry)
		}
	})
	cfgStore.RegisterFunc(func(prev, next *config.Config) {
		if !reflect.DeepEqual(prev.Security.APIKeys, next.Security.APIKeys) {
			auth.ReloadKeys(&next.Security)
		}
	})

	cfgStore.RegisterFunc(func(prev, next *config.Config) {
		if !reflect.DeepEqual(prev.ResolvedAPIKeyUsers, next.ResolvedAPIKeyUsers) {
			dbR := security.NewDBResolver(stores.sqlDB)
			if len(next.ResolvedAPIKeyUsers) > 0 {
				auth.SetKeyResolver(security.NewChainResolver(security.NewMapResolver(next.ResolvedAPIKeyUsers), dbR))
			} else {
				auth.SetKeyResolver(dbR)
			}
			log.Info("config: API key resolver updated",
				"mapped_config_keys", len(next.ResolvedAPIKeyUsers))
		}
	})
	cfgStore.RegisterFunc(func(prev, next *config.Config) {
		if prev.Worker.ClaudeCode.Command != next.Worker.ClaudeCode.Command {
			claudecode.InitConfig(next.Worker.ClaudeCode)
		}
	})
	cfgStore.RegisterFunc(func(prev, next *config.Config) {
		if prev.Worker.CodexCLI.Command != next.Worker.CodexCLI.Command {
			codexcli.InitConfig(next.Worker.CodexCLI)
		}
	})
	cfgStore.RegisterFunc(func(prev, next *config.Config) {
		if !reflect.DeepEqual(prev.Worker.ClaudeCode.MCPServers, next.Worker.ClaudeCode.MCPServers) {
			bridge.UpdateMCPConfig(buildMCPConfigJSON(next))
			log.Info("config: MCP servers updated", "count", len(next.Worker.ClaudeCode.MCPServers))
		}
	})

	// Assemble deps and start HTTP + messaging

	// Cron scheduler: init after Bridge, before messaging adapters.
	if cfg.Cron.Enabled {
		var cronStore cron.Store
		if stores.cron != nil {
			cronStore = stores.cron
		} else {
			cronStore = cron.NewSQLiteStore(stores.sqlDB, log, stores.writeMu)
		}
		cronDelivery = cron.NewDelivery(log,
			func(ctx context.Context, sessionID string) (string, error) {
				if err := stores.collector.Flush(); err != nil {
					log.Warn("cron: flush before query", "error", err)
				}
				turns, err := stores.event.QueryTurns(ctx, sessionID, 1, 0)
				if err != nil || len(turns) == 0 {
					return "", err
				}
				return turns[len(turns)-1].Content, nil
			},
			nil,
		)
		cronAttRouter = &cronAttachedRouter{bridge: bridge, sm: sm}
		cronScheduler = cron.New(cron.Deps{
			Log:            log,
			Store:          cronStore,
			Bridge:         bridge,
			SessionMgr:     sm,
			Delivery:       cronDelivery,
			AttachedRouter: cronAttRouter,
			YAMLDefs:       cronConfigToYAMLDefs(cfg.Cron.Jobs),
			Cfg: cron.Config{
				Enabled:           cfg.Cron.Enabled,
				MaxConcurrentRuns: cfg.Cron.MaxConcurrentRuns,
				MaxJobs:           cfg.Cron.MaxJobs,
				DefaultTimeoutSec: cfg.Cron.DefaultTimeoutSec,
				TickIntervalSec:   cfg.Cron.TickIntervalSec,
				YAMLConfigPath:    cfg.Cron.YAMLConfigPath,
			},
			ResolveWorkDir: func(job *cron.CronJob) string {
				return cfgStore.Load().ResolvePlatformWorkDir(job.Platform)
			},
		})
		if err := cronScheduler.Start(ctx); err != nil {
			log.Warn("cron: scheduler start failed (cron disabled)", "err", err)
			cronScheduler = nil
		} else {
			// Hot-reload cron config at runtime.
			cfgStore.RegisterFunc(func(prev, next *config.Config) {
				if prev.Cron.MaxConcurrentRuns != next.Cron.MaxConcurrentRuns ||
					prev.Cron.MaxJobs != next.Cron.MaxJobs {
					cronScheduler.UpdateConfig(next.Cron.MaxConcurrentRuns, next.Cron.MaxJobs)
				}
			})
		}
	}

	mux := http.NewServeMux()
	deps := &GatewayDeps{
		Log:             log,
		Config:          cfg,
		ConfigStore:     cfgStore,
		Hub:             hub,
		SessionMgr:      sm,
		EventStore:      stores.event,
		EventCollector:  stores.collector,
		Auth:            auth,
		Handler:         handler,
		Bridge:          bridge,
		ConfigWatcher:   configWatcher,
		CronScheduler:   cronScheduler,
		ChatAccessStore: stores.chatAccessOrNew(stores.sqlDB, log),
		DB:              stores.sqlDB,
		DBResolver:      dbResolver,
		APIKeyStore:     stores.apiKeyStore,
		WriteMu:         stores.writeMu,
		ConfigPath:      configPath,
		DevMode:         devMode,
	}

	// Brain: lightweight LLM layer for TTS summarization (fail-open).
	if err := brain.Init(log); err != nil {
		log.Warn("Brain initialization failed (fail-open)", "error", err)
	}

	go runEventsGC(ctx, stores, log, cfg.Events.Retention)

	msgAdapters, adapterStatuses := startMessagingAdapters(ctx, deps)

	// Wire cron delivery to platform adapters.
	if cronDelivery != nil {
		cronDelivery.SetDeliverer(func(ctx context.Context, platform string, platformKey map[string]string, response string) error {
			for _, a := range msgAdapters {
				if a.Platform() == messaging.PlatformType(platform) {
					if sender, ok := a.(messaging.CronResultSender); ok {
						return sender.SendCronResult(ctx, response, platformKey)
					}
				}
			}
			return fmt.Errorf("cron delivery: no adapter for platform %q", platform)
		})
	}

	adminHandler := setupRoutes(mux, deps)

	// Webchat SPA fallback
	var rootHandler http.Handler = mux
	if cfg.WebChat.Enabled {
		spa := webchat.Handler()
		rootHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, pattern := mux.Handler(r)
			if pattern != "" {
				mux.ServeHTTP(w, r)
				return
			}
			spa.ServeHTTP(w, r)
		})
	}

	server := &http.Server{
		Addr:         cfg.Gateway.Addr,
		Handler:      rootHandler,
		ReadTimeout:  cfg.Gateway.IdleTimeout,
		WriteTimeout: cfg.Gateway.WriteTimeout,
	}

	if configWatcher != nil {
		if err := configWatcher.Start(ctx); err != nil {
			log.Warn("config: watcher start failed", "err", err)
		}
	}

	serverErr := make(chan error, 2)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("gateway: server failed to start", "err", err)
			serverErr <- err
		}
	}()

	// Admin server: dedicated port for network isolation (always-on when enabled).
	var adminServer *http.Server
	var adminAddr string
	if cfg.Admin.Enabled {
		adminServer = &http.Server{
			Addr:         cfg.Admin.Addr,
			Handler:      adminHandler,
			ReadTimeout:  cfg.Gateway.IdleTimeout,
			WriteTimeout: cfg.Gateway.WriteTimeout,
		}
		adminAddr = adminServer.Addr
		log.Info("admin: starting", "addr", adminAddr)
		go func() {
			if err := adminServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Error("admin: server failed to start", "err", err)
				serverErr <- err
			}
		}()
	}
	printStartupBanner(os.Stdout, newBuildInfo(), RuntimeStatus{
		GatewayAddr:     cfg.Gateway.Addr,
		AdminAddr:       adminAddr,
		WebChatAddr:     cfg.WebChat.Addr,
		WebChatEmbedded: cfg.WebChat.Enabled,
		TLSEnabled:      cfg.Security.TLSEnabled,
		DBDriver:        cfg.DB.Driver,
		DBPath:          cfg.DB.Path,
		PoolMax:         cfg.Pool.MaxSize,
		PoolIdle:        cfg.Pool.MaxIdlePerUser,
		Adapters:        adapterStatuses,
		RetryEnabled:    cfg.Worker.AutoRetry.Enabled,
		RetryMax:        cfg.Worker.AutoRetry.MaxRetries,
		RetryDelay:      cfg.Worker.AutoRetry.BaseDelay.String(),
	}, configPath)

	// Wait for shutdown signal or SIGHUP reload
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	if runtime.GOOS != "windows" {
		signal.Notify(sig, syscall.SIGHUP)
	}

loop:
	for {
		select {
		case s := <-sig:
			if s == syscall.SIGHUP {
				if cronScheduler != nil {
					cronScheduler.ReloadIndex()
				}
				log.Info("gateway: cron index reloaded (SIGHUP)")
				continue
			}
			log.Info("gateway: shutdown", "signal", s)
			break loop
		case err := <-serverErr:
			if err != nil {
				log.Error("gateway: server failed, exiting", "err", err)
				cancel()
				shutdownGateway(ctx, log, deps, msgAdapters, server, adminServer, skillsLocator, pidTracker, cleanupWG, cronScheduler)
				return err
			}
			cancel()
			shutdownGateway(ctx, log, deps, msgAdapters, server, adminServer, skillsLocator, pidTracker, cleanupWG, cronScheduler)
			return nil
		case <-stopCh:
			log.Info("gateway: shutdown", "signal", "stopCh")
			break loop
		}
	}

	cancel()
	shutdownGateway(ctx, log, deps, msgAdapters, server, adminServer, skillsLocator, pidTracker, cleanupWG, cronScheduler)
	return nil
}

// --- Decomposed helpers ---

func initLogging(cfg *config.Config) (*slog.Logger, *config.ConfigStore, *slog.LevelVar) {
	cfgStore := config.NewConfigStore(cfg, slog.Default())

	levelVar := &slog.LevelVar{}
	if err := levelVar.UnmarshalText([]byte(cfg.Log.Level)); err != nil {
		levelVar.Set(slog.LevelInfo)
	}

	opts := &slog.HandlerOptions{
		Level: levelVar,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.String(slog.TimeKey, a.Value.Time().Format("2006-01-02T15:04:05.0000"))
			}
			return a
		},
	}

	var logHandler slog.Handler
	if cfg.Log.Format == "text" {
		logHandler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		logHandler = slog.NewJSONHandler(os.Stderr, opts)
	}

	log := slog.New(logHandler).With(
		"service", "hotplex-gateway",
		"version", versionString(),
	)
	slog.SetDefault(log)

	return log, cfgStore, levelVar
}

func initOrphanCleanup(ctx context.Context, cfg *config.Config, log *slog.Logger) (*proc.Tracker, *sync.WaitGroup) {
	pidTracker := proc.InitTracker(cfg.Worker.PIDDir, log)
	var cleanupWG sync.WaitGroup
	if err := pidTracker.EnsureDir(); err != nil {
		log.Warn("gateway: pid dir setup failed, orphan cleanup disabled", "dir", cfg.Worker.PIDDir, "err", err)
	} else {
		cleanupWG.Add(1)
		go func() {
			defer cleanupWG.Done()
			cleanupCtx, cleanupCancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cleanupCancel()
			results := pidTracker.CleanupOrphans(cleanupCtx, 3, 5*time.Second)
			killed := 0
			for _, r := range results {
				if r.Err != nil {
					log.Warn("gateway: orphan cleanup error", "key", r.Key, "pgid", r.PGID, "err", r.Err)
				} else if r.Killed {
					log.Info("gateway: killed orphan process", "key", r.Key, "pgid", r.PGID)
					killed++
				}
			}
			if len(results) > 0 {
				log.Info("gateway: orphan cleanup complete", "scanned", len(results), "killed", killed)
			}
		}()
	}
	return pidTracker, &cleanupWG
}

type gatewayStores struct {
	session     session.Store
	event       eventStoreProvider
	turnQuerier eventstore.TurnQuerier
	collector   *eventstore.Collector
	cron        cron.Store
	chatAccess  messaging.ChatAccessStorer
	writeMu     *sqlutil.WriteMu // nil when using PostgreSQL (WriteMu is SQLite-only)
	db          *dbutil.DB
	sqlDB       *sql.DB
	apiKeyStore admin.APIKeyUserStorer
	dbResolver  *security.DBResolver
}

// chatAccessOrNew returns the chat-access store if already initialized (PG path),
// or creates a new SQLite-backed store from the shared connection.
func (s *gatewayStores) chatAccessOrNew(db *sql.DB, log *slog.Logger) messaging.ChatAccessStorer {
	if s.chatAccess != nil {
		return s.chatAccess
	}
	return messaging.NewChatAccessStore(db, log, s.writeMu)
}

func initStores(ctx context.Context, cfg *config.Config, log *slog.Logger) (*gatewayStores, error) {
	switch dbutil.ParseDialect(cfg.DB.Driver) {
	case dbutil.DialectPostgres:
		return initPGStores(ctx, cfg, log)
	default:
		return initSQLiteStores(ctx, cfg, log)
	}
}

// initSQLiteStores initializes all stores using SQLite (existing logic).
func initSQLiteStores(ctx context.Context, cfg *config.Config, log *slog.Logger) (*gatewayStores, error) {
	writeMu := sqlutil.NewWriteMu(sqlutil.DialectSQLite)
	sessionStore, err := session.NewSQLiteStore(ctx, cfg, writeMu)
	if err != nil {
		return nil, err
	}

	// EventStore shares the session store's *sql.DB (schema managed by goose migration 002).
	eventStore := eventstore.NewSQLiteStore(sessionStore.DB(), writeMu)
	dbResolver := security.NewDBResolver(sessionStore.DB())

	return &gatewayStores{
		session:     sessionStore,
		event:       eventStore,
		turnQuerier: eventStore,
		collector:   eventstore.NewCollector(eventStore, log),
		writeMu:     writeMu,
		sqlDB:       sessionStore.DB(),
		dbResolver:  dbResolver,
	}, nil
}

// initPGStores initializes all stores using PostgreSQL (new driver path).
func initPGStores(ctx context.Context, cfg *config.Config, log *slog.Logger) (*gatewayStores, error) {
	db, err := dbutil.Open(dbutil.DialectPostgres, &cfg.DB)
	if err != nil {
		return nil, fmt.Errorf("pg: open db: %w", err)
	}

	sessionStore, err := session.NewPGStore(ctx, db)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pg: session store: %w", err)
	}

	eventStore := eventstore.NewPGStore(db, log)
	cronStore := cron.NewPGStore(db, log)
	chatAccessStore := messaging.NewChatAccessPGStore(db, log)
	dbResolver := security.NewDBResolver(db.DB)

	return &gatewayStores{
		session:     sessionStore,
		event:       eventStore,
		turnQuerier: eventStore,
		collector:   eventstore.NewCollector(eventStore, log),
		cron:        cronStore,
		chatAccess:  chatAccessStore,
		db:          db,
		sqlDB:       db.DB,
		apiKeyStore: admin.NewAPIKeyUserPGStore(db, dbResolver),
		dbResolver:  dbResolver,
	}, nil
}

func (s *gatewayStores) close(log *slog.Logger) {
	if s.collector != nil {
		if err := s.collector.Close(); err != nil {
			log.Warn("gateway: event collector close", "err", err)
		}
	}
	// For SQLite: EventStore.Close is a no-op (ownsDB=false); session store owns the shared connection.
	if s.session != nil {
		if err := s.session.Close(); err != nil {
			log.Warn("gateway: session store close", "err", err)
		}
	}
	// For PG: close the shared dbutil.DB connection.
	if s.db != nil {
		if err := s.db.Close(); err != nil {
			log.Warn("gateway: db close", "err", err)
		}
	}
}

// runEventsGC periodically deletes expired events and turns.
func runEventsGC(ctx context.Context, stores *gatewayStores, log *slog.Logger, retention time.Duration) {
	if retention <= 0 {
		retention = 720 * time.Hour // default 30 days
	}
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-retention)
			if n, err := stores.event.DeleteExpired(ctx, cutoff); err == nil && n > 0 {
				log.Info("events gc: deleted expired events", "count", n)
			}
			if n, err := stores.turnQuerier.DeleteExpiredTurns(ctx, cutoff); err == nil && n > 0 {
				log.Info("events gc: deleted expired turns", "count", n)
			}
		}
	}
}

func shutdownGateway(
	_ context.Context,
	log *slog.Logger,
	deps *GatewayDeps,
	msgAdapters []messaging.PlatformAdapterInterface,
	server *http.Server,
	adminServer *http.Server,
	skillsLocator *skills.Locator,
	pidTracker *proc.Tracker,
	cleanupWG *sync.WaitGroup,
	cronScheduler *cron.Scheduler,
) {
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer func() {
		if err := tracing.Shutdown(shutdownCtx); err != nil {
			log.Warn("tracing: shutdown", "error", err)
		}
		shutdownCancel()
	}()

	if err := deps.Hub.Shutdown(shutdownCtx); err != nil {
		log.Warn("gateway: hub shutdown", "err", err)
	}

	skillsLocator.Close()

	if guard := brain.GlobalGuard(); guard != nil {
		guard.Close()
	}

	if deps.ConfigWatcher != nil {
		if err := deps.ConfigWatcher.Close(); err != nil {
			log.Warn("config: watcher close", "err", err)
		}
	}

	if cronScheduler != nil {
		cronScheduler.Shutdown(shutdownCtx)
	}

	for _, adapter := range msgAdapters {
		if err := adapter.Close(shutdownCtx); err != nil {
			log.Warn("messaging: adapter close", "err", err)
		}
	}
	messaging.DefaultBotRegistry().UnregisterAll()

	closeSTTCache(shutdownCtx, log)
	closeTTSCache(shutdownCtx, log)

	// Terminate all workers BEFORE bridge.Shutdown() so forwardEvents
	// goroutines (blocked on worker stdout) can exit.
	deps.SessionMgr.TerminateAllWorkers()
	opencodeserver.ShutdownSingleton(shutdownCtx)
	codexcli.ShutdownSingleton(shutdownCtx)

	deps.Bridge.Shutdown(shutdownCtx)

	cleanupWG.Wait()
	pidTracker.RemoveAll()

	if err := deps.SessionMgr.Close(); err != nil {
		log.Warn("gateway: session manager close", "err", err)
	}

	// Shut down HTTP servers in parallel to share the 30s budget.
	var serverWG sync.WaitGroup
	serverWG.Add(1)
	go func() {
		defer serverWG.Done()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Warn("gateway: http server shutdown", "err", err)
		}
	}()
	if adminServer != nil {
		serverWG.Add(1)
		go func() {
			defer serverWG.Done()
			if err := adminServer.Shutdown(shutdownCtx); err != nil {
				log.Warn("admin: http server shutdown", "err", err)
			}
		}()
	}
	serverWG.Wait()

	log.Info("gateway: stopped")
}

// --- Config helpers ---

func loadConfig(configPath string, devMode bool) (*config.Config, error) {
	absPath, err := config.ExpandAndAbs(configPath)
	if err != nil {
		return nil, fmt.Errorf("config: resolve path %q: %w", configPath, err)
	}

	loadEnvFile(filepath.Dir(absPath))

	cfg, err := config.Load(absPath)
	if err != nil {
		return nil, fmt.Errorf("config: load %q: %w", absPath, err)
	}
	if devMode {
		cfg.Security.APIKeys = nil
		cfg.Admin.Tokens = nil
	}

	security.ConfigureFromConfig(&cfg.Security)

	return cfg, nil
}

func loadEnvFile(dir string) {
	envPath := filepath.Join(dir, ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		return
	}

	var loaded int
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"'`)
		if os.Getenv(key) == "" && !security.IsProtected(key) {
			_ = os.Setenv(key, val)
			loaded++
		}
	}
	if loaded > 0 {
		fmt.Fprintf(os.Stderr, "  env loaded %d vars from %s\n", loaded, envPath)
	}
}

func warnDeprecatedSuffixFiles(dir string, log *slog.Logger) {
	if dir == "" {
		return
	}
	platforms := []string{"slack", "feishu", "webchat"}
	bases := []string{"SOUL", "AGENTS", "SKILLS", "USER", "MEMORY"}
	for _, p := range platforms {
		for _, b := range bases {
			suffix := b + "." + p + ".md"
			if _, err := os.Stat(filepath.Join(dir, suffix)); err == nil {
				log.Warn("agent-config: deprecated suffix file found; use directory-based layout instead",
					"file", suffix,
					"migration", "move to "+p+"/"+b+".md")
			}
		}
	}
}

// cronConfigToYAMLDefs converts inline job definitions from config to YAMLJobDef slice.
func cronConfigToYAMLDefs(jobs []map[string]any) []cron.YAMLJobDef {
	if len(jobs) == 0 {
		return nil
	}
	data, _ := json.Marshal(jobs)
	var defs []cron.YAMLJobDef
	_ = json.Unmarshal(data, &defs)
	return defs
}

// buildWorkerEnv constructs the worker environment variables.
func buildWorkerEnv(cfg *config.Config) []string {
	return slices.Clone(cfg.Worker.Environment)
}

// buildCronEnv builds env vars injected only into cron platform sessions.
// Separated from buildWorkerEnv to prevent admin credentials from leaking
// to non-cron workers (env.go blocklist only filters os.Environ, not ConfigEnv).
func buildCronEnv(cfg *config.Config) []string {
	if !cfg.Cron.Enabled || !cfg.Admin.Enabled {
		return nil
	}
	var env []string
	env = append(env, "HOTPLEX_ADMIN_API_URL=http://"+cfg.Admin.Addr)
	if len(cfg.Admin.Tokens) > 0 {
		env = append(env, "HOTPLEX_ADMIN_TOKEN="+cfg.Admin.Tokens[0])
	}
	return env
}

// buildMCPConfigJSON serializes configured MCP servers into the JSON format
// expected by Claude Code's --mcp-config flag. Returns "" when no servers are
// configured, which signals the bridge to let Claude Code do default discovery.
func buildMCPConfigJSON(cfg *config.Config) string {
	if len(cfg.Worker.ClaudeCode.MCPServers) == 0 {
		return ""
	}
	// Validate each server config before serializing.
	valid := make(map[string]*config.MCPServerConfig, len(cfg.Worker.ClaudeCode.MCPServers))
	for name, srv := range cfg.Worker.ClaudeCode.MCPServers {
		if err := srv.Validate(); err != nil {
			slog.Error("config: invalid MCP server config, skipping", "server", name, "err", err)
			continue
		}
		valid[name] = srv
	}
	if len(valid) == 0 {
		return ""
	}
	wrapper := map[string]any{"mcpServers": valid}
	b, err := json.Marshal(wrapper)
	if err != nil {
		slog.Error("config: failed to serialize MCP server config", "err", err, "server_count", len(valid))
		return ""
	}
	return string(b)
}

func releaseDBStatsManual(log *slog.Logger) {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Warn("db-stats: cannot determine home dir for skill manual release", "err", err)
		return
	}
	dir := filepath.Join(home, ".hotplex", "skills")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "db-stats.md")
	if err := os.WriteFile(path, []byte(dbutil.SkillManual()), 0o644); err != nil {
		log.Warn("db-stats: failed to release skill manual", "path", path, "err", err)
		return
	}
	log.Debug("db-stats: skill manual released", "path", path)
}

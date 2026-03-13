package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/hrygo/hotplex"
	"github.com/hrygo/hotplex/brain"
	"github.com/hrygo/hotplex/chatapps"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/server"
	"github.com/hrygo/hotplex/internal/sys"
	"github.com/hrygo/hotplex/provider"
	"github.com/joho/godotenv"
)

var (
	version = "v0.0.0-dev"
	commit  = "unknown"
	builtBy = "source"
)

func main() {
	// Parse command line flags
	configDir := flag.String("config-dir", "", "ChatApps config directory (takes priority over HOTPLEX_CHATAPPS_CONFIG_DIR env var)")
	serverConfig := flag.String("config", "", "Server config YAML file (takes priority over HOTPLEX_SERVER_CONFIG env var)")
	envFileFlag := flag.String("env-file", "", "Path to .env file")
	flag.Parse()

	// 0. Ensure HOME environment variable is set (critical for path expansion)
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		if h, err := os.UserHomeDir(); err == nil {
			homeDir = h
			_ = os.Setenv("HOME", homeDir)
		}
	}

	// 1. Load .env file with robust discovery
	// Priority: 1. Flag > 2. ENV_FILE env > 3. .env in CWD > 4. .env in XDG config
	envPath := *envFileFlag
	if envPath == "" {
		envPath = os.Getenv("ENV_FILE")
	}

	if envPath != "" {
		if err := godotenv.Load(envPath); err != nil {
			slog.Warn("Failed to load specified env file", "path", envPath, "error", err)
		} else {
			_ = os.Setenv("ENV_FILE", envPath)
		}
	} else {
		// Default discovery
		candidates := []string{
			".env",                                 // Current directory
			filepath.Join(sys.ConfigDir(), ".env"), // XDG fallback
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				if err := godotenv.Load(c); err == nil {
					_ = os.Setenv("ENV_FILE", c)
					break
				}
			}
		}
	}

	// Expand tilde (~) in path environment variables after loading .env
	// godotenv.Load does not expand ~, so we use sys.ExpandPath
	pathEnvVars := []string{
		"HOTPLEX_PROJECTS_DIR",
		"HOTPLEX_DATA_ROOT",
		"HOTPLEX_MESSAGE_STORE_SQLITE_PATH",
		"HOTPLEX_CHATAPPS_CONFIG_DIR",
	}
	for _, envVar := range pathEnvVars {
		if val := os.Getenv(envVar); val != "" {
			_ = os.Setenv(envVar, sys.ExpandPath(val)) // errcheck: ignore error
		}
	}

	// 2. Configure logging level (pre-init for system info)
	logLevel := slog.LevelInfo
	if strings.ToLower(os.Getenv("HOTPLEX_LOG_LEVEL")) == "debug" {
		logLevel = slog.LevelDebug
	}

	logFormat := "text"
	if os.Getenv("HOTPLEX_LOG_FORMAT") == "json" {
		logFormat = "json"
	}

	// 1.2 Load server configuration from YAML
	serverConfigPath := config.ResolveConfigPath(*serverConfig)
	var serverCfg *config.ServerLoader
	if serverConfigPath != "" {
		var err error
		serverCfg, err = config.NewServerLoader(serverConfigPath, nil) // Logging still using default slog
		if err != nil {
			slog.Warn("Failed to load server config", "error", err)
		}
	}

	if serverCfg != nil {
		cfg := serverCfg.Get()
		// Log Level
		switch strings.ToUpper(cfg.Server.LogLevel) {
		case "DEBUG":
			logLevel = slog.LevelDebug
		case "WARN":
			logLevel = slog.LevelWarn
		case "ERROR":
			logLevel = slog.LevelError
		}
		// Log Format
		logFormat = strings.ToLower(cfg.Server.LogFormat)
	}

	var handler slog.Handler
	logOpts := &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: true, // Enable file:line for better error localization
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Customize source path format to be more concise
			if a.Key == slog.SourceKey {
				if source, ok := a.Value.Any().(*slog.Source); ok {
					file := source.File
					// 1. Strip the module prefix
					file = strings.TrimPrefix(file, "github.com/hrygo/hotplex/")
					// 2. Strip leading ./ if any
					file = strings.TrimPrefix(file, "./")

					return slog.String("source", file) // Simplified for pre-commit compliance
				}
			}
			return a
		},
	}

	if logFormat == "json" {
		handler = slog.NewJSONHandler(os.Stdout, logOpts)
	} else {
		// Default to Text logs for better readability during local development
		handler = slog.NewTextHandler(os.Stdout, logOpts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)

	// Print System Info
	logger.Info("🔥 HotPlex Daemon initialized",
		"version", version,
		"commit", commit,
		"built_by", builtBy,
		"home_dir", homeDir,
		"env_file", os.Getenv("ENV_FILE"),
		"server_config", serverConfigPath,
		"port", os.Getenv("HOTPLEX_PORT"),
		"log_level", logLevel)

	// 1.1 Initialize Native Brain (System 1)
	if err := brain.Init(logger); err != nil {
		logger.Warn("Native Brain initialization error (fail-open)", "error", err)
	}

	// Update loader with initialized logger
	if serverCfg != nil {
		// Re-initialize with proper logger
		serverCfg, _ = config.NewServerLoader(serverConfigPath, logger)
	}

	// 2. Initialize HotPlex Core Engine
	idleTimeout := 1 * time.Hour
	executionTimeout := 30 * time.Minute
	var baseSystemPrompt string

	if serverCfg != nil {
		idleTimeout = serverCfg.GetIdleTimeout()
		executionTimeout = serverCfg.GetTimeout()
		baseSystemPrompt = serverCfg.GetSystemPrompt()
	}

	// 2.1 Decide Provider
	providerType := provider.ProviderType(os.Getenv("HOTPLEX_PROVIDER_TYPE"))
	if providerType == "" {
		providerType = provider.ProviderTypeClaudeCode
	}

	providerBinary := os.Getenv("HOTPLEX_PROVIDER_BINARY")
	providerModel := os.Getenv("HOTPLEX_PROVIDER_MODEL")

	prv, err := provider.CreateProvider(provider.ProviderConfig{
		Type:         providerType,
		Enabled:      provider.PtrBool(true),
		BinaryPath:   providerBinary,
		DefaultModel: providerModel,
	})
	if err != nil {
		logger.Error("Failed to create provider", "type", providerType, "error", err)
		os.Exit(1)
	}

	// Load API key for admin operations
	var adminToken string
	if serverCfg != nil {
		adminToken = serverCfg.Get().Security.APIKey
	}

	// Warn if admin token is not configured
	if adminToken == "" {
		logger.Warn("SECURITY WARNING: No admin token configured. " +
			"Bypass mode will be unavailable. " +
			"Set HOTPLEX_API_KEY or HOTPLEX_API_KEYS environment variable for production use.")
	} else {
		logger.Info("Admin token configured", "token_length", len(adminToken))
	}

	opts := hotplex.EngineOptions{
		Timeout:          executionTimeout,
		IdleTimeout:      idleTimeout,
		Logger:           logger,
		AdminToken:       adminToken,
		Provider:         prv,
		BaseSystemPrompt: baseSystemPrompt,
	}

	engine, err := hotplex.NewEngine(opts)
	if err != nil {
		logger.Error("Failed to initialize HotPlex engine", "error", err)
		os.Exit(1)
	}

	// 2. Initialize CORS configuration and WebSocket handler
	var securityKeys []string
	if serverCfg != nil {
		securityKeys = append(securityKeys, serverCfg.Get().Security.APIKey)
	}
	corsConfig := server.NewSecurityConfig(logger, securityKeys...)
	wsHandler := server.NewHotPlexWSHandler(engine, logger, corsConfig)
	http.Handle("/ws/v1/agent", wsHandler)

	// 2.1 Initialize OpenCode compatibility server
	if os.Getenv("HOTPLEX_OPENCODE_COMPAT_ENABLED") != "false" {
		openCodeSrv := server.NewOpenCodeHTTPHandler(engine, logger, corsConfig)
		ocRouter := mux.NewRouter()
		openCodeSrv.RegisterRoutes(ocRouter)
		http.Handle("/global/", ocRouter)
		http.Handle("/session", ocRouter)
		http.Handle("/session/", ocRouter)
		http.Handle("/config", ocRouter)
		logger.Info("OpenCode compatibility server initialized")
	}

	// 2.2 Initialize Observability handlers
	healthHandler := server.NewHealthHandler()
	metricsHandler := server.NewMetricsHandler()
	readyHandler := server.NewReadyHandler(func() bool { return engine != nil })
	liveHandler := server.NewLiveHandler()

	http.Handle("/health", healthHandler)
	http.Handle("/health/ready", readyHandler)
	http.Handle("/health/live", liveHandler)
	http.Handle("/metrics", metricsHandler)

	// 3. Initialize ChatApps adapters
	var chatappsMgr *chatapps.AdapterManager
	if chatapps.IsEnabled(*configDir) {
		var chatappsHandler http.Handler
		var err error
		// configDir from --config flag takes priority over env var
		chatappsHandler, chatappsMgr, err = chatapps.Setup(context.Background(), logger, *configDir)
		if err != nil {
			logger.Error("Failed to setup chatapps", "error", err)
		} else {
			http.Handle("/webhook/", chatappsHandler)
			logger.Info("ChatApps adapters initialized and webhooks registered")
		}
	}

	// Cleanup safety net (deferred immediately after engines/mgrs are ready)
	defer func() {
		logger.Info("Executing final cleanup safety net...")
		if chatappsMgr != nil {
			if err := chatappsMgr.StopAll(); err != nil {
				logger.Error("ChatApps cleanup failed", "error", err)
			}
		}
		if engine != nil {
			if err := engine.Close(); err != nil {
				logger.Error("Core engine cleanup failed", "error", err)
			}
		}
	}()

	// 4. Start HTTP server
	port := "8080"
	if serverCfg != nil {
		port = serverCfg.GetPort()
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: http.DefaultServeMux,
	}

	go func() {
		logger.Info("Listening on", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Server failed", "error", err)
			stop <- syscall.SIGTERM
		}
	}()

	<-stop
	logger.Info("Shutting down gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown failed", "error", err)
	}
}

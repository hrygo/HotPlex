package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/hrygo/hotplex"
	"github.com/hrygo/hotplex/brain"
	"github.com/hrygo/hotplex/chatapps"
	"github.com/hrygo/hotplex/internal/server"
	"github.com/hrygo/hotplex/provider"
	"github.com/joho/godotenv"
)

var (
	version = "v0.0.0-dev"
	commit  = "unknown"
	date    = "unknown"
	builtBy = "source"
)

func main() {
	// Parse command line flags
	configDir := flag.String("config", "", "ChatApps config directory (takes priority over HOTPLEX_CHATAPPS_CONFIG_DIR env var)")
	flag.Parse()

	// 0. Load .env file
	// Priority: ENV_FILE env var > .env in working directory
	envFile := os.Getenv("ENV_FILE")
	if envFile != "" {
		if err := godotenv.Load(envFile); err != nil {
			// Log warning but continue
			fmt.Fprintf(os.Stderr, "Warning: Failed to load ENV_FILE=%s: %v\n", envFile, err)
		}
	} else {
		if err := godotenv.Load(); err != nil {
			// It's okay if .env doesn't exist, we'll use environmental variables or defaults
			_ = err
		}
	}

	// 1. Configure logging
	logLevel := slog.LevelInfo
	if val := os.Getenv("HOTPLEX_LOG_LEVEL"); val != "" {
		switch strings.ToUpper(val) {
		case "DEBUG":
			logLevel = slog.LevelDebug
		case "WARN":
			logLevel = slog.LevelWarn
		case "ERROR":
			logLevel = slog.LevelError
		}
	}

	logFormat := strings.ToLower(os.Getenv("HOTPLEX_LOG_FORMAT"))
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

					return slog.String("source", fmt.Sprintf("%s:%d", file, source.Line))
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
	logger.Info("Starting HotPlex Proxy Server...",
		"version", version,
		"commit", commit,
		"build_time", date,
		"built_by", builtBy,
		"log_level", logLevel)

	// 1.1 Initialize Native Brain (System 1)
	if err := brain.Init(logger); err != nil {
		logger.Warn("Native Brain initialization error (fail-open)", "error", err)
	}

	// 2. Initialize HotPlex Core Engine
	idleTimeout := 1 * time.Hour
	if val := os.Getenv("HOTPLEX_IDLE_TIMEOUT"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			idleTimeout = d
		}
	}

	executionTimeout := 30 * time.Minute
	if val := os.Getenv("HOTPLEX_EXECUTION_TIMEOUT"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			executionTimeout = d
		}
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
	adminToken := os.Getenv("HOTPLEX_API_KEY")
	if keys := os.Getenv("HOTPLEX_API_KEYS"); keys != "" {
		adminToken = strings.Split(keys, ",")[0]
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
		Timeout:     executionTimeout,
		IdleTimeout: idleTimeout,
		Logger:      logger,
		AdminToken:  adminToken,
		Provider:    prv,
	}

	engine, err := hotplex.NewEngine(opts)
	if err != nil {
		logger.Error("Failed to initialize HotPlex engine", "error", err)
		os.Exit(1)
	}

	// 2. Initialize CORS configuration and WebSocket handler
	corsConfig := server.NewSecurityConfig(logger)
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
	port := os.Getenv("HOTPLEX_PORT")
	if port == "" {
		port = "8080"
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

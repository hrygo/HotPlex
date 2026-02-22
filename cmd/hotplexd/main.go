package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/hrygo/hotplex"
	"github.com/hrygo/hotplex/internal/server"
)

func main() {
	// Configure logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	logger.Info("Starting HotPlex Proxy Server...")

	// 1. Initialize HotPlex Core Engine
	idleTimeout := 30 * time.Minute
	if val := os.Getenv("IDLE_TIMEOUT"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			idleTimeout = d
		}
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
		Timeout:     30 * time.Minute,
		IdleTimeout: idleTimeout,
		Logger:      logger,
		AdminToken:  adminToken,
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
	openCodeSrv := server.NewOpenCodeHTTPHandler(engine, logger, corsConfig)
	ocRouter := mux.NewRouter()
	openCodeSrv.RegisterRoutes(ocRouter)
	// We mount the mux router to the default ServeMux
	// Note: We need to handle /global, /session, /config etc.
	// We can use a prefix or just individual registrations.
	// OpenCode SDK expects these at the root by default if baseURL is set to the host.
	http.Handle("/global/", ocRouter)
	http.Handle("/session", ocRouter)
	http.Handle("/session/", ocRouter)
	http.Handle("/config", ocRouter)

	// 2.2 Initialize Observability handlers
	healthHandler := server.NewHealthHandler()
	metricsHandler := server.NewMetricsHandler()
	readyHandler := server.NewReadyHandler(func() bool { return engine != nil })
	liveHandler := server.NewLiveHandler()

	http.Handle("/health", healthHandler)
	http.Handle("/health/ready", readyHandler)
	http.Handle("/health/live", liveHandler)
	http.Handle("/metrics", metricsHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// 3. Listen for OS signals to ensure graceful shutdown of engine and child processes
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
			os.Exit(1)
		}
	}()

	<-stop
	logger.Info("Shutting down gracefully...")

	// Create a timeout context for shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown failed", "error", err)
	}

	// engine.Close() will sweep all active process groups
	if err := engine.Close(); err != nil {
		logger.Error("Engine shutdown failed", "error", err)
	}

	logger.Info("HotPlex Proxy Server stopped")
}

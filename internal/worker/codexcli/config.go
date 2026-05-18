package codexcli

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/security"
)

var globalConfig atomic.Pointer[config.CodexCLIConfig]

func InitConfig(cfg config.CodexCLIConfig) {
	if cfg.Command == "" {
		cfg.Command = "codex"
	}
	globalConfig.Store(&cfg)
	if err := security.RegisterCommand("codex"); err != nil {
		slog.Default().Warn("codexcli: failed to register command", "err", err)
	}
}

func GetConfig() config.CodexCLIConfig {
	if p := globalConfig.Load(); p != nil {
		return *p
	}
	return config.CodexCLIConfig{}
}

var globalSingleton atomic.Pointer[CodexAppServerManager]

func InitSingleton(log *slog.Logger, cfg config.CodexCLIConfig) {
	InitConfig(cfg)
	mgr := NewCodexAppServerManager(log, GetConfig())
	globalSingleton.Store(mgr)
}

func ShutdownSingleton(ctx context.Context) {
	if m := globalSingleton.Load(); m != nil {
		m.Shutdown(ctx)
		globalSingleton.Store(nil)
	}
}

func GetSingleton() *CodexAppServerManager {
	return globalSingleton.Load()
}

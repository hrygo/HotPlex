// Package config defines the gateway configuration types, default values,
// loading pipeline, and environment-variable overrides.
//
// The configuration system is split across several files by responsibility:
//   - config_types.go    — struct and constant definitions
//   - config_defaults.go — Default() and propagation/normalization helpers
//   - config_loader.go   — Load(), loadRecursive(), path resolution
//   - config_env.go      — environment-variable mapping via reflection
//   - config.go          — shared utilities (env expansion, validation, paths)
package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var envVarRe = regexp.MustCompile(`\$\{([^}:]+)(?::-([^}]*))?\}`)

// warnedEnvEntries deduplicates env-entry exclusion warnings so the same
// message is only logged once per process, even when Load is called many times.
var warnedEnvEntries sync.Map

// ExpandEnv expands ${VAR} and ${VAR:-default} references in a config value
// using os.Getenv.  This is used to reference secrets (or other values) from
// environment variables within config file values, e.g.:
//
//	db_password: "${DB_PASSWORD:-}"
//
// Unset variables without defaults expand to empty string.
func ExpandEnv(s string) string {
	return envVarRe.ReplaceAllStringFunc(s, func(match string) string {
		parts := envVarRe.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		key := parts[1]
		val := os.Getenv(key)
		if val == "" && len(parts) >= 3 {
			val = parts[2]
		}
		return val
	})
}

// expandEnvEntry expands ${VAR} references in an environment entry.
// Entries that reference unset variables without a default clause are excluded
// (returned as false) so the entry is omitted from the worker environment.
// A variable set to empty string is treated as unset; use ${VAR:-default} to preserve such entries.
func expandEnvEntry(entry string) (string, bool) {
	for _, m := range envVarRe.FindAllStringSubmatch(entry, -1) {
		if strings.Contains(m[0], ":-") {
			continue // has :-default clause, skip exclusion check
		}
		if len(m) >= 2 && os.Getenv(m[1]) == "" {
			return "", false // unset var, no default → exclude
		}
	}
	return ExpandEnv(entry), true
}

// Validate checks that all required configuration fields are set.
func (c *Config) Validate() []string {
	var errs []string

	if c.Gateway.Addr == "" {
		errs = append(errs, "gateway.addr is required (or use default :8080)")
	}
	if c.DB.Driver == "" || c.DB.Driver == "sqlite" {
		if c.DB.Path == "" && c.DB.SQLite.Path == "" {
			errs = append(errs, "db.path or db.sqlite.path is required (or use default hotplex.db)")
		}
	} else if strings.EqualFold(c.DB.Driver, "postgres") || strings.EqualFold(c.DB.Driver, "pg") || strings.EqualFold(c.DB.Driver, "postgresql") {
		if c.DB.Postgres.ConnStr == "" {
			errs = append(errs, "db.postgres.dsn is required when db.driver is postgres")
		}
	} else {
		errs = append(errs, `db.driver must be "sqlite" or "postgres" (got: `+c.DB.Driver+`)`)
	}
	if c.Session.RetentionPeriod <= 0 {
		errs = append(errs, "session.retention_period must be positive")
	}
	if c.Pool.MaxSize <= 0 {
		errs = append(errs, "pool.max_size must be positive")
	}
	if c.Pool.MaxIdlePerUser < 0 {
		errs = append(errs, "pool.max_idle_per_user must be non-negative")
	}
	if c.Pool.MaxPerWorkspace < 0 {
		errs = append(errs, "pool.max_per_workspace must be non-negative")
	}
	if c.Pool.MaxMemoryPerUser < 0 {
		errs = append(errs, "pool.max_memory_per_user must be non-negative")
	}
	// Warn (not error) for TLS on non-local address.
	if !c.Security.TLSEnabled &&
		!strings.Contains(c.Gateway.Addr, "localhost") &&
		!strings.Contains(c.Gateway.Addr, "127.0.0.1") &&
		!strings.Contains(c.Gateway.Addr, "[::1]") {
		errs = append(errs, "TLS is disabled on non-local address; enable tls_enabled for production")
	}
	if c.Log.Format != "" && c.Log.Format != "json" && c.Log.Format != "text" {
		errs = append(errs, "log.format must be either 'json' or 'text'")
	}
	// Exec mode was removed; force app-server mode.
	if !c.Worker.CodexCLI.UseAppServer {
		slog.Warn("config: codex_cli.use_app_server is deprecated, forcing app-server mode")
		c.Worker.CodexCLI.UseAppServer = true
	}

	return errs
}

// DefaultConfigPath is the default configuration file path used by the CLI
// and the gateway. Defined here as the single source of truth to avoid
// duplication across packages.
const DefaultConfigPath = "~/.hotplex/config.yaml"

// HotplexHome returns the base directory for all HotPlex state (~/.hotplex).
// It does not create the directory — callers should use ensureDir or rely on
// the components that need the directory to create it on first use.
func HotplexHome() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return TempBaseDir()
	}
	return filepath.Join(home, ".hotplex")
}

// ResolveInjectExclude resolves the inject_exclude list using 3-level fallback:
// bot → platform → global. A nil slice means "not configured" and falls back;
// a non-nil empty slice (e.g. YAML `inject_exclude: []`) means "explicitly clear,
// override parent level". Returns nil when no exclusion is configured at any level,
// which means full injection (backward-compatible default).
func ResolveInjectExclude(global, platform, bot []string) []string {
	if bot != nil {
		return bot
	}
	if platform != nil {
		return platform
	}
	return global
}

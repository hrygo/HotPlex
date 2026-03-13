package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/hrygo/hotplex/internal/sys"
)

// ServerConfig represents the YAML configuration for the hotplexd server.
type ServerConfig struct {
	Engine   EngineConfig   `yaml:"engine"`
	Server   ServerSettings `yaml:"server"`
	Security SecurityConfig `yaml:"security"`
}

// EngineConfig contains engine-level configuration.
type EngineConfig struct {
	Timeout         time.Duration `yaml:"timeout"`
	IdleTimeout     time.Duration `yaml:"idle_timeout"`
	WorkDir         string        `yaml:"work_dir"`
	SystemPrompt    string        `yaml:"system_prompt"`
	AllowedTools    []string      `yaml:"allowed_tools"`
	DisallowedTools []string      `yaml:"disallowed_tools"`
}

// ServerSettings contains server-level settings.
type ServerSettings struct {
	Port      string `yaml:"port"`
	LogLevel  string `yaml:"log_level"`
	LogFormat string `yaml:"log_format"`
}

// SecurityConfig contains security settings.
type SecurityConfig struct {
	APIKey         string `yaml:"api_key"`
	PermissionMode string `yaml:"permission_mode"`
}

// ServerLoader loads and manages server configuration from YAML files.
type ServerLoader struct {
	configPath string
	config     *ServerConfig
	mu         sync.RWMutex
	logger     *slog.Logger
}

// NewServerLoader creates a new server configuration loader.
func NewServerLoader(configPath string, logger *slog.Logger) (*ServerLoader, error) {
	if logger == nil {
		logger = slog.Default()
	}

	loader := &ServerLoader{
		configPath: configPath,
		logger:     logger,
		config:     &ServerConfig{},
	}

	if err := loader.Load(); err != nil {
		return nil, err
	}

	return loader, nil
}

// Load loads the server configuration from the YAML file.
func (l *ServerLoader) Load() error {
	// Check if file exists
	if _, err := os.Stat(l.configPath); os.IsNotExist(err) {
		l.logger.Warn("Server config file not found, using defaults", "path", l.configPath)
		return nil
	}

	data, err := os.ReadFile(l.configPath)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}

	// Expand environment variables in the YAML content itself
	expanded := os.ExpandEnv(string(data))

	if err := yaml.Unmarshal([]byte(expanded), l.config); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	// Override with environment variables (higher priority)
	l.populateFromEnv()

	// Validate configuration
	if err := l.validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	l.logger.Info("Server config loaded", "path", l.configPath)
	return nil
}

// populateFromEnv overrides configuration values with environment variables.
func (l *ServerLoader) populateFromEnv() {
	if val := os.Getenv("HOTPLEX_API_KEY"); val != "" {
		l.config.Security.APIKey = val
	}
	if val := os.Getenv("HOTPLEX_PORT"); val != "" {
		l.config.Server.Port = val
	}
	if val := os.Getenv("HOTPLEX_LOG_LEVEL"); val != "" {
		l.config.Server.LogLevel = val
	}
	if val := os.Getenv("HOTPLEX_LOG_FORMAT"); val != "" {
		l.config.Server.LogFormat = val
	}
	if val := os.Getenv("HOTPLEX_IDLE_TIMEOUT"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			l.config.Engine.IdleTimeout = d
		}
	}
	if val := os.Getenv("HOTPLEX_EXECUTION_TIMEOUT"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			l.config.Engine.Timeout = d
		}
	}
}

// validate validates the server configuration.
func (l *ServerLoader) validate() error {
	// Validate permission mode
	lowerPerm := strings.ToLower(l.config.Security.PermissionMode)
	switch lowerPerm {
	case "", "strict", "bypass-permissions":
		// valid
	default:
		return fmt.Errorf("invalid permission_mode: %q (must be 'strict', 'bypass-permissions', or empty for default)", l.config.Security.PermissionMode)
	}

	// Validate log level
	lowerLevel := strings.ToLower(l.config.Server.LogLevel)
	switch lowerLevel {
	case "", "debug", "info", "warn", "error":
		// valid
	default:
		return fmt.Errorf("invalid log_level: %q (must be 'debug', 'info', 'warn', 'error', or empty for default)", l.config.Server.LogLevel)
	}

	// Validate timeout ranges
	if l.config.Engine.Timeout > 24*time.Hour {
		return fmt.Errorf("timeout too large: %v (max 24h)", l.config.Engine.Timeout)
	}
	if l.config.Engine.IdleTimeout > 7*24*time.Hour {
		return fmt.Errorf("idle_timeout too large: %v (max 7 days)", l.config.Engine.IdleTimeout)
	}

	return nil
}

// Get returns the current server configuration.
func (l *ServerLoader) Get() *ServerConfig {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.config
}

// GetSystemPrompt returns the base system prompt.
func (l *ServerLoader) GetSystemPrompt() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.config.Engine.SystemPrompt
}

// GetTimeout returns the engine timeout.
func (l *ServerLoader) GetTimeout() time.Duration {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.config.Engine.Timeout == 0 {
		return 30 * time.Minute
	}
	return l.config.Engine.Timeout
}

// GetIdleTimeout returns the idle timeout.
func (l *ServerLoader) GetIdleTimeout() time.Duration {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.config.Engine.IdleTimeout == 0 {
		return 1 * time.Hour
	}
	return l.config.Engine.IdleTimeout
}

// GetWorkDir returns the working directory.
func (l *ServerLoader) GetWorkDir() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.config.Engine.WorkDir == "" {
		return "/tmp/hotplex_sandbox"
	}
	return l.config.Engine.WorkDir
}

// GetPort returns the server port.
func (l *ServerLoader) GetPort() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.config.Server.Port == "" {
		return "8080"
	}
	return l.config.Server.Port
}

// ResolveConfigPath resolves the config file path from various sources.
// Search order:
// 1. Explicit path passed as argument
// 2. HOTPLEX_SERVER_CONFIG environment variable
// 3. XDG standard config directory (~/.config/hotplex/server.yaml)
// 4. Current working directory (./configs/server.yaml)
// 5. Project root fallback (../configs/server.yaml)
func ResolveConfigPath(explicitPath string) string {
	// 1. Explicit path
	if explicitPath != "" {
		if _, err := os.Stat(explicitPath); err == nil {
			return explicitPath
		}
	}

	// 2. Env variable
	if envPath := os.Getenv("HOTPLEX_SERVER_CONFIG"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath
		}
	}

	// 3. XDG Config Directory
	xdgPath := filepath.Join(sys.ConfigDir(), "server.yaml")
	if _, err := os.Stat(xdgPath); err == nil {
		return xdgPath
	}

	// 4. Current working directory default
	cwd, _ := os.Getwd()
	if cwd != "" {
		cwdPath := filepath.Join(cwd, "configs", "server.yaml")
		if _, err := os.Stat(cwdPath); err == nil {
			return cwdPath
		}

		// 5. Project root fallback
		projectRoot := filepath.Join(cwd, "..", "configs", "server.yaml")
		if _, err := os.Stat(projectRoot); err == nil {
			return projectRoot
		}
	}

	return ""
}

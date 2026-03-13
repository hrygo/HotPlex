package chatapps

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/provider"
	"gopkg.in/yaml.v3"
)

type PlatformConfig struct {
	Platform         string                  `yaml:"platform"`
	Mode             string                  `yaml:"mode"`
	SystemPrompt     string                  `yaml:"system_prompt"`
	TaskInstructions string                  `yaml:"task_instructions"`
	Engine           EngineConfig            `yaml:"engine"`
	Provider         provider.ProviderConfig `yaml:"provider"`
	Security         SecurityConfig          `yaml:"security"`
	Features         FeaturesConfig          `yaml:"features"`
	Session          SessionConfig           `yaml:"session"`
	MessageStore     MessageStoreConfig      `yaml:"message_store,omitempty"`
	Options          map[string]any          `yaml:"options,omitempty"`
	SourceFile       string                  `yaml:"-"` // Tracks which file this config was loaded from
}

type SecurityConfig struct {
	VerifySignature *bool            `yaml:"verify_signature"`
	Permission      PermissionConfig `yaml:"permission"`
	Owner           *OwnerConfig     `yaml:"owner,omitempty"`
}

type PermissionConfig struct {
	DMPolicy              string                 `yaml:"dm_policy"`
	GroupPolicy           string                 `yaml:"group_policy"`
	BotUserID             string                 `yaml:"bot_user_id"`
	BroadcastResponse     string                 `yaml:"broadcast_response"` // Response for broadcast messages (multibot mode)
	AllowedUsers          []string               `yaml:"allowed_users"`
	BlockedUsers          []string               `yaml:"blocked_users"`
	SlashCommandRateLimit float64                `yaml:"slash_command_rate_limit"`
	ThreadOwnership       *ThreadOwnershipConfig `yaml:"thread_ownership,omitempty"`
}

// FeaturesConfig contains feature toggles for UI/UX experience.
type FeaturesConfig struct {
	Chunking  ChunkingConfig  `yaml:"chunking"`
	Threading ThreadingConfig `yaml:"threading"`
	RateLimit RateLimitConfig `yaml:"rate_limit"`
	Markdown  MarkdownConfig  `yaml:"markdown"`
}

type ChunkingConfig struct {
	Enabled  *bool `yaml:"enabled"`
	MaxChars int   `yaml:"max_chars"`
}

type ThreadingConfig struct {
	Enabled *bool `yaml:"enabled"`
}

type RateLimitConfig struct {
	Enabled     *bool `yaml:"enabled"`
	MaxAttempts int   `yaml:"max_attempts"`
	BaseDelayMs int   `yaml:"base_delay_ms"`
	MaxDelayMs  int   `yaml:"max_delay_ms"`
}

type MarkdownConfig struct {
	Enabled *bool `yaml:"enabled"`
}

// OwnerConfig defines bot ownership and access control (Phase 1: Bot Behavior Spec)
type OwnerConfig struct {
	Primary string   `yaml:"primary"` // slack user ID
	Trusted []string `yaml:"trusted_users"`
	Policy  string   `yaml:"policy"` // owner_only | trusted | public
}

// ThreadOwnershipConfig defines thread ownership tracking behavior (Phase 1: Bot Behavior Spec)
type ThreadOwnershipConfig struct {
	Enabled *bool         `yaml:"enabled"`
	TTL     time.Duration `yaml:"ttl"`
	Persist *bool         `yaml:"persist"`
}

type SessionConfig struct {
	Timeout         time.Duration `yaml:"timeout"`
	CleanupInterval time.Duration `yaml:"cleanup_interval"`
}

type EngineConfig struct {
	Timeout         time.Duration `yaml:"timeout"`
	IdleTimeout     time.Duration `yaml:"idle_timeout"`
	WorkDir         string        `yaml:"work_dir"`
	AllowedTools    []string      `yaml:"allowed_tools"`
	DisallowedTools []string      `yaml:"disallowed_tools"`
}

// MessageStoreConfig 消息存储配置 (Phase 3)
type MessageStoreConfig struct {
	Enabled   *bool           `yaml:"enabled"`
	Type      string          `yaml:"type"` // sqlite | postgres | memory
	SQLite    SQLiteConfig    `yaml:"sqlite"`
	Postgres  PostgresConfig  `yaml:"postgres"`
	Strategy  string          `yaml:"strategy"`
	Streaming StreamingConfig `yaml:"streaming"`
}

type SQLiteConfig struct {
	Path      string `yaml:"path"`
	MaxSizeMB int    `yaml:"max_size_mb"`
}

type PostgresConfig struct {
	DSN            string `yaml:"dsn"`
	MaxConnections int    `yaml:"max_connections"`
	Level          int    `yaml:"level"` // 1=百万级，2=亿级
}

type StreamingConfig struct {
	Enabled       *bool         `yaml:"enabled"`
	BufferSize    int           `yaml:"buffer_size"`
	Timeout       time.Duration `yaml:"timeout"`
	StoragePolicy string        `yaml:"storage_policy"` // complete_only | all_chunks
}

// BoolValue returns the value of a bool pointer if not nil, otherwise returns defaultVal.
func BoolValue(pb *bool, defaultVal bool) bool {
	if pb == nil {
		return defaultVal
	}
	return *pb
}

type Logger = slog.Logger

type ConfigLoader struct {
	configs      map[string]*PlatformConfig
	mu           sync.RWMutex
	logger       *slog.Logger
	hotReloaders map[string]*config.YAMLHotReloader
}

func NewConfigLoader(configDir string, logger *slog.Logger) (*ConfigLoader, error) {
	loader := &ConfigLoader{
		configs:      make(map[string]*PlatformConfig),
		hotReloaders: make(map[string]*config.YAMLHotReloader),
		logger:       logger,
	}

	if err := loader.Load(configDir); err != nil {
		return nil, fmt.Errorf("load configs: %w", err)
	}

	return loader, nil
}

// expandEnvRecursive expands environment variables recursively until no more variables are found.
// Supports both ${VAR} and $VAR syntax.
// Also handles HOME fallback and ~ (tilde) expansion.
func expandEnvRecursive(s string) string {
	// Expand in a loop until no more changes (recursive expansion)
	// Limit iterations to prevent infinite loops
	const maxIterations = 5

	for i := 0; i < maxIterations; i++ {
		prev := s
		s = os.Expand(s, func(vars string) string {
			val := os.Getenv(vars)

			// Handle HOME fallback
			if vars == "HOME" && val == "" {
				if home, err := os.UserHomeDir(); err == nil {
					return home
				}
			}

			// Handle ~ (tilde) expansion in the value
			if val != "" && strings.HasPrefix(val, "~") {
				val = os.Expand(val, func(v string) string {
					if v == "HOME" {
						if home, err := os.UserHomeDir(); err == nil {
							return home
						}
					}
					return os.Getenv(v)
				})
			}

			return val
		})

		// Also expand tilde directly (for paths like ~/foo)
		if strings.Contains(s, "~") {
			s = expandTilde(s)
		}

		// If no changes, we're done
		if s == prev {
			break
		}
	}

	return s
}

// expandTilde expands ~ to home directory.
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

func (c *ConfigLoader) Load(configDir string) error {
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return fmt.Errorf("read config dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		filename := filepath.Join(configDir, entry.Name())
		data, err := os.ReadFile(filename)
		if err != nil {
			c.logger.Debug("Could not read config file, skipping", "file", filename, "cause", err)
			continue
		}

		// Expand environment variables in config file with recursive expansion support
		expanded := expandEnvRecursive(string(data))

		var cfg PlatformConfig
		if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
			c.logger.Warn("Failed to parse config file", "file", filename, "error", err)
			continue
		}
		cfg.SourceFile = filename

		if cfg.Platform == "" {
			c.logger.Warn("Config missing platform field", "file", filename)
			continue
		}

		c.mu.Lock()
		c.configs[cfg.Platform] = &cfg
		c.mu.Unlock()
		c.logger.Info("Loaded platform configuration", "platform", cfg.Platform, "file", filename)
	}
	return nil
}

func (c *ConfigLoader) GetConfig(platform string) *PlatformConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if cfg, ok := c.configs[platform]; ok {
		// Return a deep copy to prevent external mutation without holding locks
		cfgCopy := *cfg
		// Deep copy slices
		if cfg.Engine.AllowedTools != nil {
			cfgCopy.Engine.AllowedTools = make([]string, len(cfg.Engine.AllowedTools))
			copy(cfgCopy.Engine.AllowedTools, cfg.Engine.AllowedTools)
		}
		if cfg.Engine.DisallowedTools != nil {
			cfgCopy.Engine.DisallowedTools = make([]string, len(cfg.Engine.DisallowedTools))
			copy(cfgCopy.Engine.DisallowedTools, cfg.Engine.DisallowedTools)
		}
		if cfg.Security.Permission.AllowedUsers != nil {
			cfgCopy.Security.Permission.AllowedUsers = make([]string, len(cfg.Security.Permission.AllowedUsers))
			copy(cfgCopy.Security.Permission.AllowedUsers, cfg.Security.Permission.AllowedUsers)
		}
		if cfg.Security.Permission.BlockedUsers != nil {
			cfgCopy.Security.Permission.BlockedUsers = make([]string, len(cfg.Security.Permission.BlockedUsers))
			copy(cfgCopy.Security.Permission.BlockedUsers, cfg.Security.Permission.BlockedUsers)
		}
		// Deep copy map
		if cfg.Options != nil {
			cfgCopy.Options = make(map[string]any, len(cfg.Options))
			for k, v := range cfg.Options {
				cfgCopy.Options[k] = v
			}
		}
		cfgCopy.SourceFile = cfg.SourceFile
		return &cfgCopy
	}
	return nil
}

func (c *ConfigLoader) GetSystemPrompt(platform string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if cfg, ok := c.configs[platform]; ok {
		return cfg.SystemPrompt
	}
	return ""
}

func (c *ConfigLoader) GetTaskInstructions(platform string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if cfg, ok := c.configs[platform]; ok {
		return cfg.TaskInstructions
	}
	return ""
}

func (c *ConfigLoader) HasPlatform(platform string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	_, ok := c.configs[platform]
	return ok
}

func (c *ConfigLoader) Platforms() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	platforms := make([]string, 0, len(c.configs))
	for p := range c.configs {
		platforms = append(platforms, p)
	}
	return platforms
}

func (c *ConfigLoader) GetOptions(platform string) map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if cfg, ok := c.configs[platform]; ok {
		return deepCopyMap(cfg.Options)
	}
	return nil
}

// deepCopyMap creates a deep copy of a map to prevent accidental mutation
func deepCopyMap(original map[string]any) map[string]any {
	if original == nil {
		return nil
	}
	// Use JSON marshal/unmarshal for deep copy
	data, err := json.Marshal(original)
	if err != nil {
		return nil
	}
	var copy map[string]any
	if err := json.Unmarshal(data, &copy); err != nil {
		return nil
	}
	return copy
}

// StartHotReload starts watching all config files for changes and automatically reloads them.
// The onReload callback is called with the updated PlatformConfig for each platform.
func (c *ConfigLoader) StartHotReload(ctx context.Context, configDir string, onReload func(platform string, cfg *PlatformConfig)) error {
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return fmt.Errorf("read config dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		filename := filepath.Join(configDir, entry.Name())
		platformName := entry.Name()[:len(entry.Name())-len(".yaml")]

		// Create initial config for this platform
		var initialCfg PlatformConfig
		reloader, err := config.NewYAMLHotReloader(filename, &initialCfg, c.logger)
		if err != nil {
			c.logger.Warn("Failed to create hot reloader", "file", filename, "error", err)
			continue
		}

		// Set up reload callback
		reloader.OnReload(func(cfg any) {
			if updatedCfg, ok := cfg.(*PlatformConfig); ok {
				c.mu.Lock()
				c.configs[platformName] = updatedCfg
				c.mu.Unlock()

				c.logger.Info("Config hot reloaded", "platform", platformName)
				if onReload != nil {
					onReload(platformName, updatedCfg)
				}
			}
		})

		// Start watching
		if err := reloader.Start(ctx); err != nil {
			c.logger.Warn("Failed to start hot reloader", "file", filename, "error", err)
			continue
		}

		c.mu.Lock()
		c.hotReloaders[platformName] = reloader
		c.mu.Unlock()
	}

	return nil
}

// Close stops all hot reload watchers and releases resources.
func (c *ConfigLoader) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var lastErr error
	for platform, reloader := range c.hotReloaders {
		if err := reloader.Close(); err != nil {
			c.logger.Error("Failed to close hot reloader", "platform", platform, "error", err)
			lastErr = err
		}
	}
	c.hotReloaders = make(map[string]*config.YAMLHotReloader)

	return lastErr
}

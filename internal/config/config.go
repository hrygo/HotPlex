package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/viper"
)

var envVarRe = regexp.MustCompile(`\$\{([^}:]+)(?::-([^}]*))?\}`)

// ExpandEnv expands environment variable references in a string.
// Supports the ${VAR} and ${VAR:-default} syntax used in config files.
// Unset variables without defaults are left as-is.
func ExpandEnv(s string) string {
	return envVarRe.ReplaceAllStringFunc(s, func(match string) string {
		parts := envVarRe.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		key := parts[1]
		val := os.Getenv(key)
		if val == "" && len(parts) >= 3 {
			val = parts[2] // Use default.
		}
		return val
	})
}

// SecretsProvider abstracts how secrets are retrieved.
type SecretsProvider interface {
	// Get returns the secret value for the given key, or "" if not found.
	Get(key string) string
}

// EnvSecretsProvider retrieves secrets from environment variables.
type EnvSecretsProvider struct{}

// NewEnvSecretsProvider creates an EnvSecretsProvider.
func NewEnvSecretsProvider() *EnvSecretsProvider {
	return &EnvSecretsProvider{}
}

// Get returns the environment variable value for the given key.
func (p *EnvSecretsProvider) Get(key string) string {
	return os.Getenv(key)
}

// ChainedSecretsProvider tries multiple providers in order until a value is found.
type ChainedSecretsProvider struct {
	providers []SecretsProvider
}

// NewChainedSecretsProvider creates a chained provider from multiple sources.
func NewChainedSecretsProvider(providers ...SecretsProvider) *ChainedSecretsProvider {
	return &ChainedSecretsProvider{providers: providers}
}

// Get returns the first non-empty value from the chained providers.
func (p *ChainedSecretsProvider) Get(key string) string {
	for _, provider := range p.providers {
		if val := provider.Get(key); val != "" {
			return val
		}
	}
	return ""
}

// Validate checks that all required configuration fields are set.
// Returns a list of validation errors.
func (c *Config) Validate() []string {
	var errs []string
	if c.Gateway.Addr == "" {
		errs = append(errs, "gateway.addr is required")
	}
	if c.DB.Path == "" {
		errs = append(errs, "db.path is required")
	}
	if c.Session.RetentionPeriod <= 0 {
		errs = append(errs, "session.retention_period must be positive")
	}
	if c.Pool.MaxSize <= 0 {
		errs = append(errs, "pool.max_size must be positive")
	}
	// Check for insecure TLS in non-dev mode.
	if !c.Security.TLSEnabled && !strings.Contains(c.Gateway.Addr, "localhost") && !strings.Contains(c.Gateway.Addr, "127.0.0.1") {
		errs = append(errs, "TLS is disabled on non-local address; enable tls_enabled for production")
	}
	return errs
}

// Config holds all gateway configuration.
type Config struct {
	Gateway  GatewayConfig  `mapstructure:"gateway"`
	DB       DBConfig       `mapstructure:"db"`
	Worker   WorkerConfig   `mapstructure:"worker"`
	Security SecurityConfig `mapstructure:"security"`
	Session  SessionConfig  `mapstructure:"session"`
	Pool     PoolConfig     `mapstructure:"pool"`
	Admin    AdminConfig    `mapstructure:"admin"`
}

// AdminConfig holds admin API settings.
type AdminConfig struct {
	Enabled            bool              `mapstructure:"enabled"`
	Addr               string            `mapstructure:"addr"`
	Tokens             []string          `mapstructure:"tokens"`
	TokenScopes        map[string][]string `mapstructure:"token_scopes"` // token → scopes
	DefaultScopes      []string          `mapstructure:"default_scopes"` // scopes for tokens not in TokenScopes
	IPWhitelistEnabled bool             `mapstructure:"ip_whitelist_enabled"`
	AllowedCIDRs       []string          `mapstructure:"allowed_cidrs"`
	RateLimitEnabled   bool              `mapstructure:"rate_limit_enabled"`
	RequestsPerSec     int               `mapstructure:"requests_per_sec"`
	Burst              int               `mapstructure:"burst"`
}

// GatewayConfig holds WS gateway settings.
type GatewayConfig struct {
	Addr               string        `mapstructure:"addr"`
	ReadBufferSize     int           `mapstructure:"read_buffer_size"`
	WriteBufferSize    int           `mapstructure:"write_buffer_size"`
	PingInterval       time.Duration `mapstructure:"ping_interval"`
	PongTimeout        time.Duration `mapstructure:"pong_timeout"`
	WriteTimeout       time.Duration `mapstructure:"write_timeout"`
	IdleTimeout        time.Duration `mapstructure:"idle_timeout"`
	MaxFrameSize       int64         `mapstructure:"max_frame_size"`
	BroadcastQueueSize int           `mapstructure:"broadcast_queue_size"`
}

// DBConfig holds SQLite settings.
type DBConfig struct {
	Path         string        `mapstructure:"path"`
	WALMode      bool          `mapstructure:"wal_mode"`
	BusyTimeout  time.Duration `mapstructure:"busy_timeout"`
	MaxOpenConns int           `mapstructure:"max_open_conns"`
}

// WorkerConfig holds per-worker defaults.
type WorkerConfig struct {
	MaxLifetime      time.Duration `mapstructure:"max_lifetime"`
	IdleTimeout      time.Duration `mapstructure:"idle_timeout"`
	ExecutionTimeout time.Duration `mapstructure:"execution_timeout"`
	AllowedEnvs      []string      `mapstructure:"allowed_envs"`
	EnvWhitelist     []string      `mapstructure:"env_whitelist"`
}

// SecurityConfig holds auth and input validation settings.
type SecurityConfig struct {
	APIKeyHeader   string   `mapstructure:"api_key_header"`
	APIKeys        []string `mapstructure:"api_keys"`
	TLSEnabled     bool     `mapstructure:"tls_enabled"`
	TLSCertFile    string   `mapstructure:"tls_cert_file"`
	TLSKeyFile     string   `mapstructure:"tls_key_file"`
	AllowedOrigins []string `mapstructure:"allowed_origins"`
	JWTSecret      []byte   `mapstructure:"-"` // loaded from env or file, not mapstructure
	JWTAudience    string   `mapstructure:"jwt_audience"`
}

// SessionConfig holds session lifecycle settings.
type SessionConfig struct {
	RetentionPeriod   time.Duration `mapstructure:"retention_period"`
	GCScanInterval    time.Duration `mapstructure:"gc_scan_interval"`
	MaxConcurrent     int           `mapstructure:"max_concurrent"`
	EventStoreEnabled bool          `mapstructure:"event_store_enabled"`
}

// PoolConfig holds session pool settings.
type PoolConfig struct {
	MinSize        int `mapstructure:"min_size"`
	MaxSize        int `mapstructure:"max_size"`
	MaxIdlePerUser int `mapstructure:"max_idle_per_user"`
}

// Default returns a Config with sensible production defaults.
func Default() *Config {
	return &Config{
		Gateway: GatewayConfig{
			Addr:               ":8080",
			ReadBufferSize:     4096,
			WriteBufferSize:    4096,
			PingInterval:       30 * time.Second,
			PongTimeout:        10 * time.Second,
			WriteTimeout:       10 * time.Second,
			IdleTimeout:        5 * time.Minute,
			MaxFrameSize:       32 * 1024,
			BroadcastQueueSize: 256,
		},
		DB: DBConfig{
			Path:         "gateway.db",
			WALMode:      true,
			BusyTimeout:  500 * time.Millisecond,
			MaxOpenConns: 1,
		},
		Worker: WorkerConfig{
			MaxLifetime:      24 * time.Hour,
			IdleTimeout:      30 * time.Minute,
			ExecutionTimeout: 10 * time.Minute,
			AllowedEnvs:      []string{},
			EnvWhitelist:     []string{},
		},
		Security: SecurityConfig{
			APIKeyHeader:   "X-API-Key",
			APIKeys:        []string{},
			TLSEnabled:     false,
			AllowedOrigins: []string{"*"},
		},
		Session: SessionConfig{
			RetentionPeriod:   7 * 24 * time.Hour,
			GCScanInterval:    1 * time.Minute,
			MaxConcurrent:     1000,
			EventStoreEnabled: true,
		},
		Pool: PoolConfig{
			MinSize:        0,
			MaxSize:        100,
			MaxIdlePerUser: 3,
		},
		Admin: AdminConfig{
			Enabled:            true,
			Addr:               ":9080",
			Tokens:             []string{},
			TokenScopes:        nil,
			DefaultScopes:      []string{"session:read", "stats:read", "health:read"},
			IPWhitelistEnabled: false,
			AllowedCIDRs:       []string{"127.0.0.0/8", "10.0.0.0/8"},
			RateLimitEnabled:  true,
			RequestsPerSec:     10,
			Burst:              20,
		},
	}}

// Load reads configuration from the given file path and environment variables.
// filePath is the path to the config file (YAML/JSON/TOML). If empty, only env vars are used.
func Load(filePath string) (*Config, error) {
	v := viper.New()
	v.SetTypeByDefaultValue(true)

	if filePath != "" {
		v.SetConfigFile(filePath)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("config: read file %q: %w", filePath, err)
		}
	}

	// Environment variable overrides: GATEWAY_ADDR, DB_PATH, etc.
	v.SetEnvPrefix("HOTPLEX")
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}

	// Post-process: merge allowed_envs into env_whitelist (union).
	if len(cfg.Worker.AllowedEnvs) > 0 {
		seen := make(map[string]bool)
		for _, e := range cfg.Worker.EnvWhitelist {
			seen[e] = true
		}
		for _, e := range cfg.Worker.AllowedEnvs {
			seen[e] = true
		}
		cfg.Worker.EnvWhitelist = nil
		for e := range seen {
			cfg.Worker.EnvWhitelist = append(cfg.Worker.EnvWhitelist, e)
		}
	}

	return &cfg, nil
}

// MustLoad is like Load but panics on error.
func MustLoad(filePath string) *Config {
	cfg, err := Load(filePath)
	if err != nil {
		panic("config.MustLoad: " + err.Error())
	}
	return cfg
}

// ReadFile loads the named config file and returns its raw bytes.
// Used by tests to verify config file parsing.
func ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

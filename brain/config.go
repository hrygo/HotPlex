package brain

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hrygo/hotplex/brain/llm"
)

// === Model Configuration ===

type ModelConfig struct {
	Provider string
	Model    string
	Endpoint string
	TimeoutS int
}

// === Cache Configuration ===

type CacheConfig struct {
	Enabled  bool
	Size     int
}

// === Retry Configuration ===

type RetryConfig struct {
	Enabled     bool
	MaxAttempts int
	MinWaitMs   int
	MaxWaitMs   int
}

// === Metrics Configuration ===

type MetricsConfig struct {
	Enabled         bool
	ServiceName     string
	Endpoint        string
	ExportInterval  time.Duration
}

// === Cost Configuration ===

type CostConfig struct {
	Enabled      bool
	EnableBudget bool
}

// === Rate Limit Configuration ===

type RateLimitConfig struct {
	Enabled         bool
	RPS             float64
	Burst           int
	QueueSize       int
	QueueTimeout    time.Duration
	PerModel        bool
}

// === Router Configuration ===

type RouterConfig struct {
	Enabled      bool
	DefaultStage string
	Models       []llm.ModelConfig
}

// === Circuit Breaker Configuration ===

type CircuitBreakerConfig struct {
	Enabled      bool
	MaxFailures  int
	Timeout      time.Duration
	Interval     time.Duration
}

// === Failover Configuration ===

type FailoverConfig struct {
	Enabled        bool
	Providers      []llm.ProviderConfig
	EnableAuto     bool
	EnableFailback bool
	Cooldown       time.Duration
}

// === Budget Configuration ===

type BudgetConfig struct {
	Enabled          bool
	Period           string
	Limit            float64
	EnableHardLimit  bool
	AlertThresholds  []float64
}

// === Priority Configuration ===

type PriorityConfig struct {
	Enabled              bool
	MaxQueueSize         int
	EnableLowPriorityDrop bool
	HighPriorityReserve  int
}

// === Main Config ===

// Config holds the configuration for the Global Brain.
type Config struct {
	// Enabled is automatically determined based on APIKey presence.
	Enabled bool
	// Model is the model configuration.
	Model ModelConfig
	// Cache is the cache configuration.
	Cache CacheConfig
	// Retry is the retry configuration.
	Retry RetryConfig
	// Metrics is the metrics configuration.
	Metrics MetricsConfig
	// Cost is the cost configuration.
	Cost CostConfig
	// RateLimit is the rate limit configuration.
	RateLimit RateLimitConfig
	// Router is the router configuration.
	Router RouterConfig
	// CircuitBreaker is the circuit breaker configuration.
	CircuitBreaker CircuitBreakerConfig
	// Failover is the failover configuration.
	Failover FailoverConfig
	// Budget is the budget configuration.
	Budget BudgetConfig
	// Priority is the priority configuration.
	Priority PriorityConfig
}

// LoadConfigFromEnv loads the brain configuration from environment variables.
func LoadConfigFromEnv() Config {
	apiKey := os.Getenv("HOTPLEX_BRAIN_API_KEY")

	return Config{
		Enabled: apiKey != "",
		Model: ModelConfig{
			Provider: getEnv("HOTPLEX_BRAIN_PROVIDER", "openai"),
			Model:    getEnv("HOTPLEX_BRAIN_MODEL", "gpt-4o-mini"),
			Endpoint: os.Getenv("HOTPLEX_BRAIN_ENDPOINT"),
			TimeoutS: getIntEnv("HOTPLEX_BRAIN_TIMEOUT_S", 10),
		},
		Cache: CacheConfig{
			Enabled: true,
			Size:    getIntEnv("HOTPLEX_BRAIN_CACHE_SIZE", 1000),
		},
		Retry: RetryConfig{
			Enabled:     true,
			MaxAttempts: getIntEnv("HOTPLEX_BRAIN_MAX_RETRIES", 3),
			MinWaitMs:   getIntEnv("HOTPLEX_BRAIN_RETRY_MIN_WAIT_MS", 100),
			MaxWaitMs:   getIntEnv("HOTPLEX_BRAIN_RETRY_MAX_WAIT_MS", 5000),
		},
		Metrics: MetricsConfig{
			Enabled:         getBoolEnv("HOTPLEX_BRAIN_METRICS_ENABLED", true),
			ServiceName:     getEnv("HOTPLEX_BRAIN_METRICS_SERVICE_NAME", "hotplex-brain"),
			ExportInterval:  getDurationEnv("HOTPLEX_BRAIN_METRICS_EXPORT_INTERVAL", 10*time.Second),
		},
		Cost: CostConfig{
			Enabled:      getBoolEnv("HOTPLEX_BRAIN_COST_TRACKING_ENABLED", true),
			EnableBudget: getBoolEnv("HOTPLEX_BRAIN_COST_ENABLE_BUDGET", false),
		},
		RateLimit: RateLimitConfig{
			Enabled:         getBoolEnv("HOTPLEX_BRAIN_RATE_LIMIT_ENABLED", false),
			RPS:             getFloatEnv("HOTPLEX_BRAIN_RATE_LIMIT_RPS", 10.0),
			Burst:           getIntEnv("HOTPLEX_BRAIN_RATE_LIMIT_BURST", 20),
			QueueSize:       getIntEnv("HOTPLEX_BRAIN_RATE_LIMIT_QUEUE_SIZE", 100),
			QueueTimeout:    getDurationEnv("HOTPLEX_BRAIN_RATE_LIMIT_QUEUE_TIMEOUT", 30*time.Second),
			PerModel:        getBoolEnv("HOTPLEX_BRAIN_RATE_LIMIT_PER_MODEL", false),
		},
		Router: RouterConfig{
			Enabled:      getBoolEnv("HOTPLEX_BRAIN_ROUTER_ENABLED", false),
			DefaultStage: getEnv("HOTPLEX_BRAIN_ROUTER_STRATEGY", "cost_priority"),
			Models:       parseRouterModels(getEnv("HOTPLEX_BRAIN_ROUTER_MODELS", "")),
		},
		CircuitBreaker: CircuitBreakerConfig{
			Enabled:     getBoolEnv("HOTPLEX_BRAIN_CIRCUIT_BREAKER_ENABLED", false),
			MaxFailures: getIntEnv("HOTPLEX_BRAIN_CIRCUIT_BREAKER_MAX_FAILURES", 5),
			Timeout:     getDurationEnv("HOTPLEX_BRAIN_CIRCUIT_BREAKER_TIMEOUT", 30*time.Second),
			Interval:    getDurationEnv("HOTPLEX_BRAIN_CIRCUIT_BREAKER_INTERVAL", 60*time.Second),
		},
		Failover: FailoverConfig{
			Enabled:        getBoolEnv("HOTPLEX_BRAIN_FAILOVER_ENABLED", false),
			EnableAuto:     getBoolEnv("HOTPLEX_BRAIN_FAILOVER_ENABLE_AUTO", true),
			EnableFailback: getBoolEnv("HOTPLEX_BRAIN_FAILOVER_ENABLE_FAILBACK", true),
			Cooldown:       getDurationEnv("HOTPLEX_BRAIN_FAILOVER_COOLDOWN", 5*time.Minute),
		},
		Budget: BudgetConfig{
			Enabled:          getBoolEnv("HOTPLEX_BRAIN_BUDGET_ENABLED", false),
			Period:           getEnv("HOTPLEX_BRAIN_BUDGET_PERIOD", "daily"),
			Limit:            getFloatEnv("HOTPLEX_BRAIN_BUDGET_LIMIT", 10.0),
			EnableHardLimit:  getBoolEnv("HOTPLEX_BRAIN_BUDGET_ENABLE_HARD_LIMIT", false),
		},
		Priority: PriorityConfig{
			Enabled:              getBoolEnv("HOTPLEX_BRAIN_PRIORITY_ENABLED", false),
			MaxQueueSize:         getIntEnv("HOTPLEX_BRAIN_PRIORITY_MAX_QUEUE_SIZE", 1000),
			EnableLowPriorityDrop: getBoolEnv("HOTPLEX_BRAIN_PRIORITY_ENABLE_LOW_PRIORITY_DROP", true),
			HighPriorityReserve:  getIntEnv("HOTPLEX_BRAIN_PRIORITY_HIGH_PRIORITY_RESERVE", 100),
		},
	}
}

func parseRouterModels(s string) []llm.ModelConfig {
	if s == "" {
		return nil
	}

	var models []llm.ModelConfig
	parts := strings.Split(s, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		fields := strings.Split(part, ":")
		if len(fields) < 5 {
			continue
		}

		costInput, _ := strconv.ParseFloat(fields[2], 64)
		costOutput, _ := strconv.ParseFloat(fields[3], 64)
		latency, _ := strconv.ParseInt(fields[4], 10, 64)

		models = append(models, llm.ModelConfig{
			Name:            fields[0],
			Provider:        fields[1],
			CostPer1KInput:  costInput,
			CostPer1KOutput: costOutput,
			AvgLatencyMs:    latency,
			Enabled:         true,
		})
	}

	return models
}

// Helper functions for loading config from environment variables

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getBoolEnv(key string, fallback bool) bool {
	if val := os.Getenv(key); val != "" {
		b, err := strconv.ParseBool(val)
		if err == nil {
			return b
		}
	}
	return fallback
}

func getIntEnv(key string, fallback int) int {
	if val := os.Getenv(key); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			return n
		}
	}
	return fallback
}

func getFloatEnv(key string, fallback float64) float64 {
	if val := os.Getenv(key); val != "" {
		if n, err := strconv.ParseFloat(val, 64); err == nil {
			return n
		}
	}
	return fallback
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		// Try parsing as duration string (e.g., "30s", "1m")
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
		// Try parsing as seconds
		if n, err := strconv.Atoi(val); err == nil {
			return time.Duration(n) * time.Second
		}
	}
	return fallback
}

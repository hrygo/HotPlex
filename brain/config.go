package brain

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hrygo/hotplex/brain/llm"
)

// Config holds the configuration for the Global Brain.
type Config struct {
	// Enabled is automatically determined based on APIKey presence.
	Enabled bool
	// Provider supports "openai" (default), "anthropic", "gemini".
	Provider string
	// APIKey is the secret for accessing the provider API.
	APIKey string
	// Endpoint is the optional base URL for the API (e.g. for DeepSeek/Groq).
	Endpoint string
	// Model is the specific model to use (default: gpt-4o-mini).
	Model string
	// Timeout is the maximum duration for a brain request.
	// Defaults to 10 seconds for standard requests.
	TimeoutS int
	// CacheSize is the LRU cache capacity (default: 1000 entries).
	// Set to 0 to disable caching.
	CacheSize int
	// MaxRetries is the maximum number of retry attempts (default: 3).
	// Set to 0 to disable retries.
	MaxRetries int
	// RetryMinWait is the minimum wait time between retries (default: 100ms).
	RetryMinWaitMs int
	// RetryMaxWait is the maximum wait time between retries (default: 5s).
	RetryMaxWaitMs int

	// === Phase 2: Observability & Cost Optimization ===

	// MetricsEnabled enables OpenTelemetry metrics collection.
	MetricsEnabled bool
	// MetricsServiceName is the service name for metrics.
	MetricsServiceName string
	// CostTrackingEnabled enables cost calculation and tracking.
	CostTrackingEnabled bool
	// RateLimitEnabled enables rate limiting.
	RateLimitEnabled bool
	// RateLimitRPS is the requests per second limit.
	RateLimitRPS float64
	// RateLimitBurst is the burst size for rate limiting.
	RateLimitBurst int
	// RateLimitQueueSize is the maximum queue size for waiting requests.
	RateLimitQueueSize int
	// RateLimitQueueTimeout is the maximum queue wait time.
	RateLimitQueueTimeout time.Duration
	// RateLimitPerModel enables per-model rate limiting.
	RateLimitPerModel bool

	// RouterEnabled enables multi-model routing.
	RouterEnabled bool
	// RouterStrategy is the default routing strategy.
	RouterStrategy string
	// RouterModels is a comma-separated list of model configurations.
	// Format: "name1:provider1:input_cost:output_cost,latency;name2:..."
	RouterModels string
}

// LoadConfigFromEnv loads the brain configuration from environment variables.
func LoadConfigFromEnv() Config {
	apiKey := os.Getenv("HOTPLEX_BRAIN_API_KEY")

	cfg := Config{
		Enabled:            apiKey != "",
		Provider:           getEnv("HOTPLEX_BRAIN_PROVIDER", "openai"),
		APIKey:             apiKey,
		Endpoint:           os.Getenv("HOTPLEX_BRAIN_ENDPOINT"),
		Model:              getEnv("HOTPLEX_BRAIN_MODEL", "gpt-4o-mini"),
		TimeoutS:           getIntEnv("HOTPLEX_BRAIN_TIMEOUT_S", 10),
		CacheSize:          getIntEnv("HOTPLEX_BRAIN_CACHE_SIZE", 1000),
		MaxRetries:         getIntEnv("HOTPLEX_BRAIN_MAX_RETRIES", 3),
		RetryMinWaitMs:     getIntEnv("HOTPLEX_BRAIN_RETRY_MIN_WAIT_MS", 100),
		RetryMaxWaitMs:     getIntEnv("HOTPLEX_BRAIN_RETRY_MAX_WAIT_MS", 5000),
		MetricsEnabled:     getBoolEnv("HOTPLEX_BRAIN_METRICS_ENABLED", true),
		MetricsServiceName: getEnv("HOTPLEX_BRAIN_METRICS_SERVICE_NAME", "hotplex-brain"),
		CostTrackingEnabled: getBoolEnv("HOTPLEX_BRAIN_COST_TRACKING_ENABLED", true),
		RateLimitEnabled:   getBoolEnv("HOTPLEX_BRAIN_RATE_LIMIT_ENABLED", false),
		RateLimitRPS:       getFloatEnv("HOTPLEX_BRAIN_RATE_LIMIT_RPS", 10.0),
		RateLimitBurst:     getIntEnv("HOTPLEX_BRAIN_RATE_LIMIT_BURST", 20),
		RateLimitQueueSize: getIntEnv("HOTPLEX_BRAIN_RATE_LIMIT_QUEUE_SIZE", 100),
		RateLimitQueueTimeout: getDurationEnv("HOTPLEX_BRAIN_RATE_LIMIT_QUEUE_TIMEOUT", 30*time.Second),
		RateLimitPerModel:  getBoolEnv("HOTPLEX_BRAIN_RATE_LIMIT_PER_MODEL", false),
		RouterEnabled:      getBoolEnv("HOTPLEX_BRAIN_ROUTER_ENABLED", false),
		RouterStrategy:     getEnv("HOTPLEX_BRAIN_ROUTER_STRATEGY", "cost_priority"),
		RouterModels:       os.Getenv("HOTPLEX_BRAIN_ROUTER_MODELS"),
	}

	return cfg
}

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

// ParseRouterModels parses the RouterModels string into llm.ModelConfig slices.
func (c *Config) ParseRouterModels() []llm.ModelConfig {
	if c.RouterModels == "" {
		return nil
	}

	var models []llm.ModelConfig
	// Format: "name1:provider:input_cost:output_cost:latency;name2:..."
	parts := strings.Split(c.RouterModels, ";")
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

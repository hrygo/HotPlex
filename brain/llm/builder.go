package llm

import (
	"fmt"
	"log/slog"
	"time"
)

// ClientBuilder provides a fluent API for constructing LLM clients
// with various middleware layers (rate limiting, caching, circuit breaker, etc.).
type ClientBuilder struct {
	// Base configuration
	apiKey   string
	endpoint string
	model    string
	logger   *slog.Logger

	// Capability switches
	withMetrics   bool
	withCache     bool
	withRetry     bool
	withCircuit   bool
	withPriority  bool
	withRateLimit bool
	withBudget    bool

	// Capability configurations
	metricsConfig   *MetricsConfig
	cacheConfig     *CacheConfig
	retryConfig     *RetryConfig
	circuitConfig   *CircuitBreakerConfig
	priorityConfig  *PriorityConfig
	rateLimitConfig *RateLimitConfig
	budgetConfig    *BudgetConfig
	rateLimitRPS    float64
	rateLimitBurst  int
	maxRetries      int
	retryMinWaitMs  int
	retryMaxWaitMs  int
	cacheSize       int
}

// CacheConfig holds configuration for the cache layer.
type CacheConfig struct {
	Size int
}

// RetryConfig holds configuration for retry behavior.
type RetryConfig struct {
	MaxRetries int
	MinWaitMs  int
	MaxWaitMs  int
}

// NewClientBuilder creates a new client builder with default logger.
func NewClientBuilder() *ClientBuilder {
	return &ClientBuilder{
		logger: slog.Default(),
	}
}

// WithAPIKey sets the API key for the LLM provider.
func (b *ClientBuilder) WithAPIKey(key string) *ClientBuilder {
	b.apiKey = key
	return b
}

// WithEndpoint sets the API endpoint (optional, for non-OpenAI providers).
func (b *ClientBuilder) WithEndpoint(endpoint string) *ClientBuilder {
	b.endpoint = endpoint
	return b
}

// WithModel sets the model to use.
func (b *ClientBuilder) WithModel(model string) *ClientBuilder {
	b.model = model
	return b
}

// WithLogger sets a custom logger.
func (b *ClientBuilder) WithLogger(logger *slog.Logger) *ClientBuilder {
	b.logger = logger
	return b
}

// WithMetrics enables metrics collection with optional custom configuration.
func (b *ClientBuilder) WithMetrics(config ...MetricsConfig) *ClientBuilder {
	b.withMetrics = true
	if len(config) > 0 {
		b.metricsConfig = &config[0]
	}
	return b
}

// WithCache enables response caching with optional custom configuration.
func (b *ClientBuilder) WithCache(config ...CacheConfig) *ClientBuilder {
	b.withCache = true
	if len(config) > 0 {
		b.cacheConfig = &config[0]
	} else {
		b.cacheSize = DefaultCacheSize
	}
	return b
}

// WithCacheSize sets the cache size (convenience method).
func (b *ClientBuilder) WithCacheSize(size int) *ClientBuilder {
	b.withCache = true
	b.cacheSize = size
	return b
}

// WithRetry enables retry logic with specified max retries.
func (b *ClientBuilder) WithRetry(maxRetries int) *ClientBuilder {
	b.withRetry = true
	b.maxRetries = maxRetries
	b.retryMinWaitMs = DefaultRetryMinWaitMs
	b.retryMaxWaitMs = DefaultRetryMaxWaitMs
	return b
}

// WithRetryConfig enables retry logic with custom configuration.
func (b *ClientBuilder) WithRetryConfig(config RetryConfig) *ClientBuilder {
	b.withRetry = true
	b.retryConfig = &config
	b.maxRetries = config.MaxRetries
	b.retryMinWaitMs = config.MinWaitMs
	b.retryMaxWaitMs = config.MaxWaitMs
	return b
}

// WithCircuitBreaker enables circuit breaker protection with optional custom configuration.
func (b *ClientBuilder) WithCircuitBreaker(config ...CircuitBreakerConfig) *ClientBuilder {
	b.withCircuit = true
	if len(config) > 0 {
		b.circuitConfig = &config[0]
	}
	return b
}

// WithPriority enables priority-based scheduling with optional custom configuration.
func (b *ClientBuilder) WithPriority(config ...PriorityConfig) *ClientBuilder {
	b.withPriority = true
	if len(config) > 0 {
		b.priorityConfig = &config[0]
	}
	return b
}

// WithRateLimit enables rate limiting with specified requests per second.
func (b *ClientBuilder) WithRateLimit(rps float64) *ClientBuilder {
	b.withRateLimit = true
	b.rateLimitRPS = rps
	b.rateLimitBurst = int(rps) // Default burst = RPS
	return b
}

// WithRateLimitConfig enables rate limiting with custom configuration.
func (b *ClientBuilder) WithRateLimitConfig(config RateLimitConfig) *ClientBuilder {
	b.withRateLimit = true
	b.rateLimitConfig = &config
	b.rateLimitRPS = config.RequestsPerSecond
	b.rateLimitBurst = config.BurstSize
	return b
}

// WithBudget enables budget tracking with optional custom configuration.
func (b *ClientBuilder) WithBudget(config ...BudgetConfig) *ClientBuilder {
	b.withBudget = true
	if len(config) > 0 {
		b.budgetConfig = &config[0]
	}
	return b
}

// Build constructs the LLM client with all configured middleware layers.
// The wrapping order is:
// 1. Base client (OpenAI)
// 2. Metrics (outermost for visibility)
// 3. Circuit Breaker
// 4. Rate Limiter
// 5. Retry
// 6. Cache (innermost for efficiency)
func (b *ClientBuilder) Build() (LLMClient, error) {
	if err := b.validate(); err != nil {
		return nil, err
	}

	// 1. Create base client
	var client LLMClient = NewOpenAIClient(b.apiKey, b.endpoint, b.model, b.logger)

	// 2. Apply cache layer (innermost - cache raw responses)
	if b.withCache {
		size := b.cacheSize
		if b.cacheConfig != nil && b.cacheConfig.Size > 0 {
			size = b.cacheConfig.Size
		}
		if size <= 0 {
			size = DefaultCacheSize
		}
		client = NewCachedClient(client, size)
	}

	// 3. Apply retry layer
	if b.withRetry {
		maxRetries := b.maxRetries
		minWait := b.retryMinWaitMs
		maxWait := b.retryMaxWaitMs

		if b.retryConfig != nil {
			if b.retryConfig.MaxRetries > 0 {
				maxRetries = b.retryConfig.MaxRetries
			}
			if b.retryConfig.MinWaitMs > 0 {
				minWait = b.retryConfig.MinWaitMs
			}
			if b.retryConfig.MaxWaitMs > 0 {
				maxWait = b.retryConfig.MaxWaitMs
			}
		}

		if maxRetries <= 0 {
			maxRetries = DefaultMaxRetries
		}

		client = NewRetryClient(client, maxRetries, minWait, maxWait)
	}

	// 4. Apply rate limit layer
	if b.withRateLimit {
		cfg := b.rateLimitConfig
		if cfg == nil {
			cfg = &RateLimitConfig{
				RequestsPerSecond: b.rateLimitRPS,
				BurstSize:         b.rateLimitBurst,
				MaxQueueSize:      DefaultMaxQueueSize,
				QueueTimeout:      DefaultQueueTimeout,
			}
		}
		if cfg.RequestsPerSecond <= 0 {
			cfg.RequestsPerSecond = DefaultRPS
		}
		if cfg.BurstSize <= 0 {
			cfg.BurstSize = int(cfg.RequestsPerSecond)
		}

		limiter := NewRateLimiter(*cfg)
		client = NewRateLimitedClient(client, limiter)
	}

	// 5. Apply circuit breaker layer
	if b.withCircuit {
		cfg := b.circuitConfig
		if cfg == nil {
			defaultCfg := DefaultCircuitBreakerConfig()
			cfg = &defaultCfg
		}
		if cfg.Logger == nil {
			cfg.Logger = b.logger
		}

		cb := NewCircuitBreaker(*cfg)
		// Wrap client with circuit breaker
		client = NewCircuitClient(client, cb)
	}

	// 6. Apply metrics layer (outermost for visibility)
	if b.withMetrics {
		cfg := b.metricsConfig
		if cfg == nil {
			cfg = &MetricsConfig{
				Enabled:           true,
				ServiceName:       "hotplex-brain",
				MaxLatencySamples: DefaultMaxLatencySamples,
			}
		}
		if cfg.MaxLatencySamples <= 0 {
			cfg.MaxLatencySamples = DefaultMaxLatencySamples
		}

		collector := NewMetricsCollector(*cfg)
		client = NewMetricsClient(client, collector, b.model)
	}

	return client, nil
}

// validate validates the builder configuration.
func (b *ClientBuilder) validate() error {
	if b.apiKey == "" {
		return fmt.Errorf("API key is required")
	}
	if b.model == "" {
		return fmt.Errorf("model is required")
	}
	if b.logger == nil {
		b.logger = slog.Default()
	}
	return nil
}

// Default configuration values.
const (
	DefaultCacheSize         = 1000
	DefaultMaxRetries        = 3
	DefaultRetryMinWaitMs    = 100
	DefaultRetryMaxWaitMs    = 5000
	DefaultRPS               = 10.0
	DefaultMaxQueueSize      = 100
	DefaultQueueTimeout      = 30 * time.Second
	DefaultMaxLatencySamples = 1000
)

// DefaultMetricsConfig returns default metrics configuration.
func DefaultMetricsConfig() MetricsConfig {
	return MetricsConfig{
		Enabled:           true,
		ServiceName:       "hotplex-brain",
		MaxLatencySamples: DefaultMaxLatencySamples,
	}
}

// DefaultCacheConfig returns default cache configuration.
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		Size: DefaultCacheSize,
	}
}

// DefaultRetryConfig returns default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: DefaultMaxRetries,
		MinWaitMs:  DefaultRetryMinWaitMs,
		MaxWaitMs:  DefaultRetryMaxWaitMs,
	}
}

// DefaultRateLimitConfig returns default rate limit configuration.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerSecond: DefaultRPS,
		BurstSize:         int(DefaultRPS),
		MaxQueueSize:      DefaultMaxQueueSize,
		QueueTimeout:      DefaultQueueTimeout,
	}
}

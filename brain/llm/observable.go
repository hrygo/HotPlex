package llm

import (
	"context"
	"time"
)

// ObservableClient extends LLMClient with observability capabilities.
// It provides unified access to internal component statistics and health.
type ObservableClient interface {
	LLMClient

	// Component access
	GetMetricsStats() MetricsStats
	GetCircuitBreakerStats() CircuitBreakerStats
	GetRateLimitStats() RateLimitStats
	GetCacheStats() CacheStats

	// Unified health check
	GetClientHealth(ctx context.Context) ClientHealth
}

// ClientHealth represents the overall health of an LLM client.
type ClientHealth struct {
	Status         HealthStatus
	CircuitState   CircuitState
	CacheHitRate   float64
	RateLimitUsage float64
	LastError      string
	LastErrorTime  time.Time
	TotalRequests  int64
	SuccessRate    float64
	AvgLatencyMs   float64
}

// CacheStats represents cache statistics.
type CacheStats struct {
	Size      int
	Hits      int64
	Misses    int64
	HitRate   float64
	Evictions int64
}

// ObservableClientImpl wraps an LLMClient with observability capabilities.
type ObservableClientImpl struct {
	client         LLMClient
	metrics        *MetricsCollector
	circuitBreaker *CircuitBreaker
	rateLimiter    *RateLimiter
	cache          *CachedClient
}

// NewObservableClient creates an observable client wrapper.
// It extracts internal components from the client chain.
func NewObservableClient(client LLMClient) ObservableClient {
	obs := &ObservableClientImpl{client: client}

	// Extract components by type assertion chain
	obs.extractComponents(client)

	return obs
}

// extractComponents traverses the client chain to extract observable components.
func (o *ObservableClientImpl) extractComponents(client LLMClient) {
	// Try to extract each component type
	if mc, ok := client.(*MetricsClient); ok {
		o.metrics = mc.GetMetrics()
		o.extractComponents(mc.client)
		return
	}

	if cc, ok := client.(*CircuitClient); ok {
		o.circuitBreaker = cc.GetCircuitBreaker()
		o.extractComponents(cc.client)
		return
	}

	if rc, ok := client.(*RateLimitedClient); ok {
		// RateLimitedClient doesn't expose limiter, skip for now
		o.extractComponents(rc.client)
		return
	}

	if c, ok := client.(*CachedClient); ok {
		o.cache = c
		o.extractComponents(c.client)
		return
	}

	if r, ok := client.(*RetryClient); ok {
		o.extractComponents(r.client)
		return
	}
}

// Chat delegates to the underlying client.
func (o *ObservableClientImpl) Chat(ctx context.Context, prompt string) (string, error) {
	return o.client.Chat(ctx, prompt)
}

// Analyze delegates to the underlying client.
func (o *ObservableClientImpl) Analyze(ctx context.Context, prompt string, target any) error {
	return o.client.Analyze(ctx, prompt, target)
}

// ChatStream delegates to the underlying client.
func (o *ObservableClientImpl) ChatStream(ctx context.Context, prompt string) (<-chan string, error) {
	return o.client.ChatStream(ctx, prompt)
}

// HealthCheck delegates to the underlying client.
func (o *ObservableClientImpl) HealthCheck(ctx context.Context) HealthStatus {
	return o.client.HealthCheck(ctx)
}

// GetMetricsStats returns metrics statistics if available.
func (o *ObservableClientImpl) GetMetricsStats() MetricsStats {
	if o.metrics != nil {
		return o.metrics.GetStats()
	}
	return MetricsStats{}
}

// GetCircuitBreakerStats returns circuit breaker statistics if available.
func (o *ObservableClientImpl) GetCircuitBreakerStats() CircuitBreakerStats {
	if o.circuitBreaker != nil {
		return o.circuitBreaker.GetStats()
	}
	return CircuitBreakerStats{}
}

// GetRateLimitStats returns rate limit statistics if available.
func (o *ObservableClientImpl) GetRateLimitStats() RateLimitStats {
	if o.rateLimiter != nil {
		return o.rateLimiter.GetStats()
	}
	return RateLimitStats{}
}

// GetCacheStats returns cache statistics if available.
func (o *ObservableClientImpl) GetCacheStats() CacheStats {
	if o.cache != nil {
		keys, _, _ := o.cache.CacheStats()
		return CacheStats{Size: keys}
	}
	return CacheStats{}
}

// GetClientHealth returns comprehensive health information.
func (o *ObservableClientImpl) GetClientHealth(ctx context.Context) ClientHealth {
	health := ClientHealth{
		Status:       o.HealthCheck(ctx),
		CircuitState: CircuitClosed,
	}

	// Collect metrics stats
	if o.metrics != nil {
		stats := o.metrics.GetStats()
		health.TotalRequests = stats.TotalRequests
		health.AvgLatencyMs = stats.AvgLatencyMs
		if stats.TotalRequests > 0 {
			health.SuccessRate = 1.0 - stats.ErrorRate
		} else {
			health.SuccessRate = 1.0
		}
	}

	// Collect circuit breaker stats
	if o.circuitBreaker != nil {
		stats := o.circuitBreaker.GetStats()
		health.CircuitState = stats.State
		if stats.TotalRequests > 0 {
			health.SuccessRate = float64(stats.SuccessRequests) / float64(stats.TotalRequests)
		}
	}

	// Collect cache stats
	if o.cache != nil {
		keys, _, _ := o.cache.CacheStats()
		// Note: golang-lru doesn't provide hit rate, estimate from size
		health.CacheHitRate = 0.0 // Would need extended cache implementation
		_ = keys
	}

	// Collect rate limit stats
	if o.rateLimiter != nil {
		stats := o.rateLimiter.GetStats()
		if stats.TotalRequests > 0 {
			health.RateLimitUsage = float64(stats.QueuedRequests) / float64(stats.TotalRequests)
		}
	}

	return health
}

// AsObservable attempts to cast an LLMClient to ObservableClient.
// Returns the ObservableClient if successful, or wraps it if not already observable.
func AsObservable(client LLMClient) ObservableClient {
	if obs, ok := client.(ObservableClient); ok {
		return obs
	}
	return NewObservableClient(client)
}

# Brain LLM Client Builder Pattern

Technical specification and developer guide for the LLM client composition system.

## Architecture

The Builder pattern provides a fluent API for composing LLM client middleware layers.

```
┌─────────────────────────────────────────────────────────────┐
│                     ClientBuilder                            │
│  WithAPIKey() → WithModel() → WithMetrics() → ... → Build() │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                 Wrapped Client Chain                         │
│  Metrics → Circuit → RateLimit → Retry → Cache → OpenAI     │
└─────────────────────────────────────────────────────────────┘
```

### Wrapping Order (Innermost to Outermost)

| Layer | File | Purpose |
|-------|------|---------|
| Base | `client.go` | OpenAI-compatible HTTP client |
| Cache | `cache.go` | LRU response caching |
| Retry | `retry.go` | Exponential backoff retries |
| Rate Limit | `ratelimit.go` | Token bucket rate limiting |
| Circuit Breaker | `circuit.go` | Fail-fast on repeated errors |
| Metrics | `metrics.go` | Observability and cost tracking |

## Usage

### Preset Configurations

```go
// Production: all capabilities enabled
client, _ := llm.ProductionClient(apiKey, "gpt-4")

// Development: minimal overhead
client, _ := llm.DevelopmentClient(apiKey, "gpt-4")

// Testing: cache + high rate limit
client, _ := llm.TestingClient(apiKey, "gpt-4")

// High throughput: aggressive caching
client, _ := llm.HighThroughputClient(apiKey, "gpt-4")

// Maximum reliability: aggressive retries + circuit breaker
client, _ := llm.ReliableClient(apiKey, "gpt-4")
```

### Custom Configuration

```go
client, err := llm.NewClientBuilder().
    WithAPIKey(apiKey).
    WithEndpoint("https://api.deepseek.com/v1"). // Custom endpoint
    WithModel("deepseek-chat").
    WithMetrics(llm.MetricsConfig{
        Enabled:          true,
        ServiceName:      "my-service",
        MaxLatencySamples: 500,
    }).
    WithCache(5000).         // 5000 entry cache
    WithRetry(5).            // 5 retries
    WithCircuitBreaker(llm.CircuitBreakerConfig{
        Name:                "my-circuit",
        MaxFailures:         10,
        Timeout:             60 * time.Second,
        HalfOpenMaxRequests: 3,
    }).
    WithRateLimit(100).      // 100 RPS
    Build()
```

### Independent Features (Non-Builder)

Budget tracking and priority scheduling are standalone features:

```go
// Budget Control
client, _ := llm.NewBudgetManagedClient(apiKey, "gpt-4", 10.0) // $10/day

// With custom tracker
tracker := llm.NewBudgetTracker(llm.BudgetConfig{
    Period:          llm.BudgetDaily,
    Limit:           50.0,
    EnableHardLimit: true,
}, "session-123")
budgetClient := llm.BudgetClientWithTracker(baseClient, tracker, "gpt-4", nil)

// Priority Scheduling
scheduler, priorityClient := llm.PrioritySchedulerWithClient(5*time.Minute, nil)
priorityClient.Submit(ctx, "req-1", llm.PriorityHigh, func() error {
    _, err := client.Chat(ctx, prompt)
    return err
})
```

## Component Reference

### LLMClient Interface

```go
type LLMClient interface {
    Chat(ctx context.Context, prompt string) (string, error)
    Analyze(ctx context.Context, prompt string, target any) error
    ChatStream(ctx context.Context, prompt string) (<-chan string, error)
    HealthCheck(ctx context.Context) HealthStatus
}
```

### Wrapper Types

| Type | Constructor | Config |
|------|-------------|--------|
| `CachedClient` | `NewCachedClient(client, size)` | `int` |
| `RetryClient` | `NewRetryClient(client, maxRetries, minWait, maxWait)` | 3 params |
| `RateLimitedClient` | `NewRateLimitedClient(client, limiter)` | `RateLimitConfig` |
| `CircuitClient` | `NewCircuitClient(client, breaker)` | `CircuitBreakerConfig` |
| `MetricsClient` | `NewMetricsClient(client, collector, model)` | `MetricsConfig` |
| `BudgetClient` | `NewBudgetClient(client, tracker, model, estimator)` | `BudgetConfig` |

### ObservableClient

Extract runtime statistics from client chain:

```go
obs := llm.AsObservable(client)
health := obs.GetClientHealth(ctx)
// health.CircuitState, health.CacheHitRate, health.TotalRequests, etc.
```

## Configuration Defaults

| Constant | Value | Description |
|----------|-------|-------------|
| `DefaultCacheSize` | 1000 | LRU cache entries |
| `DefaultMaxRetries` | 3 | Maximum retry attempts |
| `DefaultRetryMinWaitMs` | 100 | Initial retry delay |
| `DefaultRetryMaxWaitMs` | 5000 | Maximum retry delay |
| `DefaultRPS` | 10.0 | Requests per second |
| `DefaultMaxQueueSize` | 100 | Rate limit queue size |
| `DefaultQueueTimeout` | 30s | Queue wait timeout |

## Error Types

```go
var (
    ErrAPIKeyRequired  = errors.New("API key is required")
    ErrModelRequired   = errors.New("model is required")
    ErrInvalidEndpoint = errors.New("invalid endpoint URL")
)
```

## Extending

To add a new wrapper:

1. Implement `LLMClient` interface
2. Add compile-time verification: `var _ LLMClient = (*NewClient)(nil)`
3. Add `WithNewFeature()` method to `ClientBuilder`
4. Apply in `Build()` method in correct layer order
5. Update `ObservableClient.extractComponents()` if observable

## File Structure

```
brain/llm/
├── builder.go        # ClientBuilder implementation
├── builder_test.go   # Builder unit tests
├── presets.go        # Pre-configured client factories
├── observable.go     # ObservableClient interface
├── client.go         # LLMClient interface + OpenAI client
├── cache.go          # CachedClient wrapper
├── retry.go          # RetryClient wrapper
├── ratelimit.go      # RateLimitedClient wrapper
├── circuit.go        # CircuitClient wrapper
├── metrics.go        # MetricsClient wrapper
├── budget.go         # BudgetTracker (standalone)
├── budget_client.go  # BudgetClient wrapper
├── priority.go       # PriorityScheduler + PriorityClient
├── failover.go       # FailoverManager
├── router.go         # Model router
└── health.go         # Health monitoring
```

## Migration Guide

From manual chain wrapping:

```go
// Before
client := NewOpenAIClient(apiKey, endpoint, model, logger)
client = NewRateLimitedClient(client, limiter)
client = NewCachedClient(client, cache)
client = NewMetricsClient(client, metrics)

// After
client, _ := NewClientBuilder().
    WithAPIKey(apiKey).
    WithEndpoint(endpoint).
    WithModel(model).
    WithRateLimit(100).
    WithCache().
    WithMetrics().
    Build()
```

---

**References**: Issue #217
**Status**: Production Ready

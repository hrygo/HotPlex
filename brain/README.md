# Native Brain - Production-Grade LLM Client

This package provides production-ready LLM client capabilities for HotPlex, implementing enterprise-grade features for reliability, performance, and observability.

## Features

### Phase 1: Core Reliability ✅

1. **🎬 Streaming Support** (`ChatStream`)
2. **🔄 Retry with Exponential Backoff**
3. **💾 LRU Cache Layer**
4. **⏱️ Timeout Control**
5. **🏥 Health Check**

### Phase 2: Observability & Cost Optimization ✅

6. **📊 Metrics Tracking (OpenTelemetry)**
   - Request latency histogram
   - Token usage counters (input/output)
   - Cost tracking
   - Error rate monitoring
   - Active requests gauge

7. **🎯 Multi-Model Router**
   - Scenario-based dynamic routing (chat vs analyze vs code)
   - Routing strategies: cost_priority, latency_priority, quality_priority, balanced
   - Configurable model mappings
   - Automatic scenario detection from prompts

8. **💰 Token Counting & Cost Calculation**
   - Input/output token statistics
   - Per-model pricing standards (15+ models pre-configured)
   - Session-level cost aggregation
   - Budget alerts (optional)

9. **🚦 Rate Limiting**
   - Token bucket algorithm
   - Per-model independent rate limiting
   - Queue waiting mechanism
   - Configurable burst size and queue timeout

### Phase 3: High Availability & Cost Control ✨ NEW

10. **⚡ Circuit Breaker Pattern**
    - Three-state circuit: Closed → Open → Half-Open
    - Automatic trip on failure threshold
    - Half-open state for recovery detection
    - Manual reset interface
    - Force open/close for maintenance

11. **🔄 Multi-Provider Failover**
    - Primary/backup provider configuration
    - Automatic failover on timeout/errors
    - Failback mechanism (primary recovery detection)
    - Failover history tracking
    - Manual failover override

12. **💰 Token Budget Control**
    - Daily/weekly/monthly budget limits
    - Session-level budget tracking
    - Budget alerts at 80%/90% thresholds
    - Hard limit (reject) or soft limit (warn) policies
    - Automatic period reset

13. **⚡ Request Priority Queue**
    - High/Medium/Low priority levels
    - Priority-based scheduling algorithm
    - Low priority request dropping under load
    - High priority reservation slots
    - Expiration-based cleanup

## Configuration

All features are configured via environment variables:

### Core Configuration (Phase 1)

| Variable | Default | Description |
|----------|---------|-------------|
| `HOTPLEX_BRAIN_API_KEY` | (required) | LLM provider API key |
| `HOTPLEX_BRAIN_PROVIDER` | `openai` | Provider name |
| `HOTPLEX_BRAIN_MODEL` | `gpt-4o-mini` | Model identifier |
| `HOTPLEX_BRAIN_ENDPOINT` | (optional) | Custom API endpoint |
| `HOTPLEX_BRAIN_TIMEOUT_S` | `10` | Request timeout (seconds) |
| `HOTPLEX_BRAIN_CACHE_SIZE` | `1000` | LRU cache entries (0=disabled) |
| `HOTPLEX_BRAIN_MAX_RETRIES` | `3` | Max retry attempts (0=disabled) |
| `HOTPLEX_BRAIN_RETRY_MIN_WAIT_MS` | `100` | Min retry wait (ms) |
| `HOTPLEX_BRAIN_RETRY_MAX_WAIT_MS` | `5000` | Max retry wait (ms) |

### Observability & Cost (Phase 2) ✨ NEW

| Variable | Default | Description |
|----------|---------|-------------|
| `HOTPLEX_BRAIN_METRICS_ENABLED` | `true` | Enable OpenTelemetry metrics |
| `HOTPLEX_BRAIN_METRICS_SERVICE_NAME` | `hotplex-brain` | Service name for metrics |
| `HOTPLEX_BRAIN_COST_TRACKING_ENABLED` | `true` | Enable cost calculation |
| `HOTPLEX_BRAIN_RATE_LIMIT_ENABLED` | `false` | Enable rate limiting |
| `HOTPLEX_BRAIN_RATE_LIMIT_RPS` | `10` | Requests per second |
| `HOTPLEX_BRAIN_RATE_LIMIT_BURST` | `20` | Burst size |
| `HOTPLEX_BRAIN_RATE_LIMIT_QUEUE_SIZE` | `100` | Max queue size |
| `HOTPLEX_BRAIN_RATE_LIMIT_QUEUE_TIMEOUT` | `30s` | Queue timeout |
| `HOTPLEX_BRAIN_RATE_LIMIT_PER_MODEL` | `false` | Per-model rate limiting |
| `HOTPLEX_BRAIN_ROUTER_ENABLED` | `false` | Enable multi-model routing |
| `HOTPLEX_BRAIN_ROUTER_STRATEGY` | `cost_priority` | Default routing strategy |
| `HOTPLEX_BRAIN_ROUTER_MODELS` | (see below) | Model configurations |

### Router Models Format

Configure multiple models for routing:

```bash
HOTPLEX_BRAIN_ROUTER_MODELS="gpt-4o-mini:openai:0.00015:0.0006:200;gpt-4o:openai:0.005:0.015:500;qwen-plus:dashscope:0.0006:0.0012:300"
```

Format: `name:provider:cost_per_1k_input:cost_per_1k_output:avg_latency_ms`

### High Availability & Cost Control (Phase 3) ✨ NEW

| Variable | Default | Description |
|----------|---------|-------------|
| `HOTPLEX_BRAIN_CIRCUIT_BREAKER_ENABLED` | `false` | Enable circuit breaker |
| `HOTPLEX_BRAIN_CIRCUIT_BREAKER_MAX_FAILURES` | `5` | Failures before opening |
| `HOTPLEX_BRAIN_CIRCUIT_BREAKER_TIMEOUT` | `30s` | Open state duration |
| `HOTPLEX_BRAIN_CIRCUIT_BREAKER_INTERVAL` | `60s` | Failure counting window |
| `HOTPLEX_BRAIN_FAILOVER_ENABLED` | `false` | Enable multi-provider failover |
| `HOTPLEX_BRAIN_FAILOVER_PROVIDERS` | (see below) | Provider configurations |
| `HOTPLEX_BRAIN_FAILOVER_ENABLE_AUTO` | `true` | Enable automatic failover |
| `HOTPLEX_BRAIN_FAILOVER_ENABLE_FAILBACK` | `true` | Enable automatic failback |
| `HOTPLEX_BRAIN_FAILOVER_COOLDOWN` | `5m` | Failback cooldown period |
| `HOTPLEX_BRAIN_BUDGET_ENABLED` | `false` | Enable budget control |
| `HOTPLEX_BRAIN_BUDGET_PERIOD` | `daily` | Budget period (daily/weekly/monthly/session) |
| `HOTPLEX_BRAIN_BUDGET_LIMIT` | `10.0` | Budget limit in USD |
| `HOTPLEX_BRAIN_BUDGET_ENABLE_HARD_LIMIT` | `false` | Reject requests over budget |
| `HOTPLEX_BRAIN_BUDGET_ALERT_THRESHOLDS` | `80,90` | Alert percentages |
| `HOTPLEX_BRAIN_PRIORITY_ENABLED` | `false` | Enable priority queue |
| `HOTPLEX_BRAIN_PRIORITY_MAX_QUEUE_SIZE` | `1000` | Maximum queue size |
| `HOTPLEX_BRAIN_PRIORITY_ENABLE_LOW_PRIORITY_DROP` | `true` | Drop low priority when full |
| `HOTPLEX_BRAIN_PRIORITY_HIGH_PRIORITY_RESERVE` | `100` | Reserve slots for high priority |

### Failover Providers Format

Configure multiple providers for failover:

```bash
HOTPLEX_BRAIN_FAILOVER_PROVIDERS="openai:key1::1;dashscope:key2:https://dashscope.aliyuncs.com:2"
```

Format: `name:api_key:endpoint:priority` (endpoint is optional)

## Usage Examples

### Basic Usage (Unchanged from Phase 1)

```go
import (
    "log/slog"
    "github.com/hrygo/hotplex/brain"
)

logger := slog.Default()
if err := brain.Init(logger); err != nil {
    log.Fatal(err)
}

// Use global brain
response, err := brain.Global().Chat(ctx, "Hello!")
```

### Metrics & Observability ✨ NEW

```go
// Access metrics
if observable, ok := brain.Global().(brain.ObservableBrain); ok {
    stats := observable.GetMetrics()
    log.Printf("Total requests: %d", stats.TotalRequests)
    log.Printf("Total tokens: %d input, %d output", 
        stats.TotalInputTokens, stats.TotalOutputTokens)
    log.Printf("Total cost: $%.4f", stats.TotalCost)
    log.Printf("Error rate: %.2f%%", stats.ErrorRate*100)
    log.Printf("Avg latency: %.0fms", stats.AvgLatencyMs)
}

// Access cost calculator
if observable, ok := brain.Global().(brain.ObservableBrain); ok {
    cc := observable.GetCostCalculator()
    
    // Track session costs
    cc.TrackRequest("session-123", "gpt-4o-mini", 150, 300)
    
    // Set budget alert
    cc.SetSessionBudget("session-123", 10.0) // $10 budget
    
    // Get session cost
    if session, ok := cc.GetSessionCost("session-123"); ok {
        log.Printf("Session cost: $%.4f", session.TotalCost)
    }
}
```

### Model Routing ✨ NEW

```go
// Access router
if router := brain.GetRouter(); router != nil {
    // Automatic scenario detection
    scenario := router.DetectScenario("Write a function to sort an array")
    // Returns: llm.ScenarioCode
    
    // Select best model for scenario
    model, err := router.SelectModel(ctx, scenario, llm.StrategyCostPriority)
    if err == nil {
        log.Printf("Selected model: %s", model.Name)
    }
    
    // Add custom model
    router.AddModel(llm.ModelConfig{
        Name:            "custom-model",
        Provider:        "openai",
        CostPer1KInput:  0.001,
        CostPer1KOutput: 0.003,
        AvgLatencyMs:    300,
        Enabled:         true,
    })
}
```

### Rate Limiting ✨ NEW

```go
// Access rate limiter
if rl := brain.GetRateLimiter(); rl != nil {
    // Check remaining requests
    remaining := rl.Remaining()
    log.Printf("Remaining requests in burst: %d", remaining)
    
    // Get stats
    stats := rl.GetStats()
    log.Printf("Queued: %d, Rejected: %d", 
        stats.QueuedRequests, stats.RejectedRequests)
    
    // Dynamic rate adjustment
    rl.SetRate(20.0, 40) // 20 RPS, burst 40
}
```

### Routing Strategies

```go
// Cost priority: cheapest model that meets requirements
model, _ := router.SelectModel(ctx, llm.ScenarioChat, llm.StrategyCostPriority)

// Latency priority: fastest model
model, _ := router.SelectModel(ctx, llm.ScenarioChat, llm.StrategyLatencyPriority)

// Quality priority: highest quality (largest context window)
model, _ := router.SelectModel(ctx, llm.ScenarioReasoning, llm.StrategyQualityPriority)

// Balanced: cost-effective for chat, quality for analysis
model, _ := router.SelectModel(ctx, llm.ScenarioChat, llm.StrategyBalanced)
```

### Circuit Breaker ✨ NEW

```go
// Access circuit breaker
if resilient, ok := brain.Global().(brain.ResilientBrain); ok {
    cb := resilient.GetCircuitBreaker()
    
    // Check state
    state := cb.GetState()
    log.Printf("Circuit state: %s", state)
    
    // Get stats
    stats := cb.GetStats()
    log.Printf("Total requests: %d, Failures: %d", 
        stats.TotalRequests, stats.FailRequests)
    
    // Manual reset (after fixing issues)
    resilient.ResetCircuitBreaker()
    
    // Force open (for maintenance)
    cb.ForceOpen()
    
    // Force close (override protection)
    cb.ForceClose()
}
```

### Multi-Provider Failover ✨ NEW

```go
// Access failover manager
if resilient, ok := brain.Global().(brain.ResilientBrain); ok {
    fm := resilient.GetFailoverManager()
    
    // Get current provider
    provider := fm.GetCurrentProvider()
    log.Printf("Current provider: %s", provider.Name)
    
    // Get stats
    stats := fm.GetStats()
    log.Printf("Failover count: %d", stats.FailoverCount)
    log.Printf("Healthy providers: %v", stats.HealthyProviders)
    
    // Manual failover
    err := fm.ManualFailover("dashscope")
    if err != nil {
        log.Printf("Failover failed: %v", err)
    }
    
    // Reset to initial state
    fm.Reset()
}
```

### Budget Control ✨ NEW

```go
// Access budget manager
if budgetCtrl, ok := brain.Global().(brain.BudgetControlledBrain); ok {
    bm := budgetCtrl.GetBudgetManager()
    
    // Get tracker for session
    tracker := bm.GetTracker("session-123")
    
    // Check budget before request
    allowed, cost, err := tracker.CheckBudget(0.005) // Estimated $0.005
    if err != nil {
        log.Printf("Budget exceeded: %v", err)
        return
    }
    
    // Track actual cost after request
    tracker.TrackRequest(0.004) // Actual $0.004
    
    // Get stats
    stats := tracker.GetStats()
    log.Printf("Budget: $%.2f used / $%.2f limit (%.1f%%)",
        stats.CurrentCost, stats.Limit, stats.PercentageUsed)
    log.Printf("Remaining: $%.2f", stats.Remaining)
    
    // Set alert callback
    tracker.SetAlertCallback(func(alert llm.BudgetAlert) {
        log.Printf("Budget alert: %s (%.1f%%)", alert.Message, alert.Percentage)
    })
}
```

### Priority Queue ✨ NEW

```go
// Access priority scheduler
if priority, ok := brain.Global().(brain.PriorityBrain); ok {
    scheduler := priority.GetPriorityScheduler()
    
    // Submit high priority request
    err := scheduler.Enqueue(ctx, "req-1", llm.PriorityHigh, 
        func() error {
            // Execute request
            return nil
        }, 5*time.Minute)
    
    // Submit low priority request (may be dropped under load)
    err = scheduler.Enqueue(ctx, "req-2", llm.PriorityLow,
        func() error {
            return nil
        }, 5*time.Minute)
    
    // Get stats
    stats := scheduler.GetStats()
    log.Printf("Queue size: %d", stats.QueueSize)
    log.Printf("Processed: %d (High: %d, Med: %d, Low: %d)",
        stats.Processed, stats.HighProcessed, 
        stats.MediumProcessed, stats.LowProcessed)
    log.Printf("Dropped: %d", stats.Dropped)
}
```

### Pre-configured Models

Phase 2 includes pricing for 15+ models:

**OpenAI:**
- gpt-4o-mini ($0.00015/$0.0006 per 1K tokens)
- gpt-4o ($0.005/$0.015)
- gpt-4-turbo ($0.01/$0.03)

**Anthropic:**
- claude-3-haiku ($0.00025/$0.00125)
- claude-3-sonnet ($0.003/$0.015)
- claude-3-opus ($0.015/$0.075)

**Google:**
- gemini-1.5-flash ($0.000075/$0.0003)
- gemini-1.5-pro ($0.00125/$0.005)

**Alibaba/DashScope:**
- qwen-turbo ($0.0003/$0.0006)
- qwen-plus ($0.0006/$0.0012)
- qwen-max ($0.005/$0.015)

**DeepSeek:**
- deepseek-chat ($0.00027/$0.0011)

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                   Brain Interface                        │
│  - Chat(ctx, prompt) (string, error)                    │
│  - Analyze(ctx, prompt, target) error                   │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│              enhancedBrainWrapper                        │
│  - Router integration (Phase 2) ✨                       │
│  - Metrics collection (Phase 2) ✨                       │
│  - Cost tracking (Phase 2) ✨                            │
│  - Rate limiting (Phase 2) ✨                            │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│           Optional: RateLimitedClient                    │
│  - Token bucket rate limiting                            │
│  - Queue management                                      │
│  - Per-model limits                                      │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│           Optional: HealthMonitor                        │
│  - Caches health status                                  │
│  - Provides IsHealthy()                                  │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│           Optional: CachedClient                         │
│  - LRU cache for Chat/Analyze                            │
│  - Thread-safe with RWMutex                              │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│           Optional: RetryClient                          │
│  - Exponential backoff retry                             │
│  - Context-aware                                         │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│              OpenAIClient (client.go)                    │
│  - OpenAI SDK wrapper                                    │
│  - Chat, Analyze, ChatStream, HealthCheck                │
└─────────────────────────────────────────────────────────┘

=== Phase 2 Components ===

┌─────────────────────────────────────────────────────────┐
│                    Router (router.go)                    │
│  - Scenario detection                                    │
│  - Strategy-based selection                              │
│  - Multi-model support                                   │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│              MetricsCollector (metrics.go)               │
│  - OpenTelemetry integration                             │
│  - Latency histogram                                     │
│  - Token counters                                        │
│  - Cost tracking                                         │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│               CostCalculator (cost.go)                   │
│  - Per-model pricing                                     │
│  - Session aggregation                                   │
│  - Budget alerts                                         │
│  - Token estimation                                      │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│                RateLimiter (ratelimit.go)                │
│  - Token bucket algorithm                                │
│  - Queue management                                      │
│  - Per-model limits                                      │
└─────────────────────────────────────────────────────────┘
```

## Testing

Run tests:

```bash
cd brain
go test -v ./...

# With coverage
go test -cover ./...

# Short mode (skip integration tests)
go test -short ./...
```

## Dependencies

### Phase 1
- `github.com/cenkalti/backoff/v4` - Exponential backoff
- `github.com/hashicorp/golang-lru/v2` - LRU cache
- `github.com/sashabaranov/go-openai` - OpenAI SDK
- `github.com/stretchr/testify` - Testing framework

### Phase 2 ✅
- `go.opentelemetry.io/otel` - OpenTelemetry metrics
- `go.opentelemetry.io/otel/metric` - Metrics API
- `golang.org/x/time/rate` - Rate limiting (token bucket)

### Phase 3 ✨ NEW
- `github.com/sony/gobreaker` - Circuit breaker pattern
- `go.uber.org/atomic` - Atomic operations for thread safety

## Performance Considerations

1. **Cache Hit**: ~1-5ms (memory access)
2. **Cache Miss**: ~100-2000ms (API call + network)
3. **Retry Overhead**: 100ms - 5s per retry (configurable)
4. **Streaming**: First token ~100-500ms, then continuous
5. **Rate Limit Queue**: Adds 0-30s wait time (configurable)
6. **Router Overhead**: <1ms (in-memory selection)
7. **Metrics Overhead**: <0.1ms (async OTel export)

## Error Handling

All methods follow Go error handling conventions:

```go
response, err := brain.Chat(ctx, prompt)
if err != nil {
    // Check for specific error types
    if errors.Is(err, context.DeadlineExceeded) {
        // Timeout
    } else if errors.Is(err, context.Canceled) {
        // Canceled
    } else if errors.Is(err, rate.ErrLimitExceeded) {
        // Rate limited
    }
    return err
}
```

## Security Notes

- API keys loaded from environment variables only
- No logging of prompts or responses
- Context-based cancellation prevents resource leaks
- Thread-safe for concurrent access
- Rate limiting prevents API quota exhaustion

## Migration Guide (Phase 1 → Phase 2)

Phase 2 is **fully backward compatible** with Phase 1. All existing code continues to work without changes.

### Optional Enhancements

1. **Enable metrics:**
   ```bash
   export HOTPLEX_BRAIN_METRICS_ENABLED=true
   ```

2. **Enable cost tracking:**
   ```bash
   export HOTPLEX_BRAIN_COST_TRACKING_ENABLED=true
   ```

3. **Enable rate limiting:**
   ```bash
   export HOTPLEX_BRAIN_RATE_LIMIT_ENABLED=true
   export HOTPLEX_BRAIN_RATE_LIMIT_RPS=10
   ```

4. **Enable multi-model routing:**
   ```bash
   export HOTPLEX_BRAIN_ROUTER_ENABLED=true
   export HOTPLEX_BRAIN_ROUTER_MODELS="gpt-4o-mini:openai:0.00015:0.0006:200;qwen-plus:dashscope:0.0006:0.0012:300"
   ```

## Future Enhancements

- [ ] Request/response logging (opt-in)
- [ ] Prometheus metrics exporter
- [ ] Distributed tracing integration
- [ ] A/B testing framework
- [ ] Adaptive rate limiting based on provider feedback

---

**Status:** ✅ Production Ready (Phase 3)  
**Last Updated:** 2026-03-04  
**PR:** #177 (feat/nativebrain-production-enhancements)

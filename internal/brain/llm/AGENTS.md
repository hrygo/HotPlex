# LLM Client Subpackage

## OVERVIEW
Multi-provider LLM client library with decorator chain (retry → cache), standalone circuit breaker, rate limiter with per-model token buckets, 4-strategy model router, CJK-aware cost calculator, and OpenTelemetry metrics. All clients implement the `LLMClient` interface.

## STRUCTURE
```
llm/
  client.go      # LLMClient interface, ChatOptions, OpenAIClient, AnthropicClient
  retry.go       # RetryClient decorator (cenkalti/backoff v4 exponential)
  cache.go       # CachedClient decorator (hashicorp/golang-lru/v2, sha256 key)
  circuit.go     # CircuitBreaker: standalone gobreaker wrapper, 3-state (closed/open/half-open)
  ratelimit.go   # RateLimiter: token bucket + FIFO queue + per-model limiting
  router.go      # ModelRouter: 4 strategies (cost/latency/quality/balanced) + scenario routing
  cost.go        # CostCalculator: CJK-aware token estimation, per-session tracking, budget alerts
  metrics.go     # MetricsCollector: OTel histograms/counters for latency, tokens, cost, errors
  stream.go      # Streaming support via channels
  helpers.go     # Shared utilities
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Core interface | `client.go` LLMClient | Chat, ChatWithOptions, Analyze, ChatStream, HealthCheck |
| Add new provider | `client.go` | Implement LLMClient, add compile-time assertion |
| Decorator chain order | `brain/init.go` Init() | baseClient → RetryClient → CachedClient |
| Circuit breaker | `circuit.go` CircuitBreaker | Wraps in enhancedBrainWrapper, not as decorator |
| Rate limiter | `ratelimit.go` RateLimiter | Applied in enhancedBrainWrapper, per-model optional |
| Model routing | `router.go` ModelRouter | SelectModel(strategy, scenario) → best model |
| Cost tracking | `cost.go` CostCalculator | TrackRequest, GetSessionCost, budget alerts |
| Metrics export | `metrics.go` MetricsCollector | RecordRequest, RecordError, RecordTokens |
| Streaming | `stream.go` | ChatStream returns <-chan string |

## KEY PATTERNS

**Decorator chain (composed in brain/init.go)**
```
baseClient (OpenAI | Anthropic)
  → RetryClient (cenkalti/backoff exponential, maxRetries=0 disables)
    → CachedClient (sha256(prompt+opts) → LRU, goroutine-safe)
```
Rate limiter and circuit breaker are NOT decorators — applied directly inside `enhancedBrainWrapper` to avoid double rate limiting and ensure accurate latency measurement.

**Compile-time interface check**
```go
var (
    _ LLMClient = (*OpenAIClient)(nil)
    _ LLMClient = (*AnthropicClient)(nil)
    _ LLMClient = (*CachedClient)(nil)
    _ LLMClient = (*RetryClient)(nil)
)
```

**Circuit breaker (circuit.go)**
- Wraps `gobreaker.CircuitBreaker` with hotplex-specific config
- 3 states: closed → open (maxFailures) → half-open (timeout) → closed/back to open
- Metrics exported via OpenTelemetry on state transitions
- Callers use `circuitBreaker.Execute(ctx, fn)` pattern

**Model router (router.go)**
- 4 strategies: `cost_priority`, `latency_priority`, `quality_priority`, `balanced`
- 4 scenarios: `chat`, `analyze`, `code`, `reasoning`
- `SelectModel(strategy, scenario) → ModelConfig` — strategy × scenario matrix

**CJK-aware token estimation (cost.go)**
- ASCII: 4.0 chars/token, CJK: 1.5 chars/token, Other Unicode: 3.0 chars/token
- Per-session cost tracking with TTL-based eviction (amortized O(n) scan)

**Rate limiter (ratelimit.go)**
- Token bucket (`golang.org/x/time/rate`) + FIFO queue with timeout
- Optional per-model limiting via `PerModel: true`

## ANTI-PATTERNS
- ❌ Access `globalBrain` directly — use `brain.Global()` accessor from parent package
- ❌ Skip `brain.Init()` before using any LLM client — singleton must be initialized
- ❌ Apply circuit breaker or rate limiter as decorator — must be in wrapper to avoid double counting
- ❌ Use `crypto/rand` client-side — cache keys use `crypto/sha256` only
- ❌ Hard-code model names in routing logic — use `ModelConfig` from config

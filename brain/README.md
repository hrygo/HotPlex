# Native Brain - Production-Grade LLM Client

This package provides production-ready LLM client capabilities for HotPlex, implementing enterprise-grade features for reliability, performance, and observability.

## Features

### 1. 🎬 Streaming Support (`ChatStream`)

Real-time token streaming for progressive response rendering:

```go
brain := brain.Global()
if streamingBrain, ok := brain.(brain.StreamingBrain); ok {
    stream, err := streamingBrain.ChatStream(ctx, "Write a story")
    if err != nil {
        log.Fatal(err)
    }
    
    for token := range stream {
        fmt.Print(token) // Real-time output
    }
}
```

**Benefits:**
- Lower perceived latency for users
- Progressive UI rendering
- Better UX for long responses

### 2. 🔄 Retry with Exponential Backoff

Automatic retry logic using `github.com/cenkalti/backoff/v4`:

```go
// Configured via environment variables:
// HOTPLEX_BRAIN_MAX_RETRIES=3
// HOTPLEX_BRAIN_RETRY_MIN_WAIT_MS=100
// HOTPLEX_BRAIN_RETRY_MAX_WAIT_MS=5000
```

**Features:**
- Exponential backoff between retries
- Configurable max retries (0 to disable)
- Context-aware cancellation
- Permanent error detection

### 3. 💾 LRU Cache Layer

Intelligent response caching using `github.com/hashicorp/golang-lru/v2`:

```go
// Configured via:
// HOTPLEX_BRAIN_CACHE_SIZE=1000  // entries (0 to disable)
```

**Benefits:**
- Reduces API costs for repeated queries
- Lower latency for cached responses
- Thread-safe with RWMutex
- Automatic eviction (LRU policy)

**Note:** Streaming responses are not cached (by design).

### 4. ⏱️ Timeout Control

Request-level timeout enforcement:

```go
// Configured via:
// HOTPLEX_BRAIN_TIMEOUT_S=10  // seconds
```

**Features:**
- Applied to all Chat/Analyze/ChatStream calls
- Context-based cancellation
- Prevents hung requests
- Configurable per deployment

### 5. 🏥 Health Check

Service health monitoring:

```go
type HealthStatus struct {
    Healthy   bool
    Provider  string
    Model     string
    LatencyMs int64
    Error     string
}

// Usage:
if monitor, ok := brain.(interface{ HealthCheck(context.Context) HealthStatus }); ok {
    status := monitor.HealthCheck(ctx)
    if !status.Healthy {
        log.Printf("Brain unhealthy: %s", status.Error)
    }
}
```

**Features:**
- Ping-based health verification
- Latency measurement
- Cached status (configurable interval)
- Integration-ready for monitoring systems

## Configuration

All features are configured via environment variables:

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
│              StreamingBrain Interface                    │
│  - ChatStream(ctx, prompt) (<-chan string, error)       │
│  - HealthCheck(ctx) HealthStatus                        │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│              brainWrapper (init.go)                      │
│  - Applies timeout from Config                          │
│  - Wraps client stack                                    │
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
```

## Usage Examples

### Basic Initialization

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

### Streaming with Progress

```go
if streamingBrain, ok := brain.Global().(brain.StreamingBrain); ok {
    stream, err := streamingBrain.ChatStream(ctx, "Explain quantum computing")
    if err != nil {
        log.Fatal(err)
    }
    
    var fullResponse strings.Builder
    for token := range stream {
        fullResponse.WriteString(token)
        fmt.Print(token) // Real-time display
    }
}
```

### Health Monitoring

```go
// Periodic health check
ticker := time.NewTicker(30 * time.Second)
defer ticker.Stop()

for range ticker.C {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    status := brain.Global().(interface{ HealthCheck(context.Context) HealthStatus }).HealthCheck(ctx)
    cancel()
    
    if !status.Healthy {
        log.Printf("Brain unhealthy: %s (latency: %dms)", status.Error, status.LatencyMs)
    }
}
```

### Cache Management

```go
// Access underlying cached client if needed
if cachedClient, ok := getUnderlyingClient().(*llm.CachedClient); ok {
    keys, _, _ := cachedClient.CacheStats()
    log.Printf("Cache size: %d entries", keys)
    
    // Clear cache if needed
    cachedClient.ClearCache()
}
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

- `github.com/cenkalti/backoff/v4` - Exponential backoff
- `github.com/hashicorp/golang-lru/v2` - LRU cache
- `github.com/sashabaranov/go-openai` - OpenAI SDK
- `github.com/stretchr/testify` - Testing framework

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
    }
    return err
}
```

## Performance Considerations

1. **Cache Hit**: ~1-5ms (memory access)
2. **Cache Miss**: ~100-2000ms (API call + network)
3. **Retry Overhead**: 100ms - 5s per retry (configurable)
4. **Streaming**: First token ~100-500ms, then continuous

## Security Notes

- API keys loaded from environment variables only
- No logging of prompts or responses
- Context-based cancellation prevents resource leaks
- Thread-safe for concurrent access

## Future Enhancements (Phase 2)

- [ ] Rate limiting
- [ ] Token counting and cost tracking
- [ ] Multi-provider failover
- [ ] Request/response logging
- [ ] Metrics export (Prometheus)
- [ ] Circuit breaker pattern

---

**Status:** ✅ Production Ready  
**Last Updated:** 2026-03-04

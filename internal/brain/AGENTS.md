# Brain Orchestration Package

## OVERVIEW
LLM orchestration layer providing multi-provider LLM client with decorator chain (retry, cache, rate limit, circuit breaker, metrics). Optional — graceful degradation when no API key configured.

Output sanitization (redacting API keys, credentials, internal IPs) has been extracted to `internal/security/sanitize.go` as a standalone utility.

## STRUCTURE
```
brain/
  brain.go       # Core interface: Brain (Chat + ChatWithOptions) + global singleton
  init.go        # Init() orchestration + enhancedBrainWrapper (middleware pipeline + circuit breaker)
  config.go      # 8 sub-configs + 4-tier API key discovery + env loading
  extractor.go   # ConfigExtractor: Claude Code / OpenCode credential extraction from config files
  util.go        # UTF-8 safe string truncation
  llm/           # LLM client subpackage (see below)
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Brain interface | `brain.go` Brain | Chat + ChatWithOptions — single interface |
| Init + middleware chain | `init.go` Init() | Assembles: baseClient → Retry → Cache; rate limit/circuit breaker applied in wrapper |
| enhancedBrainWrapper | `init.go` | Satisfies Brain interface; applies timeout, rate limit, metrics, model routing, circuit breaker |
| 4-tier API key discovery | `config.go` systemKeySources | Dedicated key → worker configs → system env → disabled |
| Config credential extraction | `extractor.go` ConfigExtractor | Reads ~/.claude/settings.json, ~/.config/opencode/opencode.json |
| LLM client interface | `llm/client.go` LLMClient | Chat, ChatWithOptions, Analyze, ChatStream, HealthCheck |
| Model router | `llm/router.go` | 4 strategies: cost/latency/quality/balanced |
| Circuit breaker | `llm/circuit.go` | Standalone CircuitBreaker (not decorator) |
| Rate limiter | `llm/ratelimit.go` | Token bucket + queue + per-model limiting |
| Cost calculator | `llm/cost.go` | CJK-aware token estimation, per-session tracking, budget alerts |
| Metrics collector | `llm/metrics.go` | OpenTelemetry integration, RequestTimer |

## KEY PATTERNS

**Decorator chain (LLM clients)**
```
baseClient (OpenAI | Anthropic)
  → RetryClient (cenkalti/backoff exponential)
    → CachedClient (hashicorp/golang-lru)
```
3 LLMClient decorator implementations: RetryClient, CachedClient. Rate limiting and circuit breaker are applied directly inside enhancedBrainWrapper (not as decorators) to avoid double rate limiting and allow precise control.

**Global singleton (lazy init)**
- `globalBrain` — `brain.Global()` accessor, `brain.SetGlobal()` setter with io.Closer cleanup

**CJK-aware token estimation**
- ASCII: 4.0 chars/token, CJK: 1.5 chars/token, Other Unicode: 3.0 chars/token

**4-tier API key discovery (config.go)**
1. `HOTPLEX_BRAIN_API_KEY` (dedicated)
2. Worker config files (`~/.claude/settings.json`, `~/.config/opencode/opencode.json`)
3. System env vars (`ANTHROPIC_API_KEY` → `OPENAI_API_KEY` → `SILICONFLOW_API_KEY` → `DEEPSEEK_API_KEY`)
4. Disabled (graceful degradation, no LLM features)

**Circuit breaker integration**
- Standalone `CircuitBreaker` wraps LLM calls inside `ChatWithOptions()`
- When enabled, `circuitBreaker.Execute()` guards against cascading LLM API failures
- Metrics timer starts BEFORE the LLM call (inside circuit breaker callback) to capture accurate latency

## ANTI-PATTERNS
- ❌ Access `globalBrain` directly — use `brain.Global()` accessor
- ❌ Skip `Init()` before using any brain feature — singleton must be initialized

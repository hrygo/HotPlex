# Native Brain

## Intelligent Agent Orchestration

The HotPlex **Native Brain** is an advanced orchestration layer that provides intelligent routing, cost optimization, resilience patterns, and memory management for AI agents. It acts as the "brain" behind the agent's decision-making process.

---

### Enabling the Brain

The Brain is automatically enabled when `HOTPLEX_BRAIN_API_KEY` is set:

```bash
export HOTPLEX_BRAIN_API_KEY=sk-...
export HOTPLEX_BRAIN_PROVIDER=openai
export HOTPLEX_BRAIN_MODEL=gpt-4o-mini
```

---

### Core Features

#### 1. Intelligent Routing

Route requests to different models based on intent analysis or cost optimization strategies.

| Strategy | Description |
| :------- | :---------- |
| `cost_priority` | Route to cheapest suitable model |
| `quality_first` | Always use best available model |
| `balanced` | Balance cost and quality |

```bash
# Enable routing with cost_priority strategy
export HOTPLEX_BRAIN_ROUTER_ENABLED=true
export HOTPLEX_BRAIN_ROUTER_STRATEGY=cost_priority
export HOTPLEX_BRAIN_ROUTER_MODELS="claude:gpt-4o,mini:gpt-4o-mini"
```

#### 2. Circuit Breaker

Protect your system from cascading failures by detecting and stopping failing providers.

```bash
export HOTPLEX_BRAIN_CIRCUIT_BREAKER_ENABLED=true
export HOTPLEX_BRAIN_CIRCUIT_BREAKER_MAX_FAILURES=5
export HOTPLEX_BRAIN_CIRCUIT_BREAKER_TIMEOUT=30s
```

#### 3. Failover

Automatically switch to backup providers when the primary fails.

```bash
export HOTPLEX_BRAIN_FAILOVER_ENABLED=true
export HOTPLEX_BRAIN_FAILOVER_ENABLE_AUTO=true
export HOTPLEX_BRAIN_FAILOVER_COOLDOWN=5m
```

#### 4. Budget Management

Track and limit API usage costs.

```bash
export HOTPLEX_BRAIN_BUDGET_ENABLED=true
export HOTPLEX_BRAIN_BUDGET_LIMIT=10.0
export HOTPLEX_BRAIN_BUDGET_PERIOD=daily
export HOTPLEX_BRAIN_BUDGET_ENABLE_HARD_LIMIT=false
```

#### 5. Memory Compression

Automatically compress conversation history to stay within token limits.

```bash
export HOTPLEX_BRAIN_MEMORY_ENABLED=true
export HOTPLEX_BRAIN_MEMORY_TOKEN_THRESHOLD=8000
export HOTPLEX_BRAIN_MEMORY_TARGET_TOKENS=2000
export HOTPLEX_BRAIN_MEMORY_COMPRESSION_RATIO=0.25
```

#### 6. Input/Output Guard

Filter potentially dangerous or inappropriate content.

```bash
export HOTPLEX_BRAIN_GUARD_ENABLED=true
export HOTPLEX_BRAIN_GUARD_INPUT_ENABLED=true
export HOTPLEX_BRAIN_GUARD_OUTPUT_ENABLED=true
export HOTPLEX_BRAIN_GUARD_SENSITIVITY=medium
```

---

### Environment Variables

#### Core Configuration

| Variable | Description | Default |
| :------- | :---------- | :------ |
| `HOTPLEX_BRAIN_API_KEY` | API key for Brain LLM | - |
| `HOTPLEX_BRAIN_PROVIDER` | LLM provider | `openai` |
| `HOTPLEX_BRAIN_MODEL` | Model name | `gpt-4o-mini` |
| `HOTPLEX_BRAIN_ENDPOINT` | Custom API endpoint | - |
| `HOTPLEX_BRAIN_TIMEOUT_S` | Request timeout (seconds) | `10` |

#### Resilience Features

| Variable | Description | Default |
| :------- | :---------- | :------ |
| `HOTPLEX_BRAIN_CIRCUIT_BREAKER_ENABLED` | Enable circuit breaker | `false` |
| `HOTPLEX_BRAIN_CIRCUIT_BREAKER_MAX_FAILURES` | Failures before open | `5` |
| `HOTPLEX_BRAIN_CIRCUIT_BREAKER_TIMEOUT` | Circuit open duration | `30s` |
| `HOTPLEX_BRAIN_FAILOVER_ENABLED` | Enable failover | `false` |
| `HOTPLEX_BRAIN_FAILOVER_ENABLE_AUTO` | Auto-switch providers | `true` |
| `HOTPLEX_BRAIN_RATE_LIMIT_ENABLED` | Enable rate limiting | `false` |
| `HOTPLEX_BRAIN_RATE_LIMIT_RPS` | Requests per second | `10.0` |

#### Cost Management

| Variable | Description | Default |
| :------- | :---------- | :------ |
| `HOTPLEX_BRAIN_BUDGET_ENABLED` | Enable budget tracking | `false` |
| `HOTPLEX_BRAIN_BUDGET_LIMIT` | Period spending limit (USD) | `10.0` |
| `HOTPLEX_BRAIN_BUDGET_PERIOD` | Budget period | `daily` |
| `HOTPLEX_BRAIN_COST_TRACKING_ENABLED` | Enable cost tracking | `true` |

#### Memory Management

| Variable | Description | Default |
| :------- | :---------- | :------ |
| `HOTPLEX_BRAIN_MEMORY_ENABLED` | Enable memory compression | `true` |
| `HOTPLEX_BRAIN_MEMORY_TOKEN_THRESHOLD` | Tokens before compress | `8000` |
| `HOTPLEX_BRAIN_MEMORY_TARGET_TOKENS` | Target after compress | `2000` |
| `HOTPLEX_BRAIN_MEMORY_COMPRESSION_RATIO` | Compression ratio | `0.25` |

#### Intent Routing

| Variable | Description | Default |
| :------- | :---------- | :------ |
| `HOTPLEX_BRAIN_INTENT_ROUTER_ENABLED` | Enable intent routing | `true` |
| `HOTPLEX_BRAIN_INTENT_ROUTER_CONFIDENCE` | Minimum confidence | `0.7` |
| `HOTPLEX_BRAIN_INTENT_ROUTER_CACHE_SIZE` | Intent cache size | `1000` |

#### Guard (Content Filter)

| Variable | Description | Default |
| :------- | :---------- | :------ |
| `HOTPLEX_BRAIN_GUARD_ENABLED` | Enable content guard | `true` |
| `HOTPLEX_BRAIN_GUARD_INPUT_ENABLED` | Filter input | `true` |
| `HOTPLEX_BRAIN_GUARD_OUTPUT_ENABLED` | Filter output | `true` |
| `HOTPLEX_BRAIN_GUARD_SENSITIVITY` | Filter level | `medium` |
| `HOTPLEX_BRAIN_GUARD_MAX_INPUT_LENGTH` | Max input characters | `100000` |

---

### Provider Support

The Brain supports multiple LLM providers:

- `openai` - OpenAI API
- `anthropic` - Anthropic Claude API
- `dashscope` - Alibaba DashScope
- `siliconflow` - SiliconFlow
- Custom endpoint via `HOTPLEX_BRAIN_ENDPOINT`

```bash
# Using Anthropic
export HOTPLEX_BRAIN_PROVIDER=anthropic
export HOTPLEX_BRAIN_MODEL=claude-sonnet-4-20250514

# Using custom endpoint
export HOTPLEX_BRAIN_PROVIDER=openai
export HOTPLEX_BRAIN_ENDPOINT=https://api.custom.com/v1
```

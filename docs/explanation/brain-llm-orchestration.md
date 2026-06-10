---
title: Brain/LLM 编排层
weight: 4
description: HotPlex Brain 智能中间件：单接口设计、装饰器链、4 层 API Key 发现与熔断保护
---

# Brain/LLM 编排层

> HotPlex Brain 是一个可选的 LLM 编排层，为 TTS 摘要、会话历史压缩等内部功能提供 LLM 能力。当未配置 API Key 时，所有依赖功能优雅降级。

## 核心问题

HotPlex 的核心执行路径是：用户消息 → Gateway → Worker（Claude Code / OpenCode Server）。Worker 本身已经是强大的 AI Agent。

Brain 层存在的价值是**为 Gateway 内部功能提供 LLM 能力**，而不需要启动完整的 Worker 进程：

- TTS 语音合成前的文本摘要（`tts.SummarizeForTTS`）— 使用 `Brain.ChatWithOptions()` 生成摘要
- 会话历史压缩（`gateway.HistoryCompressor`）— 当 CodexCLI 恢复会话时，使用 Brain 智能压缩过长的对话历史，保留关键语义
- 输出脱敏（已提取到 `internal/security/sanitize.go`）— 独立工具，不依赖 Brain

Brain 层充当轻量级 LLM 客户端——低成本、可配置，为 Gateway 内部服务。

## 设计决策

### 单一接口设计

Brain 使用单一 `Brain` 接口，遵循 Interface Segregation Principle——消费者只需要文本生成：

```go
type Brain interface {
    Chat(ctx context.Context, prompt string) (string, error)
    ChatWithOptions(ctx context.Context, prompt string, opts llm.ChatOptions) (string, error)
}
```

`enhancedBrainWrapper` 实现该接口，内部集成：
- 超时控制（`applyTimeout`）
- 模型路由（`selectModel`）
- 请求限流（`applyRateLimit`）
- 熔断保护（`circuitBreaker.Execute`）
- 指标记录（`startMetricsTimer` + `recordMetrics`）

### 4 层 API Key 发现

Brain 的配置从环境变量加载，API Key 按以下优先级发现：

```
1. HOTPLEX_BRAIN_API_KEY    -- 专用 Key（最高优先级）
2. Worker 配置文件           -- 扫描 ~/.claude/settings.json 和 ~/.config/opencode/opencode.json
3. 系统环境变量              -- ANTHROPIC_API_KEY → OPENAI_API_KEY → SILICONFLOW_API_KEY → DEEPSEEK_API_KEY
4. 未找到                   -- Brain 禁用，所有功能优雅降级
```

**为什么需要 4 层？** 这让 Brain 在零配置下也能工作。如果用户已经配置了 Claude Code 的 API Key（`ANTHROPIC_API_KEY` 或 `~/.claude/settings.json`），Brain 会自动复用它，不需要额外的配置步骤。只有当需要 Brain 使用不同的模型或 Provider 时，才需要设置 `HOTPLEX_BRAIN_API_KEY`。

### Decorator Chain 中间件栈

Brain 的 LLM 客户端使用装饰器模式构建中间件链，按顺序叠加功能：

```
BaseClient (OpenAI / Anthropic)
  → RetryClient (指数退避重试，默认 3 次)
    → CachedClient (LRU 缓存，默认 1000 条)
```

每个装饰器实现 `LLMClient` 接口，可以独立叠加或移除。

**Rate Limiting 和 Circuit Breaker 不在装饰器链中**：它们在 `enhancedBrainWrapper` 内部直接处理。Rate limiting 在 wrapper 层面可以精确控制限流粒度（per-model 而非 per-client）；Circuit Breaker 在 wrapper 层面包装完整调用（含 metrics 计时），确保延迟统计准确。

### 8 子配置结构

Brain 的 Config 聚合了 8 个子配置，每个控制一个独立的功能面：

| 子配置 | 功能 | 默认状态 |
|--------|------|---------|
| Model | LLM 后端（provider/model/endpoint） | auto-detect |
| Cache | 响应缓存 | enabled, 1000 条 |
| Retry | 重试策略 | enabled, 3 次, 100-5000ms |
| Metrics | 可观测性 | enabled |
| Cost | 成本追踪 | enabled |
| RateLimit | 请求限流 | disabled |
| Router | 模型路由 | disabled |
| CircuitBreaker | 故障熔断 | disabled |

大多数功能开箱即用，只有高级功能（模型路由、限流、熔断）需要显式启用。

## 内部机制

### enhancedBrainWrapper 调用流程

每次 `ChatWithOptions()` 调用经过以下步骤：

```
1. applyTimeout()    → 注入超时 context
2. selectModel()     → 通过 Router 选择模型，或使用默认模型
3. applyRateLimit()  → Per-model 令牌桶限流
4. startMetricsTimer() → 开始计时（在 LLM 调用之前）
5. circuitBreaker.Execute() → 熔断保护包装
   └─ client.ChatWithOptions() → 实际 LLM 调用
6. recordMetrics()   → 记录延迟、token 数、成本、错误
```

**Metrics 计时准确性**：timer 在 LLM 调用之前启动（步骤 4），确保捕获完整延迟。当 CircuitBreaker 启用时，timer 在熔断回调内部启动，精确测量 LLM 调用耗时。

### CircuitBreaker 熔断保护

当 LLM API 连续失败超过阈值（默认 `MaxFailures=5`），CircuitBreaker 进入 Open 状态，后续请求直接返回错误而不调用 API。经过 `Timeout`（默认 60s）后进入 Half-Open 状态，允许一次试探请求；成功则恢复 Closed，失败则继续 Open。

### 全局单例与并发安全

Brain 使用 `sync.RWMutex` 保护全局实例。所有组件的 public 方法都是并发安全的——cache 有独立的 mutex，metrics 使用 `atomic.Int64`，不依赖外部锁。

`SetGlobal()` 在覆盖旧实例时自动调用 `io.Closer.Close()`，释放旧实例的 rate limiter goroutine，防止热重载时的资源泄漏。

## 权衡与限制

1. **Brain 不可用时的功能降级**：当没有配置 API Key 时，Brain 完全禁用。TTS 摘要跳过 Brain 步骤，使用原始文本；会话历史压缩回退到截断（truncation），丢弃最早的对话轮次。功能不受影响，但智能性降低。

2. **模型路由的有限策略**：只支持 `cost_priority` 和 `latency_priority` 两种策略，不支持基于场景的动态策略切换。

3. **输出脱敏独立化**：敏感数据检测（`security.RedactSensitive`）已从 Brain 提取为独立工具。目前尚未接入输出管道，需要后续在 bridge_forward 或 XML sanitizer 层集成。

## 参考

- `internal/brain/brain.go` — Brain 接口定义 + 全局单例
- `internal/brain/init.go` — 初始化编排 + enhancedBrainWrapper
- `internal/brain/config.go` — 8 子配置 + 4 层 API Key 发现
- `internal/brain/extractor.go` — Worker 配置文件凭证提取
- `internal/gateway/history_compress.go` — 会话历史压缩（Brain 消费者）
- `internal/security/sanitize.go` — 独立输出脱敏工具（提取自 Brain）

---

## 相关实践

- [配置参考 — brain 配置段](../reference/configuration.md) — Brain 的全部可配置参数

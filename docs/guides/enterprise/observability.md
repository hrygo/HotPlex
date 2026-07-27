---
title: Observability Guide
weight: 26
description: Structured logging, OTel-native metrics and tracing, health endpoints, and alerting best practices for HotPlex Gateway.
---

# Observability Guide

> 面向企业运维团队的 HotPlex Gateway 可观测性指南。涵盖结构化日志、OTel 原生指标与追踪、健康检查端点及告警最佳实践。

---

## 1. 结构化日志

HotPlex 使用 `log/slog` JSON Handler 输出结构化日志，兼容 OTel Log Data Model。

### 必填字段

| 字段 | 说明 |
|------|------|
| `timestamp` | ISO 8601 / Unix ms（slog 自动生成） |
| `level` | DEBUG / INFO / WARN / ERROR |
| `message` | 人类可读事件描述 |
| `service.name` | 固定 `hotplex-gateway` |
| `session_id` | 会话标识 |
| `agent_id` | Agent 身份标识（#848 AgentIdentity 派生，跨 session/event/audit/trace 按 agent 关联） |
| `user_id` | 用户标识 |
| `bot_id` | Bot 实例标识 |
| `trace_id` | 分布式追踪上下文（若存在） |
| `span_id` | OTel span 标识（#850 Hub.SendToSession 注入，关联事件到精确 span） |

### 示例

```json
{
  "time": "2026-05-10T22:00:00.000Z",
  "level": "INFO",
  "msg": "session created",
  "service.name": "hotplex-gateway",
  "session_id": "01234567-89ab-cdef",
  "agent_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "user_id": "U_ABC123",
  "bot_id": "B_XYZ789",
  "trace_id": "abc123def456",
  "span_id": "1234567890abcdef"
}
```

### 日志级别规范

- **ERROR**：全量记录，不采样，触发告警评估
- **WARN**：降级或非致命异常，需关注但无需立即介入
- **INFO**：正常业务事件（session 创建/销毁、worker 启动等）
- **DEBUG**：开发调试信息，生产环境默认关闭

---

## 2. OTel 原生指标体系

HotPlex 使用统一的 `internal/observability/` 包，通过 OTel Meter API 注册 60+ 个指标，前缀为 `hotplex.`。应用代码零直接依赖 `prometheus/client_golang`。

指标通过 OTel Prometheus Exporter 以标准 Prometheus 格式暴露于 `GET /admin/metrics`，同时支持通过 OTLP gRPC 导出到 OTel Collector。

### 2.1 Session 指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `hotplex.session.created` | Counter | `worker_type` | 累计创建会话数 |
| `hotplex.session.terminated` | Counter | `reason` | 会话终止原因 |
| `hotplex.session.deleted` | Counter | — | GC 保留清理数 |
| `hotplex.session.start.attempts` | Counter | `worker_type` | 启动尝试次数 |
| `hotplex.session.start.errors` | Counter | — | 启动错误数 |
| `hotplex.session.start.duration` | Histogram | — | 启动耗时 |

### 2.2 Worker 指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `hotplex.worker.starts` | Counter | `worker_type`, `result` | Worker 启动尝试 |
| `hotplex.worker.execution.duration` | Histogram | `worker_type` | 执行耗时 |
| `hotplex.worker.crashes` | Counter | `worker_type`, `exit_code` | Worker 崩溃计数 |
| `hotplex.worker.memory.bytes` | ObservableGauge | `worker_type` | 预估内存 |
| `hotplex.worker.creation.duration` | Histogram | — | 进程创建耗时 |

### 2.3 Gateway 指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `hotplex.gateway.connections` | ObservableGauge | — | 当前 WebSocket 连接数 |
| `hotplex.gateway.webchat.session_owner_connections` | ObservableGauge | — | 当前拥有直接 WebSocket owner 的 session 数 |
| `hotplex.gateway.messages` | Counter | `direction`, `event_type` | WS 消息收发 |
| `hotplex.gateway.events` | Counter | `event_type`, `direction` | AEP 事件透传 |
| `hotplex.gateway.init.handshake.duration` | Histogram | — | WS 握手耗时 |
| `hotplex.gateway.deltas.dropped` | Counter | — | 背压丢弃的 delta 事件 |
| `hotplex.gateway.platform.dropped` | Counter | `event_type` | 平台连接缓冲区溢出丢弃 |
| `hotplex.gateway.no_subscribers.dropped` | Counter | `event_type` | 无订阅者丢弃 |
| `hotplex.gateway.delta.coalesced` | Counter | — | Delta 合并数 |
| `hotplex.gateway.delta.flush` | Counter | — | 合并 delta 刷新数 |
| `hotplex.gateway.errors` | Counter | `error_code` | 错误分类计数 |
| `hotplex.gateway.webchat.duplicate_connection_rejected` | Counter | — | 因已有 session owner 被拒绝的 init 数 |
| `hotplex.gateway.webchat.non_owner_ingress_rejected` | Counter | — | 非 owner 发起受保护入站事件的拒绝数 |
| `hotplex.gateway.webchat.owner_release_not_current` | Counter | — | 非当前 owner 尝试释放 owner 的次数 |

### 2.4 Pool 指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `hotplex.pool.acquire` | Counter | `result` | 配额获取结果 |
| `hotplex.pool.release.errors` | Counter | — | 双重释放错误（代码 Bug 指标） |

### 2.5 Cron 指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `hotplex.cron.fires` | Counter | `job_name` | 任务触发次数 |
| `hotplex.cron.errors` | Counter | `job_name`, `error_type` | 执行错误分类 |
| `hotplex.cron.duration` | Histogram | `job_name` | 执行耗时 |
| `hotplex.cron.attached` | Counter | — | Session-attached 投递次数 |

### 2.6 Streaming 指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `hotplex.streaming.card.rotations` | Counter | — | TTL 触发的卡片轮转 |
| `hotplex.streaming.card.rotation_failures` | Counter | `phase` | 轮转失败 |
| `hotplex.streaming.card.flush_fallbacks` | Counter | — | CardKit 降级到 IM Patch |

### 2.7 ACP 指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `hotplex.acp.prompt_tokens` | Counter | — | ACP Worker prompt token 数 |
| `hotplex.acp.tool_calls` | Counter | — | 工具调用次数 |
| `hotplex.acp.permission_requests` | Counter | — | 权限请求次数 |
| `hotplex.acp.handshake.duration` | Histogram | — | JSON-RPC 握手耗时 |

### 2.8 LLM 重试指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `hotplex.retry.attempts` | Counter | — | LLM 重试尝试次数 |
| `hotplex.retry.exhaustion` | Counter | — | 重试耗尽（最终失败）次数 |

### 2.9 Execution & Lease-Repair 指标

Durable ingress 的输入账本、owner lease 续约与终态修复子系统。

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `hotplex.execution.accept` | Counter | — | 新输入被持久化接受 |
| `hotplex.execution.duplicate` | Counter | — | 幂等去重（相同 ID + payload） |
| `hotplex.execution.conflict` | Counter | — | Payload 冲突（相同 ID，不同 hash） |
| `hotplex.execution.session_busy` | Counter | — | Active gate 拒绝（session 忙于此前 execution），转入 mid-turn 透传或暂存兜底 |
| `hotplex.execution.mid_turn_injected` | Counter | — | busy 时追问被透传注入当前 turn（worker 支持 mid-turn，如 claude_code/codex_cli） |
| `hotplex.execution.supplement_buffered` | Counter | — | busy 时追问被暂存，待 turn 完成后重投（worker 不支持 mid-turn 的兜底） |
| `hotplex.execution.delivery_outcome` | Counter | `delivery_status` | Worker 投递结果（delivered/unknown/failed） |
| `hotplex.execution.runtime_outcome` | Counter | `runtime_status` | Worker 运行终态（completed/failed/unknown） |
| `hotplex.execution.delivery_latency` | Histogram | — | accept 到 delivery outcome 耗时 |
| `hotplex.execution.runtime_duration` | Histogram | — | Worker turn 执行耗时 |
| `hotplex.lease.renew_failure` | Counter | — | Owner lease 续约失败 |
| `hotplex.lease.expired_recovery` | Counter | — | Lease 过期恢复（runtime 置 unknown + fence） |
| `hotplex.repair.attempts` | Counter | — | 终态修复尝试 |
| `hotplex.repair.success` | Counter | — | 终态修复成功 |
| `hotplex.repair.timeout` | Counter | — | 终态修复超时（超过 MaxLifetime 放弃） |
| `hotplex.repair.dropped` | Counter | — | 终态修复入队失败（回退 lease recovery） |

### 2.10 Turn TTFT 指标

Gateway 收到输入到首个可见 Worker 输出的耗时分段。TTFT 仅使用 Gateway 侧时间戳；
浏览器绘制时间属于独立客户端遥测，不能与本指标混合。

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `hotplex.turn.ttft` | Histogram | `worker_type`, `first_output`（reasoning/text） | 输入到首个可见输出的耗时 |
| `hotplex.turn.first_text_latency` | Histogram | `worker_type` | 输入到首个文本 delta 的耗时 |
| `hotplex.turn.stage_duration` | Histogram | `worker_type`, `stage`（admission/dispatch/first_output） | 三阶段耗时拆分 |
| `hotplex.turn.without_output` | Counter | `worker_type`, `terminal_status` | 无可见输出即终止的 turn |
| `hotplex.worker.empty_success_total` | Counter | `worker_type`, `platform` | 成功但无显示内容和工具调用的 turn（Turn-Integrity） |

### 2.11 Forwarder & Turn-Integrity 诊断指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `hotplex.gateway.forwarder.panics` | Counter | `worker_type` | Worker 事件转发 goroutine 已恢复的 panic |
| `hotplex.gateway.stale_forwarder_event_total` | Counter | — | `/reset` 后旧 forwarder 观察到的事件（应永不为零） |
| `hotplex.worker.assistant_snapshot_drift_total` | Counter | — | Full snapshot 非前缀漂移被重新下发 |
| `hotplex.messaging.platform_terminal_fallback_total` | Counter | — | 平台因空内容发出合成终态回退 |

---



## 3. OpenTelemetry 分布式追踪

### 架构

HotPlex 使用统一的 OTel SDK 进行追踪初始化，支持：

- **OTLP gRPC 导出**：直接发送到 OTel Collector（gzip 压缩）
- **W3C TraceContext + Baggage 传播**：标准跨服务链路格式
- **trace_id 注入 AEP**：自动写入 Envelope Metadata，实现消息链路关联
- **ParentBased 采样**：根 span 按 `SampleRate`（默认 10%）采样，子 span 跟随父决策

### 配置

通过环境变量控制，无需修改代码：

```bash
# OTel Collector gRPC endpoint（设置即激活追踪）
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317

# 显式禁用（默认禁用）
OTEL_SDK_DISABLED=true

# 服务名（可选，默认 hotplex-gateway）
OTEL_SERVICE_NAME=hotplex-gateway

# 追踪采样率（可选，默认 0.1 即 10%）
# 仅影响根 span，子 span 跟随父决策
```

### Span 命名规范

| Span 名称 | 触发时机 |
|-----------|---------|
| `aep.init` | WS 握手初始化 |
| `aep.input` | 用户输入 |
| `aep.message.delta` | 流式输出片段 |
| `aep.done` | Turn 完成 |
| `aep.error` | 错误事件 |

### 上下文传播

- Span 的 `trace_id` 和 `span_id` 由 `Hub.SendToSession` 注入 AEP Envelope 的 `metadata` 字段（#850：`span_id` 使下游能把事件关联到产出它的精确 span，而不止于 trace）
- HTTP 端点通过 `otelhttp` 中间件自动注入/提取 W3C TraceContext
- Gateway ↔ Worker 间通过 AEP Metadata 传递 `trace_id`
- 所有语义键（`trace_id`、`span_id`、`agent_id`、`user_id`、`workspace_id`、`session_id`、`execution_id` 等）统一在 `internal/observability/keys.go` 定义，禁止散落字面量。高基数键（`agent_id`/`user_id`/`workspace_id`/`execution_id`/`session_id`）仅作 span 属性/slog 字段/AEP 元数据，严禁用作 metric label（防止 label 集爆炸）

### 采样策略

- **ParentBased(TraceIDRatioBased(0.1))**：根 span 10% 采样，子 span 跟随父决策
- **ERROR trace**：建议在 Collector 层通过 Tail Sampling 100% 保留
- **高延迟 trace**：建议通过 Collector 层按 duration 过滤保留

---

## 4. 健康检查端点

所有端点位于 Admin API（默认 `localhost:9999`）。

| 端点 | 认证 | 用途 | 适用场景 |
|------|------|------|----------|
| `GET /admin/health` | 无需认证 | Gateway 整体状态（含 DB、Workers） | 负载均衡探针 |
| `GET /admin/health/workers` | `health:read` | 按 Worker 类型主动探活 | 运维排障 |
| `GET /admin/health/ready` | 无需认证 | 就绪检查 | K8s readinessProbe / Docker HEALTHCHECK |

### 使用示例

```bash
# 负载均衡健康检查
curl http://localhost:9999/admin/health

# Worker 状态探查（需 Token）
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:9999/admin/health/workers

# K8s readinessProbe / Docker HEALTHCHECK
curl -sf http://localhost:9999/admin/health/ready
```

### Docker HEALTHCHECK 配置

Dockerfile 已内置：

```
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:9999/admin/health/ready || exit 1
```

---

## 5. Grafana Dashboard 建议

### 核心面板

| 面板 | PromQL | 类型 | 用途 |
|------|--------|------|------|
| Session 创建速率 | `rate(hotplex_session_created_total[5m])` | Stat | 容量规划 |
| WS 连接数 | `hotplex_gateway_connections` | Time series | 连接趋势 |
| Worker 崩溃率 | `rate(hotplex_worker_crashes_total[5m])` | Time series | 稳定性 |
| Worker 执行 P99 | `histogram_quantile(0.99, rate(hotplex_worker_execution_duration_bucket[5m]))` | Time series | 性能 |
| TTFT P95 | `histogram_quantile(0.95, sum by (le, worker_type) (rate(hotplex_turn_ttft_bucket[15m])))` | Time series | 首输出延迟 |
| TTFT 阶段拆分 | `histogram_quantile(0.95, sum by (le, stage) (rate(hotplex_turn_stage_duration_bucket[15m])))` | Time series | 延迟归因 |
| 投递成功率 | `sum(rate(hotplex_execution_delivery_outcome_total{delivery_status="delivered"}[5m])) / sum(rate(hotplex_execution_delivery_outcome_total[5m]))` | Stat | 输入可靠性 |
| Delta 背压丢弃 | `rate(hotplex_gateway_deltas_dropped_total[5m])` | Time series | 流量压力 |
| Cron 错误率 | `rate(hotplex_cron_errors_total[5m])` | Time series | 定时任务健康 |
| 错误分类 | `hotplex_gateway_errors_total` | Stacked bar | 错误归因 |
| 重复 WS 连接拒绝 | `rate(hotplex_gateway_webchat_duplicate_connection_rejected_total[5m])` | Time series | 发现同一 session 的并发连接或重连竞争 |
| 重试耗尽 | `rate(hotplex_retry_exhaustion_total[5m])` | Time series | LLM 稳定性 |

### 布局建议

1. **顶栏**：Session 创建速率 / WS 连接 / LLM 重试耗尽 / Uptime（4 个 Stat 面板）
2. **中间行**：Session 生命周期 + Worker 执行耗时趋势
3. **底部行**：错误分类 + 背压指标 + Cron 健康

---

## 6. 告警最佳实践

### 原则

- **症状告警**（Symptom-based）：告警用户可见的故障，而非根因指标
- **持续阈值**：连续 5 分钟超过阈值才触发，避免瞬时毛刺
- **分级响应**：P0（立即介入）→ P1（当日处理）→ P2（纳入迭代）

### 推荐告警规则

| 告警名 | PromQL | 阈值 | 级别 | 说明 |
|--------|--------|------|------|------|
| HighWorkerCrashRate | `rate(hotplex_worker_crashes_total[5m]) / rate(hotplex_worker_starts_total[5m])` | > 1% | P0 | Worker 崩溃率 |
| HighSessionFailureRate | `rate(hotplex_session_terminated_total{reason=~"crash\|zombie"}[5m])` | > 0 | P0 | 异常终止 |
| HighDeltaDropRate | `rate(hotplex_gateway_deltas_dropped_total[5m])` | > 10/s | P1 | 严重背压 |
| HighWorkerLatencyP99 | `histogram_quantile(0.99, rate(hotplex_worker_execution_duration_bucket[5m]))` | > 300s | P1 | Worker 执行卡顿 |
| CronJobFailures | `rate(hotplex_cron_errors_total[10m])` | > 0 | P2 | 定时任务异常 |
| PoolDoubleRelease | `increase(hotplex_pool_release_errors_total[1h])` | > 0 | P2 | 代码 Bug 信号 |
| LLMRetryExhaustion | `rate(hotplex_retry_exhaustion_total[5m])` | > 0 | P1 | LLM 调用不可用 |
| RepeatedSessionConnectionConflict | `rate(hotplex_gateway_webchat_duplicate_connection_rejected_total[10m])` | 持续 > 0 | P2 | 客户端并发重连或连接切换未排空 |
| HighDeliveryFailureRate | `rate(hotplex_execution_delivery_outcome_total{delivery_status!="delivered"}[5m]) / rate(hotplex_execution_delivery_outcome_total[5m])` | > 5% | P1 | Worker 投递失败率过高 |
| HighEmptySuccessTurn | `rate(hotplex_worker_empty_success_total_total[5m]) / rate(hotplex_worker_starts_total[5m])` | > 1% | P2 | 空 success turn 占比异常 |
| HighLeaseRenewFailure | `rate(hotplex_lease_renew_failure_total[5m])` | 持续 > 0 | P1 | Owner lease 续约持续失败 |
| HighTTFTP99 | `histogram_quantile(0.99, sum by (le) (rate(hotplex_turn_ttft_bucket[15m])))` | > 30s | P1 | TTFT P99 超过阈值 |

### SLO 参考

| SLO | 指标 | 目标 |
|-----|------|------|
| Session 创建成功率 | `session.start.attempts` vs `session.start.errors` | >= 99.5% |
| Worker 可用性 | `1 - crashes/starts` | >= 99% |
| Worker 执行 P99 | `worker.execution.duration` | < 300s |
| 输入投递成功率 | `execution.delivery_outcome{delivery_status="delivered"}` | >= 99% |
| Lease 续约成功率 | `lease.renew_failure` 持续为 0 | == 100% |
| TTFT P95 | `turn.ttft` P95 | < 10s |

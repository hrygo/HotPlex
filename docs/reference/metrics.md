---
title: "Metrics 参考"
description: "HotPlex Gateway 暴露的所有 OTel 指标完整参考"
---

# Metrics 参考

> 所有 OTel 指标的名称、类型、标签和使用场景参考。

## 概述

HotPlex Gateway 通过 `internal/observability/` 包使用 OTel Meter API 注册指标。所有指标前缀为 `hotplex.`（点分隔符），通过 OTel Prometheus Exporter 以标准 Prometheus 格式暴露。应用代码零直接依赖 `prometheus/client_golang`。

## 采集端点

指标通过 Admin API 暴露：

```
GET http://localhost:9999/admin/metrics
```

Prometheus scrape 配置示例：

```yaml
scrape_configs:
  - job_name: 'hotplex-gateway'
    static_configs:
      - targets: ['localhost:9999']
    metrics_path: '/admin/metrics'
    scrape_interval: 15s
```

## Session 指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex.session.created` | Counter | 累计创建 Session 数，label: `worker_type` |
| `hotplex.session.terminated` | Counter | 终止 Session 数，label: `reason` |
| `hotplex.session.deleted` | Counter | GC 物理删除 Session 数 |
| `hotplex.session.start.attempts` | Counter | Session 启动尝试次数，label: `worker_type` |
| `hotplex.session.start.errors` | Counter | Session 启动错误数 |
| `hotplex.session.start.duration` | Histogram | Session 启动耗时 |

### 终止原因标签

| reason | 说明 |
|--------|------|
| `idle_timeout` | 空闲超时 |
| `max_lifetime` | 最大生命周期 |
| `client_kill` | 客户端主动终止 |
| `admin_kill` | Admin API 终止 |
| `zombie` | Zombie 检测（无响应） |
| `crash` | Worker 进程崩溃 |

### 常用查询

```promql
# Session 创建速率
rate(hotplex_session_created_total[5m])

# 异常终止率
rate(hotplex_session_terminated_total{reason=~"crash|zombie"}[5m])

# Session 启动 P99 延迟
histogram_quantile(0.99, rate(hotplex_session_start_duration_bucket[5m]))
```

## Worker 指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex.worker.starts` | Counter | Worker 启动次数，label: `worker_type`, `result` |
| `hotplex.worker.execution.duration` | Histogram | Worker 执行时长，label: `worker_type` |
| `hotplex.worker.crashes` | Counter | Worker 崩溃次数，label: `worker_type`, `exit_code` |
| `hotplex.worker.memory.bytes` | ObservableGauge | Worker 估算内存，label: `worker_type` |
| `hotplex.worker.creation.duration` | Histogram | Worker 进程创建耗时 |

### 常用查询

```promql
# Worker 可用性 SLO
1 - (rate(hotplex_worker_crashes_total[5m]) / rate(hotplex_worker_starts_total[5m]))

# P95 执行时长
histogram_quantile(0.95, rate(hotplex_worker_execution_duration_bucket[5m]))

# Worker 内存总量
sum by (worker_type) (hotplex_worker_memory_bytes)
```

## Turn TTFT 指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex.turn.ttft` | Histogram | Gateway 收到输入至首个可见 Worker 输出的耗时，label: `worker_type`, `first_output`（`reasoning` / `text`） |
| `hotplex.turn.first_text_latency` | Histogram | Gateway 收到输入至首个文本 delta 的耗时，label: `worker_type` |
| `hotplex.turn.stage_duration` | Histogram | admission、dispatch、first_output 三个阶段耗时，label: `worker_type`, `stage` |
| `hotplex.turn.without_output` | Counter | 未产生可见输出即终止的 turn，label: `worker_type`, `terminal_status` |

TTFT 仅使用 Gateway 侧时间戳；浏览器绘制时间属于独立客户端遥测，不能与本指标混合。所有标签均为有限枚举，严禁添加 session、execution、user、workspace、提示词或原始错误等高基数字段。Worker 尚未绑定即终止的 turn 使用固定的 `worker_type="unknown"`。

阶段边界依次为 Gateway 收到输入、持久化接受完成、`Worker.Input` 成功（包括 Worker 的 `turn/start` 接受）以及首个可见输出；`first_output` 仅统计最后一段至首输出的耗时。

### 常用查询与评审阈值

```promql
# 按 Worker 类型查看 p50 / p95 / p99 TTFT
histogram_quantile(0.50, sum by (le, worker_type) (rate(hotplex_turn_ttft_bucket[15m])))
histogram_quantile(0.95, sum by (le, worker_type) (rate(hotplex_turn_ttft_bucket[15m])))
histogram_quantile(0.99, sum by (le, worker_type) (rate(hotplex_turn_ttft_bucket[15m])))

# p95 的首个输出前阶段拆分
histogram_quantile(0.95, sum by (le, stage, worker_type) (
  rate(hotplex_turn_stage_duration_bucket{stage=~"admission|dispatch|first_output"}[15m])
))

# 无输出终止占比
sum(rate(hotplex_turn_without_output_total[15m])) by (terminal_status, worker_type)
/
sum(rate(hotplex_turn_ttft_count[15m])) by (worker_type)
```

以同一 Worker 类型、相同流量窗口的基线为准：p95 TTFT 连续 30 分钟高于基线 20%，或 p99 连续 15 分钟超过 30 秒时必须评审；先用阶段指标定位 admission、dispatch 或 provider/Worker 首输出，再决定是否投入冷路径优化。

## Gateway 指标

### 连接与消息

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex.gateway.connections` | ObservableGauge | 当前 WebSocket 连接数 |
| `hotplex.gateway.webchat.session_owner_connections` | ObservableGauge | 当前拥有 WebChat WebSocket owner 的 Session 数 |
| `hotplex.gateway.messages` | Counter | 消息总数，label: `direction`, `event_type` |
| `hotplex.gateway.events` | Counter | 转发事件总数，label: `event_type`, `direction` |
| `hotplex.gateway.init.handshake.duration` | Histogram | WS 握手耗时 |

### 背压指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex.gateway.deltas.dropped` | Counter | 因背压丢弃的 `message.delta` 事件数 |
| `hotplex.gateway.platform.dropped` | Counter | 平台连接缓冲区满时丢弃的事件数，label: `event_type` |
| `hotplex.gateway.no_subscribers.dropped` | Counter | 无订阅者时丢弃的事件数，label: `event_type` |

### Delta 聚合指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex.gateway.delta.coalesced` | Counter | 被 coalescer 合并的 delta 事件数 |
| `hotplex.gateway.delta.flush` | Counter | 合并后刷新到平台连接的次数 |

### 错误指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex.gateway.errors` | Counter | 错误总数，label: `error_code` |
| `hotplex.gateway.webchat.duplicate_connection_rejected` | Counter | 因已有 WebChat owner 被拒绝的 init 数 |
| `hotplex.gateway.webchat.non_owner_ingress_rejected` | Counter | 非 owner 的敏感入站事件拒绝数 |
| `hotplex.gateway.webchat.owner_release_not_current` | Counter | 非当前 owner 尝试释放 owner 的次数 |

### 常用查询

```promql
# 每秒消息速率
rate(hotplex_gateway_messages_total[5m])

# Delta 丢弃率
rate(hotplex_gateway_deltas_dropped_total[5m])

# 按事件类型的消息分布
sum by (event_type) (rate(hotplex_gateway_messages_total{direction="outgoing"}[5m]))
```

## Pool 配额指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex.pool.acquire` | Counter | 配额获取尝试次数，label: `result` |
| `hotplex.pool.release.errors` | Counter | 双重释放错误数（表示 bug） |
| `hotplex.pool.utilization` | ObservableGauge (float) | 资源池利用率（0-1），活跃会话数 / `max_size` |
| `hotplex.pool.active_sessions` | ObservableGauge | 活跃 worker 会话数（全局，含 platform/cron） |
| `hotplex.pool.distinct_users` | ObservableGauge | 至少有一个活跃会话的去重用户数 |
| `hotplex.pool.distinct_workspaces` | ObservableGauge | 至少有一个活跃会话的去重 WebChat workspace 数（不含 platform 会话，spec ⑤） |
| `hotplex.pool.memory_reserved_bytes` | ObservableGauge | per-user 内存配额下预留的字节数（仅 `max_memory_per_user` 设置时累加；512MB/worker 估算值，非实际 RSS，spec ⑤） |

### 获取结果标签

| result | 说明 |
|--------|------|
| `success` | 成功获取 |
| `pool_exhausted` | 全局 Worker 数已满 |
| `user_quota_exceeded` | 单用户 Session 数已满 |
| `workspace_quota_exceeded` | 单 workspace Session 数已满（`max_per_workspace`，spec ①） |
| `memory_exceeded` | 单用户内存配额超限（`max_memory_per_user`） |

### 常用查询

```promql
# 配额拒绝率
rate(hotplex_pool_acquire_total{result!="success"}[5m])

# 双重释放检测
increase(hotplex_pool_release_errors_total[1h])
```

## Cron 指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex.cron.fires` | Counter | 任务触发次数，label: `job_name` |
| `hotplex.cron.errors` | Counter | 执行错误数，label: `job_name`, `error_type` |
| `hotplex.cron.duration` | Histogram | 执行时长，label: `job_name` |
| `hotplex.cron.attached` | Counter | Session-attached cron 投递次数 |
| `hotplex.cron.delivery.result` | Counter | 投递结果，label: `status`（success / exhausted / permanent） |

### 常用查询

```promql
# Cron 成功率
rate(hotplex_cron_fires_total[5m]) - rate(hotplex_cron_errors_total[5m])

# 按任务名的平均执行时长
rate(hotplex_cron_duration_sum[5m]) / rate(hotplex_cron_duration_count[5m])
```

## Streaming Card 指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex.streaming.card.rotations` | Counter | Streaming Card TTL 触发的旋转次数 |
| `hotplex.streaming.card.rotation_failures` | Counter | 旋转失败数，label: `phase` |
| `hotplex.streaming.card.flush_fallbacks` | Counter | CardKit 降级到 IM Patch 的次数 |

## ACP 指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex.acp.prompt_tokens` | Counter | ACP Worker 处理的 prompt token 数 |
| `hotplex.acp.tool_calls` | Counter | ACP Worker 工具调用次数 |
| `hotplex.acp.permission_requests` | Counter | ACP 权限请求次数 |
| `hotplex.acp.handshake.duration` | Histogram | ACP JSON-RPC 握手耗时 |

## LLM 重试指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex.retry.attempts` | Counter | LLM 重试尝试次数 |
| `hotplex.retry.exhaustion` | Counter | 重试耗尽（最终失败）次数 |

## SLO 参考

| SLO | 指标 | 目标 |
|-----|------|------|
| Session 创建成功率 | `session.start.attempts` vs `session.start.errors` | >= 99.5% |
| P99 执行延迟 | `worker.execution.duration` P99 | < 5s |
| Worker 可用性 | `1 - crashes/starts` | >= 99% |
| Pool 拒绝率 | `pool.acquire{result!="success"}` | < 0.1% |

## 参考

- [可观测性配置](../guides/enterprise/observability.md)：日志、追踪和指标配置
- [资源限制](../guides/enterprise/resource-limits.md)：资源配额详解

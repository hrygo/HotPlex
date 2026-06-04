---
type: spec
tags:
  - project/HotPlex
  - observability
  - opentelemetry
  - prometheus
  - metrics
  - tracing
date: 2026-06-04
status: proposed
progress: 0
version: v1.0
---

# HotPlex Observability Spec — 统一可观测性体系

> 文档版本: v1.0-proposed
> 维护者: HotPlex Engineering
> 最后更新: 2026-06-04

本规范定义 HotPlex Gateway 的**统一可观测性架构**。由于当前模块无任何生产用户，采用**零兼容包袱**的整洁架构设计。

**设计原则**：
- **单包集中**：`internal/observability/` 统一管理所有可观测性，消除 `internal/metrics/` + `internal/tracing/` 双包
- **OTel 纯粹**：应用层零 `prometheus/client_golang` 依赖。所有指标通过 OTel Meter API 定义，OTel Prometheus exporter 负责 `/admin/metrics` 兼容
- **语义约定优先**：HTTP/RPC 等标准场景使用 OTel 语义约定；业务自定义指标使用 `hotplex.<domain>.<metric>` 层级命名
- **生产采样默认正确**：SDK 端 `ParentBased(TraceIDRatioBased(0.1))`，Collector 端 tail sampling

---

## 目录

1. [架构总览](#1-架构总览)
2. [SDK 初始化规范](#2-sdk-初始化规范)
3. [Metrics 指标体系](#3-metrics-指标体系)
4. [Tracing 规范](#4-tracing-规范)
5. [日志-Trace 关联](#5-日志-trace-关联)
6. [HTTP 暴露端点](#6-http-暴露端点)
7. [告警规则](#7-告警规则)
8. [SLO 定义](#8-slo-定义)
9. [Grafana 仪表板](#9-grafana-仪表板)
10. [OTel Collector 部署](#10-otel-collector-部署)
11. [实施路线图](#11-实施路线图)
12. [验收标准](#12-验收标准)

---

## 1. 架构总览

```
┌──────────────────────────────────────────────────────────────────────┐
│                         HotPlex Gateway                               │
│                                                                       │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │              internal/observability (single package)            │  │
│  │                                                                  │  │
│  │  Init(ctx, cfg) → (TracerProvider, MeterProvider, shutdown)     │  │
│  │                                                                  │  │
│  │  ┌──────────────┐  ┌──────────────────┐                        │  │
│  │  │ TracerProvider│  │ MeterProvider     │                        │  │
│  │  │ Sampler:      │  │ Readers:          │                        │  │
│  │  │ ParentBased   │  │  PeriodicReader   │                        │  │
│  │  │ (0.1 ratio)   │  │  + Prometheus     │                        │  │
│  │  └──────┬───────┘  └────────┬──────────┘                        │  │
│  │         │                   │                                     │  │
│  │  ┌──────┴───────────────────┴──────────────────────────────────┐ │  │
│  │  │         OTLP gRPC Exporter (shared connection)               │ │  │
│  │  │         compression: gzip  |  retry: enabled                │ │  │
│  │  └────────────────────────────┬─────────────────────────────────┘ │  │
│  └───────────────────────────────┼──────────────────────────────────┘  │
│                                  │                                     │
│  ┌───────────────────────────────┼──────────────────────────────────┐  │
│  │  Signal Sources (domain packages import observability)            │  │
│  │                                                                   │  │
│  │  otelhttp (auto) ────────────┤  HTTP RED metrics + spans          │  │
│  │  runtime   (auto) ───────────┤  Go goroutine/memory/GC            │  │
│  │  gateway   (manual span) ────┤  conn.recv, hub.broadcast          │  │
│  │  session   (manual meter) ───┤  hotplex.session.*                 │  │
│  │  worker    (manual meter) ───┤  hotplex.worker.*                  │  │
│  │  cron      (manual meter) ───┤  hotplex.cron.*                    │  │
│  │  brain     (manual meter) ───┤  hotplex.brain.*                   │  │
│  └───────────────────────────────┼──────────────────────────────────┘  │
│                                  │                                     │
│  ┌───────────────────────────────▼──────────────────────────────────┐  │
│  │  GET /admin/metrics                                              │  │
│  │  promhttp.Handler() ← OTel Prometheus exporter                   │  │
│  └──────────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────────┘
                                   │
                                   │ OTLP gRPC :4317
                                   ▼
┌──────────────────────────────────────────────────────────────────────┐
│                       OTel Collector (optional)                       │
│                                                                       │
│  receivers: otlp                                                      │
│  processors: memory_limiter → batch → tail_sampling                  │
│  exporters: otlp (Tempo) + prometheusremotewrite (Mimir)             │
└──────────────────────────────────────────────────────────────────────┘
```

---

## 2. SDK 初始化规范

### 2.1 包结构

```
internal/observability/           ← 单包，替代 internal/metrics/ + internal/tracing/
  observability.go                Init(), Shutdown(), Meter, Tracer 全局访问器
  config.go                       Config struct + env 解析
  attributes.go                   共享 attribute keys
```

### 2.2 统一初始化入口

```go
// internal/observability/observability.go

func Init(ctx context.Context, log *slog.Logger, cfg Config) (shutdown func(context.Context) error) {
    // sync.Once guarded
    // returns errors.Join(tp.Shutdown, mp.Shutdown)
}

// Meter returns the global OTel Meter for creating instruments.
func Meter() metric.Meter

// Tracer returns the global OTel Tracer for creating spans.
func Tracer() trace.Tracer
```

### 2.2 Resource Attributes（强制）

| 属性 | 值来源 | 说明 |
|------|--------|------|
| `service.name` | `cfg.ServiceName` | 固定 `hotplex-gateway`，跨实例一致 |
| `service.version` | `cfg.ServiceVersion` | 编译时嵌入的 `VERSION` |
| `service.namespace` | 硬编码 | `hotplex` |
| `service.instance.id` | `os.Hostname()` 或 env `HOSTNAME` | K8s Pod 名 |
| `deployment.environment` | env `DEPLOY_ENV` | `production` / `staging` / `development` |
| `host.name` | `resource.WithHost()` | 自动检测 |
| `process.pid` | `resource.WithProcess()` | 自动检测 |

### 2.3 TracerProvider

```go
tp := sdktrace.NewTracerProvider(
    sdktrace.WithBatcher(traceExp,
        sdktrace.WithMaxExportBatchSize(512),
        sdktrace.WithBatchTimeout(5*time.Second),
    ),
    sdktrace.WithResource(res),
    sdktrace.WithSampler(sdktrace.ParentBased(
        sdktrace.TraceIDRatioBased(cfg.SampleRate),
    )),
)
```

### 2.4 MeterProvider

```go
// Prometheus exporter — serves /admin/metrics in Prometheus format
promExporter, _ := otelprom.New(
    otelprom.WithoutScopeInfo(),
    otelprom.WithoutTargetInfo(),
)

// PeriodicReader — pushes metrics via OTLP
otlpReader := sdkmetric.NewPeriodicReader(metricExp,
    sdkmetric.WithInterval(30*time.Second),
)

mp := sdkmetric.NewMeterProvider(
    sdkmetric.WithReader(promExporter),
    sdkmetric.WithReader(otlpReader),
    sdkmetric.WithResource(res),
    sdkmetric.WithCardinalityLimit(5000),
)
```

> 注：`otelprom.New()` 内部依赖 `prometheus/client_golang`，但**应用代码完全不 import client_golang**。这是隔离边界。

### 2.5 优雅关闭

```go
// shutdown 顺序：先 trace 后 metric
shutdown := func(ctx context.Context) error {
    return errors.Join(
        tp.Shutdown(ctx),
        mp.Shutdown(ctx),
    )
}
```

---

## 3. Metrics 指标体系

### 3.1 命名约定

| 来源 | 命名规范 | 示例 |
|------|---------|------|
| OTel 语义约定 (HTTP) | `http.server.request.duration` | otelhttp 自动生成 |
| OTel 语义约定 (Runtime) | `go.goroutine.count` | runtime instrumentation 自动生成 |
| HotPlex 自定义 | `hotplex.<domain>.<metric>` | `hotplex.session.active` |
| Brain LLM | `hotplex.brain.<metric>` | `hotplex.brain.tokens.input` |

> Prometheus 端点自动转换：`.` → `_`，添加 `_total` 后缀。OTel 原生名称通过 `otel_metric_name` 属性保留。

### 3.2 完整指标目录

#### 3.2.1 HTTP Server Metrics（auto — otelhttp）

| OTel 指标 | 类型 | Prometheus 名称 | 说明 |
|-----------|------|-----------------|------|
| `http.server.request.duration` | Histogram | `http_server_request_duration_seconds` | 请求延迟，标签：`http.method`, `http.route`, `http.status_code` |
| `http.server.active_requests` | UpDownCounter | `http_server_active_requests` | 并发活跃请求数 |
| `http.server.request.body.size` | Histogram | `http_server_request_body_size_bytes` | 请求体大小 |
| `http.server.response.body.size` | Histogram | `http_server_response_body_size_bytes` | 响应体大小 |

#### 3.2.2 Go Runtime Metrics（auto — runtime instrumentation）

| OTel 指标 | 类型 | Prometheus 名称 | 说明 |
|-----------|------|-----------------|------|
| `go.goroutine.count` | UpDownCounter | `go_goroutine_count` | 当前 goroutine 数 |
| `go.memory.used` | UpDownCounter | `go_memory_used_bytes` | 内存用量，按 `go.memory.type` 区分 |
| `go.memory.gc.pause` | Histogram | `go_memory_gc_pause_seconds` | GC 暂停时间 |
| `go.cpu.time` | Counter | `go_cpu_time_seconds_total` | 累计 CPU 时间 |
| `process.runtime.go.gc.count` | Counter | `process_runtime_go_gc_count_total` | GC 次数 |

#### 3.2.3 Session Metrics

> **Meter**: `hotplex-gateway` | **Instrument Kind**: listed per row

| OTel 指标 | 类型 | 标签 | 说明 |
|-----------|------|------|------|
| `hotplex.session.active` | ObservableGauge | `state` (created/running/idle) | 当前活跃 session 数 |
| `hotplex.session.created` | Counter | `worker_type` | 累计创建数 |
| `hotplex.session.terminated` | Counter | `reason` (idle_timeout/max_lifetime/client_kill/admin_kill/zombie/crash) | 累计终止数 |
| `hotplex.session.deleted` | Counter | — | GC 清理数 |
| `hotplex.session.start.attempts` | Counter | `worker_type` | 启动尝试数 |
| `hotplex.session.start.errors` | Counter | `worker_type`, `error_type` | 启动失败数 |
| `hotplex.session.start.duration` | Histogram | `worker_type` | 启动耗时（seconds），buckets: 0.5,1,2,5,10,30,60 |

#### 3.2.4 Worker Metrics

| OTel 指标 | 类型 | 标签 | 说明 |
|-----------|------|------|------|
| `hotplex.worker.running` | ObservableGauge | `worker_type` | 当前运行 Worker 数 |
| `hotplex.worker.starts` | Counter | `worker_type`, `result` (success/failed) | 启动次数 |
| `hotplex.worker.execution.duration` | Histogram | `worker_type` | 执行耗时（seconds），buckets: 1,5,15,30,60,120,300,600,1800 |
| `hotplex.worker.crashes` | Counter | `worker_type`, `exit_code` | 崩溃次数 |
| `hotplex.worker.memory.bytes` | ObservableGauge | `worker_type` | 预估内存（RLIMIT_AS） |
| `hotplex.worker.creation.duration` | Histogram | `worker_type` | 创建耗时（seconds），buckets: 0.5,1,2,5,10,30,60 |

#### 3.2.5 Gateway Metrics

| OTel 指标 | 类型 | 标签 | 说明 |
|-----------|------|------|------|
| `hotplex.gateway.connections` | ObservableGauge | — | 当前 WS 连接数 |
| `hotplex.gateway.messages` | Counter | `direction` (incoming/outgoing), `event_type` | 消息收发数 |
| `hotplex.gateway.events` | Counter | `event_type`, `direction` (s2c/c2s) | AEP 事件透传数 |
| `hotplex.gateway.deltas.dropped` | Counter | — | 背压丢弃 delta 数 |
| `hotplex.gateway.platform.dropped` | Counter | `event_type` | 平台缓冲丢弃数 |
| `hotplex.gateway.no_subscribers.dropped` | Counter | `event_type` | 无订阅者丢弃数 |
| `hotplex.gateway.delta.coalesced` | Counter | — | Delta 合并数 |
| `hotplex.gateway.delta.flush` | Counter | — | Delta 刷新数 |
| `hotplex.gateway.errors` | Counter | `error_code` | 错误分类计数 |
| `hotplex.gateway.init.handshake.duration` | Histogram | — | WS init 耗时，buckets: 0.1,0.25,0.5,1,2,5 |

#### 3.2.6 Pool Metrics

| OTel 指标 | 类型 | 标签 | 说明 |
|-----------|------|------|------|
| `hotplex.pool.acquire` | Counter | `result` (success/pool_exhausted/user_quota_exceeded/memory_exceeded/toctou_retry) | 配额获取 |
| `hotplex.pool.release.errors` | Counter | — | 双重释放错误（Bug 信号） |
| `hotplex.pool.utilization` | ObservableGauge | — | Pool 利用率（0-1） |

#### 3.2.7 Cron Metrics

| OTel 指标 | 类型 | 标签 | 说明 |
|-----------|------|------|------|
| `hotplex.cron.fires` | Counter | `job_name` | 触发次数 |
| `hotplex.cron.errors` | Counter | `job_name`, `error_type` | 错误次数 |
| `hotplex.cron.duration` | Histogram | `job_name` | 执行耗时，buckets: 1,5,15,30,60,120,300,600,1800 |
| `hotplex.cron.attached` | Counter | `result` (success/session_not_found/resume_failed/inject_failed/no_router) | 回调结果 |

#### 3.2.8 Retry Metrics

| OTel 指标 | 类型 | 标签 | 说明 |
|-----------|------|------|------|
| `hotplex.retry.attempts` | Counter | `reason` | 重试次数 |
| `hotplex.retry.exhaustion` | Counter | — | 重试耗尽次数 |

#### 3.2.9 ACP Worker Metrics

| OTel 指标 | 类型 | 标签 | 说明 |
|-----------|------|------|------|
| `hotplex.acp.prompt_tokens` | Counter | `type` (input/output/cached_read/cached_write/thought) | Token 用量 |
| `hotplex.acp.tool_calls` | Counter | `kind` (read/edit/delete/execute/search/other) | Tool 调用数 |
| `hotplex.acp.permission_requests` | Counter | `outcome` (approved/denied/timeout) | 权限请求 |
| `hotplex.acp.handshake.duration` | Histogram | — | 握手耗时，buckets: 0.1,0.25,0.5,1,2,5,10 |

#### 3.2.10 Streaming Card Metrics

| OTel 指标 | 类型 | 标签 | 说明 |
|-----------|------|------|------|
| `hotplex.streaming.card.rotations` | Counter | — | TTL 轮转次数 |
| `hotplex.streaming.card.rotation_failures` | Counter | `phase` (close_old/ensure_card) | 轮转失败 |
| `hotplex.streaming.card.flush_fallbacks` | Counter | — | CardKit→IM Patch 降级 |

#### 3.2.11 Brain LLM Metrics

| OTel 指标 | 类型 | 标签 | 说明 |
|-----------|------|------|------|
| `hotplex.brain.request.latency` | Histogram | `model`, `scenario` | LLM 延迟（ms），buckets: 10,50,100,250,500,1000,2500,5000,10000 |
| `hotplex.brain.tokens.input` | Counter | `model`, `scenario` | 输入 token 数 |
| `hotplex.brain.tokens.output` | Counter | `model`, `scenario` | 输出 token 数 |
| `hotplex.brain.cost` | Counter | `model`, `scenario` | 成本（USD） |
| `hotplex.brain.errors` | Counter | `model`, `scenario` | 错误次数 |

> 注：`RecordRoutingDecision` 在 Brain 精简后（PR #638）无调用方，保留方法但暂不录制指标。

#### 3.2.12 Other Internal Metrics

| OTel 指标 | 类型 | 标签 | 说明 |
|-----------|------|------|------|
| `hotplex.brain.cache.hits` | ObservableGauge | — | LLM 缓存命中 |
| `hotplex.brain.cache.misses` | ObservableGauge | — | LLM 缓存未命中 |
| `hotplex.eventstore.dropped` | ObservableGauge | — | 事件采集丢弃 |

> **总计：约 55 个指标**，全部通过 OTel Meter API 采集，统一 Prometheus 端点暴露。

---

## 4. Tracing 规范

### 4.1 Span 命名

| Span 名称 | 位置 | 属性 | 说明 |
|-----------|------|------|------|
| `HTTP {METHOD} {ROUTE}` | otelhttp（自动） | `http.method`, `http.route`, `http.status_code` | HTTP 请求 span |
| `conn.init` | `gateway/conn.go` | `session_id`, `worker_type` | AEP init 握手 |
| `conn.recv` | `gateway/conn.go` | `session_id`, `event_type`, `seq` | 入站 AEP 事件 |
| `hub.send_to_session` | `gateway/hub.go` | `session_id`, `event_type`, `priority` | 出站事件投递 |
| `hub.broadcast` | `gateway/hub.go` | `session_id`, `event_type`, `seq` | Hub 广播 |
| `session.start` | `gateway/bridge.go` | `session_id`, `worker_type` | Session 启动 |
| `worker.exec` | `gateway/bridge_forward.go` | `session_id`, `worker_type` | Worker 执行 |
| `brain.llm.request` | `internal/brain/` | `model`, `scenario` | LLM 请求 |

### 4.2 Context 传播（强制执行）

- **所有请求处理路径**：使用 `r.Context()`，禁止 `context.Background()`
- **goroutine**：显式传递 `ctx` 参数，不捕获外部 context
- **Span 属性**：使用 `span.IsRecording()` guard 避免非采样 span 的昂贵序列化

### 4.3 trace_id 注入 AEP

```go
// hub.go — routeMessage / SendToSession
if spanCtx := trace.SpanContextFromContext(ctx); spanCtx.IsValid() {
    if env.Metadata == nil {
        env.Metadata = make(map[string]any)
    }
    env.Metadata["trace_id"] = spanCtx.TraceID().String()
}
```

### 4.4 采样策略

| 层级 | 策略 | 配置 |
|------|------|------|
| SDK（head） | `ParentBased(TraceIDRatioBased(0.1))` | 10% 基准采样率；尊重父 span 决策 |
| Collector（tail） | 多策略 OR | errors 100%，latency >5s 100%，其余 5% |

---

## 5. 日志-Trace 关联

### 5.1 slog 注入 trace context

```go
// 自定义 slog.Handler 在每条日志中注入 trace_id 和 span_id
type OtelHandler struct {
    slog.Handler
}

func (h *OtelHandler) Handle(ctx context.Context, r slog.Record) error {
    spanCtx := trace.SpanContextFromContext(ctx)
    if spanCtx.IsValid() {
        r.AddAttrs(
            slog.String("trace_id", spanCtx.TraceID().String()),
            slog.String("span_id", spanCtx.SpanID().String()),
        )
    }
    return h.Handler.Handle(ctx, r)
}
```

### 5.2 使用规范

```go
// 在所有业务日志中传递 ctx
slog.InfoContext(ctx, "session created", "session_id", sid)
// 输出: {"msg":"session created","session_id":"xxx","trace_id":"abc","span_id":"def"}
```

---

## 6. HTTP 暴露端点

### 6.1 端点清单

| 端点 | 方法 | 认证 | 说明 |
|------|------|------|------|
| `GET /admin/metrics` | GET | Bearer Token | Prometheus 兼容指标端点（OTel exporter + promhttp） |
| `GET /health` | GET | 无 | 存活探针 |
| `GET /admin/health` | GET | 无 | 综合健康（DB + Worker + Gateway） |
| `GET /admin/health/ready` | GET | 无 | 就绪探针（K8s readinessProbe） |
| `GET /admin/health/workers` | GET | `health:read` | 各 Worker 健康详情 |
| `GET /admin/stats` | GET | `stats:read` | 运行时统计摘要 |

### 6.2 otelhttp 包装

```go
// routes.go — 所有 HTTP handler 统一包装
handler := otelhttp.NewHandler(mux, "hotplex-gateway",
    otelhttp.WithSpanNameFormatter(func(op string, r *http.Request) string {
        return r.Method + " " + r.URL.Path
    }),
)
```

---

## 7. 告警规则

### 7.1 告警级别定义

| 级别 | 响应时间 | 通道 | 示例 |
|------|---------|------|------|
| **critical** | 5min | PagerDuty / 电话 | Session 创建完全失败、Pool 耗尽、无活跃 Worker |
| **warning** | 30min | Slack / 飞书 | Worker 崩溃率上升、高延迟 P99、GC 压力 |

### 7.2 告警规则清单

```yaml
groups:
  - name: hotplex-session
    rules:
      - alert: SessionCreationFailureRate
        expr: |
          rate(hotplex_session_start_errors_total[5m]) /
          rate(hotplex_session_start_attempts_total[5m]) > 0.01
        for: 5m
        severity: critical

      - alert: SessionAbnormalTermination
        expr: rate(hotplex_session_terminated_total{reason=~"crash|zombie"}[5m]) > 0
        for: 5m
        severity: critical

  - name: hotplex-worker
    rules:
      - alert: HighWorkerCrashRate
        expr: |
          rate(hotplex_worker_crashes_total[5m]) /
          rate(hotplex_worker_starts_total[5m]) > 0.01
        for: 5m
        severity: warning

      - alert: NoActiveWorkers
        expr: sum(hotplex_worker_running) == 0
        for: 1m
        severity: critical

  - name: hotplex-pool
    rules:
      - alert: PoolExhaustion
        expr: hotplex_pool_utilization_ratio > 0.95
        for: 10m
        severity: critical

      - alert: PoolDoubleReleaseBug
        expr: increase(hotplex_pool_release_errors_total[1h]) > 0
        for: 5m
        severity: warning

  - name: hotplex-latency
    rules:
      - alert: HighWorkerLatencyP99
        expr: |
          histogram_quantile(0.99,
            rate(hotplex_worker_execution_duration_seconds_bucket[5m])
          ) > 300
        for: 10m
        severity: warning

      - alert: HighInitLatencyP99
        expr: |
          histogram_quantile(0.99,
            rate(hotplex_gateway_init_handshake_duration_seconds_bucket[5m])
          ) > 5
        for: 5m
        severity: warning

  - name: hotplex-gateway
    rules:
      - alert: HighDeltaDropRate
        expr: rate(hotplex_gateway_deltas_dropped_total[5m]) > 10
        for: 5m
        severity: warning

      - alert: HighErrorRate
        expr: rate(hotplex_gateway_errors_total[5m]) > 1
        for: 5m
        severity: warning

  - name: hotplex-cron
    rules:
      - alert: CronJobFailures
        expr: rate(hotplex_cron_errors_total[10m]) > 0
        for: 10m
        severity: warning

  - name: hotplex-brain
    rules:
      - alert: BrainHighErrorRate
        expr: rate(hotplex_brain_errors_total[5m]) > 0.1
        for: 5m
        severity: warning
```

### 7.3 告警文件位置

`configs/monitoring/alerts.yml` — 与上述规则同步更新。

---

## 8. SLO 定义

### 8.1 SLO 目标

| SLO | SLI | 目标 | 窗口 |
|-----|-----|------|------|
| Session 创建成功率 | `good / valid` | ≥ 99.5% | 30d 滚动 |
| Worker 可用性 | `1 - crashes/(starts+crashes)` | ≥ 99% | 30d 滚动 |
| Worker 执行 P99 延迟 | `histogram_quantile(0.99, ...)` | < 300s | 30d 滚动 |
| Gateway HTTP P99 延迟 | `histogram_quantile(0.99, ...)` | < 1s | 30d 滚动 |

### 8.2 Burn Rate Alerts

| SLO | 短期 burn rate | 长期 burn rate |
|-----|---------------|---------------|
| Session 创建成功率 | 14.4x for 1h | 6x for 6h |
| Worker 可用性 | 14.4x for 1h | 6x for 6h |

### 8.3 SLO 文件位置

`configs/monitoring/slo.yaml` — 与上述定义同步更新。

---

## 9. Grafana 仪表板

### 9.1 面板布局

| 行 | 面板 | 类型 | PromQL |
|----|------|------|--------|
| **Row 1: Overview** | | | |
| | 活跃 Session | Stat | `sum(hotplex_session_active)` |
| | WS 连接 | Stat | `hotplex_gateway_connections` |
| | Pool 利用率 | Gauge | `hotplex_pool_utilization_ratio * 100` |
| | Worker 运行数 | Stat | `sum(hotplex_worker_running)` |
| **Row 2: HTTP** | | | |
| | HTTP 请求速率 | TimeSeries | `rate(hotplex_http_server_request_duration_seconds_count[5m])` |
| | HTTP P99 延迟 | TimeSeries | `histogram_quantile(0.99, rate(hotplex_http_server_request_duration_seconds_bucket[5m]))` |
| | HTTP 错误率 | TimeSeries | `rate(hotplex_http_server_request_duration_seconds_count{http_status_code=~"5.."}[5m])` |
| **Row 3: Session** | | | |
| | Session 按状态 | TimeSeries | `sum by(state)(hotplex_session_active)` |
| | Session 创建率 | TimeSeries | `rate(hotplex_session_created_total[5m])` |
| | Session 终止原因 | StackedBar | `rate(hotplex_session_terminated_total[5m])` |
| **Row 4: Worker** | | | |
| | Worker P99 延迟 | TimeSeries | `histogram_quantile(0.99, rate(hotplex_worker_execution_duration_seconds_bucket[5m]))` |
| | Worker 崩溃率 | TimeSeries | `rate(hotplex_worker_crashes_total[5m])` |
| **Row 5: Gateway** | | | |
| | 消息吞吐 | TimeSeries | `rate(hotplex_gateway_messages_total[5m])` |
| | Delta 丢弃率 | TimeSeries | `rate(hotplex_gateway_deltas_dropped_total[5m])` |
| | 错误分类 | StackedBar | `hotplex_gateway_errors_total` |
| **Row 6: Brain** | | | |
| | LLM 延迟 P99 | TimeSeries | `histogram_quantile(0.99, rate(hotplex_brain_request_latency_milliseconds_bucket[5m]))` |
| | Token 用量 | TimeSeries | `rate(hotplex_brain_tokens_input_total[5m]) + rate(hotplex_brain_tokens_output_total[5m])` |
| **Row 7: Cron** | | | |
| | Cron 触发率 | TimeSeries | `rate(hotplex_cron_fires_total[5m])` |
| | Cron 错误 | TimeSeries | `rate(hotplex_cron_errors_total[5m])` |
| **Row 8: Runtime** | | | |
| | Goroutine 数 | TimeSeries | `go_goroutine_count` |
| | 堆内存 | TimeSeries | `go_memory_used_bytes{go_memory_type="heap"}` |
| | GC 暂停 | TimeSeries | `rate(go_memory_gc_pause_seconds_count[5m])` |

### 9.2 仪表板文件位置

`configs/monitoring/grafana/dashboards/hotplex-gateway.json`

---

## 10. OTel Collector 部署

### 10.1 部署策略

| 规模 | 推荐 | 原因 |
|------|------|------|
| 小规模（<1K events/s，单服务） | **无 Collector** — SDK 直推后端 | 降低运维复杂度 |
| 中规模（1-10K events/s，多服务） | **Gateway Collector** — 2-3 副本 | 集中式 tail sampling、多后端分发 |
| 大规模（>10K events/s，多集群） | **Agent + Gateway 双层** | DaemonSet agent 做过滤 + Gateway 做采样/路由 |

### 10.2 最小生产配置

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318

processors:
  memory_limiter:    # MUST be first
    check_interval: 2s
    limit_percentage: 80
    spike_limit_percentage: 25
  tail_sampling:
    decision_wait: 10s
    num_traces: 100000
    policies:
      - name: errors
        type: status_code
        status_code: {status_codes: [ERROR]}
      - name: slow-traces
        type: latency
        latency: {threshold_ms: 5000}
      - name: baseline
        type: probabilistic
        probabilistic: {sampling_percentage: 5}
  batch:              # MUST be last
    send_batch_size: 8192
    timeout: 10s

exporters:
  otlp:
    endpoint: "${BACKEND_ENDPOINT}:4317"
    compression: gzip
    retry_on_failure:
      enabled: true

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, tail_sampling, batch]
      exporters: [otlp]
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [otlp]
```

---

## 11. 实施路线图

### Phase 1：核心现代化（第 1-2 周）

| # | 任务 | 文件 | 验收 |
|---|------|------|------|
| 1.1 | 新建 `internal/observability/` 单包，删除 `internal/metrics/` + `internal/tracing/` | `internal/observability/` | 单 `Init()` 同时创建 TracerProvider + MeterProvider + 全局 accessor |
| 1.2 | Resource Attributes 标准化 | 同上 | 所有 span/metric 带 `service.name=hotplex-gateway` |
| 1.3 | 正确采样器 + OTLP gRPC 导出 | 同上 | `ParentBased(0.1)`；gzip 压缩；retry |
| 1.4 | otelhttp 全局包装 | `cmd/hotplex/routes.go` | 所有 HTTP 端点自动 RED + span |
| 1.5 | Context 传播修复 | `gateway/conn.go`, `hub.go` | 零 `context.Background()` 在请求路径中 |
| 1.6 | trace_id 注入 AEP | `gateway/hub.go` | `env.Metadata["trace_id"]` 在广播中设置 |
| 1.7 | Runtime 指标集成 | `internal/observability/` | `go.*` 和 `process.runtime.*` 指标暴露 |

### Phase 2：指标迁移（第 3-4 周）

| # | 任务 | 文件 | 验收 |
|---|------|------|------|
| 2.1 | 37 指标从 promauto → OTel Meter API | `internal/observability/` → 各 domain 包 | 指标通过 `observability.Meter()` 创建，不再 import client_golang |
| 2.2 | /admin/metrics 通过 OTel Prom exporter 服务 | `cmd/hotplex/routes.go` | `promhttp.Handler()` 端口不变，底层无 promauto |
| 2.3 | Brain 指标迁移 | `internal/brain/llm/metrics.go` | `observability.Meter()` 共享 |
| 2.4 | 内部计数器 Gauge 化 | `internal/brain/`, `internal/eventstore/` | cache + eventstore → ObservableGauge |

### Phase 3：生产加固（第 5-6 周）

| # | 任务 | 文件 | 验收 |
|---|------|------|------|
| 3.1 | Collector 部署配置 | `configs/monitoring/otel-collector-config.yaml` | 生产级 pipeline |
| 3.2 | 日志-Trace 关联 | `internal/observability/`, `cmd/hotplex/` | 每条日志带 `trace_id` |
| 3.3 | Grafana 仪表板 | `configs/monitoring/grafana/` | 8 行完整仪表板 |
| 3.4 | 端到端验证 | 全部 | OTLP 数据到达后端；告警触发测试 |

---

## 12. 验收标准

### OBS-001 — OTel SDK 初始化正确
- Given Gateway 启动, When `observability.Init()` 完成, Then `otel.GetTracerProvider()` 返回非 noop TracerProvider
- Given Gateway 启动, When `observability.Init()` 完成, Then `otel.GetMeterProvider()` 返回非 noop MeterProvider
- Given `OTEL_SDK_DISABLED=true`, When Gateway 启动, Then 所有 provider 为 noop，不影响业务逻辑
- Given Gateway 运行中, When `SIGTERM` 触发, Then `shutdown()` 在 5s 内完成 trace + metric flush
- Given 项目编译, When `go mod graph`, Then 应用代码**零直接依赖** `prometheus/client_golang`

### OBS-002 — HTTP 自动插桩
- Given 任何 HTTP 请求, When 到达 Gateway, Then 自动创建名为 `{METHOD} {ROUTE}` 的 OTel span
- Given 任何 HTTP 请求, When /admin/metrics scrape, Then `http_server_request_duration_seconds` histogram 包含该请求
- Given HTTP 请求带 W3C traceparent header, When Gateway 收到, Then Span 正确建立父子关系

### OBS-003 — Metrics 暴露
- Given Gateway 运行, When `curl /admin/metrics`, Then 返回 200，包含 `hotplex_session_active` 等所有指标
- Given Gateway 运行, When `curl /admin/metrics`, Then `http_server_request_duration_seconds` 存在（otelhttp 自动）
- Given Gateway 运行, When `curl /admin/metrics`, Then `go_goroutine_count` 存在（runtime 自动）

### OBS-004 — trace_id 传播
- Given Client 建立 WS 连接, When 完成 init 握手, Then `init_ack` 的 metadata 包含 `trace_id`
- Given 任意 AEP 事件广播, When `hub.routeMessage` 执行, Then `env.Metadata["trace_id"]` 已设置

### OBS-005 — 告警规则正确性
- Given 告警规则文件, When Prometheus 加载, Then 所有规则语法正确，0 个 unknown metric 错误
- Given `rate(hotplex_session_terminated_total{reason="crash"}[5m]) > 0`, When 持续 5min, Then `SessionAbnormalTermination` 告警触发

### OBS-006 — Cardinality 安全
- Given Gateway 运行, When 查看 `/admin/metrics` 指标数, Then 无因高基数导致的指标爆炸（< 500 唯一时间序列）

---

## 附录 A：迁移对照表

| 旧指标名 (client_golang) | 新 OTel 指标名 | 变更 |
|-------------------------|---------------|------|
| `hotplex_sessions_active` | `hotplex.session.active` | 命名规范化 |
| `hotplex_sessions_total` | `hotplex.session.created` | 命名更精确（total → created） |
| `hotplex_worker_crashes_total` | `hotplex.worker.crashes` | 下划线 → 点 |
| `hotplex_gateway_deltas_dropped_total` | `hotplex.gateway.deltas.dropped` | 层级化 |
| `hotplex_pool_acquire_total` | `hotplex.pool.acquire` | 简化 |
| `hotplex_cron_fires_total` | `hotplex.cron.fires` | 简化 |
| (none) | `http.server.request.duration` | 🆕 otelhttp 自动 |
| (none) | `go.goroutine.count` | 🆕 runtime 自动 |
| (none) | `hotplex.brain.cache.hits` | 🆕 Brain 内部指标 Prometheus 化 |
| `brain.request.latency.ms` | `hotplex.brain.request.latency` | 命名前缀统一 |

## 附录 B：依赖清单

| 包 | 版本 | 用途 |
|----|------|------|
| `go.opentelemetry.io/otel` | v1.44+ | OTel API |
| `go.opentelemetry.io/otel/sdk` | v1.44+ | OTel SDK |
| `go.opentelemetry.io/otel/sdk/metric` | v1.44+ | MeterProvider |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` | v1.44+ | Trace OTLP gRPC |
| `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc` | v1.44+ | Metric OTLP gRPC |
| `go.opentelemetry.io/otel/exporters/prometheus` | v0.46+ | Prometheus 兼容端点（内部封装 client_golang，应用代码不直接依赖） |
| `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` | v1.44+ | HTTP 自动插桩 |
| `go.opentelemetry.io/contrib/instrumentation/runtime` | v0.60+ | Go runtime 指标 |
| `github.com/prometheus/client_golang` | v1.23+ | 仅 OTel Prometheus exporter 间接依赖；promhttp.Handler 用于 HTTP 端点 |


*查看其他语言: [English](production-guide.md), [简体中文](production-guide_zh.md).*

# 生产环境部署指南

## 架构概览

```
┌─────────────────────────────────────────────────────────────┐
│                     负载均衡器 (LB)                          │
│                  (nginx / 云厂商 LB)                         │
└─────────────────────────────────────────────────────────────┘
                            │
         ┌──────────────────┼──────────────────┐
         ▼                  ▼                  ▼
   ┌──────────┐       ┌──────────┐       ┌──────────┐
   │ HotPlex  │       │ HotPlex  │       │ HotPlex  │
   │  节点 1  │       │  节点 2  │       │  节点 3  │
   └──────────┘       └──────────┘       └──────────┘
         │                  │                  │
         └──────────────────┴──────────────────┘
                            │
         ┌──────────────────┼──────────────────┐
         ▼                  ▼                  ▼
   ┌──────────┐       ┌──────────┐       ┌──────────┐
   │ Prometheus│       │  Jaeger  │       │  Loki    │
   │  (指标)   │       │  (追踪)  │       │  (日志)  │
   └──────────┘       └──────────┘       └──────────┘
                            │
                            ▼
         ┌──────────────────┴──────────────────┐
         ▼                                     ▼
   ┌──────────┐                         ┌──────────┐
   │  Slack   │                         │  飞书    │
   │  告警    │                         │  告警    │
   └──────────┘                         └──────────┘
```

## 扩容建议

| 并发用户数 | 实例数量 | 单实例 CPU | 单实例内存 |
| ---------- | -------- | ---------- | ---------- |
| 1-100      | 1        | 0.5 核     | 512MB      |
| 100-500    | 2-3      | 1 核       | 1GB        |
| 500-2000   | 5-10     | 2 核       | 2GB        |
| 2000+      | 10+      | 2-4 核     | 2-4GB      |

## 配置说明

### ChatApps 平台配置

在 `configs/chatapps/` 目录下创建配置文件：

```yaml
# configs/chatapps/slack.yaml
platform: slack
system_prompt: |
  You are an AI coding assistant.
task_instructions: |
  Help users with coding tasks.
engine:
  timeout: 5m
  idle_timeout: 30m
  work_dir: /tmp/hotplex
  allowed_tools:
    - Bash
    - Edit
    - Glob
    - Read
provider:
  type: claude-code
  model: claude-sonnet-4-20250514
security:
  verify_signature: true
  permission:
    dm_policy: allow
    group_policy: allow
    bot_user_id: U1234567890
```

### 环境变量

| `HOTPLEX_PORT` | HTTP 服务端口 | `8080` |
| `HOTPLEX_API_KEY` | 用于控制平面身份验证的主 API Key | - |
| `HOTPLEX_API_KEYS` | 多个 API Key（逗号分隔，优先于 HOTPLEX_API_KEY） | - |
| `HOTPLEX_LOG_LEVEL` | 日志级别 (debug/info/warn/error) | `info` |
| `HOTPLEX_ALLOWED_ORIGINS` | 允许的跨域来源（逗号分隔） | `localhost` |
| `HOTPLEX_CONFIG_DIR` | 配置目录 | `./configs` |
| `HOTPLEX_METRICS_PATH` | 指标端点路径 | `/metrics` |

## 监控配置

### Prometheus 配置

```yaml
scrape_configs:
  - job_name: 'hotplex'
    static_configs:
      - targets: ['hotplex:8080']
    metrics_path: /metrics
```

### 核心指标

#### 引擎指标
- `hotplex_engine_sessions_active`: 活跃会话数
- `hotplex_engine_sessions_total`: 创建的会话总数
- `hotplex_engine_executions_total`: 执行总数
- `hotplex_engine_execution_duration_seconds`: 执行耗时

#### ChatApps 指标
- `hotplex_chatapps_messages_received_total`: 接收的消息数
- `hotplex_chatapps_messages_sent_total`: 发送的消息数
- `hotplex_chatapps_processing_duration_seconds`: 消息处理耗时
- `hotplex_chatapps_errors_total`: 处理错误数

#### Provider 指标
- `hotplex_provider_tokens_total`: 消耗的 token 总数
- `hotplex_provider_cost_usd_total`: 总成本（美元）
- `hotplex_provider_tool_invocations_total`: 工具调用次数

### Grafana 仪表盘

关键面板：
- 活动会话数 (Active Sessions)
- 请求延迟 (p50, p95, p99)
- 各平台错误率
- Token 使用量与成本
- 工具调用频率

### 告警规则

```yaml
groups:
- name: hotplex
  rules:
  - alert: HighErrorRate
    expr: rate(hotplex_engine_errors_total[5m]) > 0.1
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: 检测到高错误率

  - alert: SessionPoolExhausted
    expr: hotplex_engine_sessions_active > 800
    for: 2m
    labels:
      severity: critical
    annotations:
      summary: 会话池即将耗尽

  - alert: HighLatency
    expr: histogram_quantile(0.95, rate(hotplex_engine_execution_duration_seconds_bucket[5m])) > 60
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: 95 分位延迟超过 60 秒

  - alert: PlatformRateLimit
    expr: rate(hotplex_chatapps_rate_limits_total[5m]) > 0
    for: 1m
    labels:
      severity: warning
    annotations:
      summary: 触发平台速率限制
```

## 安全检查清单

- [x] 在负载均衡器启用 TLS 终止
- [x] 配置网络策略 (Network Policies)
- [x] 启用平台级频率限制
- [x] 启用签名验证 (Slack/飞书)
- [x] 配置资源限额 (Resource Limits)
- [x] 启用审计日志
- [x] 配置 WAF 正则规则
- [x] 设置 AllowedTools 白名单

#### 飞书
- 使用 SHA256 验证签名
- 验证请求时间戳

## 健康检查

### 端点
```
GET /health
```

响应：
```json
{
  "status": "healthy",
  "version": "v0.17.0",
  "uptime": "24h30m",
  "active_sessions": 42
}
```

### 就绪探针
```
GET /ready
```

返回 200 时表示可以接收流量。

## 备份与恢复

### 会话状态

会话是短暂的 (Hot-Multiplexing)，无需备份持久化状态。
对于关键会话，可使用 `internal/persistence/` 中的会话持久化标记。

### 配置信息

```bash
# 备份配置
kubectl get configmap hotplex-config -o yaml > hotplex-config-backup.yaml

# 恢复配置
kubectl apply -f hotplex-config-backup.yaml
```

## 故障分析排查

### 内存占用过高

```bash
# 检查堆内存分析
kubectl exec -it hotplex-xxx -- curl localhost:8080/debug/pprof/heap

# 检查活跃会话数
curl http://hotplex:8080/metrics | grep hotplex_engine_sessions_active
```

### 请求响应变慢

在 Jaeger 中检查追踪，找出瓶颈所在的 Spans。

### 会话泄漏

```bash
# 监控活跃会话
curl http://hotplex:8080/metrics | grep hotplex_engine_sessions_active

# 检查僵尸进程
ps aux | grep -E "(claude-code|opencode)" | grep defunct
```

#### 飞书
- 验证 AppID 和 AppSecret
- 检查回调 URL 可访问性
- 验证签名配置

## Docker 部署

```yaml
# docker-compose.yaml
version: '3.8'
services:
  hotplex:
    image: hotplex:latest
    ports:
      - "8080:8080"
    volumes:
      - ./configs:/app/configs
      - /tmp/hotplex:/tmp/hotplex
    environment:
      - HOTPLEX_LOG_LEVEL=info
      - HOTPLEX_CONFIG_DIR=/app/configs
    restart: unless-stopped
    resources:
      limits:
        cpu: "2"
        memory: 2Gi
```

## Kubernetes 部署

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hotplex
spec:
  replicas: 3
  selector:
    matchLabels:
      app: hotplex
  template:
    metadata:
      labels:
        app: hotplex
    spec:
      containers:
      - name: hotplex
        image: hotplex:latest
        ports:
        - containerPort: 8080
        resources:
          requests:
            cpu: "1"
            memory: 1Gi
          limits:
            cpu: "2"
            memory: 2Gi
        env:
        - name: HOTPLEX_LOG_LEVEL
          value: "info"
        volumeMounts:
        - name: config
          mountPath: /app/configs
      volumes:
      - name: config
        configMap:
          name: hotplex-config
```

---

*更多详情请参阅 [架构文档](architecture_zh.md) 和 [SDK 指南](sdk-guide_zh.md)。*

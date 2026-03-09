# Issue #248 实施方案

**验证时间**: 2026-03-09
**验证方式**: 代码审查 + 日志分析

---

## 🔴 P0-1: API Key 在 Query Parameter 中传输

### 验证结果: ✅ **问题存在**

**位置**: `internal/server/security.go:176-183`

**当前代码**:
```go
func (c *SecurityConfig) validateAPIKey(r *http.Request) bool {
    // Try header first
    apiKey := r.Header.Get("X-API-Key")

    // Fall back to query parameter
    if apiKey == "" {
        apiKey = r.URL.Query().Get("api_key")  // ← 安全风险
    }
    ...
}
```

### 实施方案

**修改文件**: `internal/server/security.go`

```go
func (c *SecurityConfig) validateAPIKey(r *http.Request) bool {
    // 仅允许 header 认证，移除 query parameter 支持
    apiKey := r.Header.Get("X-API-Key")

    // 对于 WebSocket 连接，支持 Sec-WebSocket-Protocol 子协议传递 token
    if apiKey == "" {
        // 从 WebSocket 子协议中提取: Sec-WebSocket-Protocol: hotplex, api-key-xxx
        if protocols := r.Header.Get("Sec-WebSocket-Protocol"); protocols != "" {
            for _, p := range strings.Split(protocols, ",") {
                p = strings.TrimSpace(p)
                if strings.HasPrefix(p, "hotplex-api-") {
                    apiKey = strings.TrimPrefix(p, "hotplex-api-")
                    break
                }
            }
        }
    }

    if apiKey == "" {
        return false
    }
    ...
}
```

**影响范围**:
- 现有使用 `?api_key=xxx` 的客户端需要迁移到 header
- WebSocket 客户端需使用 `Sec-WebSocket-Protocol` 子协议

**测试用例**:
```go
func TestValidateAPIKey_HeaderOnly(t *testing.T) {
    // Query parameter 应被拒绝
    req := httptest.NewRequest("GET", "/ws?api_key=test", nil)
    assert.False(t, sec.validateAPIKey(req))

    // Header 应被接受
    req = httptest.NewRequest("GET", "/ws", nil)
    req.Header.Set("X-API-Key", "test")
    assert.True(t, sec.validateAPIKey(req))
}
```

---

## 🔴 P0-2: 流状态同步失败导致数据丢失

### 验证结果: ✅ **问题存在**

**位置**: `chatapps/slack/streaming_writer.go:140-152`

**当前逻辑**:
- 流有 10 分钟 TTL
- Loki Mode 可能运行 18+ 分钟
- 超时后内容被丢弃到 `failedFlushChunks`

### 实施方案

**方案 A: 长任务预检测** (推荐)

**修改文件**: `chatapps/engine_handler.go` 或 `chatapps/base/adapter.go`

```go
// 在执行前检测是否为长任务模式
func (h *EngineHandler) shouldUseStreaming(prompt string) bool {
    // Loki Mode 等长任务关键词
    longTaskKeywords := []string{"loki mode", "autonomous", "multi-agent"}
    promptLower := strings.ToLower(prompt)
    for _, kw := range longTaskKeywords {
        if strings.Contains(promptLower, kw) {
            return false // 禁用流式，使用非流式模式
        }
    }
    return true
}
```

**方案 B: 流 TTL 动态续期** (需要 Slack API 支持)

```go
// 在 streaming_writer.go 中添加
func (w *NativeStreamingWriter) refreshIfNeeded() error {
    if time.Since(w.streamStartTime) > StreamTTL*8/10 {
        // 尝试续期（如果 Slack API 支持）
        // 或提前切换到非流式
        w.adapter.Logger().Info("Stream approaching TTL, switching to non-streaming")
        w.streamExpired = true
    }
    return nil
}
```

**推荐**: 方案 A 更简单可靠

---

## 🟠 P1-3: Origin Header 为空时允许连接

### 验证结果: ✅ **问题存在**

**位置**: `internal/server/security.go:153-157`

**当前代码**:
```go
if origin == "" {
    // Non-browser clients may not send Origin
    c.logger.Debug("WebSocket connection without Origin header")
    return true  // ← 直接允许
}
```

### 实施方案

**修改文件**: `internal/server/security.go`

```go
if origin == "" {
    // 非浏览器客户端必须提供有效 API Key
    if c.apiKeyEnabled && c.validateAPIKey(r) {
        c.logger.Debug("WebSocket connection authenticated via API key (no Origin)")
        return true
    }
    // 生产环境拒绝无 Origin 且无 API Key 的连接
    if c.apiKeyEnabled {
        c.logger.Warn("Rejected WebSocket: no Origin and no valid API key",
            "remote_addr", r.RemoteAddr)
        return false
    }
    // 无 API Key 模式（开发环境）允许无 Origin
    c.logger.Debug("WebSocket connection without Origin header (dev mode)")
    return true
}
```

---

## 🟠 P1-4: WAF Backtick 拦截误判 Loki Mode 文件引用

### 验证结果: ✅ **问题存在**

**位置**: `internal/security/detector.go:476-520` (loadSafePatterns)

**当前逻辑**:
- 没有 Loki mode 文件引用的安全规则
- `@\`path\`` 被误判为 shell 命令注入

### 实施方案

**修改文件**: `internal/security/detector.go`

在 `loadSafePatterns()` 函数中添加:

```go
func (dd *Detector) loadSafePatterns() {
    patterns := []struct {
        pattern     string
        description string
        category    string
    }{
        // ... 现有规则 ...

        // Loki Mode 文件引用语法: @`path`
        {`@\`[^`]+\`(?:\s|$)`, "Loki mode file reference", "develop-tools"},
        // 普通文件引用: @path (无 backtick)
        {`@[a-zA-Z0-9_./-]+(?:\s|$)`, "File reference", "develop-tools"},
    }
    // ...
}
```

**测试用例**:
```go
func TestSafePattern_LokiModeReference(t *testing.T) {
    detector := NewDetector(nil)

    // 应该允许
    inputs := []string{
        "loki mode @`docs/prd.md` 完成任务",
        "读取 @`src/main.go` 文件",
    }
    for _, input := range inputs {
        result := detector.CheckInput(context.Background(), input)
        assert.False(t, result.Dangerous, "Should allow: %s", input)
    }
}
```

---

## 🟠 P1-5: TTL 超时后重复警告

### 验证结果: ✅ **问题存在**

**位置**: `chatapps/slack/streaming_writer.go:140-151`

**当前代码**:
```go
if streamExpired || time.Since(streamStartTime) > StreamTTL {
    w.adapter.Logger().Warn("Stream TTL exceeded...")  // ← 每次调用都打印
    ...
}
```

### 实施方案

**修改文件**: `chatapps/slack/streaming_writer.go`

1. 添加字段到 `NativeStreamingWriter` 结构体:
```go
type NativeStreamingWriter struct {
    // ... 现有字段 ...
    ttlWarningLogged bool  // 新增：TTL 警告是否已记录
}
```

2. 修改 TTL 检测逻辑:
```go
if streamExpired || time.Since(streamStartTime) > StreamTTL {
    w.mu.Lock()
    if !w.ttlWarningLogged {
        w.adapter.Logger().Warn("Stream TTL exceeded, marking as expired",
            "channel_id", w.channelID,
            "message_ts", w.messageTS,
            "stream_age", time.Since(streamStartTime),
            "ttl", StreamTTL)
        w.ttlWarningLogged = true
    }
    w.streamExpired = true
    w.failedFlushChunks = append(w.failedFlushChunks, content)
    w.mu.Unlock()
    return
}
```

---

## 🟠 P1-6: Safe Command Patterns 重复加载

### 验证结果: ⚠️ **问题部分存在**

**发现**: `NewDetector()` 仅在 `engine/runner.go:88` 调用一次
**日志中的 5 次加载来自**:
- 1 次主 Engine
- 4 个平台配置加载时的日志（可能是其他组件）

**结论**: Detector 本身是单例，但日志显示 5 次可能是其他原因。

### 实施方案

**修改文件**: `internal/security/detector.go`

改为真正的单例模式:

```go
var (
    globalDetector     *Detector
    globalDetectorOnce sync.Once
    globalDetectorMu   sync.RWMutex
)

// GetDetector 返回全局 Detector 单例
func GetDetector(logger *slog.Logger) *Detector {
    globalDetectorOnce.Do(func() {
        globalDetector = newDetectorInternal(logger)
    })
    return globalDetector
}

// NewDetector 保留向后兼容，但内部使用单例
func NewDetector(logger *slog.Logger) *Detector {
    return GetDetector(logger)
}

// newDetectorInternal 内部构造函数
func newDetectorInternal(logger *slog.Logger) *Detector {
    // ... 原有逻辑 ...
}
```

---

## 🟠 P1-7: 版本信息缺失

### 验证结果: ✅ **问题存在**

**main.go 定义** (行 24-28):
```go
var (
    version = "v0.0.0-dev"
    commit  = "unknown"
    date    = "unknown"      // ← 注意: date
    builtBy = "source"       // ← 注意: builtBy
)
```

**Dockerfile ldflags** (行 29):
```dockerfile
-ldflags="-s -w -X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildTime=${BUILD_TIME}"
```

**问题**:
| Dockerfile | main.go | 匹配? |
|------------|---------|-------|
| `main.Version` | `version` | ❌ 大小写不匹配 |
| `main.Commit` | `commit` | ❌ 大小写不匹配 |
| `main.BuildTime` | `date` | ❌ 变量名不匹配 |
| - | `builtBy` | ❌ 未设置 |

### 实施方案

**修改文件**: `Dockerfile`

```dockerfile
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w \
        -X main.version=${VERSION} \
        -X main.commit=${COMMIT} \
        -X main.date=${BUILD_TIME} \
        -X main.builtBy=docker" \
    -o hotplexd ./cmd/hotplexd
```

**注意**: Go ldflags 中变量名必须与源码中完全一致（包括大小写）

---

## 📋 实施优先级

| 优先级 | 问题 | 预估工时 | 风险 |
|--------|------|----------|------|
| P0-1 | API Key Query Parameter | 2h | 需要客户端迁移 |
| P0-2 | 流状态同步 | 4h | 低风险 |
| P1-4 | WAF 误判 | 1h | 低风险 |
| P1-5 | TTL 重复警告 | 0.5h | 低风险 |
| P1-7 | 版本信息 | 0.5h | 低风险 |
| P1-3 | Origin bypass | 1h | 需测试兼容性 |
| P1-6 | Detector 单例 | 1h | 低风险 |

**总工时**: ~10h

---

## ✅ 验证通过的问题

| # | 问题 | 验证结果 |
|---|------|----------|
| P0-1 | API Key Query Parameter | ✅ 存在 (行 176-183) |
| P0-2 | 流状态同步失败 | ✅ 存在 (行 140-152) |
| P1-3 | Origin bypass | ✅ 存在 (行 153-157) |
| P1-4 | WAF Backtick 误判 | ✅ 存在 (缺少规则) |
| P1-5 | TTL 重复警告 | ✅ 存在 (无去重) |
| P1-6 | Detector 重复加载 | ⚠️ 部分存在 |
| P1-7 | 版本信息缺失 | ✅ 存在 (变量名不匹配) |

# Bot 消息中断问题分析报告

## 问题现象
Bot 在回复消息时经常出现：
- 消息说到一半停止
- 回复中断不持续
- 内容丢失

## 根因分析

### 1. 流式写入器的缓冲刷新机制 (HIGH RISK)

**文件**: `chatapps/slack/streaming_writer.go`

```go
const (
    flushInterval = 150 * time.Millisecond  // 刷新间隔
    flushSize     = 20                       // rune 阈值
)
```

**问题**:
- 缓冲区内容需要等待 150ms 或 20 个字符才触发 flush
- 如果 stream 在 flush 之前关闭，缓冲区内容会丢失
- `Close()` 方法虽然等待 `wg.Wait()`，但如果 context 取消，`flushLoop` 会提前退出

**代码路径**:
```go
// flushLoop 退出条件
case <-w.ctx.Done():
    w.flushBuffer()  // 尝试刷新，但可能失败
    return
case <-w.closeChan:
    w.flushBuffer()
    return
```

### 2. 原生流初始化失败导致 answer 丢失 (CRITICAL)

**文件**: `chatapps/engine_handler.go:870-871`

```go
// Use native streaming if available (Pure Pipeline Mode)
if active && writer != nil {
    // ... 写入流
    return nil
}

c.logger.Error("Native streaming could not be initialized, dropping answer")
return fmt.Errorf("native streaming unavailable")
```

**问题**:
- 如果 `NewStreamWriter` 返回 nil（例如 adapter 未找到或不支持流式），**整个 answer 会被丢弃**
- 没有降级方案（fallback）将消息发送到频道

### 3. Slack API 调用失败静默处理 (HIGH RISK)

**文件**: `chatapps/slack/streaming_writer.go:104-110`

```go
// 增量推送到流
if err := w.adapter.AppendStream(w.ctx, w.channelID, w.messageTS, content); err != nil {
    w.adapter.Logger().Warn("AppendStream failed, content may be lost",
        "channel_id", w.channelID,
        "message_ts", w.messageTS,
        "content_runes", utf8.RuneCountInString(content),
        "error", err)
    // ⚠️ 注意：只记录警告，内容已丢失！
}
```

**问题**:
- `AppendStream` 失败时只记录警告
- 已写入缓冲区的内容在 API 失败后**永久丢失**
- 没有重试机制

### 4. Session 进程死亡导致流中断 (HIGH RISK)

**文件**: `internal/engine/session.go:263-276`

```go
for scanner.Scan() {
    line := scanner.Text()
    if line == "" {
        continue
    }
    cb := s.GetCallback()
    if cb != nil {
        if err := cb("raw_line", line); err != nil {
            s.logger.Debug("ReadStdout: dispatch callback error", "error", err)
        }
    }
}
// scanner 结束后，发送 runner_exit
```

**问题**:
- CLI 进程意外退出时，stdout 管道关闭
- `runner_exit` 事件触发 `doneChan` 关闭
- 但此时可能还有未 flush 的内容在缓冲区

### 5. Context 取消导致管道断裂 (MEDIUM RISK)

**文件**: `engine/runner.go:387-395`

```go
select {
case <-doneChan:
    return nil
case <-ctx.Done():
    return ctx.Err()  // 用户取消
case <-timer.C:
    return fmt.Errorf("execution timeout after %v", r.opts.Timeout)
}
```

**问题**:
- 用户取消或超时直接返回，不等待流式写入完成
- `StreamCallback.Close()` 可能未被调用

### 6. 并发锁竞争导致延迟 (MEDIUM RISK)

**文件**: `chatapps/engine_handler.go:817-854`

```go
c.mu.Lock()
// ... 初始化 streamWriter
c.mu.Unlock()

// 在锁外调用 writer.Write
writer := c.streamWriter
active := c.streamWriterActive
c.mu.Unlock()

if active && writer != nil {
    n, err := writer.Write([]byte(content))
```

**问题**:
- 每次 answer 都需要获取锁
- 如果其他 goroutine 持有锁（如 handleThinking、handleToolUse），会导致写入延迟
- 延迟累积可能导致用户体验上的"中断感"

### 7. Status 消息更新阻塞流式写入 (MEDIUM RISK)

**文件**: `chatapps/engine_handler.go:839-843`

```go
// Clear the status indicator immediately now that text is physically appearing
go func() {
    if err := c.updateStatusMessage(base.MessageTypeAnswer, StatusAnswerLabel); err != nil {
        c.logger.Warn("Failed to update status for answer", "error", err)
    }
}()
```

**问题**:
- 虽然用 goroutine 异步更新，但 `updateStatusMessage` 可能涉及 Slack API 调用
- API 限流可能导致后续操作延迟

## 修复建议

### 短期修复

1. **添加 Fallback 机制**
```go
// 在 handleAnswer 中，如果 streaming 不可用，降级到普通消息发送
if !active || writer == nil {
    c.logger.Warn("Native streaming unavailable, falling back to direct message")
    return c.sendFallbackMessage(channelID, threadTS, content)
}
```

2. **缓冲区内容保护**
```go
// 在 streaming_writer.go 的 Close() 中添加重试
func (w *NativeStreamingWriter) Close() error {
    // ... 现有逻辑

    // 添加：如果 flush 失败，尝试直接发送完整内容
    if flushErr != nil {
        w.adapter.Logger().Warn("Flush failed on close, attempting direct send")
        // 尝试直接发送消息
    }
}
```

3. **API 失败重试**
```go
// 在 AppendStream 失败时添加重试
maxRetries := 3
for i := 0; i < maxRetries; i++ {
    if err := w.adapter.AppendStream(...); err == nil {
        break
    }
    time.Sleep(100 * time.Millisecond)
}
```

### 长期优化

1. **添加消息完整性校验**
   - 在 session 结束时比较发送的字节数和确认的字节数
   - 不匹配时触发补发机制

2. **改进流控机制**
   - 使用背压（backpressure）机制
   - 在消费者慢时减慢生产者速度

3. **增强可观测性**
   - 添加 metrics 追踪消息丢失率
   - 在日志中记录每次 flush 的内容长度

## 调试建议

1. 启用 DEBUG 日志级别，观察：
   - `Native streaming initialized` - 确认流初始化成功
   - `Successfully wrote to native stream` - 确认每次写入
   - `AppendStream failed` - API 调用失败
   - `Failed to close stream` - 流关闭异常

2. 监控 Slack API 响应时间和错误率

3. 检查 CLI 进程是否意外退出（OOM、timeout 等）

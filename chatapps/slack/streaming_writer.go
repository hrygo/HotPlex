package slack

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hrygo/hotplex/chatapps/base"
)

const (
	flushInterval    = 150 * time.Millisecond
	flushSize        = 20 // rune count threshold for immediate flush
	maxAppendRetries = 3  // max retry attempts for AppendStream
	retryDelay       = 50 * time.Millisecond

	// StreamTTL is the maximum duration a Slack stream can live (~10 min TTL)
	// Improved: Increased from 4m to 10m to support complex AI tasks
	StreamTTL = 10 * time.Minute
)

// NativeStreamingWriter 实现 io.Writer 接口，封装 Slack 原生流式消息的生命周期管理
// 首次 Write 调用时启动流，后续调用追加内容，Close 时结束流
type NativeStreamingWriter struct {
	ctx       context.Context
	adapter   *Adapter
	channelID string
	userID    string
	threadTS  string
	messageTS string

	mu         sync.Mutex
	started    bool
	closed     bool
	onComplete func(string) // 流结束时的回调，用于获取最终 messageTS

	// 缓冲流控机制
	buf          bytes.Buffer
	flushTrigger chan struct{}
	closeChan    chan struct{}
	wg           sync.WaitGroup

	// Fallback 机制：累积所有内容用于最终 fallback
	accumulatedContent bytes.Buffer
	fallbackUsed       bool // 标记是否使用了 fallback

	// 完整性校验：追踪发送和确认的字节数
	bytesWritten      int64    // 成功写入的总字节数
	bytesFlushed      int64    // 成功 flush 的总字节数
	failedFlushChunks []string // 失败的 flush 块（用于潜在恢复）

	// TTL 监控：检测流超时
	streamStartTime time.Time // 流启动时间
	streamExpired   bool      // 流是否已超时

	// 存储回调（可选）
	storeCallback func(content string)
}

// NewNativeStreamingWriter 创建新的原生流式写入器
func NewNativeStreamingWriter(
	ctx context.Context,
	adapter *Adapter,
	userID, channelID, threadTS string,
	onComplete func(string),
) *NativeStreamingWriter {
	w := &NativeStreamingWriter{
		ctx:          ctx,
		adapter:      adapter,
		userID:       userID,
		channelID:    channelID,
		threadTS:     threadTS,
		onComplete:   onComplete,
		flushTrigger: make(chan struct{}, 1),
		closeChan:    make(chan struct{}),
	}

	w.wg.Add(1)
	go w.flushLoop()

	return w
}

// SetStoreCallback sets the callback to store the complete message content
// when the stream is closed. This enables persistent storage of streaming responses.
func (w *NativeStreamingWriter) SetStoreCallback(callback func(content string)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.storeCallback = callback
}

func (w *NativeStreamingWriter) flushLoop() {
	defer w.wg.Done()

	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			w.flushBuffer()
			return
		case <-w.closeChan:
			w.flushBuffer()
			return
		case <-w.flushTrigger:
			w.flushBuffer()
		case <-ticker.C:
			w.flushBuffer()
		}
	}
}

func (w *NativeStreamingWriter) flushBuffer() {
	w.mu.Lock()
	if w.buf.Len() == 0 {
		w.mu.Unlock()
		return
	}

	content := w.buf.String()
	contentLen := len(content)
	w.buf.Reset()
	started := w.started
	streamExpired := w.streamExpired
	streamStartTime := w.streamStartTime
	w.mu.Unlock()

	// 理论上只要有内容必然 started，前置拦截防空指针
	if !started {
		return
	}

	// TTL 检测：如果流已超时，不再尝试 AppendStream，直接记录失败
	if streamExpired || time.Since(streamStartTime) > StreamTTL {
		w.adapter.Logger().Warn("Stream TTL exceeded, marking as expired",
			"channel_id", w.channelID,
			"message_ts", w.messageTS,
			"stream_age", time.Since(streamStartTime),
			"ttl", StreamTTL)

		w.mu.Lock()
		w.streamExpired = true
		w.failedFlushChunks = append(w.failedFlushChunks, content)
		w.mu.Unlock()
		return
	}

	// 增量推送到流（带重试机制）
	var lastErr error
	for attempt := 1; attempt <= maxAppendRetries; attempt++ {
		if err := w.adapter.AppendStream(w.ctx, w.channelID, w.messageTS, content); err != nil {
			lastErr = err

			// 检测流状态错误：如果是 message_not_in_streaming_state，立即停止重试
			// 这表示流已经超时或被服务端关闭
			if isStreamStateError(err) {
				w.adapter.Logger().Warn("Stream state error detected, marking stream as expired",
					"channel_id", w.channelID,
					"message_ts", w.messageTS,
					"error", err)

				w.mu.Lock()
				w.streamExpired = true
				w.failedFlushChunks = append(w.failedFlushChunks, content)
				w.mu.Unlock()
				return
			}

			w.adapter.Logger().Warn("AppendStream failed, will retry",
				"channel_id", w.channelID,
				"message_ts", w.messageTS,
				"content_runes", utf8.RuneCountInString(content),
				"attempt", attempt,
				"max_retries", maxAppendRetries,
				"error", err)
			if attempt < maxAppendRetries {
				time.Sleep(retryDelay * time.Duration(attempt))
			}
			continue
		}
		// 成功：更新已 flush 字节数
		w.mu.Lock()
		w.bytesFlushed += int64(contentLen)
		w.mu.Unlock()
		return
	}

	// 所有重试都失败：记录失败块用于潜在恢复
	w.mu.Lock()
	w.failedFlushChunks = append(w.failedFlushChunks, content)
	w.mu.Unlock()

	w.adapter.Logger().Error("AppendStream failed after all retries",
		"channel_id", w.channelID,
		"message_ts", w.messageTS,
		"content_runes", utf8.RuneCountInString(content),
		"error", lastErr)
}

// isStreamStateError checks if the error indicates the stream is no longer in streaming state
func isStreamStateError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "message_not_in_streaming_state") ||
		strings.Contains(errStr, "streaming_state") ||
		strings.Contains(errStr, "not_in_streaming")
}

// Write 实现 io.Writer 接口
// 首次调用执行 StartStream 获取 TS；后续调用将内容追加到缓冲区并触发异步 AppendStream
func (w *NativeStreamingWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, fmt.Errorf("stream already closed")
	}

	if len(p) == 0 {
		return 0, nil
	}

	// 首次调用，同步启动流
	if !w.started {
		messageTS, err := w.adapter.StartStream(w.ctx, w.channelID, w.threadTS)
		if err != nil {
			return 0, fmt.Errorf("start stream: %w", err)
		}
		w.messageTS = messageTS
		w.started = true
		w.streamStartTime = time.Now() // 记录流启动时间用于 TTL 检测
	}

	w.buf.Write(p)
	w.accumulatedContent.Write(p)   // 累积内容用于潜在 fallback
	w.bytesWritten += int64(len(p)) // 追踪写入字节数

	// 如果超过 rune 阈值，立即触发一次 flush
	if utf8.RuneCount(w.buf.Bytes()) >= flushSize {
		select {
		case w.flushTrigger <- struct{}{}:
		default:
		}
	}

	return len(p), nil
}

// Close 结束流，清理并固化消息
// 如果流失败，会尝试 fallback 到普通消息发送
func (w *NativeStreamingWriter) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}

	w.closed = true
	w.mu.Unlock()

	// 停止处理并等待残留缓冲区发送完成 (必须在捕获状态前完成)
	close(w.closeChan)
	w.wg.Wait()

	// 在停止所有后台活动后，最后一次捕获所有状态
	w.mu.Lock()
	started := w.started
	accumulated := w.accumulatedContent.String()
	bytesWritten := w.bytesWritten
	bytesFlushed := w.bytesFlushed
	failedChunks := w.failedFlushChunks
	streamExpired := w.streamExpired
	storeCallback := w.storeCallback
	w.mu.Unlock()

	if !started {
		return nil
	}

	// 完整性校验：检查是否所有写入的内容都成功送达了 Slack
	integrityOK := len(failedChunks) == 0 && bytesWritten == bytesFlushed

	if !integrityOK {
		w.adapter.Logger().Warn("Stream integrity check failed",
			"channel_id", w.channelID,
			"bytes_written", bytesWritten,
			"bytes_flushed", bytesFlushed,
			"failed_chunks", len(failedChunks),
			"stream_expired", streamExpired)
	}

	// 结束远端流
	stopErr := w.adapter.StopStream(w.ctx, w.channelID, w.messageTS)

	// 调用完成回调
	if w.onComplete != nil {
		w.onComplete(w.messageTS)
	}

	// 存储完整内容
	if storeCallback != nil && accumulated != "" {
		storeCallback(accumulated)
	}

	// =========================================================================
	// 智能化回退逻辑 (Optimized Fallback)
	// =========================================================================

	// 如果数据层面已全达，禁止回退，但若 StopStream 报错仍需上报 metadata 异常
	if integrityOK {
		if stopErr != nil {
			w.adapter.Logger().Debug("StopStream failed but content arrived. Skipping fallback.",
				"channel_id", w.channelID,
				"error", stopErr)
			return fmt.Errorf("stop stream failed: %w", stopErr)
		}
		return nil
	}

	// 如果内容确实没发齐，且积累了内容，则尝试补发
	if len(accumulated) > 0 {
		var fallbackText string
		if bytesFlushed > 0 {
			// 如果部分送达，重写时带上状态提示，避免看起来像随机重复
			fallbackText = "⚠️ *Stream interrupted, sending complete response below:*\n\n" + accumulated
		} else {
			fallbackText = accumulated
		}

		w.adapter.Logger().Warn("Attempting fallback message due to incomplete stream (leakage prevention)",
			"channel_id", w.channelID,
			"written", bytesWritten,
			"flushed", bytesFlushed,
			"failed_chunks", len(failedChunks),
			"content_len", len(accumulated))

		if fallbackErr := w.adapter.SendThreadReply(w.ctx, w.channelID, w.threadTS, fallbackText); fallbackErr != nil {
			w.adapter.Logger().Error("Critical: Fallback message failed during stream closure",
				"channel_id", w.channelID,
				"error", fallbackErr)
			return fmt.Errorf("stream integrity check failed (%d/%d bytes); fallback send also failed: %w",
				bytesFlushed, bytesWritten, fallbackErr)
		}

		w.mu.Lock()
		w.fallbackUsed = true
		w.mu.Unlock()
	}

	return nil
}

// MessageTS 返回流式消息的 timestamp
func (w *NativeStreamingWriter) MessageTS() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.messageTS
}

// BufferContent 返回当前缓存的内容，用于 fallback 恢复
func (w *NativeStreamingWriter) BufferContent() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// IsStartFailed 检查流是否启动失败（有缓存内容但未启动）
func (w *NativeStreamingWriter) IsStartFailed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return !w.started && w.buf.Len() > 0
}

// IsStarted 返回流是否已启动
func (w *NativeStreamingWriter) IsStarted() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.started
}

// IsClosed 返回流是否已关闭
func (w *NativeStreamingWriter) IsClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

// FallbackUsed 返回是否使用了 fallback 机制
func (w *NativeStreamingWriter) FallbackUsed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.fallbackUsed
}

// GetAccumulatedContent 返回累积的所有内容（用于调试）
func (w *NativeStreamingWriter) GetAccumulatedContent() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.accumulatedContent.String()
}

// StreamWriterStats returns stream statistics for integrity validation and monitoring
type StreamWriterStats struct {
	BytesWritten     int64 // Total bytes successfully written
	BytesFlushed     int64 // Total bytes successfully flushed
	FailedChunkCount int   // Number of failed flush chunks
	IntegrityOK      bool  // Whether integrity check passed
	FallbackUsed     bool  // Whether fallback mechanism was used
	ContentLength    int   // Total length of accumulated content
}

// GetStats returns stream statistics
func (w *NativeStreamingWriter) GetStats() StreamWriterStats {
	w.mu.Lock()
	defer w.mu.Unlock()

	return StreamWriterStats{
		BytesWritten:     w.bytesWritten,
		BytesFlushed:     w.bytesFlushed,
		FailedChunkCount: len(w.failedFlushChunks),
		IntegrityOK:      len(w.failedFlushChunks) == 0 && w.bytesWritten == w.bytesFlushed && !w.streamExpired,
		FallbackUsed:     w.fallbackUsed,
		ContentLength:    w.accumulatedContent.Len(),
	}
}

// Ensure NativeStreamingWriter implements io.WriteCloser at compile time
var _ io.WriteCloser = (*NativeStreamingWriter)(nil)

// Ensure NativeStreamingWriter implements base.StreamWriter at compile time
var _ base.StreamWriter = (*NativeStreamingWriter)(nil)

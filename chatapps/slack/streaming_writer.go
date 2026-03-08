package slack

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hrygo/hotplex/chatapps/base"
)

const (
	flushInterval = 150 * time.Millisecond
	flushSize     = 20 // rune count threshold for immediate flush
)

// NativeStreamingWriter 实现 io.Writer 接口，封装 Slack 原生流式消息的生命周期管理
// 首次 Write 调用时启动流，后续调用追加内容，Close 时结束流
type NativeStreamingWriter struct {
	ctx       context.Context
	adapter   *Adapter
	channelID string
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

	// 完整内容累积（用于存储）
	fullContent bytes.Buffer

	// 存储回调（可选）
	storeCallback func(content string)
}

// NewNativeStreamingWriter 创建新的原生流式写入器
func NewNativeStreamingWriter(
	ctx context.Context,
	adapter *Adapter,
	channelID, threadTS string,
	onComplete func(string),
) *NativeStreamingWriter {
	w := &NativeStreamingWriter{
		ctx:          ctx,
		adapter:      adapter,
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
	w.buf.Reset()
	started := w.started
	w.mu.Unlock()

	// 理论上只要有内容必然 started，前置拦截防空指针
	if !started {
		return
	}

	// 增量推送到流
	if err := w.adapter.AppendStream(w.ctx, w.channelID, w.messageTS, content); err != nil {
		w.adapter.Logger().Warn("AppendStream failed, content may be lost",
			"channel_id", w.channelID,
			"message_ts", w.messageTS,
			"content_runes", utf8.RuneCountInString(content),
			"error", err)
	}
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
	}

	w.buf.Write(p)
	w.fullContent.Write(p) // 累积完整内容用于存储

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
func (w *NativeStreamingWriter) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}

	w.closed = true
	started := w.started
	fullContent := w.fullContent.String()
	storeCallback := w.storeCallback
	w.mu.Unlock()

	// 停止处理并等待残留缓冲区发送完成
	close(w.closeChan)
	w.wg.Wait()

	if !started {
		return nil
	}

	// 结束远端流
	stopErr := w.adapter.StopStream(w.ctx, w.channelID, w.messageTS)

	// 调用完成回调
	if w.onComplete != nil {
		w.onComplete(w.messageTS)
	}

	// 存储完整内容（如果有存储回调）
	if storeCallback != nil && fullContent != "" {
		storeCallback(fullContent)
	}

	if stopErr != nil {
		return fmt.Errorf("stop stream: %w", stopErr)
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

// Ensure NativeStreamingWriter implements io.WriteCloser at compile time
var _ io.WriteCloser = (*NativeStreamingWriter)(nil)

// Ensure NativeStreamingWriter implements base.StreamWriter at compile time
var _ base.StreamWriter = (*NativeStreamingWriter)(nil)

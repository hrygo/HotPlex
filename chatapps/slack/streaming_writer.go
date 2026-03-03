package slack

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/hrygo/hotplex/chatapps/base"
)

// NativeStreamingWriter 实现 io.Writer 接口，封装 Slack 原生流式消息的生命周期管理
// 首次 Write 调用时启动流，后续调用追加内容，Close 时结束流
type NativeStreamingWriter struct {
	ctx        context.Context
	adapter    *Adapter
	channelID  string
	threadTS   string
	messageTS  string
	mu         sync.Mutex
	started    bool
	closed     bool
	onComplete func(string) // 流结束时的回调，用于获取最终 messageTS
}

// NewNativeStreamingWriter 创建新的原生流式写入器
func NewNativeStreamingWriter(
	ctx context.Context,
	adapter *Adapter,
	channelID, threadTS string,
	onComplete func(string),
) *NativeStreamingWriter {
	return &NativeStreamingWriter{
		ctx:        ctx,
		adapter:    adapter,
		channelID:  channelID,
		threadTS:   threadTS,
		onComplete: onComplete,
	}
}

// Write 实现 io.Writer 接口
// 首次调用执行 StartStream 获取 TS；后续调用执行 AppendStream 增量推送
func (w *NativeStreamingWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, fmt.Errorf("stream already closed")
	}

	content := string(p)
	if content == "" {
		return len(p), nil
	}

	// 首次调用，启动流
	if !w.started {
		messageTS, err := w.adapter.StartStream(w.ctx, w.channelID, w.threadTS)
		if err != nil {
			return 0, fmt.Errorf("start stream: %w", err)
		}
		w.messageTS = messageTS
		w.started = true
	}

	// 追加内容到流
	if err := w.adapter.AppendStream(w.ctx, w.channelID, w.messageTS, content); err != nil {
		return 0, fmt.Errorf("append stream: %w", err)
	}

	return len(p), nil
}

// Close 结束流并固化消息
func (w *NativeStreamingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}

	if !w.started {
		// 如果从未启动过流（空内容），直接返回
		w.closed = true
		return nil
	}

	// 结束流
	stopErr := w.adapter.StopStream(w.ctx, w.channelID, w.messageTS)

	// 无论 StopStream 是否成功，都标记为已关闭
	w.closed = true

	// 调用完成回调，传递最终的 messageTS（即使 StopStream 失败）
	if w.onComplete != nil {
		w.onComplete(w.messageTS)
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

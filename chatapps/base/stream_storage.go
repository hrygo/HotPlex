package base

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/hrygo/hotplex/plugins/storage"
)

// StreamBuffer 流式消息缓冲区 (内存)
type StreamBuffer struct {
	SessionID   string
	Chunks      []string
	IsComplete  bool
	LastUpdated time.Time
	mu          sync.RWMutex
}

// Append 追加 chunk 到缓冲区
func (b *StreamBuffer) Append(chunk string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Chunks = append(b.Chunks, chunk)
	b.LastUpdated = time.Now()
}

// Merge 合并所有 chunk 为完整消息
func (b *StreamBuffer) Merge() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(b.Chunks) == 0 {
		return ""
	}
	result := make([]byte, 0)
	for _, chunk := range b.Chunks {
		result = append(result, []byte(chunk)...)
	}
	return string(result)
}

// IsExpired 检查缓冲区是否超时
func (b *StreamBuffer) IsExpired(timeout time.Duration) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return time.Since(b.LastUpdated) > timeout
}

// Clear 清空缓冲区
func (b *StreamBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Chunks = nil
	b.IsComplete = false
}

// StreamMessageStore 流式消息存储管理器
type StreamMessageStore struct {
	buffers     map[string]*StreamBuffer
	mu          sync.RWMutex
	store       storage.ChatAppMessageStore
	timeout     time.Duration
	maxBuffers  int
	logger      *slog.Logger
	cleanupStop chan struct{}
	cleanupWg   sync.WaitGroup
}

// ErrBufferFull 缓冲区已满错误
var ErrBufferFull = errors.New("stream buffer full, cannot accept new chunks")

// NewStreamMessageStore 创建流式消息存储管理器
func NewStreamMessageStore(store storage.ChatAppMessageStore, timeout time.Duration, maxBuffers int, logger *slog.Logger) *StreamMessageStore {
	if logger == nil {
		logger = slog.Default()
	}
	s := &StreamMessageStore{
		buffers:     make(map[string]*StreamBuffer),
		store:       store,
		timeout:     timeout,
		maxBuffers:  maxBuffers,
		logger:      logger,
		cleanupStop: make(chan struct{}),
	}
	s.startCleanup()
	return s
}

// startCleanup 启动后台清理 goroutine
func (s *StreamMessageStore) startCleanup() {
	s.cleanupWg.Add(1)
	go func() {
		defer s.cleanupWg.Done()
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-s.cleanupStop:
				return
			case <-ticker.C:
				s.cleanupExpired()
			}
		}
	}()
}

// cleanupExpired 清理超时的缓冲区
func (s *StreamMessageStore) cleanupExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sessionID, buf := range s.buffers {
		if buf.IsExpired(s.timeout) {
			delete(s.buffers, sessionID)
		}
	}
}

// Close 停止清理 goroutine
func (s *StreamMessageStore) Close() {
	close(s.cleanupStop)
	s.cleanupWg.Wait()
}

// OnStreamChunk 接收流式消息块 (不存储，仅缓存)
func (s *StreamMessageStore) OnStreamChunk(ctx context.Context, sessionID, chunk string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 限制缓冲区数量
	if len(s.buffers) >= s.maxBuffers {
		// 查找并删除过期的缓冲区
		evicted := false
		for id, buf := range s.buffers {
			if buf.IsExpired(s.timeout) {
				// 记录警告：过期缓冲区的数据将被丢弃
				s.logger.Warn("evicting expired stream buffer, data will be lost",
					"session_id", id,
					"chunk_count", len(buf.Chunks),
					"reason", "buffer overflow")
				delete(s.buffers, id)
				evicted = true
				break
			}
		}
		// 如果没有过期的缓冲区可删除，返回错误而不是静默丢弃
		if !evicted {
			s.logger.Error("stream buffer full, cannot accept new chunks",
				"max_buffers", s.maxBuffers,
				"session_id", sessionID)
			return ErrBufferFull
		}
	}

	buf, exists := s.buffers[sessionID]
	if !exists {
		buf = &StreamBuffer{
			SessionID:   sessionID,
			Chunks:      make([]string, 0),
			LastUpdated: time.Now(),
		}
		s.buffers[sessionID] = buf
	}

	buf.Append(chunk)
	return nil
}

// OnStreamComplete 流式消息完成 (合并后存储)
func (s *StreamMessageStore) OnStreamComplete(ctx context.Context, sessionID string, msg *storage.ChatAppMessage) error {
	s.mu.Lock()
	buf, exists := s.buffers[sessionID]
	if exists {
		buf.IsComplete = true
	}
	s.mu.Unlock()

	if !exists {
		// 没有缓冲区，直接存储 (非流式消息)
		return s.store.StoreBotResponse(ctx, msg)
	}

	// 合并 chunk
	mergedContent := buf.Merge()
	if mergedContent == "" {
		return nil
	}

	// 更新消息内容为合并后的完整内容
	msg.Content = mergedContent

	// 存储最终结果
	err := s.store.StoreBotResponse(ctx, msg)

	// 清理缓冲区
	s.mu.Lock()
	delete(s.buffers, sessionID)
	s.mu.Unlock()

	return err
}

// GetBuffer 获取指定 session 的缓冲区 (用于调试/监控)
func (s *StreamMessageStore) GetBuffer(sessionID string) *StreamBuffer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.buffers[sessionID]
}

// GetBufferCount 获取当前活跃的缓冲区数量
func (s *StreamMessageStore) GetBufferCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.buffers)
}

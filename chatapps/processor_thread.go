package chatapps

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
)

// ThreadProcessor manages thread_ts caching for message chunking.
// It tracks the first message's timestamp for each session to associate
// subsequent chunked messages in the same thread.
type ThreadProcessor struct {
	logger          *slog.Logger
	threads         sync.Map // sessionID -> ThreadInfo
	cleanupInterval time.Duration
	ttl             time.Duration
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
}

// ThreadInfo holds thread-related metadata for a session.
type ThreadInfo struct {
	ThreadTS     string
	ChannelID    string
	LastActivity time.Time
}

// ThreadProcessorOptions configures the ThreadProcessor.
type ThreadProcessorOptions struct {
	CleanupInterval time.Duration // How often to run cleanup (default: 5 min)
	TTL             time.Duration // How long to keep thread info (default: 30 min)
}

// NewThreadProcessor creates a new ThreadProcessor.
func NewThreadProcessor(logger *slog.Logger, opts ThreadProcessorOptions) *ThreadProcessor {
	if logger == nil {
		logger = slog.Default()
	}

	// Set defaults
	if opts.CleanupInterval == 0 {
		opts.CleanupInterval = 5 * time.Minute
	}
	if opts.TTL == 0 {
		opts.TTL = 30 * time.Minute
	}

	ctx, cancel := context.WithCancel(context.Background())

	p := &ThreadProcessor{
		logger:          logger,
		cleanupInterval: opts.CleanupInterval,
		ttl:             opts.TTL,
		ctx:             ctx,
		cancel:          cancel,
	}

	// Start cleanup goroutine
	p.wg.Add(1)
	go p.cleanupLoop()

	return p
}

// Name returns the processor name.
func (p *ThreadProcessor) Name() string {
	return "ThreadProcessor"
}

// Order returns the processor order (runs after rate limit, before aggregation).
func (p *ThreadProcessor) Order() int {
	return int(OrderThread)
}

// Process manages thread_ts for the message.
// For the first message: stores thread_ts from metadata if present.
// For subsequent messages: attaches stored thread_ts to metadata.
func (p *ThreadProcessor) Process(ctx context.Context, msg *base.ChatMessage) (*base.ChatMessage, error) {
	if msg == nil {
		return nil, nil
	}

	sessionID := msg.SessionID
	if sessionID == "" {
		return msg, nil
	}

	// Get or create thread info for this session
	threadInfo, loaded := p.threads.LoadOrStore(sessionID, &ThreadInfo{
		LastActivity: time.Now(),
	})

	info := threadInfo.(*ThreadInfo)
	info.LastActivity = time.Now()

	// Extract thread_ts and channel_id from incoming message
	incomingThreadTS := ""
	incomingChannelID := ""

	if msg.Metadata != nil {
		if ts, ok := msg.Metadata["thread_ts"].(string); ok && ts != "" {
			incomingThreadTS = ts
		}
		if ch, ok := msg.Metadata["channel_id"].(string); ok && ch != "" {
			incomingChannelID = ch
		}
	}

	// If this is the first message (not loaded) and has thread info, store it
	if !loaded {
		if incomingThreadTS != "" {
			info.ThreadTS = incomingThreadTS
			p.logger.Debug("ThreadProcessor: stored thread_ts",
				"session_id", sessionID,
				"thread_ts", incomingThreadTS)
		}
		if incomingChannelID != "" {
			info.ChannelID = incomingChannelID
		}
	} else {
		// For subsequent messages, if incoming has thread_ts, update stored
		if incomingThreadTS != "" && info.ThreadTS == "" {
			info.ThreadTS = incomingThreadTS
			p.logger.Debug("ThreadProcessor: updated thread_ts from incoming",
				"session_id", sessionID,
				"thread_ts", incomingThreadTS)
		}
		// Update channel_id if different
		if incomingChannelID != "" && info.ChannelID != incomingChannelID {
			info.ChannelID = incomingChannelID
		}
	}

	// Attach thread info to message metadata for downstream processors
	if msg.Metadata == nil {
		msg.Metadata = make(map[string]any)
	}

	// Use stored thread_ts if no incoming thread_ts
	if _, ok := msg.Metadata["thread_ts"]; !ok {
		if info.ThreadTS != "" {
			msg.Metadata["thread_ts"] = info.ThreadTS
		}
	}

	// Always preserve channel_id
	if _, ok := msg.Metadata["channel_id"]; !ok {
		if info.ChannelID != "" {
			msg.Metadata["channel_id"] = info.ChannelID
		}
	}

	return msg, nil
}

// GetThreadTS returns the stored thread_ts for a session.
// Returns empty string if no thread info exists.
func (p *ThreadProcessor) GetThreadTS(sessionID string) string {
	if val, ok := p.threads.Load(sessionID); ok {
		info := val.(*ThreadInfo)
		return info.ThreadTS
	}
	return ""
}

// SetThreadTS explicitly sets the thread_ts for a session.
// This is useful when the first message response provides the thread_ts.
func (p *ThreadProcessor) SetThreadTS(sessionID, threadTS, channelID string) {
	info := &ThreadInfo{
		ThreadTS:     threadTS,
		ChannelID:    channelID,
		LastActivity: time.Now(),
	}
	p.threads.Store(sessionID, info)
	p.logger.Debug("ThreadProcessor: explicitly set thread_ts",
		"session_id", sessionID,
		"thread_ts", threadTS,
		"channel_id", channelID)
}

// Delete removes thread info for a session.
func (p *ThreadProcessor) Delete(sessionID string) {
	p.threads.Delete(sessionID)
	p.logger.Debug("ThreadProcessor: deleted thread info",
		"session_id", sessionID)
}

// Stop stops the cleanup goroutine.
func (p *ThreadProcessor) Stop() {
	p.cancel()
	p.wg.Wait()
	p.logger.Debug("ThreadProcessor: stopped")
}

// cleanupLoop periodically removes expired thread entries.
func (p *ThreadProcessor) cleanupLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.cleanup()
		}
	}
}

// cleanup removes thread entries that have exceeded the TTL.
func (p *ThreadProcessor) cleanup() {
	now := time.Now()
	cutoff := now.Add(-p.ttl)

	var toDelete []string

	p.threads.Range(func(key, value any) bool {
		info := value.(*ThreadInfo)
		if info.LastActivity.Before(cutoff) {
			toDelete = append(toDelete, key.(string))
		}
		return true
	})

	for _, sessionID := range toDelete {
		p.threads.Delete(sessionID)
	}

	if len(toDelete) > 0 {
		p.logger.Debug("ThreadProcessor: cleanup completed",
			"deleted", len(toDelete))
	}
}

package chatapps

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
)

// RateLimitProcessor implements rate limiting for message sending
type RateLimitProcessor struct {
	logger *slog.Logger

	// Per-session rate limiting
	sessionLimits map[string]*time.Time
	mu            sync.Mutex

	// Configuration
	minInterval time.Duration
	maxBurst    int
	burstWindow time.Duration
}

// RateLimitProcessorOptions configures the rate limit processor
type RateLimitProcessorOptions struct {
	MinInterval time.Duration // Minimum interval between messages
	MaxBurst    int           // Maximum messages in burst window
	BurstWindow time.Duration // Time window for burst calculation
}

// NewRateLimitProcessor creates a new RateLimitProcessor
func NewRateLimitProcessor(logger *slog.Logger, opts RateLimitProcessorOptions) *RateLimitProcessor {
	if logger == nil {
		logger = slog.Default()
	}

	// Set defaults
	if opts.MinInterval == 0 {
		opts.MinInterval = 100 * time.Millisecond
	}
	if opts.MaxBurst == 0 {
		opts.MaxBurst = 5
	}
	if opts.BurstWindow == 0 {
		opts.BurstWindow = time.Second
	}

	return &RateLimitProcessor{
		logger:        logger,
		sessionLimits: make(map[string]*time.Time),
		minInterval:   opts.MinInterval,
		maxBurst:      opts.MaxBurst,
		burstWindow:   opts.BurstWindow,
	}
}

// Name returns the processor name
func (p *RateLimitProcessor) Name() string {
	return "RateLimitProcessor"
}

// Order returns the processor order (should run first)
func (p *RateLimitProcessor) Order() int {
	return int(OrderRateLimit)
}

// Process applies rate limiting to the message
// It will wait if necessary to enforce the minimum interval between messages
func (p *RateLimitProcessor) Process(ctx context.Context, msg *base.ChatMessage) (*base.ChatMessage, error) {
	if msg == nil {
		return nil, nil
	}

	// Create session key
	sessionKey := msg.Platform + ":" + msg.SessionID

	p.mu.Lock()
	lastSend := p.sessionLimits[sessionKey]
	now := time.Now()

	// Calculate wait time if needed
	var waitDuration time.Duration
	if lastSend != nil {
		elapsed := now.Sub(*lastSend)
		if elapsed < p.minInterval {
			waitDuration = p.minInterval - elapsed
		}
	}

	// Update last send time (accounting for wait)
	targetTime := now.Add(waitDuration)
	p.sessionLimits[sessionKey] = &targetTime
	p.mu.Unlock()

	// Wait outside the lock to allow other sessions to proceed
	if waitDuration > 0 {
		p.logger.Debug("Rate limit - waiting before sending",
			"session_key", sessionKey,
			"wait_ms", waitDuration.Milliseconds())

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(waitDuration):
			// Continue with message
		}
	}

	p.logger.Debug("Rate limit check passed",
		"session_key", sessionKey,
		"platform", msg.Platform)

	return msg, nil
}

// Cleanup removes old session rate limits
func (p *RateLimitProcessor) Cleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-p.burstWindow)

	for key, lastTime := range p.sessionLimits {
		if lastTime.Before(cutoff) {
			delete(p.sessionLimits, key)
		}
	}

	p.logger.Debug("Rate limit cleanup completed",
		"remaining_sessions", len(p.sessionLimits))
}

// GetSessionStats returns rate limit stats for a session
func (p *RateLimitProcessor) GetSessionStats(platform, sessionID string) (lastSend time.Time, exists bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	sessionKey := platform + ":" + sessionID
	lastTime, ok := p.sessionLimits[sessionKey]
	if !ok {
		return time.Time{}, false
	}
	return *lastTime, true
}

// Verify RateLimitProcessor implements MessageProcessor at compile time
var _ MessageProcessor = (*RateLimitProcessor)(nil)

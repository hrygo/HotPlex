package chatapps

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
)

// Zone indices – fixed ordering for message areas.
const (
	ZoneInitialization = 0 // Initialization Zone (初始化) - session_start, engine_starting
	ZoneThinking       = 1 // Thinking Zone (思考区) - thinking, plan_mode
	ZoneAction         = 2 // Action Zone (行动区) - tool_use, tool_result, etc.
	ZoneOutput         = 3 // Output Zone (展示区) - answer, ask_user_question
	ZoneSummary        = 4 // Summary Zone (总结区) - session_stats
)

// eventToZone maps event_type strings to their zone index.
// Events not in this map are allowed through without zone enforcement.
var eventToZone = map[string]int{
	// Zone 0 – Initialization
	"session_start":   ZoneInitialization,
	"engine_starting": ZoneInitialization,

	// Zone 1 – Thinking
	"thinking":  ZoneThinking,
	"plan_mode": ZoneThinking,

	// Zone 2 – Action
	"tool_use":           ZoneAction,
	"tool_result":        ZoneAction,
	"permission_request": ZoneAction,
	"danger_block":       ZoneAction,
	"command_progress":   ZoneAction,
	"command_complete":   ZoneAction,
	"step_start":         ZoneAction,
	"step_finish":        ZoneAction,

	// Zone 3 – Output
	"answer":            ZoneOutput,
	"ask_user_question": ZoneOutput,
	"error":             ZoneOutput,
	"exit_plan_mode":    ZoneOutput,

	// Zone 4 – Summary
	"session_stats": ZoneSummary,
}

// ZoneOrderProcessor ensures messages respect zone ordering within a session.
// Earlier zones (lower index) are always sent before later zones.
// If an event arrives for a zone that should have already passed, it is still
// allowed through (late arrival is better than lost messages).
type ZoneOrderProcessor struct {
	logger *slog.Logger
	// Per-session tracking for initialization synchronization.
	// Key: platform:sessionID
	sessions map[string]*zoneState
	mu       sync.Mutex
}

type zoneState struct {
	initReceived chan struct{}
	once         sync.Once
}

func newZoneState() *zoneState {
	return &zoneState{
		initReceived: make(chan struct{}),
	}
}

func (s *zoneState) markInitReceived() {
	s.once.Do(func() {
		close(s.initReceived)
	})
}

// NewZoneOrderProcessor creates a new ZoneOrderProcessor.
func NewZoneOrderProcessor(logger *slog.Logger) *ZoneOrderProcessor {
	if logger == nil {
		logger = slog.Default()
	}
	return &ZoneOrderProcessor{
		logger:   logger,
		sessions: make(map[string]*zoneState),
	}
}

// Name returns the processor name.
func (p *ZoneOrderProcessor) Name() string {
	return "ZoneOrderProcessor"
}

// Order returns the processor order.
func (p *ZoneOrderProcessor) Order() int {
	return int(OrderZoneOrder)
}

// Process validates zone ordering. It annotates messages with their zone index
// in metadata for downstream processors (e.g., aggregator) to use.
func (p *ZoneOrderProcessor) Process(ctx context.Context, msg *base.ChatMessage) (*base.ChatMessage, error) {
	if msg == nil || msg.Metadata == nil {
		return msg, nil
	}

	eventType, _ := msg.Metadata["event_type"].(string)
	zone, known := eventToZone[eventType]
	if !known {
		return msg, nil
	}

	// Annotate message with zone index for downstream use.
	msg.Metadata["zone_index"] = zone

	sessionKey := msg.Platform + ":" + msg.SessionID

	p.mu.Lock()
	state, exists := p.sessions[sessionKey]
	if !exists {
		state = newZoneState()
		p.sessions[sessionKey] = state
	}

	// If this is initialization, mark it and proceed immediately.
	if zone == ZoneInitialization {
		state.markInitReceived()
		p.mu.Unlock()
		return msg, nil
	}

	// session_stats marks turn end; initialization state persists until session ends.
	p.mu.Unlock()

	// For all other zones, wait briefly for Initialization.
	// This ensures "Starting session" always appears at the top.
	select {
	case <-state.initReceived:
		// Initialization arrived, proceed.
	case <-time.After(500 * time.Millisecond):
		// Safety timeout, proceed anyway.
		p.logger.Debug("Zone order: timeout waiting for initialization",
			"event_type", eventType,
			"session", sessionKey)
	case <-ctx.Done():
		// Context cancelled, stop waiting.
		return nil, ctx.Err()
	}

	return msg, nil
}

// ResetSession clears zone state for a session (call on session end).
func (p *ZoneOrderProcessor) ResetSession(platform, sessionID string) {
	p.mu.Lock()
	delete(p.sessions, platform+":"+sessionID)
	p.mu.Unlock()
}

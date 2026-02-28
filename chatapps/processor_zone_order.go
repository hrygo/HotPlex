package chatapps

import (
	"context"
	"log/slog"
	"sync"

	"github.com/hrygo/hotplex/chatapps/base"
)

// Zone indices – fixed ordering for message areas.
const (
	ZoneThinking = 0 // Thinking Zone (思考区)
	ZoneAction   = 1 // Action Zone (行动区)
	ZoneOutput   = 2 // Output Zone (展示区)
	ZoneSummary  = 3 // Summary Zone (总结区)
)

// eventToZone maps event_type strings to their zone index.
// Events not in this map are allowed through without zone enforcement.
var eventToZone = map[string]int{
	// Zone 0 – Thinking
	"thinking":  ZoneThinking,
	"plan_mode": ZoneThinking,

	// Zone 1 – Action
	"tool_use":           ZoneAction,
	"tool_result":        ZoneAction,
	"permission_request": ZoneAction,
	"danger_block":       ZoneAction,
	"command_progress":   ZoneAction,
	"command_complete":   ZoneAction,
	"step_start":         ZoneAction,
	"step_finish":        ZoneAction,
	"session_start":      ZoneAction,
	"engine_starting":    ZoneAction,

	// Zone 2 – Output
	"answer":            ZoneOutput,
	"ask_user_question": ZoneOutput,
	"error":             ZoneOutput,
	"exit_plan_mode":    ZoneOutput,

	// Zone 3 – Summary
	"session_stats": ZoneSummary,
}

// ZoneOrderProcessor ensures messages respect zone ordering within a session.
// Earlier zones (lower index) are always sent before later zones.
// If an event arrives for a zone that should have already passed, it is still
// allowed through (late arrival is better than lost messages).
type ZoneOrderProcessor struct {
	logger *slog.Logger
	// Per-session tracking of highest zone seen so far.
	// Key: platform:sessionID
	sessions map[string]*zoneState
	mu       sync.Mutex
}

type zoneState struct {
	highestZone int  // Highest zone index seen so far
	anchorSet   bool // Whether Zone 0 anchor (first thinking msg) has been recorded
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
func (p *ZoneOrderProcessor) Process(_ context.Context, msg *base.ChatMessage) (*base.ChatMessage, error) {
	if msg == nil || msg.Metadata == nil {
		return msg, nil
	}

	eventType, _ := msg.Metadata["event_type"].(string)
	zone, known := eventToZone[eventType]
	if !known {
		// Unknown events pass through without zone enforcement.
		return msg, nil
	}

	// Annotate message with zone index for downstream use.
	msg.Metadata["zone_index"] = zone

	sessionKey := msg.Platform + ":" + msg.SessionID

	p.mu.Lock()
	state, exists := p.sessions[sessionKey]
	if !exists {
		state = &zoneState{highestZone: -1}
		p.sessions[sessionKey] = state
	}

	// Track highest zone seen.
	if zone > state.highestZone {
		state.highestZone = zone
	}

	// Track Index 0 anchor for Thinking Zone.
	if zone == ZoneThinking && !state.anchorSet {
		state.anchorSet = true
		msg.Metadata["zone_anchor"] = true // Mark as anchor – should never be evicted.
	}

	p.mu.Unlock()

	// Log zone transitions for debugging.
	p.logger.Debug("Zone order check",
		"event_type", eventType,
		"zone", zone,
		"session_key", sessionKey)

	return msg, nil
}

// ResetSession clears zone state for a session (call on session end).
func (p *ZoneOrderProcessor) ResetSession(platform, sessionID string) {
	p.mu.Lock()
	delete(p.sessions, platform+":"+sessionID)
	p.mu.Unlock()
}

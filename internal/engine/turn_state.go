package engine

import (
	"sync"
	"time"
)

// CleanupMsgRecord stores message metadata for cleanup
type CleanupMsgRecord struct {
	ChannelID string
	MessageTS string
	ZoneIndex int
	EventType string
}

// TurnState holds the state for a single turn in a session
// This allows concurrent turns to maintain independent cleanup records
type TurnState struct {
	TurnID             string
	CreatedAt          time.Time
	CleanupMsgRecords  []CleanupMsgRecord // Message records to be cleaned up at turn end
	mu                 sync.Mutex
}

// NewTurnState creates a new TurnState
func NewTurnState(turnID string) *TurnState {
	return &TurnState{
		TurnID:    turnID,
		CreatedAt: time.Now(),
	}
}

// AddCleanupMsg adds a message record to the cleanup list
func (t *TurnState) AddCleanupMsg(rec CleanupMsgRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.CleanupMsgRecords = append(t.CleanupMsgRecords, rec)
}

// GetAndClearCleanupMsgs returns and clears the cleanup records
// This is called when the turn completes to delete all tracked messages
func (t *TurnState) GetAndClearCleanupMsgs() []CleanupMsgRecord {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := t.CleanupMsgRecords
	t.CleanupMsgRecords = nil
	return result
}

// HasMessageTS checks if a message TS is already tracked
func (t *TurnState) HasMessageTS(ts string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, rec := range t.CleanupMsgRecords {
		if rec.MessageTS == ts {
			return true
		}
	}
	return false
}

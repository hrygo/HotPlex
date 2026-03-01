package engine

import (
	"sync"
	"time"
)

// maxSlidingMessages is the maximum number of messages to keep in the sliding window
const maxSlidingMessages = 5

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
	TurnID            string
	CreatedAt         time.Time
	CleanupMsgRecords []CleanupMsgRecord // Message records to be cleaned up at turn end
	mu                sync.Mutex
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

// Len returns the number of cleanup records
func (t *TurnState) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.CleanupMsgRecords)
}

// GetAllAndClear returns all records and clears them (thread-safe)
func (t *TurnState) GetAllAndClear() []CleanupMsgRecord {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := t.CleanupMsgRecords
	t.CleanupMsgRecords = nil
	return result
}

// EnforceSlidingWindow enforces the sliding window limit for a specific zone
func (t *TurnState) EnforceSlidingWindow(zone int, deleteFn func(CleanupMsgRecord)) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var zoneRecords []CleanupMsgRecord
	var otherRecords []CleanupMsgRecord

	// Split records into current zone and others
	for _, rec := range t.CleanupMsgRecords {
		if rec.ZoneIndex == zone {
			zoneRecords = append(zoneRecords, rec)
		} else {
			otherRecords = append(otherRecords, rec)
		}
	}

	if len(zoneRecords) <= maxSlidingMessages {
		return
	}

	// Find the oldest evictable record (skip startup messages in Zone 1)
	var toEvict CleanupMsgRecord
	var remainingInZone []CleanupMsgRecord
	found := false

	for _, rec := range zoneRecords {
		if !found && zone > 0 {
			// Protection: never evict startup markers from sliding window
			if rec.EventType == "session_start" || rec.EventType == "engine_starting" {
				remainingInZone = append(remainingInZone, rec)
				continue
			}
		}

		if !found {
			toEvict = rec
			found = true
			continue
		}
		remainingInZone = append(remainingInZone, rec)
	}

	if !found {
		return
	}

	// Rebuild the final records slice
	t.CleanupMsgRecords = append(remainingInZone, otherRecords...)

	// Delete evicted message
	if deleteFn != nil {
		deleteFn(toEvict)
	}
}

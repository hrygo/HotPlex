package dedup_test

import (
	"testing"
	"time"

	"github.com/hrygo/hotplex/chatapps/dedup"
)

func TestDeduplicator_Check(t *testing.T) {
	d := dedup.NewDeduplicator(30*time.Second, 10*time.Second)
	defer d.Shutdown()

	key := "test:event:1"

	// First check should return false (new event)
	if d.Check(key) {
		t.Error("First check should return false for new event")
	}

	// Second check should return true (duplicate)
	if !d.Check(key) {
		t.Error("Second check should return true for duplicate event")
	}

	// Third check should also return true (still within window)
	if !d.Check(key) {
		t.Error("Third check should return true for duplicate event")
	}
}

func TestDeduplicator_Cleanup(t *testing.T) {
	// Use very short window for testing
	d := dedup.NewDeduplicator(100*time.Millisecond, 50*time.Millisecond)
	defer d.Shutdown()

	key := "test:event:2"
	d.Check(key)

	if d.Size() != 1 {
		t.Errorf("Expected size 1, got %d", d.Size())
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Check should return false (expired)
	if d.Check(key) {
		t.Error("Check should return false for expired event")
	}
}

func TestSlackKeyStrategy_GenerateKey(t *testing.T) {
	s := dedup.NewSlackKeyStrategy()

	eventData := map[string]any{
		"platform":  "slack",
		"event_type": "app_mention",
		"channel":   "C123",
		"event_ts":  "1234567890.123456",
	}

	key := s.GenerateKey(eventData)
	expected := "slack:app_mention:C123:1234567890.123456"

	if key != expected {
		t.Errorf("Expected key %s, got %s", expected, key)
	}
}

func TestSlackKeyStrategy_GenerateKey_Fallback(t *testing.T) {
	s := dedup.NewSlackKeyStrategy()

	eventData := map[string]any{
		"platform":   "slack",
		"event_type": "message",
		"channel":    "C123",
		"session_id": "session-123",
	}

	key := s.GenerateKey(eventData)
	expected := "slack:message:C123:session-123"

	if key != expected {
		t.Errorf("Expected key %s, got %s", expected, key)
	}
}

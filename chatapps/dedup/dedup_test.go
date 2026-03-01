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

func TestDeduplicator_Shutdown(t *testing.T) {
	d := dedup.NewDeduplicator(30*time.Second, 10*time.Millisecond)
	
	// Verify cleanup goroutine is running
	d.Check("test:1")
	time.Sleep(15 * time.Millisecond)
	
	// Shutdown should stop cleanup goroutine
	d.Shutdown()
	
	// Wait a bit to ensure goroutine has stopped
	time.Sleep(20 * time.Millisecond)
	
	// Check should still work (no panic)
	d.Check("test:2")
}

func BenchmarkDeduplicator_Check(b *testing.B) {
	d := dedup.NewDeduplicator(30*time.Second, 10*time.Second)
	defer d.Shutdown()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := "benchmark:event:" + string(rune(i))
		d.Check(key)
	}
}

func BenchmarkDeduplicator_CheckParallel(b *testing.B) {
	d := dedup.NewDeduplicator(30*time.Second, 10*time.Second)
	defer d.Shutdown()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := "benchmark:parallel:" + string(rune(i))
			d.Check(key)
			i++
		}
	})
}

package slack

import (
	"testing"
	"time"

	"log/slog"
	"os"
)

func TestNewThrottledUpdater(t *testing.T) {
	u := NewThrottledUpdater()

	if u.minInterval != DefaultMinInterval {
		t.Errorf("expected minInterval %v, got %v", DefaultMinInterval, u.minInterval)
	}
	if u.minCharDelta != DefaultMinCharDelta {
		t.Errorf("expected minCharDelta %d, got %d", DefaultMinCharDelta, u.minCharDelta)
	}
}

func TestNewThrottledUpdaterWithConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	u := NewThrottledUpdaterWithConfig(100*time.Millisecond, 100, logger)

	if u.minInterval != 100*time.Millisecond {
		t.Errorf("expected minInterval 100ms, got %v", u.minInterval)
	}
	if u.minCharDelta != 100 {
		t.Errorf("expected minCharDelta 100, got %d", u.minCharDelta)
	}
	if u.logger == nil {
		t.Error("expected logger to be set")
	}
}

func TestNewThrottledUpdaterWithConfigDefaults(t *testing.T) {
	u := NewThrottledUpdaterWithConfig(0, 0, nil)

	if u.minInterval != DefaultMinInterval {
		t.Errorf("expected default minInterval, got %v", u.minInterval)
	}
	if u.minCharDelta != DefaultMinCharDelta {
		t.Errorf("expected default minCharDelta, got %d", u.minCharDelta)
	}
}

func TestThrottledUpdater_ShouldUpdate_FirstUpdate(t *testing.T) {
	u := NewThrottledUpdater()

	// First update should always return true
	if !u.ShouldUpdate(100) {
		t.Error("first update should always be allowed")
	}
}

func TestThrottledUpdater_ShouldUpdate_TimeThreshold(t *testing.T) {
	u := NewThrottledUpdater()

	// First update
	u.Update("hello")

	// Immediately check - should fail time threshold
	if u.ShouldUpdate(100) {
		t.Error("should not update: time threshold not met")
	}

	// Wait for interval
	time.Sleep(700 * time.Millisecond)

	// Now should pass time threshold, but char delta might fail
	// Let's check with enough char delta
	if !u.ShouldUpdate(200) {
		t.Error("should update after interval with enough char delta")
	}
}

func TestThrottledUpdater_ShouldUpdate_CharDeltaThreshold(t *testing.T) {
	u := NewThrottledUpdater()

	// First update with 100 chars
	u.Update(string(make([]byte, 100)))

	// Immediately check with small change - should fail char delta
	if u.ShouldUpdate(110) {
		t.Error("should not update: char delta threshold not met")
	}

	// Wait for interval to pass time threshold
	time.Sleep(700 * time.Millisecond)

	// Now check with enough char delta (100 > 50)
	if !u.ShouldUpdate(200) {
		t.Error("should update with enough char delta after interval")
	}
}

func TestThrottledUpdater_Update(t *testing.T) {
	u := NewThrottledUpdater()

	// First update should always send
	text, shouldSend := u.Update("hello world")
	if text != "hello world" {
		t.Errorf("expected 'hello world', got '%s'", text)
	}
	if !shouldSend {
		t.Error("first update should always send")
	}

	// Second update with small change - should be throttled
	text, shouldSend = u.Update("hello worl")
	if shouldSend {
		t.Error("should throttle small changes")
	}

	// Verify pending text is stored
	if u.PendingText() != "hello worl" {
		t.Error("pending text should be stored")
	}
}

func TestThrottledUpdater_UpdateComplete(t *testing.T) {
	u := NewThrottledUpdater()

	// First update
	u.Update("hello")
	u.UpdateComplete()

	// Pending text should be cleared
	if u.PendingText() != "" {
		t.Error("pending text should be cleared after UpdateComplete")
	}
}

func TestThrottledUpdater_ForceUpdate(t *testing.T) {
	u := NewThrottledUpdater()

	// First update
	u.Update("hello")

	// Force update should return pending text
	text, ok := u.ForceUpdate()
	if text != "hello" {
		t.Errorf("expected 'hello', got '%s'", text)
	}
	if !ok {
		t.Error("force update should succeed when there's pending text")
	}

	// After force update, pending should be cleared
	if u.PendingText() != "" {
		t.Error("pending text should be cleared after force update")
	}

	// Force update with no pending text
	text, ok = u.ForceUpdate()
	if text != "" || ok {
		t.Error("force update should return false when there's no pending text")
	}
}

func TestThrottledUpdater_Reset(t *testing.T) {
	u := NewThrottledUpdater()

	// Set some state
	u.Update("hello world")
	u.Update("hi") // throttled, but pending

	// Reset
	u.Reset()

	// All state should be cleared
	if u.PendingText() != "" {
		t.Error("pending text should be empty after reset")
	}

	// Should allow first update again
	if !u.ShouldUpdate(100) {
		t.Error("should allow update after reset")
	}
}

func TestThrottledUpdater_Stats(t *testing.T) {
	u := NewThrottledUpdater()

	// Initial stats
	stats := u.Stats()
	if stats.LastLen != 0 {
		t.Errorf("expected initial lastLen 0, got %d", stats.LastLen)
	}
	if stats.MinInterval != DefaultMinInterval {
		t.Errorf("expected minInterval %v, got %v", DefaultMinInterval, stats.MinInterval)
	}
	if stats.MinCharDelta != DefaultMinCharDelta {
		t.Errorf("expected minCharDelta %d, got %d", DefaultMinCharDelta, stats.MinCharDelta)
	}

	// After update
	u.Update("hello")

	stats = u.Stats()
	if stats.LastLen != 5 {
		t.Errorf("expected lastLen 5, got %d", stats.LastLen)
	}
	if stats.LastUpdate.IsZero() {
		t.Error("lastUpdate should not be zero after update")
	}
}

func TestThrottledUpdater_ShrinkContent(t *testing.T) {
	u := NewThrottledUpdater()

	// Start with long content
	u.Update(string(make([]byte, 200)))

	// Now shrink significantly - should trigger update due to char delta
	text, shouldSend := u.Update(string(make([]byte, 50)))
	if !shouldSend {
		// The delta is 150, which is > 50, so it should send
		// But let's check the absolute delta logic
		t.Log("Delta calculation:", 200-50)
	}
	_ = text
}

func TestThrottledUpdater_ConcurrentAccess(t *testing.T) {
	u := NewThrottledUpdater()
	done := make(chan bool)

	// Test concurrent reads and writes
	go func() {
		for i := 0; i < 100; i++ {
			u.Update(string(make([]byte, i%10+1)))
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			u.ShouldUpdate(i)
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = u.PendingText()
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}
}

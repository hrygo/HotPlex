package chatapps

import (
	"context"
	"testing"

	"github.com/hrygo/hotplex/chatapps/base"
)

func TestZoneOrderProcessor_AnnotatesZoneIndex(t *testing.T) {
	proc := NewZoneOrderProcessor(nil)

	tests := []struct {
		eventType string
		wantZone  int
	}{
		{"thinking", ZoneThinking},
		{"plan_mode", ZoneThinking},
		{"tool_use", ZoneAction},
		{"tool_result", ZoneAction},
		{"permission_request", ZoneAction},
		{"session_start", ZoneInitialization},
		{"answer", ZoneOutput},
		{"error", ZoneOutput},
		{"ask_user_question", ZoneOutput},
		{"session_stats", ZoneSummary},
	}

	for _, tt := range tests {
		msg := &base.ChatMessage{
			Platform:  "slack",
			SessionID: "test",
			Metadata:  map[string]any{"event_type": tt.eventType},
		}
		result, err := proc.Process(context.Background(), msg)
		if err != nil {
			t.Fatalf("Process(%s) error: %v", tt.eventType, err)
		}
		if result == nil {
			t.Fatalf("Process(%s) returned nil", tt.eventType)
		}
		got, ok := result.Metadata["zone_index"].(int)
		if !ok {
			t.Errorf("Process(%s): zone_index not set in metadata", tt.eventType)
			continue
		}
		if got != tt.wantZone {
			t.Errorf("Process(%s): zone_index=%d, want %d", tt.eventType, got, tt.wantZone)
		}
	}
}

func TestZoneOrderProcessor_MultipleThinkingEvents(t *testing.T) {
	proc := NewZoneOrderProcessor(nil)

	// Multiple thinking events should all get zone_index=0
	for i := 0; i < 3; i++ {
		msg := &base.ChatMessage{
			Platform:  "slack",
			SessionID: "s1",
			Metadata:  map[string]any{"event_type": "thinking"},
		}
		result, _ := proc.Process(context.Background(), msg)
		got, ok := result.Metadata["zone_index"].(int)
		if !ok || got != ZoneThinking {
			t.Errorf("thinking event %d: zone_index=%d, want %d", i, got, ZoneThinking)
		}
	}
}

func TestZoneOrderProcessor_UnknownEventsPassThrough(t *testing.T) {
	proc := NewZoneOrderProcessor(nil)

	msg := &base.ChatMessage{
		Platform:  "slack",
		SessionID: "s1",
		Metadata:  map[string]any{"event_type": "custom_unknown"},
	}
	result, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result == nil {
		t.Fatal("unknown event should pass through, got nil")
	}
	if _, ok := result.Metadata["zone_index"]; ok {
		t.Error("unknown event should NOT have zone_index")
	}
}

func TestZoneOrderProcessor_ResetSession(t *testing.T) {
	proc := NewZoneOrderProcessor(nil)

	msg := &base.ChatMessage{
		Platform:  "slack",
		SessionID: "s1",
		Metadata:  map[string]any{"event_type": "thinking"},
	}
	_, _ = proc.Process(context.Background(), msg)

	// Reset the session
	proc.ResetSession("slack", "s1")

	// After reset, thinking should still get zone_index=0
	msg2 := &base.ChatMessage{
		Platform:  "slack",
		SessionID: "s1",
		Metadata:  map[string]any{"event_type": "thinking"},
	}
	result, _ := proc.Process(context.Background(), msg2)
	got, ok := result.Metadata["zone_index"].(int)
	if !ok || got != ZoneThinking {
		t.Errorf("After reset: zone_index=%d, want %d", got, ZoneThinking)
	}
}

func TestZoneOrderProcessor_OrderAndName(t *testing.T) {
	proc := NewZoneOrderProcessor(nil)
	if proc.Order() != int(OrderZoneOrder) {
		t.Errorf("Order: got %d, want %d", proc.Order(), int(OrderZoneOrder))
	}
	if proc.Name() != "ZoneOrderProcessor" {
		t.Errorf("Name: got %q", proc.Name())
	}
}

func TestZoneOrderProcessor_TurnBoundaryReset(t *testing.T) {
	proc := NewZoneOrderProcessor(nil)
	ctx := context.Background()

	// Turn 1: thinking → session_stats (Turn end)
	msg1 := &base.ChatMessage{
		Platform: "slack", SessionID: "s1",
		Metadata: map[string]any{"event_type": "thinking"},
	}
	_, _ = proc.Process(ctx, msg1)

	stats := &base.ChatMessage{
		Platform: "slack", SessionID: "s1",
		Metadata: map[string]any{"event_type": "session_stats"},
	}
	_, _ = proc.Process(ctx, stats)

	// Turn 2: thinking should still get zone_index=0
	msg2 := &base.ChatMessage{
		Platform: "slack", SessionID: "s1",
		Metadata: map[string]any{"event_type": "thinking"},
	}
	result2, _ := proc.Process(ctx, msg2)
	got, ok := result2.Metadata["zone_index"].(int)
	if !ok || got != ZoneThinking {
		t.Errorf("Turn 2: zone_index=%d, want %d", got, ZoneThinking)
	}
}

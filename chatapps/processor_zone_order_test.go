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
		{"session_start", ZoneAction},
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

func TestZoneOrderProcessor_AnchorFirstThinking(t *testing.T) {
	proc := NewZoneOrderProcessor(nil)

	// First thinking event should be marked as anchor
	msg1 := &base.ChatMessage{
		Platform:  "slack",
		SessionID: "s1",
		Metadata:  map[string]any{"event_type": "thinking"},
	}
	result, _ := proc.Process(context.Background(), msg1)
	if anchor, ok := result.Metadata["zone_anchor"].(bool); !ok || !anchor {
		t.Error("First thinking event should have zone_anchor=true")
	}

	// Second thinking event should NOT have anchor
	msg2 := &base.ChatMessage{
		Platform:  "slack",
		SessionID: "s1",
		Metadata:  map[string]any{"event_type": "thinking"},
	}
	result2, _ := proc.Process(context.Background(), msg2)
	if _, ok := result2.Metadata["zone_anchor"]; ok {
		t.Error("Second thinking event should NOT have zone_anchor")
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

	// Next thinking should be anchor again
	msg2 := &base.ChatMessage{
		Platform:  "slack",
		SessionID: "s1",
		Metadata:  map[string]any{"event_type": "thinking"},
	}
	result, _ := proc.Process(context.Background(), msg2)
	if anchor, ok := result.Metadata["zone_anchor"].(bool); !ok || !anchor {
		t.Error("After reset, first thinking should be anchor again")
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

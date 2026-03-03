package chatapps

import (
	"context"
	"testing"

	"github.com/hrygo/hotplex/chatapps/base"
)

func TestMessageFilterProcessor_FilterHiddenEvents(t *testing.T) {
	filter := NewMessageFilterProcessor(nil)

	hidden := []string{"system", "user", "raw"}
	for _, ev := range hidden {
		msg := &base.ChatMessage{
			Platform:  "slack",
			SessionID: "s1",
			Content:   "noise",
			Metadata:  map[string]any{"event_type": ev},
		}
		result, err := filter.Process(context.Background(), msg)
		if err != nil {
			t.Fatalf("Process(%s) error: %v", ev, err)
		}
		if result != nil {
			t.Errorf("Expected event_type=%q to be filtered (nil), got non-nil", ev)
		}
	}
}

func TestMessageFilterProcessor_PassNonHiddenEvents(t *testing.T) {
	filter := NewMessageFilterProcessor(nil)

	allowed := []string{"thinking", "tool_use", "tool_result", "answer", "error", "session_stats", "permission_request", "user_message_received"}
	for _, ev := range allowed {
		msg := &base.ChatMessage{
			Platform:  "slack",
			SessionID: "s1",
			Content:   "data",
			Metadata:  map[string]any{"event_type": ev},
		}
		result, err := filter.Process(context.Background(), msg)
		if err != nil {
			t.Fatalf("Process(%s) error: %v", ev, err)
		}
		if result == nil {
			t.Errorf("Expected event_type=%q to pass through, got nil", ev)
		}
	}
}

func TestMessageFilterProcessor_PassNilAndNoMetadata(t *testing.T) {
	filter := NewMessageFilterProcessor(nil)

	// nil message
	result, err := filter.Process(context.Background(), nil)
	if err != nil || result != nil {
		t.Errorf("Expected nil for nil input, got result=%v err=%v", result, err)
	}

	// message without metadata
	msg := &base.ChatMessage{Content: "hi"}
	result, err = filter.Process(context.Background(), msg)
	if err != nil || result == nil {
		t.Errorf("Expected pass-through for no metadata, got result=%v err=%v", result, err)
	}
}

func TestMessageFilterProcessor_OrderAndName(t *testing.T) {
	filter := NewMessageFilterProcessor(nil)
	if filter.Order() != int(OrderFilter) {
		t.Errorf("Order: got %d, want %d", filter.Order(), int(OrderFilter))
	}
	if filter.Name() != "MessageFilterProcessor" {
		t.Errorf("Name: got %q", filter.Name())
	}
}

package slack

import (
	"testing"

	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

func TestHandleAppHomeOpenedEvent(t *testing.T) {
	adapter := newTestAdapter()
	
	// Test without appHomeHandler (should not panic)
	event := &slackevents.AppHomeOpenedEvent{
		User:    "U123",
		Channel: "C123",
		Tab:     "home",
	}
	adapter.handleAppHomeOpenedEvent("T123", event)
}

func TestHandleSocketModeMessageEvent(t *testing.T) {
	adapter := newTestAdapter()
	
	// Test with bot message (should return early)
	event := &slackevents.MessageEvent{
		BotID:   "B123",
		User:    "U123",
		Channel: "C123",
		Text:    "Hello",
	}
	adapter.handleSocketModeMessageEvent("T123", event)
}

func TestHandleSocketModeMessageEvent_EmptyText(t *testing.T) {
	adapter := newTestAdapter()
	
	// Test with empty text (should return early)
	event := &slackevents.MessageEvent{
		User:    "U123",
		Channel: "C123",
		Text:   "",
	}
	adapter.handleSocketModeMessageEvent("T123", event)
}

func TestHandleSocketModeMessageEvent_MessageChanged(t *testing.T) {
	adapter := newTestAdapter()
	
	// Test with message_changed subtype (should return early)
	event := &slackevents.MessageEvent{
		User:    "U123",
		Channel: "C123",
		Text:    "Hello",
		SubType: "message_changed",
	}
	adapter.handleSocketModeMessageEvent("T123", event)
}

func TestHandleSocketModeSlashCommand(t *testing.T) {
	adapter := newTestAdapter()
	
	// Test slash command handling - create a minimal socketmode.Event
	evt := socketmode.Event{
		Type: socketmode.EventTypeSlashCommand,
	}
	adapter.handleSocketModeSlashCommand(evt)
}

func TestHandleSocketModeInteractive(t *testing.T) {
	adapter := newTestAdapter()
	
	// Test interactive callback handling - create a minimal socketmode.Event
	evt := socketmode.Event{
		Type: socketmode.EventTypeInteractive,
	}
	adapter.handleSocketModeInteractive(evt)
}

func TestHandleSocketModeEventsAPI(t *testing.T) {
	adapter := newTestAdapter()
	
	// Test EventsAPI callback handling - create a minimal socketmode.Event
	evt := socketmode.Event{
		Type: socketmode.EventTypeEventsAPI,
	}
	adapter.handleSocketModeEventsAPI(evt)
}

func TestHandleSocketModeEvent(t *testing.T) {
	adapter := newTestAdapter()
	
	// Test with nil event (should handle gracefully)
	evt := socketmode.Event{}
	adapter.handleSocketModeEvent(evt)
}

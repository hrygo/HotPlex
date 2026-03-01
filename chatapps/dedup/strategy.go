package dedup

// KeyStrategy defines the interface for generating deduplication keys
type KeyStrategy interface {
	// GenerateKey generates a deduplication key from event data
	GenerateKey(eventData map[string]any) string
}

// SlackKeyStrategy implements KeyStrategy for Slack events
type SlackKeyStrategy struct{}

// NewSlackKeyStrategy creates a new Slack key strategy
func NewSlackKeyStrategy() *SlackKeyStrategy {
	return &SlackKeyStrategy{}
}

// GenerateKey generates a deduplication key for Slack events
// Format: {platform}:{event_type}:{channel}:{event_ts}
func (s *SlackKeyStrategy) GenerateKey(eventData map[string]any) string {
	platform, _ := eventData["platform"].(string)
	eventType, _ := eventData["event_type"].(string)
	channel, _ := eventData["channel"].(string)
	eventTS, _ := eventData["event_ts"].(string)

	// Fallback for missing event_type (common in early lifecycle or simple messages)
	if eventType == "" {
		if msgType, ok := eventData["type"].(string); ok && msgType != "" {
			eventType = msgType
		} else {
			eventType = "unknown_event"
		}
	}

	// Fallback for missing channel
	if channel == "" {
		channel = "unknown_channel"
	}

	// Fallback to session_id if event_ts is not available
	if eventTS == "" {
		sessionID, _ := eventData["session_id"].(string)
		if sessionID == "" {
			sessionID = "unknown_session"
		}
		return platform + ":" + eventType + ":" + channel + ":" + sessionID
	}

	return platform + ":" + eventType + ":" + channel + ":" + eventTS
}

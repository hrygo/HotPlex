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

	// Fallback to session_id if event_ts is not available
	if eventTS == "" {
		sessionID, _ := eventData["session_id"].(string)
		return platform + ":" + eventType + ":" + channel + ":" + sessionID
	}

	return platform + ":" + eventType + ":" + channel + ":" + eventTS
}

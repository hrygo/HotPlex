package groupchat

// RoundRobinSelector selects the next speaker in round-robin order.
type RoundRobinSelector struct{}

// Next returns the bot ID of the next speaker.
// turnNum is the 1-based turn number.
// participants is the ordered list of bot IDs.
func (r *RoundRobinSelector) Next(turnNum int, participants []string) string {
	if len(participants) == 0 {
		return ""
	}
	idx := (turnNum - 1) % len(participants)
	return participants[idx]
}

// RemoveFromParticipants returns a new slice without the specified bot.
// Returns the original slice if the bot is not found.
func RemoveFromParticipants(participants []string, botID string) []string {
	for i, id := range participants {
		if id == botID {
			return append(participants[:i], participants[i+1:]...)
		}
	}
	return participants
}

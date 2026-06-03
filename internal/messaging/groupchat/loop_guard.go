package groupchat

import "context"

// TerminateCheck evaluates whether the turn loop should stop.
type TerminateCheck struct {
	MaxTurns          int
	CostLimitUSD      float64
	MaxConsecutiveTMO int // consecutive timeouts before bot removal (default: 2)
}

// ShouldTerminate checks all termination conditions and returns a reason
// if the loop should stop. Returns empty EndReason if the loop should continue.
func (g *TerminateCheck) ShouldTerminate(ctx context.Context, gs *GroupSession, recentTurns []*TurnRecord) EndReason {
	// 1. Context cancellation (user stopped or gateway restart).
	if ctx.Err() != nil {
		return EndUserStopped
	}

	// 2. Max turns reached.
	if g.MaxTurns > 0 && gs.TurnCount >= g.MaxTurns {
		return EndMaxTurns
	}

	// 3. Cost limit exceeded.
	if g.CostLimitUSD > 0 && gs.CostAccumulated >= g.CostLimitUSD {
		return EndCostLimit
	}

	// 4. All participants skipped (check last N turns where N = len(participants)).
	n := len(gs.BotIDs)
	if n > 0 && len(recentTurns) >= n {
		allSkipped := true
		for _, t := range recentTurns[len(recentTurns)-n:] {
			if !t.Skipped {
				allSkipped = false
				break
			}
		}
		if allSkipped {
			return EndAllSkip
		}
	}

	return ""
}

// ShouldEvictBot checks if a bot should be removed from the discussion
// due to consecutive timeouts.
func (g *TerminateCheck) ShouldEvictBot(botID string, recentTurns []*TurnRecord) bool {
	maxConsec := g.MaxConsecutiveTMO
	if maxConsec <= 0 {
		maxConsec = 2
	}

	consec := 0
	for i := len(recentTurns) - 1; i >= 0; i-- {
		t := recentTurns[i]
		if t.BotID != botID {
			continue
		}
		if t.TimeoutCount > 0 {
			consec++
			if consec >= maxConsec {
				return true
			}
		} else {
			break
		}
	}
	return false
}

package groupchat

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// GroupState tracks the lifecycle of a group chat session.
type GroupState string

const (
	GroupStateActive         GroupState = "active"
	GroupStateCompleted      GroupState = "completed"
	GroupStateStopped        GroupState = "stopped"
	GroupStateError          GroupState = "error"
	GroupStateGatewayRestart GroupState = "gateway_restart"
)

// EndReason explains why a group chat ended.
type EndReason string

const (
	EndMaxTurns       EndReason = "max_turns"
	EndCostLimit      EndReason = "cost_limit"
	EndAllSkip        EndReason = "all_skip"
	EndUserStopped    EndReason = "user_stopped"
	EndGatewayRestart EndReason = "gateway_restart"
	EndError          EndReason = "error"
	EndConsecutiveTMO EndReason = "consecutive_timeout"
)

// GroupSession represents an active group chat discussion.
type GroupSession struct {
	ID              string
	Topic           string
	Platform        string
	ChannelID       string
	ThreadTS        string
	OwnerID         string
	Initiator       string
	BotIDs          []string          // ordered list of participating bot IDs
	BotNames        map[string]string // botID → display name
	MaxTurns        int
	TurnCount       int
	CostLimitUSD    float64
	CostAccumulated float64
	TurnTimeoutSec  int
	CooldownMS      int
	State           GroupState
	EndReason       EndReason
	CreatedAt       time.Time
	UpdatedAt       time.Time
	EndedAt         *time.Time
}

// NewGroupSession creates a new group session with a deterministic ID.
func NewGroupSession(topic, platform, channelID, threadTS, ownerID string, botIDs []string, cfg Config) *GroupSession {
	now := time.Now()
	gs := &GroupSession{
		ID:             uuid.NewSHA1(uuid.NameSpaceURL, fmt.Appendf(nil, "groupchat:%s:%s:%d", ownerID, topic, now.UnixNano())).String(),
		Topic:          topic,
		Platform:       platform,
		ChannelID:      channelID,
		ThreadTS:       threadTS,
		OwnerID:        ownerID,
		BotIDs:         botIDs,
		BotNames:       make(map[string]string),
		MaxTurns:       cfg.MaxTurns,
		CostLimitUSD:   cfg.CostLimitUSD,
		TurnTimeoutSec: cfg.TurnTimeoutSec,
		CooldownMS:     cfg.CooldownMS,
		State:          GroupStateActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	return gs
}

// TurnRecord captures one bot's response in a group discussion turn.
type TurnRecord struct {
	ID             string
	GroupID        string
	BotID          string
	BotName        string
	TurnNum        int
	Content        string
	Skipped        bool
	Sanitized      bool
	SanitizeReason string
	TimeoutCount   int
	CostUSD        float64
	Err            error
	CreatedAt      time.Time
}

// AuditEvent records a group chat event for auditing.
type AuditEvent struct {
	EventType string
	SessionID string
	BotID     string
	Initiator string
	TurnNum   int
	Detail    string
	CreatedAt time.Time
}

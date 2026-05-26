package messaging

import (
	"context"

	"github.com/hrygo/hotplex/pkg/events"
)

// PlatformAdapterInterface is the minimal interface that all platform adapters must implement.
type PlatformAdapterInterface interface {
	// Platform returns the platform type identifier.
	Platform() PlatformType

	// Start initiates the platform connection.
	// It must be non-blocking: long-running setup runs in background goroutines.
	Start(ctx context.Context) error

	// HandleTextMessage processes an incoming text message from the platform.
	// The adapter maps the platform message to an AEP Envelope and delegates to PlatformBridge.Handle.
	// teamID and threadTS are optional; adapters that don't use them should ignore them.
	HandleTextMessage(ctx context.Context, platformMsgID, channelID, teamID, threadTS, userID, text string) error

	// Close gracefully terminates the platform connection.
	Close(ctx context.Context) error

	// ConfigureWith applies a unified configuration to the adapter.
	ConfigureWith(config AdapterConfig) error

	// GetBotID returns the platform bot identity (Slack UserID, Feishu OpenID, etc.).
	GetBotID() string
}

// CronResultSender sends a cron job execution result to a platform target.
type CronResultSender interface {
	SendCronResult(ctx context.Context, text string, platformKey map[string]string) error
}

// HubInterface is the subset of gateway.Hub methods needed by platform adapters.
type HubInterface interface {
	JoinPlatformSession(sessionID string, pc PlatformConn)
	NextSeq(sessionID string) int64
}

// HandlerInterface is the subset of gateway.Handler methods needed by platform adapters.
type HandlerInterface interface {
	Handle(ctx context.Context, env *events.Envelope) error
}

// SessionStarter creates a new gateway session for a platform message.
// Implemented by gateway.Bridge and injected during wiring.
type SessionStarter interface {
	StartPlatformSession(ctx context.Context, sessionID, ownerID, workerType, workDir, platform string, platformKey map[string]string, botID string) error
}

package messaging

import (
	"log/slog"
	"sync/atomic"
	"time"
)

// PlatformAdapter is the base type for all messaging platform adapters.
// Each adapter embeds this struct and implements Start, HandleTextMessage, and Close.
type PlatformAdapter struct {
	Log *slog.Logger

	hub     HubInterface
	handler HandlerInterface
	bridge  *Bridge

	// Shared adapter state (promoted from Slack/Feishu adapters).
	started          atomic.Bool
	closed           atomic.Bool
	Dedup            *Dedup
	Gate             *Gate
	Interactions     *InteractionManager
	BackoffBaseDelay time.Duration
	BackoffMaxDelay  time.Duration
}

// Bridge returns the messaging bridge.
func (a *PlatformAdapter) Bridge() *Bridge { return a.bridge }

// ConfigureWith sets the common adapter dependencies from config.
func (a *PlatformAdapter) ConfigureWith(config AdapterConfig) error {
	a.hub = config.Hub
	a.handler = config.Handler
	a.bridge = config.Bridge
	return nil
}

// ConfigureShared extracts common adapter config fields (gate, backoff delays).
func (a *PlatformAdapter) ConfigureShared(config AdapterConfig) {
	if config.Gate != nil {
		a.Gate = config.Gate
	}
	if bd := config.ExtrasDuration("reconnect_base_delay"); bd > 0 {
		a.BackoffBaseDelay = bd
	}
	if md := config.ExtrasDuration("reconnect_max_delay"); md > 0 {
		a.BackoffMaxDelay = md
	}
}

// StartGuard atomically marks the adapter as started. Returns true on the
// first call, false on subsequent calls.
func (a *PlatformAdapter) StartGuard() bool { return a.started.CompareAndSwap(false, true) }

// MarkClosed marks the adapter as closed.
func (a *PlatformAdapter) MarkClosed() { a.closed.Store(true) }

// IsClosed reports whether the adapter has been closed.
func (a *PlatformAdapter) IsClosed() bool { return a.closed.Load() }

// InitSharedState creates the default Dedup (5000 entries, 12h TTL) and
// InteractionManager. Adapters with custom dedup params should overwrite
// a.Dedup after calling this.
func (a *PlatformAdapter) InitSharedState() {
	a.Dedup = NewDedup(0, 0)
	a.Dedup.StartCleanup()
	a.Interactions = NewInteractionManager(a.Log)
}

// CloseSharedState closes the shared dedup tracker. Safe to call multiple times.
func (a *PlatformAdapter) CloseSharedState() {
	if a.Dedup != nil {
		a.Dedup.Close()
		a.Dedup = nil
	}
}

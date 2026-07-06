// Package audit implements the User Behavior Audit System (issue #833, spec §5).
// It is an independent, append-only audit log with sha256 hash chain integrity,
// zero-loss collector with WAL spill, and pluggable AlertSink fan-out.
package audit

import (
	"context"
	"time"
)

// Action constants for audit events. See spec §5.2.
const (
	ActionAuthLogin                = "auth.login"
	ActionAuthTokenValidated       = "auth.token_validated"
	ActionAuthAPIKeyUsed           = "auth.apikey_used"
	ActionAuthDenied               = "auth.denied"
	ActionSessionCreate            = "session.create"
	ActionSessionTerminate         = "session.terminate"
	ActionSessionDelete            = "session.delete"
	ActionMessageInbound           = "message.inbound"
	ActionToolCall                 = "tool.call" // P2 - reserved
	ActionSystemAuditConfigChanged = "system.audit_config_changed"
	ActionSystemAuditExport        = "system.audit_export"
)

// Outcome constants. See spec §5.1.
const (
	OutcomeSuccess = "success"
	OutcomeFailure = "failure"
	OutcomeDenied  = "denied"
)

// UserIDType constants. See spec §5.4.
const (
	UserIDTypePlatform   = "platform"   // feishu open_id, slack user_id
	UserIDTypeRegistered = "registered" // webchat UUID
	UserIDTypeAnonymous  = "anonymous"  // unauthenticated attacker
	UserIDTypeSystem     = "system"     // cronjob, system process
)

// Platform constants. See spec §5.4.
const (
	PlatformWebChat = "webchat"
	PlatformFeishu  = "feishu"
	PlatformSlack   = "slack"
	PlatformCron    = "cron"
	PlatformAdmin   = "admin"
	PlatformAPI     = "api"
)

// AnonymousUserID is the sentinel used for unauthenticated events.
// Per spec §5.4: ip + user_agent MUST be populated alongside this.
const AnonymousUserID = "anonymous"

// UserActivity is the row stored in the user_activity table.
// Mirrors the 16-column schema in spec §5.1.
type UserActivity struct {
	ID           int64  // assigned by DB
	Ts           int64  // Unix ms
	UserID       string // NOT NULL
	UserIDType   string // NOT NULL
	Platform     string // NOT NULL
	SessionID    string // empty for admin/api
	Action       string // NOT NULL
	ResourceType string // optional
	ResourceID   string // optional
	Outcome      string // NOT NULL: success/failure/denied
	DetailJSON   string // NOT NULL: whitelisted fields per §5.9
	EventRef     string // optional: events.id or turns.id
	IP           string // optional
	UserAgent    string // optional
	PrevHash     string // NOT NULL: "" for genesis
	SelfHash     string // NOT NULL: sha256(PrevHash || canonical(rest))
}

// Checkpoint is the rebase anchor written before pruning.
// See spec §5.5.
type Checkpoint struct {
	ID           int64
	PrunedAt     time.Time
	LastSelfHash string
	NextID       int64 // id of the first surviving row
}

// AuditEvent is the read-only snapshot passed to AlertSink.
// See spec §5.6.
type AuditEvent struct {
	EventID      string // UUIDv7 - stable, sink dedup
	Ts           time.Time
	UserID       string
	UserIDType   string
	Platform     string
	SessionID    string
	Action       string
	ResourceType string
	ResourceID   string
	Outcome      string
	Detail       map[string]any
	EventRef     string
	IP           string
	UserAgent    string
}

// AlertSink is the extension point for real-time alerting. See spec §5.6.
// Implementations must be non-blocking (internal async) — failures must NOT
// affect the main audit write path.
type AlertSink interface {
	OnAuditEvent(ctx context.Context, e AuditEvent) error
}

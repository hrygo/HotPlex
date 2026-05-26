package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hrygo/hotplex/internal/dbutil"
)

// Compile-time check that pgStore satisfies ChatAccessStorer.
var _ ChatAccessStorer = (*pgStore)(nil)

// pgStore provides dedup + cooldown + persistence for chat-entered events,
// backed by PostgreSQL.
type pgStore struct {
	db      *dbutil.DB
	dialect dbutil.Dialect
	log     *slog.Logger
}

// NewChatAccessPGStore creates a store backed by the given PostgreSQL connection.
func NewChatAccessPGStore(db *dbutil.DB, log *slog.Logger) ChatAccessStorer {
	return &pgStore{
		db:      db,
		dialect: db.Dialect(),
		log:     log,
	}
}

// Record inserts the event. Returns false (with nil error) when event_id
// already exists (duplicate event from the platform).
func (s *pgStore) Record(ctx context.Context, r ChatAccessRecord) (bool, error) {
	now := time.Now().Unix()
	r.CreatedAt = now
	var inserted bool

	res, err := s.db.ExecContext(ctx,
		s.dialect.Rebind(
			`INSERT INTO chat_access_events (event_id, platform, chat_id, user_id, bot_id, last_message_at, welcome_sent, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		),
		r.EventID, r.Platform, r.ChatID, r.UserID, r.BotID, r.LastMessageAt, s.dialect.BoolValue(r.WelcomeSent), r.CreatedAt,
	)
	if err != nil {
		if s.dialect.IsUniqueViolation(err) {
			return false, nil
		}
		return false, fmt.Errorf("chat_access insert: %w", err)
	}
	n, _ := res.RowsAffected()
	inserted = n > 0
	return inserted, nil
}

// Classify determines whether a welcome should be sent.
//
// Feishu: pass lastMessageAtMs from the event payload.
// Slack:  pass 0; the function falls back to the DB.
func (s *pgStore) Classify(ctx context.Context, platform, chatID, botID, userID string, lastMessageAtMs int64) ChatAccessType {
	cooldown := int64(3600) // 1 h debounce window in seconds.

	// Check recent row for (platform, chat_id, bot_id).
	var lastCreatedAt int64
	err := s.db.QueryRowContext(ctx,
		s.dialect.Rebind(
			`SELECT created_at FROM chat_access_events
			 WHERE platform = ? AND chat_id = ? AND bot_id = ?
			 ORDER BY created_at DESC LIMIT 1`,
		),
		platform, chatID, botID,
	).Scan(&lastCreatedAt)

	if err != nil {
		// No prior record — first contact, always welcome.
		return ChatAccessNew
	}

	// Existing record — check cooldown.
	now := time.Now().Unix()
	if now-lastCreatedAt < cooldown {
		return ChatAccessActive
	}

	// Outside cooldown — check activity level.

	// Feishu fast-path: use event payload timestamp directly.
	if lastMessageAtMs > 0 {
		lastSec := lastMessageAtMs / 1000
		since := now - lastSec
		if since > 3600 {
			return ChatAccessReturning
		}
		return ChatAccessActive
	}

	// Slack path: check user's last recorded activity.
	var lastAct int64
	err = s.db.QueryRowContext(ctx,
		s.dialect.Rebind(
			`SELECT COALESCE(MAX(created_at), 0) FROM chat_access_events
			 WHERE platform = ? AND user_id = ? AND bot_id = ?`,
		),
		platform, userID, botID,
	).Scan(&lastAct)
	if err != nil {
		return ChatAccessReturning
	}
	since := now - lastAct
	if since > 3600 {
		return ChatAccessReturning
	}
	return ChatAccessActive
}

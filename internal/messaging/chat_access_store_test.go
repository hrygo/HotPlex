package messaging

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/sqlutil"
)

func init() {
	_ = sqlutil.DriverName
}

type testChatStore struct {
	*ChatAccessStore
	db *sql.DB
}

func newTestChatAccessStore(t *testing.T) *testChatStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open(sqlutil.DriverName, dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS chat_access_events (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		event_id        TEXT NOT NULL UNIQUE,
		platform        TEXT NOT NULL CHECK(platform IN ('feishu', 'slack')),
		chat_id         TEXT NOT NULL,
		user_id         TEXT NOT NULL,
		bot_id          TEXT NOT NULL DEFAULT '',
		last_message_at INTEGER NOT NULL DEFAULT 0,
		welcome_sent    INTEGER NOT NULL DEFAULT 0 CHECK(welcome_sent IN (0, 1)),
		created_at      INTEGER NOT NULL
	)`)
	require.NoError(t, err)

	return &testChatStore{
		ChatAccessStore: NewChatAccessStore(db, nil, sqlutil.NewWriteMu()),
		db:              db,
	}
}

func TestChatAccessStore_Record(t *testing.T) {
	s := newTestChatAccessStore(t)
	ctx := context.Background()

	t.Run("insert new record", func(t *testing.T) {
		inserted, err := s.Record(ctx, ChatAccessRecord{
			EventID:  "evt_1",
			Platform: "feishu",
			ChatID:   "oc_abc",
			UserID:   "ou_123",
			BotID:    "bot_1",
		})
		require.NoError(t, err)
		require.True(t, inserted)
	})

	t.Run("duplicate event_id returns false", func(t *testing.T) {
		inserted, err := s.Record(ctx, ChatAccessRecord{
			EventID:  "evt_1",
			Platform: "feishu",
			ChatID:   "oc_abc",
			UserID:   "ou_123",
			BotID:    "bot_1",
		})
		require.NoError(t, err)
		require.False(t, inserted)
	})

	t.Run("different event_id inserts", func(t *testing.T) {
		inserted, err := s.Record(ctx, ChatAccessRecord{
			EventID:  "evt_2",
			Platform: "slack",
			ChatID:   "C123",
			UserID:   "U456",
			BotID:    "bot_1",
		})
		require.NoError(t, err)
		require.True(t, inserted)
	})
}

func TestChatAccessStore_Classify(t *testing.T) {
	s := newTestChatAccessStore(t)
	ctx := context.Background()

	t.Run("no prior record returns new", func(t *testing.T) {
		got := s.Classify(ctx, "feishu", "oc_new", "bot_1", "ou_1", 0)
		require.Equal(t, ChatAccessNew, got)
	})

	t.Run("recent record returns active", func(t *testing.T) {
		_, err := s.Record(ctx, ChatAccessRecord{
			EventID:  "evt_recent",
			Platform: "feishu",
			ChatID:   "oc_active",
			UserID:   "ou_1",
			BotID:    "bot_1",
		})
		require.NoError(t, err)

		got := s.Classify(ctx, "feishu", "oc_active", "bot_1", "ou_1", time.Now().UnixMilli())
		require.Equal(t, ChatAccessActive, got)
	})

	t.Run("old record + stale payload returns returning", func(t *testing.T) {
		_, err := s.Record(ctx, ChatAccessRecord{
			EventID:  "evt_old",
			Platform: "feishu",
			ChatID:   "oc_return",
			UserID:   "ou_2",
			BotID:    "bot_1",
		})
		require.NoError(t, err)

		// Backdate created_at to 2 hours ago.
		_, err = s.db.Exec(
			`UPDATE chat_access_events SET created_at = ? WHERE event_id = ?`,
			time.Now().Unix()-7200, "evt_old")
		require.NoError(t, err)

		// Feishu fast-path: payload lastMessageAtMs > 2h ago.
		oldMs := time.Now().Add(-2 * time.Hour).UnixMilli()
		got := s.Classify(ctx, "feishu", "oc_return", "bot_1", "ou_2", oldMs)
		require.Equal(t, ChatAccessReturning, got)
	})

	t.Run("slack path checks user activity", func(t *testing.T) {
		_, err := s.Record(ctx, ChatAccessRecord{
			EventID:  "evt_slack_old",
			Platform: "slack",
			ChatID:   "C_slack",
			UserID:   "U_slack",
			BotID:    "bot_1",
		})
		require.NoError(t, err)

		_, err = s.db.Exec(
			`UPDATE chat_access_events SET created_at = ? WHERE event_id = ?`,
			time.Now().Unix()-7200, "evt_slack_old")
		require.NoError(t, err)

		// lastMessageAtMs=0 triggers Slack DB fallback path.
		got := s.Classify(ctx, "slack", "C_slack", "bot_1", "U_slack", 0)
		require.Equal(t, ChatAccessReturning, got)
	})
}

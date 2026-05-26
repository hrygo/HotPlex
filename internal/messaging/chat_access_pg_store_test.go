package messaging

import (
	"context"
	"log/slog"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/dbutil"
)

func newChatAccessPGMock(t *testing.T) (*pgStore, sqlmock.Sqlmock, func()) {
	t.Helper()
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)

	db := &dbutil.DB{DB: mockDB}

	store := &pgStore{
		db:      db,
		dialect: dbutil.DialectPostgres,
		log:     slog.Default(),
	}

	return store, mock, func() {
		require.NoError(t, mock.ExpectationsWereMet())
		mockDB.Close()
	}
}

func TestChatAccessPGStore_Record(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newChatAccessPGMock(t)
	defer cleanup()

	q := dbutil.DialectPostgres.Rebind(
		`INSERT INTO chat_access_events (event_id, platform, chat_id, user_id, bot_id, last_message_at, welcome_sent, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)

	r := ChatAccessRecord{
		EventID:       "evt-1",
		Platform:      "slack",
		ChatID:        "C123",
		UserID:        "U456",
		BotID:         "bot-1",
		LastMessageAt: 1000,
		WelcomeSent:   true,
	}

	mock.ExpectExec(regexp.QuoteMeta(q)).
		WithArgs(r.EventID, r.Platform, r.ChatID, r.UserID, r.BotID, r.LastMessageAt, true, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	inserted, err := store.Record(context.Background(), r)
	require.NoError(t, err)
	require.True(t, inserted)
}

func TestChatAccessPGStore_Record_Duplicate(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newChatAccessPGMock(t)
	defer cleanup()

	q := dbutil.DialectPostgres.Rebind(
		`INSERT INTO chat_access_events (event_id, platform, chat_id, user_id, bot_id, last_message_at, welcome_sent, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)

	r := ChatAccessRecord{
		EventID:  "evt-dup",
		Platform: "slack",
		ChatID:   "C123",
		UserID:   "U456",
		BotID:    "bot-1",
	}

	mock.ExpectExec(regexp.QuoteMeta(q)).
		WithArgs(r.EventID, r.Platform, r.ChatID, r.UserID, r.BotID, r.LastMessageAt, false, sqlmock.AnyArg()).
		WillReturnError(&pgconn.PgError{Code: "23505", Message: "duplicate key value"})

	inserted, err := store.Record(context.Background(), r)
	require.NoError(t, err)
	require.False(t, inserted)
}

func TestChatAccessPGStore_Classify_New(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newChatAccessPGMock(t)
	defer cleanup()

	firstQ := dbutil.DialectPostgres.Rebind(
		`SELECT created_at FROM chat_access_events
		 WHERE platform = ? AND chat_id = ? AND bot_id = ?
		 ORDER BY created_at DESC LIMIT 1`)

	mock.ExpectQuery(regexp.QuoteMeta(firstQ)).
		WithArgs("slack", "C_new", "bot-1").
		WillReturnError(fakeNoRows{})

	result := store.Classify(context.Background(), "slack", "C_new", "bot-1", "U_new", 0)
	require.Equal(t, ChatAccessNew, result)
}

type fakeNoRows struct{}

func (f fakeNoRows) Error() string { return "sql: no rows in result set" }

func TestChatAccessPGStore_Classify_Active(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newChatAccessPGMock(t)
	defer cleanup()

	firstQ := dbutil.DialectPostgres.Rebind(
		`SELECT created_at FROM chat_access_events
		 WHERE platform = ? AND chat_id = ? AND bot_id = ?
		 ORDER BY created_at DESC LIMIT 1`)

	recentTime := time.Now().Unix() - 30

	rows := sqlmock.NewRows([]string{"created_at"}).AddRow(recentTime)
	mock.ExpectQuery(regexp.QuoteMeta(firstQ)).
		WithArgs("slack", "C_act", "bot-1").
		WillReturnRows(rows)

	result := store.Classify(context.Background(), "slack", "C_act", "bot-1", "U_act", 0)
	require.Equal(t, ChatAccessActive, result)
}

func TestChatAccessPGStore_Classify_Returning(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newChatAccessPGMock(t)
	defer cleanup()

	firstQ := dbutil.DialectPostgres.Rebind(
		`SELECT created_at FROM chat_access_events
		 WHERE platform = ? AND chat_id = ? AND bot_id = ?
		 ORDER BY created_at DESC LIMIT 1`)

	oldTime := time.Now().Unix() - 7200

	firstRows := sqlmock.NewRows([]string{"created_at"}).AddRow(oldTime)
	mock.ExpectQuery(regexp.QuoteMeta(firstQ)).
		WithArgs("slack", "C_old", "bot-1").
		WillReturnRows(firstRows)

	secondQ := dbutil.DialectPostgres.Rebind(
		`SELECT COALESCE(MAX(created_at), 0) FROM chat_access_events
		 WHERE platform = ? AND user_id = ? AND bot_id = ?`)

	secondRows := sqlmock.NewRows([]string{"coalesce"}).AddRow(int64(0))
	mock.ExpectQuery(regexp.QuoteMeta(secondQ)).
		WithArgs("slack", "U_old", "bot-1").
		WillReturnRows(secondRows)

	result := store.Classify(context.Background(), "slack", "C_old", "bot-1", "U_old", 0)
	require.Equal(t, ChatAccessReturning, result)
}

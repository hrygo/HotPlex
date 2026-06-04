package eventstore

import (
	"context"
	"log/slog"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/dbutil"
)

func newEventPGMock(t *testing.T) (*pgStore, sqlmock.Sqlmock, func()) {
	t.Helper()
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)

	db := &dbutil.DB{DB: mockDB}

	pg := dbutil.DialectPostgres
	sqlMap := map[string]string{
		"insert":       pg.Rebind("INSERT INTO events (session_id, seq, type, data, direction, source, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)"),
		"query_latest": pg.Rebind("SELECT session_id, seq, type, data, direction, source, created_at FROM events WHERE session_id = ? ORDER BY seq DESC LIMIT ?"),
	}

	store := &pgStore{
		db:      db,
		dialect: pg,
		sql:     sqlMap,
		log:     slog.Default(),
	}

	return store, mock, func() {
		require.NoError(t, mock.ExpectationsWereMet())
		mockDB.Close()
	}
}

func eventColumns() []string {
	return []string{"session_id", "seq", "type", "data", "direction", "source", "created_at"}
}

func TestPGEventStore_AppendEvent(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newEventPGMock(t)
	defer cleanup()

	event := &StoredEvent{
		SessionID: "sess-1",
		Seq:       1,
		Type:      "message.delta",
		Data:      []byte(`{"content":"hello"}`),
		Direction: "out",
		Source:    "normal",
		CreatedAt: 1000000,
	}

	q := dbutil.DialectPostgres.Rebind(
		"INSERT INTO events (session_id, seq, type, data, direction, source, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)")

	mock.ExpectExec(regexp.QuoteMeta(q)).
		WithArgs(event.SessionID, event.Seq, event.Type, event.Data, event.Direction, event.Source, event.CreatedAt).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := store.Append(context.Background(), event)
	require.NoError(t, err)
}

func TestPGEventStore_QueryBySession_Latest(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newEventPGMock(t)
	defer cleanup()

	sessionID := "sess-1"
	fetchLimit := 201

	q := dbutil.DialectPostgres.Rebind(
		"SELECT session_id, seq, type, data, direction, source, created_at FROM events WHERE session_id = ? ORDER BY seq DESC LIMIT ?")

	rows := sqlmock.NewRows(eventColumns()).
		AddRow(sessionID, int64(2), "message.delta", []byte(`{"c":"b"}`), "out", "normal", int64(2000)).
		AddRow(sessionID, int64(1), "message.delta", []byte(`{"c":"a"}`), "out", "normal", int64(1000))

	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs(sessionID, fetchLimit).WillReturnRows(rows)

	page, err := store.QueryBySession(context.Background(), sessionID, 0, CursorLatest, 200)
	require.NoError(t, err)
	require.NotNil(t, page)
	require.Len(t, page.Events, 2)
	require.Equal(t, int64(1), page.Events[0].Seq)
	require.Equal(t, int64(2), page.Events[1].Seq)
	require.Equal(t, int64(1), page.OldestSeq)
	require.Equal(t, int64(2), page.NewestSeq)
	require.False(t, page.HasOlder)
}

func TestPGEventStore_Close(t *testing.T) {
	t.Parallel()
	store, _, cleanup := newEventPGMock(t)
	defer cleanup()

	err := store.Close()
	require.NoError(t, err)
}

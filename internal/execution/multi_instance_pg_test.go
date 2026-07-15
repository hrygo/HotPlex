//go:build pg

package execution

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/pkg/events"
)

func openPGStore(t *testing.T) (*SQLStore, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("HOTPLEX_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("HOTPLEX_TEST_PG_DSN not set; skipping PG multi-instance test")
	}
	db, err := dbutil.Open(dbutil.DialectPostgres, &config.DBConfig{
		Driver:   "postgres",
		Postgres: config.PostgresConfig{ConnStr: dsn},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.DB.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS public CASCADE")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(context.Background(), "CREATE SCHEMA public")
	require.NoError(t, err)

	require.NoError(t, session.RunMigrations(context.Background(), db.DB, dbutil.DialectPostgres))

	now := time.Now()
	sm, err := session.NewPGStore(context.Background(), db)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sm.Close() })
	require.NoError(t, sm.Upsert(context.Background(), &session.SessionInfo{
		ID: "session-pg", UserID: "user-1", WorkerType: "claude_code",
		State: events.StateRunning, CreatedAt: now, UpdatedAt: now,
	}))

	store, err := NewSQLStore(context.Background(), db.DB, dbutil.DialectPostgres, nil, nil)
	require.NoError(t, err)
	return store, db.DB
}

func TestPGMultiInstance_StartupDoesNotTouchOtherInstancesRecords(t *testing.T) {
	storeA, db := openPGStore(t)
	ctx := context.Background()

	rec, _, err := storeA.Accept(ctx, AcceptRequest{
		SessionID: "session-pg", ClientMessageID: "msg-pg-multi", PayloadHash: "h",
		OwnerInstanceID: "gw-A", WorkerRunID: "run-A",
	})
	require.NoError(t, err)
	require.NoError(t, storeA.MarkRunning(ctx, rec.ExecutionID, "gw-A", "run-A"))

	storeB, err := NewSQLStore(ctx, db, dbutil.DialectPostgres, nil, nil)
	require.NoError(t, err)

	recovered, err := storeB.RecoverExpiredLeases(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, int64(0), recovered.Recovered, "instance B must not recover A's unexpired lease")

	stored, err := storeA.getByID(ctx, rec.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, RuntimeRunning, stored.RuntimeStatus)
	require.Equal(t, "gw-A", stored.OwnerInstanceID)
}

func TestPGMultiInstance_ExpiredLeaseRecoveredExactlyOnce(t *testing.T) {
	storeA, db := openPGStore(t)
	ctx := context.Background()

	rec, _, err := storeA.Accept(ctx, AcceptRequest{
		SessionID: "session-pg", ClientMessageID: "msg-pg-expire", PayloadHash: "h",
		OwnerInstanceID: "gw-A", WorkerRunID: "run-A",
	})
	require.NoError(t, err)

	storeB, err := NewSQLStore(ctx, db, dbutil.DialectPostgres, nil, nil)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `UPDATE execution_inputs SET lease_until = 0 WHERE execution_id = $1`, rec.ExecutionID)
	require.NoError(t, err)
	recoveredB, err := storeB.RecoverExpiredLeases(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, int64(1), recoveredB.Recovered)

	recoveredA, err := storeA.RecoverExpiredLeases(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, int64(0), recoveredA.Recovered)

	stored, err := storeA.getByID(ctx, rec.ExecutionID)
	require.NoError(t, err)
	require.Equal(t, RuntimeUnknown, stored.RuntimeStatus)
	require.Equal(t, "GATEWAY_LEASE_EXPIRED", stored.FenceReason)
}

func TestPGMultiInstance_PartialUniqueIndexRejectsSecondActive(t *testing.T) {
	store, _ := openPGStore(t)
	ctx := context.Background()

	_, _, err := store.Accept(ctx, AcceptRequest{
		SessionID: "session-pg", ClientMessageID: "msg-pg-first", PayloadHash: "h1",
		OwnerInstanceID: "gw-A", WorkerRunID: "run-A",
	})
	require.NoError(t, err)

	_, _, err = store.Accept(ctx, AcceptRequest{
		SessionID: "session-pg", ClientMessageID: "msg-pg-second", PayloadHash: "h2",
		OwnerInstanceID: "gw-A", WorkerRunID: "run-B",
	})
	require.ErrorIs(t, err, ErrSessionBusy,
		"PG partial unique index must reject second active execution per session")
}

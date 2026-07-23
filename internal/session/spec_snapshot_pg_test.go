//go:build pg

package session

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/agentspec"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

// TestPGStore_SpecSnapshotRoundTrip: the effective AgentSpec snapshot round-trips
// through the PostgreSQL store under the same context_json TEXT column, restoring
// the typed field and the in-memory-only AllowedTools. Guards on
// HOTPLEX_TEST_PG_DSN; run with `go test -tags pg -p 1 -run PGStore_SpecSnapshot`.
//
// The marshal/scan helpers are shared by the SQLite and PG stores, so the SQLite
// suite already covers the folding logic; this test additionally proves the
// folded blob survives the PG TEXT column and its driver scan path.
func TestPGStore_SpecSnapshotRoundTrip(t *testing.T) {
	dsn := os.Getenv("HOTPLEX_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("HOTPLEX_TEST_PG_DSN not set; skipping PG snapshot test")
	}
	ctx := context.Background()

	db, err := dbutil.Open(dbutil.DialectPostgres, &config.DBConfig{
		Driver:   "postgres",
		Postgres: config.PostgresConfig{ConnStr: dsn},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store, err := NewPGStore(ctx, db)
	require.NoError(t, err)
	pgs := store.(*pgStore)

	now := time.Now().UTC()
	snap := agentspec.SnapshotFromSpec(agentspec.AgentSpec{
		Worker: agentspec.WorkerSpec{Type: "claude_code"},
		Policy: agentspec.PolicySpec{PermissionMode: "workspace", AllowedTools: []string{"git", "grep"}},
	})
	id := "pg-sess-snap-rt"
	t.Cleanup(func() { _, _ = pgs.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = $1`, id) })

	require.NoError(t, store.Upsert(ctx, &SessionInfo{
		ID: id, UserID: "u1", WorkerType: worker.TypeClaudeCode,
		State: events.StateCreated, CreatedAt: now, UpdatedAt: now,
		SpecSnapshot: &snap,
	}))

	got, err := store.Get(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, got.SpecSnapshot, "snapshot must round-trip on PG")
	require.Equal(t, snap.Hash, got.SpecSnapshot.Hash)
	require.Equal(t, []string{"git", "grep"}, got.AllowedTools, "AllowedTools restored from snapshot on PG")
	require.Equal(t, "workspace", got.SpecSnapshot.PermissionMode)
	require.NotContains(t, got.Context, agentspec.SnapshotContextKey)

	// Legacy row (no snapshot) remains readable with no restore.
	legacyID := "pg-sess-snap-legacy"
	t.Cleanup(func() { _, _ = pgs.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = $1`, legacyID) })
	require.NoError(t, store.Upsert(ctx, &SessionInfo{
		ID: legacyID, UserID: "u1", WorkerType: worker.TypeClaudeCode,
		State: events.StateCreated, CreatedAt: now, UpdatedAt: now,
	}))
	legacy, err := store.Get(ctx, legacyID)
	require.NoError(t, err)
	require.Nil(t, legacy.SpecSnapshot, "PG legacy row has no snapshot")
	require.Empty(t, legacy.AllowedTools)
}

package audit

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSpillFile_WriteReadRoundtrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "spill.wal")
	sf, err := OpenSpill(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sf.Close() })

	rec1 := SpillRecord{TsMs: 1700000000000, UA: &UserActivity{
		Ts: 1700000000000, UserID: "u1", UserIDType: UserIDTypePlatform,
		Platform: PlatformFeishu, Action: ActionAuthLogin, Outcome: OutcomeSuccess,
		DetailJSON: `{"k":"v"}`, PrevHash: "", SelfHash: "abc",
	}}
	rec2 := SpillRecord{TsMs: 1700000001000, UA: &UserActivity{
		Ts: 1700000001000, UserID: "u2", UserIDType: UserIDTypeRegistered,
		Platform: PlatformWebChat, Action: ActionSessionCreate, Outcome: OutcomeSuccess,
		DetailJSON: `{}`, PrevHash: "abc", SelfHash: "def",
	}}

	require.NoError(t, sf.Write(rec1))
	require.NoError(t, sf.Write(rec2))

	records, err := sf.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, rec1.TsMs, records[0].TsMs)
	require.Equal(t, "u1", records[0].UA.UserID)
	require.Equal(t, rec2.TsMs, records[1].TsMs)
	require.Equal(t, "u2", records[1].UA.UserID)
}

func TestSpillFile_ReadEmptyFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "spill_empty.wal")
	sf, err := OpenSpill(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sf.Close() })

	records, err := sf.ReadAll()
	require.NoError(t, err)
	require.Empty(t, records)
}

func TestSpillFile_Truncate(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "spill_trunc.wal")
	sf, err := OpenSpill(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sf.Close() })

	rec := SpillRecord{TsMs: 1000, UA: &UserActivity{
		Ts: 1000, UserID: "u1", Action: ActionAuthLogin, Outcome: OutcomeSuccess,
		DetailJSON: `{}`, SelfHash: "h1",
	}}
	require.NoError(t, sf.Write(rec))

	// Truncate clears the file
	require.NoError(t, sf.Truncate())

	records, err := sf.ReadAll()
	require.NoError(t, err)
	require.Empty(t, records)
}

func TestSpillFile_WriteAfterTruncate(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "spill_rewrite.wal")
	sf, err := OpenSpill(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sf.Close() })

	rec1 := SpillRecord{TsMs: 1, UA: &UserActivity{
		Ts: 1, UserID: "u1", Action: ActionAuthLogin, Outcome: OutcomeSuccess,
		DetailJSON: `{}`, SelfHash: "h1",
	}}
	rec2 := SpillRecord{TsMs: 2, UA: &UserActivity{
		Ts: 2, UserID: "u2", Action: ActionSessionCreate, Outcome: OutcomeSuccess,
		DetailJSON: `{}`, SelfHash: "h2",
	}}

	require.NoError(t, sf.Write(rec1))
	require.NoError(t, sf.Truncate())
	require.NoError(t, sf.Write(rec2))

	records, err := sf.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, rec2.TsMs, records[0].TsMs)
	require.Equal(t, "u2", records[0].UA.UserID)
}

func TestSpillFile_ManyRecords(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "spill_many.wal")
	sf, err := OpenSpill(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sf.Close() })

	const n = 100
	for i := 0; i < n; i++ {
		rec := SpillRecord{TsMs: int64(i), UA: &UserActivity{
			Ts: int64(i), UserID: "u1", Action: ActionAuthLogin, Outcome: OutcomeSuccess,
			DetailJSON: `{}`, SelfHash: "h",
		}}
		require.NoError(t, sf.Write(rec))
	}

	records, err := sf.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, n)
	for i := 0; i < n; i++ {
		require.Equal(t, int64(i), records[i].TsMs)
	}
}

func TestSpillFile_RecoveryAfterClose(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "spill_recovery.wal")

	// Write some records, then close
	sf, err := OpenSpill(path)
	require.NoError(t, err)
	rec := SpillRecord{TsMs: 42, UA: &UserActivity{
		Ts: 42, UserID: "u1", Action: ActionAuthLogin, Outcome: OutcomeSuccess,
		DetailJSON: `{}`, SelfHash: "h",
	}}
	require.NoError(t, sf.Write(rec))
	require.NoError(t, sf.Close())

	// Reopen and read — data should survive
	sf2, err := OpenSpill(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sf2.Close() })

	records, err := sf2.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, int64(42), records[0].TsMs)
}

func TestSpillFile_LargePayload(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "spill_large.wal")
	sf, err := OpenSpill(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sf.Close() })

	// Create a record with a large DetailJSON
	bigJSON := make([]byte, 64*1024) // 64KB
	for i := range bigJSON {
		bigJSON[i] = 'A'
	}
	rec := SpillRecord{TsMs: 1, UA: &UserActivity{
		Ts: 1, UserID: "u1", Action: ActionAuthLogin, Outcome: OutcomeSuccess,
		DetailJSON: string(bigJSON), SelfHash: "h",
	}}
	require.NoError(t, sf.Write(rec))

	records, err := sf.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Len(t, records[0].UA.DetailJSON, 64*1024)
}

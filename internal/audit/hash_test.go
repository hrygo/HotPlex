package audit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestComputeSelfHash_GenesisHasStableValue(t *testing.T) {
	t.Parallel()
	ua := &UserActivity{
		Ts: 1700000000000, UserID: "u1", UserIDType: UserIDTypePlatform,
		Platform: "feishu", Action: ActionAuthLogin, Outcome: OutcomeSuccess,
		DetailJSON: `{"k":"v"}`,
	}
	// Genesis: prev_hash = ""
	h, err := ComputeSelfHash("", ua)
	require.NoError(t, err)
	require.Len(t, h, 64) // sha256 hex
	require.Equal(t, "dc037a6227117cd7c8e1656d3d7112f46f6d4ea5e86dba9ef82197d01eeaffd3", h)
	h2, _ := ComputeSelfHash("", ua)
	require.Equal(t, h, h2, "hash must be deterministic")
}

func TestComputeSelfHash_ChangesWithPrev(t *testing.T) {
	t.Parallel()
	ua := &UserActivity{Ts: 1700000000000, UserID: "u1", Action: ActionAuthLogin, Outcome: OutcomeSuccess}
	h1, _ := ComputeSelfHash("", ua)
	h2, _ := ComputeSelfHash(h1, ua)
	require.NotEqual(t, h1, h2, "different prev_hash must produce different self_hash")
}

func TestComputeSelfHash_DifferentFieldsDifferentHashes(t *testing.T) {
	t.Parallel()
	ua1 := &UserActivity{Ts: 1, UserID: "u1", Action: ActionAuthLogin, Outcome: OutcomeSuccess}
	ua2 := &UserActivity{Ts: 2, UserID: "u1", Action: ActionAuthLogin, Outcome: OutcomeSuccess}
	h1, _ := ComputeSelfHash("", ua1)
	h2, _ := ComputeSelfHash("", ua2)
	require.NotEqual(t, h1, h2, "different ts must produce different hashes")
}

func TestComputeSelfHash_NilErrors(t *testing.T) {
	t.Parallel()
	_, err := ComputeSelfHash("", nil)
	require.Error(t, err)
}

func TestVerifyChain_Valid(t *testing.T) {
	t.Parallel()
	var rows []UserActivity
	prev := ""
	for i := 0; i < 5; i++ {
		ua := UserActivity{
			ID: int64(i + 1), Ts: int64(1700000000000 + i*1000),
			UserID: "u1", UserIDType: UserIDTypePlatform, Platform: "feishu",
			Action: ActionAuthLogin, Outcome: OutcomeSuccess, DetailJSON: `{}`,
			PrevHash: prev,
		}
		h, _ := ComputeSelfHash(prev, &ua)
		ua.SelfHash = h
		rows = append(rows, ua)
		prev = h
	}
	brokenID, reason := VerifyChain(rows, "")
	require.Equal(t, int64(0), brokenID, "valid chain should not break")
	require.Empty(t, reason)
}

func TestVerifyChain_TamperedRowDetected(t *testing.T) {
	t.Parallel()
	var rows []UserActivity
	prev := ""
	for i := 0; i < 3; i++ {
		ua := UserActivity{
			ID: int64(i + 1), Ts: int64(1700000000000 + i*1000),
			UserID: "u1", Action: ActionAuthLogin, Outcome: OutcomeSuccess, DetailJSON: `{}`,
			PrevHash: prev,
		}
		h, _ := ComputeSelfHash(prev, &ua)
		ua.SelfHash = h
		rows = append(rows, ua)
		prev = h
	}
	// Tamper: change UserID on row 2
	rows[1].UserID = "attacker"
	brokenID, reason := VerifyChain(rows, "")
	require.Equal(t, int64(2), brokenID)
	require.Contains(t, reason, "self_hash_mismatch")
}

func TestVerifyChain_GenesisPrevMustBeEmpty(t *testing.T) {
	t.Parallel()
	ua := UserActivity{
		ID: 1, Ts: 1700000000000, UserID: "u1", Action: ActionAuthLogin, Outcome: OutcomeSuccess,
		DetailJSON: `{}`, PrevHash: "should_be_empty",
	}
	h, _ := ComputeSelfHash("", &ua)
	ua.SelfHash = h
	rows := []UserActivity{ua}
	brokenID, reason := VerifyChain(rows, "")
	require.Equal(t, int64(1), brokenID)
	require.Contains(t, reason, "prev_hash_mismatch")
}

func TestVerifyChain_AcceptsCheckpointOverride(t *testing.T) {
	t.Parallel()
	ua := UserActivity{
		ID: 100, Ts: 1700000000000, UserID: "u1", Action: ActionAuthLogin, Outcome: OutcomeSuccess,
		DetailJSON: `{}`, PrevHash: "abc123checkpoint",
	}
	h, _ := ComputeSelfHash("abc123checkpoint", &ua)
	ua.SelfHash = h
	rows := []UserActivity{ua}
	brokenID, reason := VerifyChain(rows, "abc123checkpoint")
	require.Equal(t, int64(0), brokenID)
	require.Empty(t, reason)
}

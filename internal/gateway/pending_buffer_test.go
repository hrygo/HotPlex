package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

func newInputEnvelope(t *testing.T, sid, content string) *events.Envelope {
	t.Helper()
	return events.NewEnvelope("evt_orig", sid, 0, events.Input, map[string]any{
		"content":           content,
		"client_message_id": "cmid_orig",
	})
}

func TestPendingBuffer_DrainAndMerge(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		inputs  []string
		wantSub []string // merged 中应包含的子串
		single  bool
	}{
		{"single passthrough", []string{"继续"}, []string{"继续"}, true},
		{"multi merged numbered", []string{"继续", "补充", "换角度"},
			[]string{"3 条消息", "1. 继续", "2. 补充", "3. 换角度"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pb := NewPendingBuffer()
			env := newInputEnvelope(t, "sess_1", "orig")
			for _, c := range tc.inputs {
				pb.Append("sess_1", c, env)
			}
			merged, repr, ok := pb.DrainAndMerge("sess_1")
			require.True(t, ok)
			require.NotNil(t, repr)
			if tc.single {
				require.Equal(t, tc.inputs[0], merged)
			} else {
				for _, s := range tc.wantSub {
					require.Contains(t, merged, s)
				}
			}
			// drain 后清空
			_, _, ok2 := pb.DrainAndMerge("sess_1")
			require.False(t, ok2)
		})
	}
}

func TestPendingBuffer_DedupAdjacentAndCap(t *testing.T) {
	t.Parallel()
	pb := NewPendingBuffer()
	env := newInputEnvelope(t, "s", "x")
	// 相邻完全相同去重
	pb.Append("s", "同", env)
	pb.Append("s", "同", env)
	merged, _, _ := pb.DrainAndMerge("s")
	require.Equal(t, "同", merged) // 只剩一条

	// 上限 20：塞 25 条，只保留最新 20，合并后编号 1..20
	for i := 0; i < 25; i++ {
		pb.Append("s", fmt.Sprintf("m%d", i), env)
	}
	merged, _, _ = pb.DrainAndMerge("s")
	require.Contains(t, merged, "20 条消息")
	require.Contains(t, merged, "1. m5") // 最旧保留的是 m5（丢 m0..m4）
	require.Contains(t, merged, "20. m24")
}

func TestPendingBuffer_Concurrent(t *testing.T) {
	t.Parallel()
	pb := NewPendingBuffer()
	env := newInputEnvelope(t, "s", "x")
	done := make(chan struct{})
	for i := 0; i < 50; i++ {
		go func() { pb.Append("s", "c", env); done <- struct{}{} }()
		go func() { pb.DrainAndMerge("s"); done <- struct{}{} }()
	}
	for i := 0; i < 100; i++ {
		<-done
	}
}

func TestCloneForReplay_NewIDs(t *testing.T) {
	t.Parallel()
	orig := newInputEnvelope(t, "s", "old")
	orig.Event.Data.(map[string]any)["client_message_id"] = "cmid_orig"
	c := cloneForReplay(orig, "new content")
	require.NotEqual(t, orig.ID, c.ID)
	require.NotEqual(t, "cmid_orig", c.Event.Data.(map[string]any)["client_message_id"])
	require.Equal(t, "new content", c.Event.Data.(map[string]any)["content"])
	require.Equal(t, "s", c.SessionID) // 复用 session
	require.Zero(t, c.Seq)             // 由 hub 重分配
}

// newTestBridgeForPending builds a minimal Bridge for pending-buffer cleanup
// tests. Only Log is needed — ClearPending/ClearAllPending are pure in-memory
// operations and don't touch Hub/SM.
func newTestBridgeForPending(t *testing.T) *Bridge {
	t.Helper()
	return NewBridge(BridgeDeps{Log: slog.Default()})
}

// TestPendingClearedOnSessionEnd verifies the per-session cleanup contract:
// after Bridge.ClearPending(sid), a subsequent DrainAndMerge(sid) returns
// ok=false so stale supplements cannot replay into a dead session. Mirrors
// the contract called from sm.OnTerminate in cmd/hotplex/gateway_run.go.
func TestPendingClearedOnSessionEnd(t *testing.T) {
	t.Parallel()
	b := newTestBridgeForPending(t)
	env := newInputEnvelope(t, "s1", "x")
	b.pending.Append("s1", "x", env)
	// Sanity: buffer holds one entry before clear.
	_, _, ok := b.pending.DrainAndMerge("s1")
	require.True(t, ok, "pre-clear: expected one buffered supplement")

	// Repopulate (drain removed it) and clear via the Bridge-level method.
	b.pending.Append("s1", "x", env)
	b.ClearPending("s1")
	_, _, ok = b.pending.DrainAndMerge("s1")
	require.False(t, ok, "post-clear: expected buffer empty for s1")
}

// TestPendingClearedOnSessionEnd_OnlyTargetSession verifies ClearPending(sid)
// does not disturb other sessions' buffers.
func TestPendingClearedOnSessionEnd_OnlyTargetSession(t *testing.T) {
	t.Parallel()
	b := newTestBridgeForPending(t)
	env := newInputEnvelope(t, "s", "x")
	b.pending.Append("s1", "x", env)
	b.pending.Append("s2", "y", env)

	b.ClearPending("s1")

	_, _, ok := b.pending.DrainAndMerge("s1")
	require.False(t, ok, "s1 should be cleared")
	_, _, ok = b.pending.DrainAndMerge("s2")
	require.True(t, ok, "s2 should be untouched")
}

// TestClearAllPendingOnShutdown verifies the global cleanup contract: after
// Bridge.ClearAllPending(), every session's buffer is empty. This mirrors the
// call made in Bridge.Shutdown so stale supplements don't survive a gateway
// restart's in-memory state (buffer is not persisted).
func TestClearAllPendingOnShutdown(t *testing.T) {
	t.Parallel()
	b := newTestBridgeForPending(t)
	env := newInputEnvelope(t, "s", "x")
	b.pending.Append("s1", "a", env)
	b.pending.Append("s2", "b", env)
	b.pending.Append("s3", "c", env)

	b.ClearAllPending()

	for _, sid := range []string{"s1", "s2", "s3"} {
		_, _, ok := b.pending.DrainAndMerge(sid)
		require.False(t, ok, "post-ClearAllPending: %s should be empty", sid)
	}
}

// TestBridge_ShutdownClearsAllPending drives Bridge.Shutdown (the real hook)
// and verifies it clears the buffer via the internal ClearAllPending call.
func TestBridge_ShutdownClearsAllPending(t *testing.T) {
	t.Parallel()
	b := newTestBridgeForPending(t)
	env := newInputEnvelope(t, "s", "x")
	b.pending.Append("s1", "a", env)
	b.pending.Append("s2", "b", env)

	// Shutdown uses context.Background for the test; no forwarders are
	// registered so WaitForwarders returns immediately.
	b.Shutdown(context.Background())

	for _, sid := range []string{"s1", "s2"} {
		_, _, ok := b.pending.DrainAndMerge(sid)
		require.False(t, ok, "post-Shutdown: %s should be empty", sid)
	}
}

package acp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
)

// TestWorkerInjectHistoryPrefix_WithHistory verifies B-acp (issue #816): when
// an ACP session is freshly created after loadSession fails (historyLost), the
// first user prompt is prefixed with ConversationHistory text so the new ACP
// session gets text-level context continuity — mirroring codex's
// injectHistoryPrefix fallback.
func TestWorkerInjectHistoryPrefix_WithHistory(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	w.pendingHistory = []worker.ConversationTurn{
		{Role: "user", Content: "为何展示只有 timer 和 tokens"},
		{Role: "assistant", Content: "因为卡片渲染逻辑…"},
	}

	out := w.injectHistoryPrefix("header 存在")

	require.Contains(t, out, "CONVERSATION_HISTORY_RECOVERY_START")
	require.Contains(t, out, "为何展示只有 timer 和 tokens")
	require.Contains(t, out, "因为卡片渲染逻辑…")
	require.Contains(t, out, "header 存在")
	require.True(t, w.historyInjected.Load(), "historyInjected flag set after injection")

	// Second call must not re-inject (CompareAndSwap guard).
	require.Equal(t, "second", w.injectHistoryPrefix("second"))
}

// TestWorkerInjectHistoryPrefix_EmptyNoOp verifies that a worker without
// pending history (normal fresh start, or loadSession succeeded) passes content
// through unchanged.
func TestWorkerInjectHistoryPrefix_EmptyNoOp(t *testing.T) {
	t.Parallel()
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}

	require.Equal(t, "hello", w.injectHistoryPrefix("hello"))
	require.False(t, w.historyInjected.Load(), "flag must not be set when nothing was injected")
}

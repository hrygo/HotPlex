package gateway

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

// TestPrepareWorkerInfo_CodexFreshStartInjectsHistory verifies that a codex
// session freshly restarted after zombie reclamation (State=Created) still
// gets ConversationHistory injected from the turns table.
//
// Background (issue #815, spec L3): when a messaging session is reaped by the
// 30m zombie ExecutionTimeout, codex cannot resume terminated state and bridge
// takes the fresh-start path (CreateWithBot → prepareWorkerInfo). Because
// DeriveSessionKey deterministically reuses the same sessionID for the same
// chat, the turns table still holds the prior turns — but the old guard
// `si.State != StateCreated` skipped history loading on this path, leaving the
// new codex thread context-free. This test pins the fix.
func TestPrepareWorkerInfo_CodexFreshStartInjectsHistory(t *testing.T) {
	t.Parallel()
	hub := newTestHub(t)
	ts := new(mockTurnsStore)
	turns := []*eventstore.TurnRecord{
		{ID: 1, SessionID: "sess-codex", Role: "user", Content: "为何展示只有 timer 和 tokens", CreatedAt: 1_782_780_660},
		{ID: 2, SessionID: "sess-codex", Role: "assistant", Content: "因为卡片渲染逻辑…", CreatedAt: 1_782_780_697},
	}
	ts.On("QueryTurns", mock.Anything, "sess-codex", 50, 0).Return(turns, nil)

	b := &Bridge{
		log:           slog.Default(),
		hub:           hub,
		turnsQuerier:  ts,
		shutdownCtx:   context.Background(),
		compressCache: sync.Map{},
	}
	b.closed.Store(true) // skip async compression goroutine

	si := &session.SessionInfo{
		ID:         "sess-codex",
		UserID:     "u1",
		WorkerType: worker.TypeCodexCLI,
		State:      events.StateCreated, // fresh-restart record, just created
	}

	info := b.prepareWorkerInfo("sess-codex", "u1", "", si)

	require.NotEmpty(t, info.ConversationHistory,
		"codex fresh-restart (State=Created) must inject turns-table history for context recovery (L3/L4)")
	// TruncateHistory preserves order; first turn is the user message.
	require.Equal(t, "user", info.ConversationHistory[0].Role)
	require.Contains(t, info.ConversationHistory[0].Content, "timer 和 tokens")
}

// TestPrepareWorkerInfo_CodexFreshStartEmptyTurnsNoInjection verifies the
// len(turns) > 0 guard: a genuinely new codex session (different chat →
// different sessionID → empty turns table) is not injected, even though the
// StateCreated guard was lifted.
func TestPrepareWorkerInfo_CodexFreshStartEmptyTurnsNoInjection(t *testing.T) {
	t.Parallel()
	hub := newTestHub(t)
	ts := new(mockTurnsStore)
	ts.On("QueryTurns", mock.Anything, "sess-new", 50, 0).
		Return([]*eventstore.TurnRecord{}, nil)

	b := &Bridge{
		log:           slog.Default(),
		hub:           hub,
		turnsQuerier:  ts,
		shutdownCtx:   context.Background(),
		compressCache: sync.Map{},
	}
	b.closed.Store(true)

	si := &session.SessionInfo{
		ID:         "sess-new",
		UserID:     "u1",
		WorkerType: worker.TypeCodexCLI,
		State:      events.StateCreated,
	}

	info := b.prepareWorkerInfo("sess-new", "u1", "", si)
	require.Empty(t, info.ConversationHistory,
		"genuinely new session (empty turns) must not be injected")
}

func TestPrepareWorkerInfo_ACPInjectsHistory(t *testing.T) {
	t.Parallel()
	hub := newTestHub(t)
	ts := new(mockTurnsStore)
	turns := []*eventstore.TurnRecord{
		{ID: 11, SessionID: "sess-acp", Role: "user", Content: "ACP literal user turn", CreatedAt: 1_782_780_700},
		{ID: 12, SessionID: "sess-acp", Role: "assistant", Content: "ACP literal assistant turn", CreatedAt: 1_782_780_701},
	}
	ts.On("QueryTurns", mock.Anything, "sess-acp", 50, 0).Return(turns, nil).Once()

	b := &Bridge{
		log:           slog.Default(),
		hub:           hub,
		turnsQuerier:  ts,
		shutdownCtx:   context.Background(),
		compressCache: sync.Map{},
	}
	b.closed.Store(true) // skip async compression goroutine

	si := &session.SessionInfo{
		ID:         "sess-acp",
		UserID:     "u1",
		WorkerType: worker.TypeACP,
		State:      events.StateCreated,
	}

	info := b.prepareWorkerInfo("sess-acp", "u1", "", si)

	require.Equal(t, []worker.ConversationTurn{
		{Role: "user", Content: "ACP literal user turn"},
		{Role: "assistant", Content: "ACP literal assistant turn"},
	}, info.ConversationHistory)
	ts.AssertExpectations(t)
}

func TestPrepareWorkerInfo_ACPEmptyTurnsNoInjection(t *testing.T) {
	t.Parallel()
	hub := newTestHub(t)
	ts := new(mockTurnsStore)
	ts.On("QueryTurns", mock.Anything, "sess-acp-empty", 50, 0).
		Return([]*eventstore.TurnRecord{}, nil).Once()

	b := &Bridge{
		log:           slog.Default(),
		hub:           hub,
		turnsQuerier:  ts,
		shutdownCtx:   context.Background(),
		compressCache: sync.Map{},
	}
	b.closed.Store(true)

	si := &session.SessionInfo{
		ID:         "sess-acp-empty",
		UserID:     "u1",
		WorkerType: worker.TypeACP,
		State:      events.StateCreated,
	}

	info := b.prepareWorkerInfo("sess-acp-empty", "u1", "", si)
	require.Empty(t, info.ConversationHistory,
		"a genuinely new ACP session must not receive synthetic history")
	ts.AssertExpectations(t)
}

func TestPrepareWorkerInfo_ACPQueryErrorNoInjection(t *testing.T) {
	t.Parallel()
	hub := newTestHub(t)
	ts := new(mockTurnsStore)
	ts.On("QueryTurns", mock.Anything, "sess-acp-error", 50, 0).
		Return(nil, errors.New("turn store unavailable")).Once()

	b := &Bridge{
		log:           slog.Default(),
		hub:           hub,
		turnsQuerier:  ts,
		shutdownCtx:   context.Background(),
		compressCache: sync.Map{},
	}
	b.closed.Store(true)

	si := &session.SessionInfo{
		ID:         "sess-acp-error",
		UserID:     "u1",
		WorkerType: worker.TypeACP,
		State:      events.StateCreated,
	}

	info := b.prepareWorkerInfo("sess-acp-error", "u1", "", si)
	require.Empty(t, info.ConversationHistory,
		"history query errors must not inject partial ACP history")
	ts.AssertExpectations(t)
}

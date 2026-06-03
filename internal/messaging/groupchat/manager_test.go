package groupchat

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockStore struct {
	mu     sync.Mutex
	groups map[string]*GroupSession
	turns  map[string][]*TurnRecord
	audits []*AuditEvent
}

func newMockStore() *mockStore {
	return &mockStore{
		groups: make(map[string]*GroupSession),
		turns:  make(map[string][]*TurnRecord),
	}
}

func (m *mockStore) CreateGroup(_ context.Context, gs *GroupSession) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.groups[gs.ID] = gs
	return nil
}

func (m *mockStore) GetGroup(_ context.Context, id string) (*GroupSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	gs, ok := m.groups[id]
	if !ok {
		return nil, ErrGroupNotFound
	}
	return gs, nil
}

func (m *mockStore) UpdateGroupState(_ context.Context, id string, state GroupState, endReason EndReason) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if gs, ok := m.groups[id]; ok {
		gs.State = state
		gs.EndReason = endReason
		if state != GroupStateActive {
			now := time.Now()
			gs.EndedAt = &now
		}
	}
	return nil
}

func (m *mockStore) UpdateGroupCost(_ context.Context, id string, turnCount int, costAccumulated float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if gs, ok := m.groups[id]; ok {
		gs.TurnCount = turnCount
		gs.CostAccumulated = costAccumulated
	}
	return nil
}

func (m *mockStore) ListActiveByOwner(_ context.Context, ownerID string) ([]*GroupSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*GroupSession
	for _, gs := range m.groups {
		if gs.OwnerID == ownerID && gs.State == GroupStateActive {
			result = append(result, gs)
		}
	}
	return result, nil
}

func (m *mockStore) CountActive(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, gs := range m.groups {
		if gs.State == GroupStateActive {
			count++
		}
	}
	return count, nil
}

func (m *mockStore) CountActiveByOwner(_ context.Context, ownerID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, gs := range m.groups {
		if gs.OwnerID == ownerID && gs.State == GroupStateActive {
			count++
		}
	}
	return count, nil
}

func (m *mockStore) ListActive(_ context.Context) ([]*GroupSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*GroupSession
	for _, gs := range m.groups {
		if gs.State == GroupStateActive {
			result = append(result, gs)
		}
	}
	return result, nil
}

func (m *mockStore) AppendTurn(_ context.Context, t *TurnRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turns[t.GroupID] = append(m.turns[t.GroupID], t)
	return nil
}

func (m *mockStore) ListTurns(_ context.Context, groupID string) ([]*TurnRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.turns[groupID], nil
}

func (m *mockStore) RecordAudit(_ context.Context, e *AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.audits = append(m.audits, e)
	return nil
}

func (m *mockStore) Close() error { return nil }

type mockBotLookup struct {
	bots map[string]BotEntry
}

func (m *mockBotLookup) GetByName(name string) (BotEntry, bool) {
	b, ok := m.bots[name]
	return b, ok
}

type mockResponseSender struct {
	mu       sync.Mutex
	messages []string
}

func (m *mockResponseSender) SendTurnResponse(_ context.Context, _, _, _, botName, content string, turnNum int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, botName+":"+content)
	return nil
}

func (m *mockResponseSender) SendControlMessage(_ context.Context, _, _, _, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, message)
	return nil
}

type mockBridgeStarter struct {
	started atomic.Int32
}

func (m *mockBridgeStarter) StartSession(_ context.Context, _, _, _ string, _ worker.WorkerType, _ []string, _, _ string, _ map[string]string, _, _ string, _ ...string) error {
	m.started.Add(1)
	return nil
}

type mockSessionStateChecker struct {
	writers map[string]worker.Worker
	states  map[string]events.SessionState
}

func (m *mockSessionStateChecker) Get(_ context.Context, id string) (*session.SessionInfo, error) {
	state := events.StateRunning
	if s, ok := m.states[id]; ok {
		state = s
	}
	return &session.SessionInfo{State: state}, nil
}

func (m *mockSessionStateChecker) GetWorker(id string) worker.Worker {
	return m.writers[id]
}

func (m *mockSessionStateChecker) Transition(_ context.Context, id string, to events.SessionState) error {
	m.states[id] = to
	return nil
}

// mockWorker is not currently used but kept for future turn-loop integration tests.
// Uncomment when adding tests that exercise executeTurn.
// type mockWorker struct {
// 	inputs []string
// }
//
// func (m *mockWorker) Input(_ context.Context, content string, _ []any) error {
// 	m.inputs = append(m.inputs, content)
// 	return nil
// }
//
// func (m *mockWorker) Start(_ context.Context, _ []string) error { return nil }
// func (m *mockWorker) Terminate(_ context.Context) error         { return nil }
// func (m *mockWorker) Kill() error                               { return nil }
// func (m *mockWorker) Wait() error                               { return nil }
// func (m *mockWorker) Health() error                             { return nil }
// func (m *mockWorker) LastIO() time.Time                         { return time.Now() }
// func (m *mockWorker) ResetContext(string, string) error         { return nil }
// func (m *mockWorker) Metadata() map[string]string               { return nil }
// func (m *mockWorker) SetMetadata(string, string)                {}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestManager_StartDiscussion_NeedTwoBots(t *testing.T) {
	m := newTestManager(newMockStore())
	ctx := context.Background()

	_, err := m.StartDiscussion(ctx, "owner", "feishu", "ch", "", []string{"alice"}, "topic")
	require.Error(t, err)
	require.Contains(t, err.Error(), "need at least 2 bots")
}

func TestManager_StartDiscussion_BotNotFound(t *testing.T) {
	store := newMockStore()
	m := newTestManager(store)
	ctx := context.Background()

	_, err := m.StartDiscussion(ctx, "owner", "feishu", "ch", "", []string{"alice", "unknown"}, "topic")
	require.Error(t, err)
	require.Contains(t, err.Error(), `bot "unknown" not found`)
}

func TestManager_StartDiscussion_Success(t *testing.T) {
	store := newMockStore()
	m := newTestManager(store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	groupID, err := m.StartDiscussion(ctx, "owner_1", "feishu", "ch_1", "ts_1",
		[]string{"alice", "bob"}, "test topic")
	require.NoError(t, err)
	require.NotEmpty(t, groupID)

	// Verify session persisted.
	gs, err := store.GetGroup(ctx, groupID)
	require.NoError(t, err)
	require.Equal(t, "test topic", gs.Topic)
	require.Equal(t, "owner_1", gs.OwnerID)
	require.Equal(t, GroupStateActive, gs.State)
	require.Equal(t, []string{"bot_a", "bot_b"}, gs.BotIDs)

	// Cancel immediately to stop turn loop before it accesses nil worker.
	cancel()
	require.Eventually(t, func() bool {
		return m.GetActiveForChannel("ch_1", "ts_1") == nil
	}, 2*time.Second, 50*time.Millisecond)
}

func TestManager_StopDiscussion(t *testing.T) {
	store := newMockStore()
	m := newTestManager(store)
	ctx := context.Background()

	groupID, err := m.StartDiscussion(ctx, "owner_1", "feishu", "ch_1", "",
		[]string{"alice", "bob"}, "test topic")
	require.NoError(t, err)

	require.NoError(t, m.StopDiscussion(ctx, groupID))

	// Wait for cleanup.
	time.Sleep(200 * time.Millisecond)

	// Should no longer be active.
	require.Nil(t, m.GetActiveForChannel("ch_1", ""))
}

func TestManager_StopDiscussion_NotActive(t *testing.T) {
	m := newTestManager(newMockStore())
	err := m.StopDiscussion(context.Background(), "nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no active discussion")
}

func TestManager_GetActiveForChannel(t *testing.T) {
	store := newMockStore()
	m := newTestManager(store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// No active sessions.
	require.Nil(t, m.GetActiveForChannel("ch_1", ""))

	groupID, err := m.StartDiscussion(ctx, "owner_1", "feishu", "ch_1", "ts_1",
		[]string{"alice", "bob"}, "topic")
	require.NoError(t, err)

	// Should find the session.
	gs := m.GetActiveForChannel("ch_1", "ts_1")
	require.NotNil(t, gs)
	require.Equal(t, groupID, gs.ID)

	// Different channel/thread should not match.
	require.Nil(t, m.GetActiveForChannel("ch_2", ""))
	require.Nil(t, m.GetActiveForChannel("ch_1", "ts_other"))

	cancel()
	require.Eventually(t, func() bool {
		return m.GetActiveForChannel("ch_1", "ts_1") == nil
	}, 2*time.Second, 50*time.Millisecond)
}

func TestManager_StopAll(t *testing.T) {
	store := newMockStore()
	m := newTestManager(store)
	ctx := context.Background()

	_, err := m.StartDiscussion(ctx, "owner_1", "feishu", "ch_1", "",
		[]string{"alice", "bob"}, "topic 1")
	require.NoError(t, err)

	_, err = m.StartDiscussion(ctx, "owner_2", "feishu", "ch_2", "",
		[]string{"alice", "bob"}, "topic 2")
	require.NoError(t, err)

	m.StopAll(ctx)

	// All should be cleaned up.
	require.Nil(t, m.GetActiveForChannel("ch_1", ""))
	require.Nil(t, m.GetActiveForChannel("ch_2", ""))
}

func TestManager_RepairRunningSessions(t *testing.T) {
	store := newMockStore()
	m := newTestManager(store)
	ctx := context.Background()

	// Create a stale active session directly in store.
	gs := makeTestGroupSession("gs_repair")
	require.NoError(t, store.CreateGroup(ctx, gs))

	m.RepairRunningSessions(ctx)

	got, err := store.GetGroup(ctx, "gs_repair")
	require.NoError(t, err)
	require.Equal(t, GroupStateGatewayRestart, got.State)
}

func TestManager_checkQuotas(t *testing.T) {
	t.Parallel()

	t.Run("global limit", func(t *testing.T) {
		store := newMockStore()
		cfg := DefaultConfig()
		cfg.MaxGroupSessions = 1
		m := newTestManagerWithConfig(store, cfg)

		// Insert one active session directly.
		gs := makeTestGroupSession("gs_q1")
		require.NoError(t, store.CreateGroup(context.Background(), gs))

		err := m.checkQuotas(context.Background(), "owner")
		require.Error(t, err)
		require.Contains(t, err.Error(), "global limit")
	})

	t.Run("user limit", func(t *testing.T) {
		store := newMockStore()
		cfg := DefaultConfig()
		cfg.MaxSessionsPerUser = 1
		m := newTestManagerWithConfig(store, cfg)

		gs := makeTestGroupSession("gs_qu")
		gs.OwnerID = "user_1"
		require.NoError(t, store.CreateGroup(context.Background(), gs))

		err := m.checkQuotas(context.Background(), "user_1")
		require.Error(t, err)
		require.Contains(t, err.Error(), "user limit")
	})

	t.Run("under limits ok", func(t *testing.T) {
		store := newMockStore()
		cfg := DefaultConfig()
		m := newTestManagerWithConfig(store, cfg)

		err := m.checkQuotas(context.Background(), "owner")
		require.NoError(t, err)
	})
}

func TestManager_buildTurnPrompt(t *testing.T) {
	m := newTestManager(newMockStore())

	prompt := m.buildTurnPrompt("Alice", "Test Topic", "")
	require.Contains(t, prompt, "多人讨论")
	require.Contains(t, prompt, "Test Topic")
	require.Contains(t, prompt, "Alice")
	require.Contains(t, prompt, "SKIP")
}

func TestManager_buildTurnPrompt_WithTranscript(t *testing.T) {
	m := newTestManager(newMockStore())

	transcript := "\n## Bob:\nHello world\n"
	prompt := m.buildTurnPrompt("Alice", "Topic", transcript)
	require.Contains(t, prompt, "讨论历史")
	require.Contains(t, prompt, "Hello world")
}

func TestManager_buildTurnPrompt_TruncatesTranscript(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxTotalContextLength = 50
	store := newMockStore()
	m := newTestManagerWithConfig(store, cfg)

	var b strings.Builder
	for range 200 {
		b.WriteString("这是一段很长的讨论内容。")
	}
	longTranscript := b.String()
	prompt := m.buildTurnPrompt("Alice", "Topic", longTranscript)
	// The transcript part should be truncated.
	require.Contains(t, prompt, "讨论历史")
}

func TestManager_SetResponseSender(t *testing.T) {
	m := newTestManager(newMockStore())
	sender := &mockResponseSender{}
	m.SetResponseSender(sender)
	// Verify no panic.
}

func TestManager_extractResponse_NilExtractor(t *testing.T) {
	m := newTestManager(newMockStore())
	result := m.extractResponse(context.Background(), "session_1")
	require.Equal(t, "", result)
}

func TestFormatBotList(t *testing.T) {
	gs := &GroupSession{
		BotIDs:   []string{"bot_a", "bot_b"},
		BotNames: map[string]string{"bot_a": "Alice", "bot_b": "Bob"},
	}
	result := formatBotList(gs)
	require.Equal(t, "@Alice, @Bob", result)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestManager(store *mockStore) *Manager {
	return newTestManagerWithConfig(store, DefaultConfig())
}

func newTestManagerWithConfig(store *mockStore, cfg Config) *Manager {
	bots := &mockBotLookup{
		bots: map[string]BotEntry{
			"alice": {Name: "alice", BotID: "bot_a", WorkerType: "claude_code"},
			"bob":   {Name: "bob", BotID: "bot_b", WorkerType: "claude_code"},
		},
	}
	sender := &mockResponseSender{}

	return NewManager(
		slog.New(slog.NewTextHandler(io.Discard, nil)), // log
		cfg,
		store,
		&mockBridgeStarter{},
		&mockSessionStateChecker{
			writers: make(map[string]worker.Worker),
			states:  make(map[string]events.SessionState),
		},
		bots,
		sender,
		nil, // extractor
	)
}

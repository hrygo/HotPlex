package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"hotplex-worker/internal/config"
	"hotplex-worker/internal/worker"
	"hotplex-worker/pkg/events"
)

// ─── mockStore implements Store for testing ───────────────────────────────────

type mockStore struct {
	mock.Mock
}

func (m *mockStore) Upsert(ctx context.Context, info *SessionInfo) error {
	args := m.Called(ctx, info)
	if args.Error(0) == nil {
		// Copy fields back to info so callers see updated state
		if ms, ok := args.Get(0).(*SessionInfo); ok {
			*info = *ms
		}
	}
	return args.Error(0)
}

func (m *mockStore) Get(ctx context.Context, id string) (*SessionInfo, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*SessionInfo), args.Error(1)
}

func (m *mockStore) List(ctx context.Context, limit, offset int) ([]*SessionInfo, error) {
	args := m.Called(ctx, limit, offset)
	return args.Get(0).([]*SessionInfo), args.Error(1)
}

func (m *mockStore) GetExpiredMaxLifetime(ctx context.Context, now time.Time) ([]string, error) {
	args := m.Called(ctx, now)
	return args.Get(0).([]string), args.Error(1)
}

func (m *mockStore) GetExpiredIdle(ctx context.Context, now time.Time) ([]string, error) {
	args := m.Called(ctx, now)
	return args.Get(0).([]string), args.Error(1)
}

func (m *mockStore) DeleteTerminated(ctx context.Context, cutoff time.Time) error {
	args := m.Called(ctx, cutoff)
	return args.Error(0)
}

func (m *mockStore) Close() error {
	args := m.Called()
	return args.Error(0)
}

// ─── state transition tests ───────────────────────────────────────────────────

func TestStateTransition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		from    events.SessionState
		to      events.SessionState
		wantErr bool
	}{
		// CREATED transitions
		{"CREATED → RUNNING", events.StateCreated, events.StateRunning, false},
		{"CREATED → TERMINATED", events.StateCreated, events.StateTerminated, false},
		{"CREATED → IDLE invalid", events.StateCreated, events.StateIdle, true},
		{"CREATED → DELETED invalid", events.StateCreated, events.StateDeleted, true},

		// RUNNING transitions
		{"RUNNING → IDLE", events.StateRunning, events.StateIdle, false},
		{"RUNNING → TERMINATED", events.StateRunning, events.StateTerminated, false},
		{"RUNNING → DELETED", events.StateRunning, events.StateDeleted, false},
		{"RUNNING → CREATED invalid", events.StateRunning, events.StateCreated, true},

		// IDLE transitions
		{"IDLE → RUNNING", events.StateIdle, events.StateRunning, false},
		{"IDLE → TERMINATED", events.StateIdle, events.StateTerminated, false},
		{"IDLE → DELETED", events.StateIdle, events.StateDeleted, false},
		{"IDLE → CREATED invalid", events.StateIdle, events.StateCreated, true},

		// TERMINATED transitions
		{"TERMINATED → RUNNING (resume)", events.StateTerminated, events.StateRunning, false},
		{"TERMINATED → DELETED", events.StateTerminated, events.StateDeleted, false},
		{"TERMINATED → IDLE invalid", events.StateTerminated, events.StateIdle, true},
		{"TERMINATED → CREATED invalid", events.StateTerminated, events.StateCreated, true},

		// DELETED is terminal
		{"DELETED → RUNNING invalid", events.StateDeleted, events.StateRunning, true},
		{"DELETED → IDLE invalid", events.StateDeleted, events.StateIdle, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ok := events.IsValidTransition(tt.from, tt.to)
			if tt.wantErr {
				require.False(t, ok)
			} else {
				require.True(t, ok)
			}
		})
	}
}

func TestManager_Create(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Upsert", ctx, mock.AnythingOfType("*session.SessionInfo")).
		Return(nil)
	store.On("Close").Return(nil)

	m, err := NewManager(ctx, nil, cfg, store, nil)
	require.NoError(t, err)
	defer m.Close()

	info, err := m.Create(ctx, "sess_new", "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, "sess_new", info.ID)
	require.Equal(t, "user1", info.UserID)
	require.Equal(t, worker.TypeClaudeCode, info.WorkerType)
	require.Equal(t, events.StateCreated, info.State)
	require.NotNil(t, info.ExpiresAt)
}

func TestManager_Get(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, store, nil)
	require.NoError(t, err)
	defer m.Close()

	// Not in memory, falls back to store
	now := time.Now()
	expected := &SessionInfo{
		ID:         "sess_existing",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	store.On("Get", ctx, "sess_existing").Return(expected, nil)

	info, err := m.Get("sess_existing")
	require.NoError(t, err)
	require.Equal(t, "sess_existing", info.ID)
	require.Equal(t, events.StateRunning, info.State)

	// After Get, session should be in memory map
	store.On("Upsert", ctx, mock.AnythingOfType("*session.SessionInfo")).Return(nil)
	store.On("Get", ctx, "sess_existing").Return(expected, nil).Maybe()

	// In-memory hit
	info2, err := m.Get("sess_existing")
	require.NoError(t, err)
	require.Equal(t, "sess_existing", info2.ID)
}

func TestManager_Get_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Get", ctx, "sess_missing").Return(nil, ErrSessionNotFound)
	store.On("Close").Return(nil)

	m, err := NewManager(ctx, nil, cfg, store, nil)
	require.NoError(t, err)
	defer m.Close()

	_, err = m.Get("sess_missing")
	require.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestManager_Transition(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, store, nil)
	require.NoError(t, err)
	defer m.Close()

	// Seed a session in memory
	now := time.Now()
	seed := &SessionInfo{
		ID:         "sess_trans",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateCreated,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	m.mu.Lock()
	m.sessions["sess_trans"] = &managedSession{info: *seed}
	m.mu.Unlock()

	store.On("Upsert", ctx, mock.AnythingOfType("*session.SessionInfo")).Return(nil)

	err = m.Transition(ctx, "sess_trans", events.StateRunning)
	require.NoError(t, err)

	info, _ := m.Get("sess_trans")
	require.Equal(t, events.StateRunning, info.State)
}

func TestManager_Transition_Invalid(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, store, nil)
	require.NoError(t, err)
	defer m.Close()

	// Seed a CREATED session
	seed := &SessionInfo{
		ID:         "sess_invalid",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateCreated,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_invalid"] = &managedSession{info: *seed}
	m.mu.Unlock()

	// Cannot go CREATED → IDLE directly
	err = m.Transition(ctx, "sess_invalid", events.StateIdle)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidTransition))
}

func TestManager_Transition_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Get", ctx, "sess_ghost").Return(nil, ErrSessionNotFound)
	store.On("Close").Return(nil)

	m, err := NewManager(ctx, nil, cfg, store, nil)
	require.NoError(t, err)
	defer m.Close()

	err = m.Transition(ctx, "sess_ghost", events.StateRunning)
	require.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestManager_TransitionWithInput_Atomic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, store, nil)
	require.NoError(t, err)
	defer m.Close()

	seed := &SessionInfo{
		ID:         "sess_atomic",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_atomic"] = &managedSession{info: *seed}
	m.mu.Unlock()

	store.On("Upsert", ctx, mock.AnythingOfType("*session.SessionInfo")).Return(nil)

	// TransitionWithInput should succeed atomically
	err = m.TransitionWithInput(ctx, "sess_atomic", events.StateIdle, "user input", nil)
	require.NoError(t, err)
}

func TestManager_TransitionWithInput_InvalidTransition(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, store, nil)
	require.NoError(t, err)
	defer m.Close()

	seed := &SessionInfo{
		ID:         "sess_atomic_inv",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateCreated,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_atomic_inv"] = &managedSession{info: *seed}
	m.mu.Unlock()

	err = m.TransitionWithInput(ctx, "sess_atomic_inv", events.StateIdle, "input", nil)
	require.Error(t, err)
}

func TestSessionBusy_RejectWhenNotActive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, store, nil)
	require.NoError(t, err)
	defer m.Close()

	// Seed a TERMINATED session
	seed := &SessionInfo{
		ID:         "sess_busy",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateTerminated,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_busy"] = &managedSession{info: *seed}
	m.mu.Unlock()

	// Attempt TransitionWithInput on TERMINATED → IDLE is invalid (TERMINATED → IDLE not allowed)
	store.On("Upsert", ctx, mock.AnythingOfType("*session.SessionInfo")).Return(nil)

	err = m.TransitionWithInput(ctx, "sess_busy", events.StateIdle, "input", nil)
	require.Error(t, err)
}

func TestManager_Delete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, store, nil)
	require.NoError(t, err)
	defer m.Close()

	seed := &SessionInfo{
		ID:         "sess_del",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateTerminated,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_del"] = &managedSession{info: *seed}
	m.mu.Unlock()

	store.On("Upsert", ctx, mock.AnythingOfType("*session.SessionInfo")).Return(nil)

	err = m.Delete(ctx, "sess_del")
	require.NoError(t, err)

	// Session should be removed from in-memory map
	m.mu.RLock()
	_, ok := m.sessions["sess_del"]
	m.mu.RUnlock()
	require.False(t, ok)
}

func TestManager_ValidateOwnership(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, store, nil)
	require.NoError(t, err)
	defer m.Close()

	seed := &SessionInfo{
		ID:         "sess_owner",
		UserID:     "user_owner",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_owner"] = &managedSession{info: *seed}
	m.mu.Unlock()

	// Owner matches
	err = m.ValidateOwnership(ctx, "sess_owner", "user_owner", "")
	require.NoError(t, err)

	// Owner mismatch
	err = m.ValidateOwnership(ctx, "sess_owner", "wrong_user", "")
	require.True(t, errors.Is(err, ErrOwnershipMismatch))

	// Admin bypass
	err = m.ValidateOwnership(ctx, "sess_owner", "wrong_user", "admin_user")
	require.NoError(t, err)
}

func TestManager_ValidateOwnership_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Get", ctx, "sess_missing").Return(nil, ErrSessionNotFound)
	store.On("Close").Return(nil)

	m, err := NewManager(ctx, nil, cfg, store, nil)
	require.NoError(t, err)
	defer m.Close()

	err = m.ValidateOwnership(ctx, "sess_missing", "user1", "")
	require.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestManager_Lock(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, store, nil)
	require.NoError(t, err)
	defer m.Close()

	seed := &SessionInfo{
		ID:         "sess_lock",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_lock"] = &managedSession{info: *seed}
	m.mu.Unlock()

	// Lock and immediately unlock
	unlock, err := m.Lock("sess_lock")
	require.NoError(t, err)
	require.NotNil(t, unlock)
	unlock()
}

func TestManager_Lock_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Get", ctx, "sess_ghost_lock").Return(nil, ErrSessionNotFound)
	store.On("Close").Return(nil)

	m, err := NewManager(ctx, nil, cfg, store, nil)
	require.NoError(t, err)
	defer m.Close()

	_, err = m.Lock("sess_ghost_lock")
	require.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestManager_List(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, store, nil)
	require.NoError(t, err)
	defer m.Close()

	expected := []*SessionInfo{
		{ID: "sess_1", UserID: "user1", WorkerType: worker.TypeClaudeCode, State: events.StateRunning},
		{ID: "sess_2", UserID: "user2", WorkerType: worker.TypeClaudeCode, State: events.StateIdle},
	}
	store.On("List", ctx, 50, 0).Return(expected, nil)

	list, err := m.List(ctx, 50, 0)
	require.NoError(t, err)
	require.Len(t, list, 2)
}

func TestManager_ListActive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, store, nil)
	require.NoError(t, err)
	defer m.Close()

	// Seed sessions
	for _, id := range []string{"sess_a", "sess_b"} {
		m.mu.Lock()
		m.sessions[id] = &managedSession{info: SessionInfo{
			ID:         id,
			UserID:     "user1",
			WorkerType: worker.TypeClaudeCode,
			State:      events.StateRunning,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}}
		m.mu.Unlock()
	}

	active := m.ListActive()
	require.Len(t, active, 2)
}

func TestManager_Stats(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, store, nil)
	require.NoError(t, err)
	defer m.Close()

	total, max, users := m.Stats()
	require.Equal(t, 0, total)
	require.Equal(t, cfg.Pool.MaxSize, max)
	require.Equal(t, 0, users)
}

func TestSessionInfo_IsActive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state events.SessionState
		active bool
	}{
		{events.StateCreated, true},
		{events.StateRunning, true},
		{events.StateIdle, true},
		{events.StateTerminated, false},
		{events.StateDeleted, false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(string(tt.state), func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.active, tt.state.IsActive())
		})
	}
}

func TestSessionInfo_IsTerminal(t *testing.T) {
	t.Parallel()

	require.True(t, events.StateDeleted.IsTerminal())
	require.False(t, events.StateTerminated.IsTerminal())
	require.False(t, events.StateRunning.IsTerminal())
}

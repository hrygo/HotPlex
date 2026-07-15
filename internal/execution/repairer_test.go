package execution

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/pkg/events"
)

type flakyStore struct {
	*SQLStore
	mu    sync.Mutex
	fail  bool
	calls int64
}

func (f *flakyStore) SetDelivery(ctx context.Context, execID, ownerID string, status Status, ec string) error {
	atomic.AddInt64(&f.calls, 1)
	f.mu.Lock()
	fail := f.fail
	f.mu.Unlock()
	if fail {
		return errors.New("db temporarily unavailable")
	}
	return f.SQLStore.SetDelivery(ctx, execID, ownerID, status, ec)
}

func (f *flakyStore) FinishRuntime(ctx context.Context, execID, runID string, status RuntimeStatus, ec string) error {
	atomic.AddInt64(&f.calls, 1)
	f.mu.Lock()
	fail := f.fail
	f.mu.Unlock()
	if fail {
		return errors.New("db temporarily unavailable")
	}
	return f.SQLStore.FinishRuntime(ctx, execID, runID, status, ec)
}

func (f *flakyStore) setFail(v bool) {
	f.mu.Lock()
	f.fail = v
	f.mu.Unlock()
}

func fastRepairConfig() RepairConfig {
	return RepairConfig{
		QueueCapacity:    4,
		InitialBackoff:   20 * time.Millisecond,
		MaxBackoff:       100 * time.Millisecond,
		MaxLifetime:      2 * time.Second,
		ShutdownTimeout:  500 * time.Millisecond,
		SyncRetryTimeout: 20 * time.Millisecond,
		TickInterval:     10 * time.Millisecond,
	}
}

func newRepairTestStore(t *testing.T) (*flakyStore, *session.SQLiteStore) {
	t.Helper()
	store, sessionStore := newTestSQLStore(t)
	return &flakyStore{SQLStore: store}, sessionStore
}

func ensureRepairSession(t *testing.T, ss *session.SQLiteStore, sessionID string) {
	t.Helper()
	if sessionID == "session-1" {
		return
	}
	now := time.Now()
	require.NoError(t, ss.Upsert(context.Background(), &session.SessionInfo{
		ID: sessionID, UserID: "u1", WorkerType: "claude_code",
		State: events.StateRunning, CreatedAt: now, UpdatedAt: now,
	}))
}

func TestRepairer_SuccessfulProcessing(t *testing.T) {
	t.Parallel()
	store, sessionStore := newRepairTestStore(t)
	ensureRepairSession(t, sessionStore, "session-rp1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec, _, err := store.Accept(ctx, AcceptRequest{
		SessionID: "session-rp1", ClientMessageID: "msg-rp1", PayloadHash: "h",
		OwnerInstanceID: testOwner, WorkerRunID: testRun,
	})
	require.NoError(t, err)

	r := NewRepairer(store, fastRepairConfig(), nil)
	r.Start(ctx)

	r.Enqueue(RepairIntent{
		ExecutionID: rec.ExecutionID,
		OwnerID:     testOwner,
		Kind:        RepairDelivery,
		Status:      string(StatusDelivered),
	})

	require.Eventually(t, func() bool {
		stored, err := store.getByID(context.Background(), rec.ExecutionID)
		if err != nil {
			return false
		}
		return stored.Status == StatusDelivered
	}, 2*time.Second, 10*time.Millisecond, "delivery status must be repaired to delivered")

	r.Shutdown(context.Background())
}

func TestRepairer_RetryAfterFailure(t *testing.T) {
	t.Parallel()
	store, sessionStore := newRepairTestStore(t)
	ensureRepairSession(t, sessionStore, "session-rp2")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec, _, err := store.Accept(ctx, AcceptRequest{
		SessionID: "session-rp2", ClientMessageID: "msg-rp2", PayloadHash: "h",
		OwnerInstanceID: testOwner, WorkerRunID: testRun,
	})
	require.NoError(t, err)

	store.setFail(true)
	r := NewRepairer(store, fastRepairConfig(), nil)
	r.Start(ctx)

	r.Enqueue(RepairIntent{
		ExecutionID: rec.ExecutionID,
		OwnerID:     testOwner,
		Kind:        RepairDelivery,
		Status:      string(StatusDelivered),
	})

	require.Eventually(t, func() bool {
		return atomic.LoadInt64(&store.calls) >= 3
	}, 2*time.Second, 10*time.Millisecond, "must have attempted at least 3 retries")

	require.Greater(t, r.Backlog(), 0, "intent must still be in queue while DB is down")

	store.setFail(false)

	require.Eventually(t, func() bool {
		stored, err := store.getByID(context.Background(), rec.ExecutionID)
		if err != nil {
			return false
		}
		return stored.Status == StatusDelivered && r.Backlog() == 0
	}, 2*time.Second, 10*time.Millisecond, "must succeed after DB recovers")

	r.Shutdown(context.Background())
}

func TestRepairer_MergeTerminalPreferred(t *testing.T) {
	t.Parallel()
	store, sessionStore := newRepairTestStore(t)
	ensureRepairSession(t, sessionStore, "session-rp3")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec, _, err := store.Accept(ctx, AcceptRequest{
		SessionID: "session-rp3", ClientMessageID: "msg-rp3", PayloadHash: "h",
		OwnerInstanceID: testOwner, WorkerRunID: testRun,
	})
	require.NoError(t, err)
	require.NoError(t, store.MarkRunning(ctx, rec.ExecutionID, testOwner, testRun))

	r := NewRepairer(store, fastRepairConfig(), nil)
	r.Start(ctx)
	store.setFail(true)

	r.Enqueue(RepairIntent{
		ExecutionID: rec.ExecutionID,
		WorkerRunID: testRun,
		Kind:        RepairRuntime,
		Status:      string(RuntimeUnknown),
	})

	r.Enqueue(RepairIntent{
		ExecutionID: rec.ExecutionID,
		WorkerRunID: testRun,
		Kind:        RepairRuntime,
		Status:      string(RuntimeCompleted),
	})

	status, _, found := r.Lookup(rec.ExecutionID)
	require.True(t, found)
	require.Equal(t, string(RuntimeCompleted), status, "completed must replace unknown")

	r.Enqueue(RepairIntent{
		ExecutionID: rec.ExecutionID,
		WorkerRunID: testRun,
		Kind:        RepairRuntime,
		Status:      string(RuntimeUnknown),
	})

	status, _, found = r.Lookup(rec.ExecutionID)
	require.True(t, found)
	require.Equal(t, string(RuntimeCompleted), status, "completed must NOT regress to unknown")

	store.setFail(false)
	r.Shutdown(context.Background())
}

func TestRepairer_MaxLifetimeTimeout(t *testing.T) {
	t.Parallel()
	store, sessionStore := newRepairTestStore(t)
	ensureRepairSession(t, sessionStore, "session-rp4")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec, _, err := store.Accept(ctx, AcceptRequest{
		SessionID: "session-rp4", ClientMessageID: "msg-rp4", PayloadHash: "h",
		OwnerInstanceID: testOwner, WorkerRunID: testRun,
	})
	require.NoError(t, err)

	cfg := fastRepairConfig()
	cfg.MaxLifetime = 100 * time.Millisecond

	store.setFail(true)
	r := NewRepairer(store, cfg, nil)
	r.Start(ctx)

	r.Enqueue(RepairIntent{
		ExecutionID: rec.ExecutionID,
		OwnerID:     testOwner,
		Kind:        RepairDelivery,
		Status:      string(StatusDelivered),
	})

	require.Eventually(t, func() bool {
		_, _, timedOut, _ := r.Stats()
		return timedOut > 0
	}, 3*time.Second, 10*time.Millisecond, "intent must time out after MaxLifetime")

	require.Equal(t, 0, r.Backlog(), "timed-out intent must be removed from queue")

	store.setFail(false)
	r.Shutdown(context.Background())
}

func TestRepairer_LookupForDuplicateOverlay(t *testing.T) {
	t.Parallel()
	store, sessionStore := newRepairTestStore(t)
	ensureRepairSession(t, sessionStore, "session-rp5")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec, _, err := store.Accept(ctx, AcceptRequest{
		SessionID: "session-rp5", ClientMessageID: "msg-rp5", PayloadHash: "h",
		OwnerInstanceID: testOwner, WorkerRunID: testRun,
	})
	require.NoError(t, err)

	store.setFail(true)
	r := NewRepairer(store, fastRepairConfig(), nil)
	r.Start(ctx)

	r.Enqueue(RepairIntent{
		ExecutionID: rec.ExecutionID,
		OwnerID:     testOwner,
		Kind:        RepairDelivery,
		Status:      string(StatusDelivered),
	})

	status, kind, found := r.Lookup(rec.ExecutionID)
	require.True(t, found)
	require.Equal(t, string(StatusDelivered), status)
	require.Equal(t, "delivery", kind)

	store.setFail(false)
	r.Shutdown(context.Background())
}

func TestRepairer_ShutdownDrainCompletes(t *testing.T) {
	t.Parallel()
	store, sessionStore := newRepairTestStore(t)
	ensureRepairSession(t, sessionStore, "session-rp6")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec, _, err := store.Accept(ctx, AcceptRequest{
		SessionID: "session-rp6", ClientMessageID: "msg-rp6", PayloadHash: "h",
		OwnerInstanceID: testOwner, WorkerRunID: testRun,
	})
	require.NoError(t, err)

	r := NewRepairer(store, fastRepairConfig(), nil)
	r.Start(ctx)

	r.Enqueue(RepairIntent{
		ExecutionID: rec.ExecutionID,
		OwnerID:     testOwner,
		Kind:        RepairDelivery,
		Status:      string(StatusDelivered),
	})

	start := time.Now()
	r.Shutdown(context.Background())
	elapsed := time.Since(start)

	require.Less(t, elapsed, 1*time.Second, "shutdown must complete quickly when DB is healthy")
	require.Equal(t, 0, r.Backlog(), "backlog must be empty after successful drain")
}

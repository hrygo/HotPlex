package opencodeserver

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

func TestNewSingletonProcessManager(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{
		IdleDrainPeriod: 30 * time.Minute,
		ReadyTimeout:    10 * time.Second,
		HTTPTimeout:     30 * time.Second,
	}
	mgr := NewSingletonProcessManager(log, cfg)

	require.NotNil(t, mgr)
	require.Equal(t, stateIdle, mgr.state)
	require.Equal(t, 0, mgr.refs)
	require.NotNil(t, mgr.client)
	require.NotNil(t, mgr.sseClient)
	require.NotNil(t, mgr.crashCh)
}

func TestSingletonProcessManager_IsRunning(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	// Initial state is idle → not running.
	require.False(t, mgr.IsRunning())

	// Simulate running state.
	mgr.mu.Lock()
	mgr.state = stateRunning
	mgr.mu.Unlock()

	require.True(t, mgr.IsRunning())
}

func TestSingletonProcessManager_PID_NoProcess(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	// No process started yet.
	require.Equal(t, 0, mgr.PID())
}

func TestSingletonProcessManager_allocatePort(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	port, err := mgr.allocatePort()
	require.NoError(t, err)
	require.Greater(t, port, 0)
	require.Less(t, port, 65536)
}

func TestSingletonProcessManager_allocatePort_Multiple(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	for range 5 {
		port, err := mgr.allocatePort()
		require.NoError(t, err)
		require.Greater(t, port, 0)
		require.Less(t, port, 65536)
	}
}

func TestSingletonProcessManager_Release_NoRefs(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	// Release with zero refs: should not panic, idempotent.
	mgr.Release()
	require.Equal(t, 0, mgr.refs)
}

func TestSingletonProcessManager_Release_DecrementsRef(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	// Manually set state to running and refs > 0.
	mgr.mu.Lock()
	mgr.state = stateRunning
	mgr.refs = 2
	mgr.mu.Unlock()

	mgr.Release()
	require.Equal(t, 1, mgr.refs)
}

func TestSingletonProcessManager_Release_StartsIdleDrain(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{
		IdleDrainPeriod: 100 * time.Millisecond,
	}
	mgr := NewSingletonProcessManager(log, cfg)

	// Set running with 1 ref.
	mgr.mu.Lock()
	mgr.state = stateRunning
	mgr.refs = 1
	mgr.mu.Unlock()

	mgr.Release()
	require.Equal(t, 0, mgr.refs)
	// Idle drain should start.
	mgr.mu.Lock()
	hasTimer := mgr.idleTimer != nil
	mgr.mu.Unlock()
	require.True(t, hasTimer, "idle drain timer should be started when refs reach 0")
}

func TestSingletonProcessManager_Shutdown(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	// Set state=running, refs>0, proc=nil (proc is nil when no real process started).
	mgr.mu.Lock()
	mgr.state = stateRunning
	mgr.refs = 5
	mgr.mu.Unlock()

	mgr.Shutdown(context.Background())

	require.Equal(t, stateStopped, mgr.state)
	// Refs unchanged when proc is nil (procure not started in test).
	mgr.mu.Lock()
	refs := mgr.refs
	mgr.mu.Unlock()
	require.Equal(t, 5, refs)
}

func TestSingletonProcessManager_Shutdown_AlreadyStopped(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	// Already stopped.
	mgr.mu.Lock()
	mgr.state = stateStopped
	mgr.mu.Unlock()

	// Shutdown again should not panic.
	mgr.Shutdown(context.Background())
	require.Equal(t, stateStopped, mgr.state)
}

func TestSingletonProcessManager_buildEnv(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	env := mgr.buildEnv()
	require.NotEmpty(t, env)
	// buildEnv delegates to base.BuildEnv which should populate env vars.
	for _, e := range env {
		require.NotEmpty(t, e)
	}
}

func TestSingletonProcessManager_Acquire_StoppedState(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	mgr.mu.Lock()
	mgr.state = stateStopped
	mgr.mu.Unlock()

	addr, client, sseClient, crash, err := mgr.Acquire(context.Background())
	require.Error(t, err)
	require.Empty(t, addr)
	require.Nil(t, client)
	require.Nil(t, sseClient)
	require.Nil(t, crash)
}

func TestInitSingleton(t *testing.T) {
	InitSingleton(slog.Default(), config.OpenCodeServerConfig{})
	require.NotNil(t, singleton.Load())
}

func TestShutdownSingleton_Nil(t *testing.T) {
	ShutdownSingleton(context.Background())
}

func TestShutdownSingleton_Real(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	InitSingleton(log, config.OpenCodeServerConfig{})
	require.NotNil(t, singleton.Load())

	ShutdownSingleton(context.Background())
	require.Nil(t, singleton.Load())
}

func TestNewSingletonProcessManager_SSEClient(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{
		HTTPTimeout: 30 * time.Second,
	}
	mgr := NewSingletonProcessManager(log, cfg)

	// Verify sseClient is created without timeout
	require.NotNil(t, mgr.sseClient)
	require.Zero(t, mgr.sseClient.Timeout, "sseClient should have no timeout")

	// Verify regular client has timeout
	require.NotNil(t, mgr.client)
	require.Equal(t, cfg.HTTPTimeout, mgr.client.Timeout)
}

func TestSingletonProcessManager_Acquire_ReturnsSSEClient(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	// Simulate running state
	mgr.mu.Lock()
	mgr.state = stateRunning
	mgr.httpAddr = "http://127.0.0.1:8080"
	mgr.mu.Unlock()

	addr, client, sseClient, crashCh, err := mgr.Acquire(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, addr)
	require.NotNil(t, client)
	require.NotNil(t, sseClient, "sseClient should be returned")
	require.NotNil(t, crashCh)
}

// ─── P0-1 + P0-2: closeAllSubscribers / isDroppable / sendCritical ────────

func TestIsDroppable(t *testing.T) {
	t.Parallel()

	require.True(t, isDroppable(events.MessageDelta), "MessageDelta should be droppable")
	require.True(t, isDroppable(events.Raw), "Raw should be droppable")
	require.False(t, isDroppable(events.Done), "Done should not be droppable")
	require.False(t, isDroppable(events.Error), "Error should not be droppable")
	require.False(t, isDroppable(events.State), "State should not be droppable")
	require.False(t, isDroppable(events.MessageStart), "MessageStart should not be droppable")
}

func TestCloseAllSubscribers(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	// Manually add subscribers.
	ch1 := make(chan *events.Envelope, 16)
	ch2 := make(chan *events.Envelope, 16)
	mgr.busMu.Lock()
	mgr.subscribers["s1"] = ch1
	mgr.subscribers["s2"] = ch2
	mgr.busMu.Unlock()

	mgr.closeAllSubscribers()

	// Channels should be closed — reading from them returns immediately.
	_, ok1 := <-ch1
	_, ok2 := <-ch2
	require.False(t, ok1, "subscriber ch1 should be closed")
	require.False(t, ok2, "subscriber ch2 should be closed")

	// Map should be empty.
	mgr.busMu.RLock()
	n := len(mgr.subscribers)
	mgr.busMu.RUnlock()
	require.Zero(t, n)
}

func TestCloseAllSubscribers_Empty(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	// No subscribers — should not panic.
	mgr.closeAllSubscribers()
}

func TestSendCritical_Success(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	ch := make(chan *events.Envelope, 1)
	env := events.NewEnvelope(aep.NewID(), "s1", 0, events.Done, events.DoneData{Success: true})

	mgr.sendCritical(ch, env, "s1")

	select {
	case got := <-ch:
		require.Equal(t, events.Done, got.Event.Type)
	default:
		t.Fatal("critical event should have been sent")
	}
}

func TestSendCritical_ClosedChannel(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	ch := make(chan *events.Envelope, 1)
	close(ch) // Close before sending.

	env := events.NewEnvelope(aep.NewID(), "s1", 0, events.Done, events.DoneData{Success: true})

	// Should not panic — recover catches send on closed channel.
	mgr.sendCritical(ch, env, "s1")
}

func TestSendCritical_Timeout(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	// Full channel, no reader — will timeout.
	ch := make(chan *events.Envelope, 1)
	ch <- events.NewEnvelope(aep.NewID(), "s1", 0, events.State, nil) // fill buffer

	env := events.NewEnvelope(aep.NewID(), "s1", 0, events.Done, events.DoneData{Success: true})

	done := make(chan struct{})
	go func() {
		mgr.sendCritical(ch, env, "s1")
		close(done)
	}()

	select {
	case <-done:
		// Completed (after timeout) — expected.
	case <-time.After(criticalEventSendTimeout + 2*time.Second):
		t.Fatal("sendCritical should have timed out")
	}
}

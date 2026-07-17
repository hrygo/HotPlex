package opencodeserver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/worker/proc"
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

// TestSingletonProcessManager_IdleDrain_KillsWithoutProcMuDeadlock verifies
// the singleton's idle-drain timer kills the process via package-level
// proc.ForceKill(pgid) rather than proc.Manager.Kill().
//
// Historical context (#836): proc.Manager.Kill() used to acquire proc.mu and
// then call cmd.Wait() under it, deadlocking against monitorProcess's Wait()
// which already held proc.mu. Calling Kill() from the idle-drain callback
// would block on proc.mu forever, never reaching ForceKill, so the process
// was never killed and the timer goroutine leaked. proc.Kill() has been
// async-safe since #838 (cmd.Wait moved to a background goroutine), so this
// deadlock can no longer occur via that path; the singleton nonetheless
// keeps the ForceKill fast path (defense-in-depth, lighter than Kill()).
// This test guards the fast path's correctness: ForceKill must not touch
// proc.mu, letting monitorProcess's Wait() observe the exit.
//
// This test spawns a real long-lived subprocess and runs a background Wait()
// (simulating monitorProcess holding proc.mu via its own waitOnce). Under the
// fast path, idle-drain's ForceKill kills the process and the background
// Wait() returns promptly. If anyone ever routed idle-drain back through
// proc.Manager.Kill() while holding s.mu, neither would happen → timeout.
func TestSingletonProcessManager_IdleDrain_KillsWithoutProcMuDeadlock(t *testing.T) {
	if testing.Short() {
		t.Skip("requires spawning a real subprocess")
	}
	if runtime.GOOS == "windows" {
		t.Skip("relies on POSIX process groups")
	}

	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{
		IdleDrainPeriod: 10 * time.Millisecond,
	}
	mgr := NewSingletonProcessManager(log, cfg)

	// Spawn a real long-lived process as if it were the OCS server.
	pm := proc.New(proc.Opts{Logger: log})
	_, _, _, err := pm.Start(context.Background(), "sleep", []string{"30"}, nil, "")
	require.NoError(t, err)

	// Simulate monitorProcess: a goroutine blocked in pm.Wait() (holding
	// proc.mu via cmd.Wait) — the production deadlock condition.
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		_, _ = pm.Wait()
	}()

	mgr.mu.Lock()
	mgr.state = stateRunning
	mgr.refs = 0
	mgr.proc = pm
	mgr.pgid = pm.PGID()
	mgr.startIdleDrainLocked()
	mgr.mu.Unlock()

	// Under the fix: idle-drain calls proc.ForceKill(pgid) (package-level,
	// does NOT touch proc.mu) → process dies → pm.Wait() returns.
	// Under the regression (proc.Kill under s.mu): Kill blocks on proc.mu
	// (held by pm.Wait) → process never killed → pm.Wait never returns.
	select {
	case <-monitorDone:
		// success: idle-drain killed the process without deadlocking proc.mu
	case <-time.After(3 * time.Second):
		t.Fatal("idle-drain deadlocked on proc.mu — process never killed (P1-1 regression)")
	}
}

func TestSingletonProcessManager_readStderr(t *testing.T) {
	t.Parallel()

	// Capture log output to verify stderr lines are forwarded to the logger.
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	mgr := NewSingletonProcessManager(log, config.OpenCodeServerConfig{})

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	// Write two lines then close the write-end to simulate process exit (EOF).
	_, err = io.WriteString(w, "err one\nerr two\n")
	require.NoError(t, err)
	require.NoError(t, w.Close())

	// readStderr must drain to EOF and return, not block forever.
	done := make(chan struct{})
	go func() {
		mgr.readStderr(r)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readStderr did not return after stderr was closed")
	}

	output := buf.String()
	require.Contains(t, output, "err one")
	require.Contains(t, output, "err two")
}

func TestSingletonProcessManager_readStderr_FillsRing(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	mgr := NewSingletonProcessManager(log, config.OpenCodeServerConfig{})

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	_, err = io.WriteString(w, "err one\nerr two\n")
	require.NoError(t, err)
	require.NoError(t, w.Close())

	done := make(chan struct{})
	go func() {
		mgr.readStderr(r)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readStderr did not return after stderr was closed")
	}

	// Ring must capture the same lines that went to the logger, in order.
	require.Equal(t, []string{"err one", "err two"}, mgr.StderrTail())
}

func TestSingletonProcessManager_LastExitCode_InitialState(t *testing.T) {
	t.Parallel()

	mgr := NewSingletonProcessManager(slog.Default(), config.OpenCodeServerConfig{})
	code, ok := mgr.LastExitCode()
	require.False(t, ok, "no exit code available before any process has run")
	require.Equal(t, 0, code)
}

func TestSingletonProcessManager_StartupFailureError_WithTail(t *testing.T) {
	t.Parallel()

	mgr := NewSingletonProcessManager(slog.Default(), config.OpenCodeServerConfig{})
	mgr.stderrRing.Add("line one")
	mgr.stderrRing.Add("line two")

	err := mgr.startupFailureError("discover port", fmt.Errorf("boom"))
	s := err.Error()
	require.Contains(t, s, "discover port")
	require.Contains(t, s, "boom")
	require.Contains(t, s, "line one")
	require.Contains(t, s, "line two")
}

func TestSingletonProcessManager_StartupFailureError_NoTail(t *testing.T) {
	t.Parallel()

	mgr := NewSingletonProcessManager(slog.Default(), config.OpenCodeServerConfig{})
	err := mgr.startupFailureError("spawn", fmt.Errorf("x"))
	require.Contains(t, err.Error(), "spawn")
	require.NotContains(t, err.Error(), "recent stderr")
}

func TestSingletonProcessManager_StartupFailureError_TruncatesLongTail(t *testing.T) {
	t.Parallel()

	mgr := NewSingletonProcessManager(slog.Default(), config.OpenCodeServerConfig{})
	mgr.stderrRing.Add(strings.Repeat("a", stderrTailMaxBytes*2))

	err := mgr.startupFailureError("health check", fmt.Errorf("e"))
	// Error string must be bounded, not carry the full 2x overflow payload.
	require.Less(t, len(err.Error()), stderrTailMaxBytes*2)
}

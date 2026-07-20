package opencodeserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

func hasOpenCodeBinary() bool {
	_, err := exec.LookPath("opencode")
	return err == nil
}

func TestOpenCodeServerWorker_Capabilities(t *testing.T) {
	t.Parallel()
	w := New()

	require.Equal(t, worker.TypeOpenCodeSrv, w.Type())
	require.True(t, w.SupportsResume())
	require.True(t, w.SupportsStreaming())
	require.True(t, w.SupportsTools())
	require.NotNil(t, w.EnvBlocklist())
	require.Empty(t, w.SessionStoreDir())
	require.Zero(t, w.MaxTurns())
	require.Equal(t, []string{"text", "code", "image"}, w.Modalities())
}

func TestOpenCodeServerWorker_New(t *testing.T) {
	t.Parallel()
	w := New()

	require.NotNil(t, w)
	require.NotNil(t, w.BaseWorker)
	require.NotNil(t, w.client)
	require.Nil(t, w.sseCancel, "sseCancel should be nil until Start/Resume")
	require.Nil(t, w.httpConn)
}

func TestOpenCodeServerWorker_InitSessionConnFailsClosedWhenPermissionSetupFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPatch, r.Method)
		require.Equal(t, "/session/ocs-session-1", r.URL.Path)
		rw.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	w := New()
	w.httpAddr = server.URL
	w.client = server.Client()
	w.singleton = NewSingletonProcessManager(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		config.OpenCodeServerConfig{},
	)

	err := w.initSessionConn(context.Background(), "ocs-session-1", worker.SessionInfo{
		UserID:         "test-user",
		PermissionMode: worker.PermissionModeReadOnly,
	})

	require.ErrorContains(t, err, "opencode set permission")
	require.Nil(t, w.httpConn, "permission setup failure must not leave a usable connection")
}

func TestOpenCodeServerWorker_ApplyPermissionsPreservesUnifiedTier(t *testing.T) {
	t.Parallel()

	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPatch, r.Method)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		rw.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	w := New()
	w.cmd = &ServerCommander{
		client:    server.Client(),
		baseURL:   server.URL,
		sessionID: "ocs-session-1",
	}

	err := w.applyPermissions(t.Context(), worker.SessionInfo{PermissionMode: worker.PermissionModeAutoEdit})
	require.NoError(t, err)
	perms := got["permission"].([]any)
	for _, p := range perms {
		rule := p.(map[string]any)
		if rule["permission"] == "external_directory" {
			require.Equal(t, "allow", rule["action"])
			return
		}
	}
	t.Fatal("missing external_directory rule")
}

func TestOpenCodeServerWorker_EnvBlocklist(t *testing.T) {
	t.Parallel()
	w := New()

	bl := w.EnvBlocklist()
	require.Contains(t, bl, "CLAUDECODE")
	require.Contains(t, bl, "HOTPLEX_")
	require.Contains(t, bl, "CLAUDE_")
	require.Contains(t, bl, "ANTHROPIC_")
}

func TestOpenCodeServerWorker_ConnBeforeStart(t *testing.T) {
	t.Parallel()
	w := New()
	// require.Nil uses reflection and treats a typed nil as nil. Production
	// callers compare the interface value directly, so preserve that contract.
	require.True(t, w.Conn() == nil)
}

func TestOpenCodeServerWorker_HealthBeforeStart(t *testing.T) {
	t.Parallel()
	w := New()
	w.singleton = nil

	h := w.Health()
	require.Equal(t, worker.TypeOpenCodeSrv, h.Type)
	require.False(t, h.Running)
	require.True(t, h.Healthy)
	require.Empty(t, h.SessionID)
}

func TestOpenCodeServerWorker_LastIOBeforeStart(t *testing.T) {
	t.Parallel()
	w := New()
	require.True(t, w.LastIO().IsZero())
}

func TestOpenCodeServerWorker_TerminateWithoutStart(t *testing.T) {
	t.Parallel()

	w := New()
	ctx := context.Background()

	err := w.Terminate(ctx)
	require.NoError(t, err)
}

func TestOpenCodeServerWorker_KillWithoutStart(t *testing.T) {
	t.Parallel()

	w := New()
	err := w.Kill()
	require.NoError(t, err)
}

func TestOpenCodeServerWorker_Terminate_CallsSSECancel(t *testing.T) {
	t.Parallel()

	w := New()
	ctx := context.Background()

	sseCtx, sseCancel := context.WithCancel(context.Background())
	w.Mu.Lock()
	w.sseCancel = sseCancel
	w.Mu.Unlock()

	err := w.Terminate(ctx)
	require.NoError(t, err)

	select {
	case <-sseCtx.Done():
	default:
		t.Fatal("sseCancel was not called by Terminate")
	}
}

func TestOpenCodeServerWorker_TerminatePreservesServerSession(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	mgr := NewSingletonProcessManager(slog.New(slog.NewTextHandler(io.Discard, nil)), config.OpenCodeServerConfig{IdleDrainPeriod: time.Hour})
	mgr.mu.Lock()
	mgr.state = stateRunning
	mgr.refs = 1
	mgr.mu.Unlock()

	w := New()
	w.singleton = mgr
	w.httpAddr = server.URL
	w.client = server.Client()
	w.httpConn = &conn{sessionID: "ocs-session-1", recvCh: make(chan *events.Envelope)}

	require.NoError(t, w.Terminate(context.Background()))
	require.Zero(t, requests, "ordinary worker termination must preserve the resumable OCS session")
}

func TestDeleteOCSSession(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var deleted string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodDelete, r.Method)
				deleted = r.URL.Path
				w.WriteHeader(status)
			}))
			defer server.Close()

			require.NoError(t, deleteOCSSession(context.Background(), "ocs-session-1", server.URL, server.Client()))
			require.Equal(t, "/session/ocs-session-1", deleted)
		})
	}
}

func TestOCSSessionExists_OnlyNotFoundAllowsFreshStart(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		exists     bool
		wantErr    bool
	}{
		{name: "exists", statusCode: http.StatusOK, exists: true},
		{name: "not found", statusCode: http.StatusNotFound, exists: false},
		{name: "server failure", statusCode: http.StatusServiceUnavailable, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			w := New()
			w.httpAddr = server.URL
			w.client = server.Client()
			exists, err := w.ocsSessionExists(context.Background(), "ocs-session-1")
			require.Equal(t, tt.exists, exists)
			if tt.wantErr {
				require.ErrorIs(t, err, worker.ErrResumeCheckFailed)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestOpenCodeServerWorker_ResumeDoesNotFreshStartAfterCheckFailure(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	mgr := NewSingletonProcessManager(slog.New(slog.NewTextHandler(io.Discard, nil)), config.OpenCodeServerConfig{IdleDrainPeriod: time.Hour})
	mgr.mu.Lock()
	mgr.state = stateRunning
	mgr.httpAddr = server.URL
	mgr.client = server.Client()
	mgr.mu.Unlock()

	w := New()
	w.singleton = mgr
	err := w.Resume(context.Background(), worker.SessionInfo{SessionID: "hotplex-session-1", WorkerSessionID: "ocs-session-1"})
	require.ErrorIs(t, err, worker.ErrResumeCheckFailed)
	require.Equal(t, []string{"GET /session/ocs-session-1/message"}, requests)
}

func TestOpenCodeServerWorker_Kill_CallsSSECancel(t *testing.T) {
	t.Parallel()

	w := New()

	sseCtx, sseCancel := context.WithCancel(context.Background())
	w.Mu.Lock()
	w.sseCancel = sseCancel
	w.Mu.Unlock()

	err := w.Kill()
	require.NoError(t, err)

	select {
	case <-sseCtx.Done():
	default:
		t.Fatal("sseCancel was not called by Kill")
	}
}

func TestOpenCodeServerWorker_Terminate_NilSSECancel(t *testing.T) {
	t.Parallel()

	w := New()
	ctx := context.Background()

	err := w.Terminate(ctx)
	require.NoError(t, err)
}

func TestOpenCodeServerWorker_Kill_NilSSECancel(t *testing.T) {
	t.Parallel()

	w := New()

	err := w.Kill()
	require.NoError(t, err)
}

func TestOpenCodeServerWorker_Terminate_ReleasesSingleton(t *testing.T) {
	t.Parallel()

	w := New()
	ctx := context.Background()

	err := w.Terminate(ctx)
	require.NoError(t, err)

	err = w.Terminate(ctx)
	require.NoError(t, err)
}

func TestOpenCodeServerWorker_Kill_ReleasesSingleton(t *testing.T) {
	t.Parallel()

	w := New()

	err := w.Kill()
	require.NoError(t, err)

	err = w.Kill()
	require.NoError(t, err)
}

func TestOpenCodeServerWorker_WaitWithoutStart(t *testing.T) {
	t.Parallel()

	w := New()
	w.singleton = nil
	_, err := w.Wait()
	require.Error(t, err)
	require.Contains(t, err.Error(), "not started")
}

func TestOpenCodeServerWorker_Input_WithoutStart(t *testing.T) {
	t.Parallel()

	w := New()
	ctx := context.Background()
	err := w.Input(ctx, "hello", nil)
	require.Error(t, err)
}

func TestOpenCodeServerWorker_Resume_WithBinary(t *testing.T) {
	if !hasOpenCodeBinary() {
		t.Skip("opencode binary not found, skipping integration test")
	}

	w := New()
	ctx := context.Background()
	session := worker.SessionInfo{
		SessionID:  "test-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
	}

	err := w.Resume(ctx, session)
	if err != nil {
		t.Logf("Resume returned error (expected if server API not configured): %v", err)
		return
	}

	conn := w.Conn()
	if conn != nil {
		require.Equal(t, "test-session", conn.SessionID())
	}

	_ = w.Kill()
}

func TestOpenCodeServerWorker_Start_WithBinary(t *testing.T) {
	if !hasOpenCodeBinary() {
		t.Skip("opencode binary not found, skipping integration test")
	}

	w := New()
	ctx := context.Background()
	session := worker.SessionInfo{
		SessionID:  "test-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
	}

	err := w.Start(ctx, session)
	if err != nil {
		t.Logf("Start returned error (expected if server API not configured): %v", err)
		return
	}

	conn := w.Conn()
	if conn != nil {
		require.Equal(t, "test-session", conn.SessionID())
	}

	_ = w.Kill()
}

// ─── Input interaction response tests (httptest-backed) ──────────────────────

func newWorkerWithMockServer(t *testing.T, handler http.HandlerFunc) (*Worker, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	w := New()
	w.httpAddr = srv.URL
	w.client = srv.Client()
	w.httpConn = &conn{
		sessionID: "test-session",
		userID:    "test-user",
		httpAddr:  srv.URL,
		client:    srv.Client(),
		recvCh:    make(chan *events.Envelope, 16),
		log:       w.Log,
	}
	return w, srv
}

func TestCreateSession_CreatesProjectDir(t *testing.T) {
	t.Parallel()

	w, _ := newWorkerWithMockServer(t, func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session" {
			rw.WriteHeader(http.StatusNotFound)
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rw).Encode(map[string]string{"id": "ses_new"})
	})

	// Nested, non-existent target under a temp dir.
	projectDir := filepath.Join(t.TempDir(), "a", "b", "c")
	require.NoDirExists(t, projectDir)

	sessionID, err := w.createSession(context.Background(), projectDir)
	require.NoError(t, err)
	require.Equal(t, "ses_new", sessionID)
	require.DirExists(t, projectDir) // MkdirAll created the nested path
}

func TestCreateSession_EmptyProjectDir_NoError(t *testing.T) {
	t.Parallel()

	w, _ := newWorkerWithMockServer(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rw).Encode(map[string]string{"id": "ses_new"})
	})

	// Empty projectDir must skip MkdirAll and still succeed.
	sessionID, err := w.createSession(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, "ses_new", sessionID)
}

func TestInput_PermissionResponse_Allowed(t *testing.T) {
	t.Parallel()

	var receivedPath string
	var receivedBody map[string]string
	w, _ := newWorkerWithMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusOK)
	})

	md := map[string]any{
		"permission_response": map[string]any{
			"request_id": "perm_123",
			"allowed":    true,
		},
	}
	err := w.Input(context.Background(), "", md)
	require.NoError(t, err)
	require.Equal(t, "/permission/perm_123/reply", receivedPath)
	require.Equal(t, "once", receivedBody["reply"])
}

func TestInput_PermissionResponse_Denied(t *testing.T) {
	t.Parallel()

	var receivedBody map[string]string
	w, _ := newWorkerWithMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusOK)
	})

	md := map[string]any{
		"permission_response": map[string]any{
			"request_id": "perm_456",
			"allowed":    false,
		},
	}
	err := w.Input(context.Background(), "", md)
	require.NoError(t, err)
	require.Equal(t, "reject", receivedBody["reply"])
}

func TestInput_QuestionResponse(t *testing.T) {
	t.Parallel()

	var receivedPath string
	w, _ := newWorkerWithMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})

	md := map[string]any{
		"question_response": map[string]any{
			"id":      "q_789",
			"answers": map[string]string{"q1": "yes"},
		},
	}
	err := w.Input(context.Background(), "", md)
	require.NoError(t, err)
	require.Equal(t, "/question/q_789/reply", receivedPath)
}

func TestInput_ElicitationResponse(t *testing.T) {
	t.Parallel()

	var receivedPath string
	var receivedBody map[string]any
	w, _ := newWorkerWithMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusOK)
	})

	md := map[string]any{
		"elicitation_response": map[string]any{
			"id":     "e_001",
			"action": "accept",
			"content": map[string]any{
				"key": "value",
			},
		},
	}
	err := w.Input(context.Background(), "", md)
	require.NoError(t, err)
	require.Equal(t, "/elicitation/e_001/reply", receivedPath)
	require.Equal(t, "accept", receivedBody["action"])
	require.Equal(t, map[string]any{"key": "value"}, receivedBody["content"])
}

func TestInput_ElicitationResponse_Decline(t *testing.T) {
	t.Parallel()

	var receivedBody map[string]any
	w, _ := newWorkerWithMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusOK)
	})

	md := map[string]any{
		"elicitation_response": map[string]any{
			"id":     "e_002",
			"action": "decline",
		},
	}
	err := w.Input(context.Background(), "", md)
	require.NoError(t, err)
	require.Equal(t, "decline", receivedBody["action"])
	_, hasContent := receivedBody["content"]
	require.False(t, hasContent)
}

// ─── P0-2 + P1-1 + P1-2: forwardBusEvents / Wait crash recovery ───────────

func TestForwardBusEvents_CriticalEventDelivery(t *testing.T) {
	t.Parallel()

	w := New()
	recvCh := make(chan *events.Envelope, 256)
	w.httpConn = &conn{
		sessionID: "test-ses",
		userID:    "u1",
		recvCh:    recvCh,
		log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	busCh := make(chan *events.Envelope, 16)
	go w.forwardBusEvents(ctx, "test-ses", busCh)

	// Send a critical event (Done).
	doneEnv := events.NewEnvelope(aep.NewID(), "test-ses", 0, events.Done, events.DoneData{Success: true})
	busCh <- doneEnv

	// Should arrive on recvCh.
	select {
	case got := <-recvCh:
		require.Equal(t, events.Done, got.Event.Type)
	case <-time.After(2 * time.Second):
		t.Fatal("critical Done event not received on recvCh")
	}
}

func TestForwardBusEvents_DroppableEventDiscard(t *testing.T) {
	t.Parallel()

	w := New()
	recvCh := make(chan *events.Envelope, 1) // capacity 1
	// Fill the channel to simulate backpressure.
	recvCh <- events.NewEnvelope(aep.NewID(), "test-ses", 0, events.State, nil)

	w.httpConn = &conn{
		sessionID: "test-ses",
		userID:    "u1",
		recvCh:    recvCh,
		log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	busCh := make(chan *events.Envelope, 16)
	go w.forwardBusEvents(ctx, "test-ses", busCh)

	// Send a droppable event (MessageDelta) — should be silently dropped.
	deltaEnv := events.NewEnvelope(aep.NewID(), "test-ses", 0, events.MessageDelta, events.MessageDeltaData{Content: "x"})
	busCh <- deltaEnv

	// Verify recvCh never gets the droppable event (it was silently dropped).
	require.Eventually(t, func() bool {
		return len(recvCh) == 1 // only the pre-filled event remains
	}, 200*time.Millisecond, 10*time.Millisecond, "droppable event should have been dropped when recvCh is full")
}

func TestForwardBusEvents_ClosedConnStops(t *testing.T) {
	t.Parallel()

	w := New()
	recvCh := make(chan *events.Envelope, 16)
	c := &conn{
		sessionID: "test-ses",
		userID:    "u1",
		recvCh:    recvCh,
		log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	w.httpConn = c

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	busCh := make(chan *events.Envelope, 16)
	done := make(chan struct{})
	go func() {
		w.forwardBusEvents(ctx, "test-ses", busCh)
		close(done)
	}()

	// Close the conn while forwardBusEvents is running.
	c.Close()

	// Send an event — forwardBusEvents should detect closed and exit.
	busCh <- events.NewEnvelope(aep.NewID(), "test-ses", 0, events.Done, nil)

	select {
	case <-done:
		// forwardBusEvents exited — expected.
	case <-time.After(2 * time.Second):
		t.Fatal("forwardBusEvents should have exited after conn.Close()")
	}
}

func TestForwardBusEvents_CancelledCtx(t *testing.T) {
	t.Parallel()

	w := New()
	recvCh := make(chan *events.Envelope, 16)
	w.httpConn = &conn{
		sessionID: "test-ses",
		userID:    "u1",
		recvCh:    recvCh,
		log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	ctx, cancel := context.WithCancel(context.Background())
	busCh := make(chan *events.Envelope, 16)

	done := make(chan struct{})
	go func() {
		w.forwardBusEvents(ctx, "test-ses", busCh)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// Exited via context cancellation — expected.
	case <-time.After(2 * time.Second):
		t.Fatal("forwardBusEvents should have exited on context cancellation")
	}
}

func TestWait_CrashSub_RecoveredSingleton(t *testing.T) {
	t.Parallel()

	w := New()

	// Set up a singleton that IsRunning() = true (simulates recovered after crash).
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)
	mgr.mu.Lock()
	mgr.state = stateRunning
	mgr.mu.Unlock()

	w.singleton = mgr

	// Create a closed crashSub (simulates crash signal already fired).
	crashCh := make(chan struct{})
	close(crashCh)
	w.crashSub = crashCh

	// Wait should return 0 because singleton has recovered.
	code, err := w.Wait()
	require.NoError(t, err)
	require.Equal(t, 0, code, "Wait should return 0 when singleton has recovered after crash")
}

func TestWait_CrashSub_NotRecovered(t *testing.T) {
	t.Parallel()

	w := New()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)
	// state is idle by default → IsRunning() = false.

	w.singleton = mgr

	// Closed crashSub.
	crashCh := make(chan struct{})
	close(crashCh)
	w.crashSub = crashCh

	// Wait should return 1 because singleton is NOT running.
	code, err := w.Wait()
	require.NoError(t, err)
	require.Equal(t, 1, code, "Wait should return 1 when singleton has NOT recovered")
}

func TestInput_QuestionResponse_WithProjectDir(t *testing.T) {
	t.Parallel()

	var receivedPath string
	var receivedQuery string
	w, _ := newWorkerWithMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	})

	w.httpConn.projectDir = "/test/workspace"

	md := map[string]any{
		"question_response": map[string]any{
			"id":      "q_789",
			"answers": map[string]string{"q1": "yes"},
		},
	}
	err := w.Input(context.Background(), "", md)
	require.NoError(t, err)
	require.Equal(t, "/question/q_789/reply", receivedPath)
	require.Equal(t, "directory=%2Ftest%2Fworkspace", receivedQuery)
}

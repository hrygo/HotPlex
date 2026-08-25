package opencodeserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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
	require.NoError(t, w.permissionCeiling.Capture(worker.PermissionModeAutoEdit))

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

// ─── abortOCSSession (POST /session/{id}/abort) ───────────────────────────────

func TestAbortOCSSession_RequestContract(t *testing.T) {
	t.Parallel()

	var (
		gotMethod string
		gotPath   string
		gotQuery  string
		gotBody   []byte
	)
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.EscapedPath()
		gotQuery = r.URL.Query().Get("directory")
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = rw.Write([]byte("true"))
	}))
	t.Cleanup(server.Close)

	err := abortOCSSession(context.Background(), "ses/contract", server.URL, "/tmp/project with space", server.Client())
	require.NoError(t, err)

	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/session/ses%2Fcontract/abort", gotPath)
	require.Equal(t, "/tmp/project with space", gotQuery)
	require.Empty(t, gotBody)
}

func TestAbortOCSSession_RequestContract_NoDirectoryWhenEmpty(t *testing.T) {
	t.Parallel()

	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = rw.Write([]byte("true"))
	}))
	t.Cleanup(server.Close)

	err := abortOCSSession(context.Background(), "ses/contract", server.URL, "", server.Client())
	require.NoError(t, err)
	require.Empty(t, gotQuery, "directory query param must be omitted when projectDir is empty")
}

func TestAbortOCSSession_ResponseSemantics(t *testing.T) {
	t.Parallel()

	bigBody := strings.Repeat("a", 4096) + strings.Repeat("b", 4096)

	tests := []struct {
		name        string
		status      int
		body        string
		handler     http.HandlerFunc // overrides status/body when set
		callTimeout time.Duration
		wantErr     bool
		errContains []string
		errNotIn    []string
		errIs       []error
	}{
		{name: "200 true is success", status: http.StatusOK, body: "true"},
		{name: "200 false is success (no active turn)", status: http.StatusOK, body: "false"},
		{name: "200 malformed body is stable decode error", status: http.StatusOK, body: "not-json", wantErr: true, errContains: []string{"opencodeserver: abort session"}},
		{
			name:        "500 non-200 error reads at most 4096 body bytes",
			status:      http.StatusInternalServerError,
			body:        bigBody,
			wantErr:     true,
			errContains: []string{"opencodeserver: abort session", strings.Repeat("a", 4096)},
			errNotIn:    []string{"bbb"}, // tail marker; "abort"/"body" contain no triple-b
		},
		{
			name: "server holds request until ctx done → caller deadline",
			handler: func(rw http.ResponseWriter, r *http.Request) {
				<-r.Context().Done()
			},
			callTimeout: 50 * time.Millisecond,
			wantErr:     true,
			errIs:       []error{context.DeadlineExceeded, context.Canceled},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				if tt.handler != nil {
					tt.handler(rw, r)
					return
				}
				rw.WriteHeader(tt.status)
				_, _ = rw.Write([]byte(tt.body))
			}))
			t.Cleanup(server.Close)

			ctx := context.Background()
			if tt.callTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.callTimeout)
				t.Cleanup(cancel)
			}

			err := abortOCSSession(ctx, "ses/contract", server.URL, "", server.Client())
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			for _, s := range tt.errContains {
				require.ErrorContains(t, err, s)
			}
			for _, s := range tt.errNotIn {
				require.NotContains(t, err.Error(), s)
			}
			if len(tt.errIs) > 0 {
				matched := false
				for _, want := range tt.errIs {
					if errors.Is(err, want) {
						matched = true
						break
					}
				}
				require.True(t, matched, "error should wrap one of %v, got: %v", tt.errIs, err)
			}
		})
	}
}

func TestAbortOCSSession_ArgumentValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sessionID string
		httpAddr  string
		client    *http.Client
	}{
		{name: "empty sessionID", sessionID: "", httpAddr: "http://localhost:1", client: &http.Client{}},
		{name: "empty httpAddr", sessionID: "s1", httpAddr: "", client: &http.Client{}},
		{name: "nil client", sessionID: "s1", httpAddr: "http://localhost:1", client: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := abortOCSSession(context.Background(), tt.sessionID, tt.httpAddr, "", tt.client)
			require.ErrorContains(t, err, "opencodeserver: abort session")
		})
	}
}

// ─── StopCurrentTurn (in-place abort, issue #954) ─────────────────────────────

// newRunningSingleton returns a singleton manager in the running state holding
// exactly one ref, so tests can observe that StopCurrentTurn does not release it.
func newRunningSingleton(t *testing.T) *SingletonProcessManager {
	t.Helper()
	mgr := NewSingletonProcessManager(slog.New(slog.NewTextHandler(io.Discard, nil)), config.OpenCodeServerConfig{IdleDrainPeriod: time.Hour})
	mgr.mu.Lock()
	mgr.state = stateRunning
	mgr.refs = 1
	mgr.mu.Unlock()
	return mgr
}

// stopTurnTestWorker wires a Worker to an httptest-backed OCS server with an
// active httpConn for sessionID/projectDir, plus a singleton holding one ref.
func stopTurnTestWorker(t *testing.T, server *httptest.Server, mgr *SingletonProcessManager, sessionID, projectDir string) (*Worker, *conn) {
	t.Helper()
	live := &conn{
		sessionID:  sessionID,
		userID:     "u1",
		httpAddr:   server.URL,
		client:     server.Client(),
		recvCh:     make(chan *events.Envelope, 16),
		projectDir: projectDir,
		log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	w := New()
	w.singleton = mgr
	w.httpAddr = server.URL
	w.client = server.Client()
	w.httpConn = live
	return w, live
}

// assertStopTurnRetention asserts the frozen StopCurrentTurn retention contract:
// same conn, same worker session ID, singleton ref unchanged, SSE subscription
// not cancelled. expectStopped reflects the stop outcome: a SUCCESSFUL abort
// keeps the marker set (post-abort terminal envelopes are suppressed); a FAILED
// abort (HTTP error or deadline) unmarks the worker so the turn's legitimate
// terminal event flows normally (the gateway rolls back its stop fence).
func assertStopTurnRetention(t *testing.T, w *Worker, mgr *SingletonProcessManager, live *conn, sseCtx context.Context, expectStopped bool) {
	t.Helper()
	require.Equal(t, expectStopped, w.IsStopped(), "StopCurrentTurn stopped marker must reflect the abort outcome")
	require.Same(t, live, w.Conn(), "StopCurrentTurn must retain the active conn")
	require.Equal(t, "ses-live", w.GetWorkerSessionID(), "StopCurrentTurn must retain the worker session ID")
	mgr.mu.Lock()
	require.Equal(t, 1, mgr.refs, "StopCurrentTurn must not release the singleton ref")
	mgr.mu.Unlock()
	if sseCtx != nil {
		require.Nil(t, sseCtx.Err(), "StopCurrentTurn must not cancel the SSE subscription")
	}
}

func TestWorker_StopCurrentTurn_AbortsWithoutReleasingSession(t *testing.T) {
	t.Parallel()

	var (
		mu     sync.Mutex
		aborts []*http.Request
	)
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		mu.Lock()
		aborts = append(aborts, r)
		mu.Unlock()
		_, _ = rw.Write([]byte("true"))
	}))
	t.Cleanup(server.Close)

	mgr := newRunningSingleton(t)
	w, live := stopTurnTestWorker(t, server, mgr, "ses-live", "/tmp/live")

	sseCtx, sseCancel := context.WithCancel(context.Background())
	w.Mu.Lock()
	w.sseCancel = sseCancel
	w.Mu.Unlock()

	require.NoError(t, w.StopCurrentTurn(context.Background()))

	mu.Lock()
	require.Len(t, aborts, 1, "exactly one abort request expected")
	if len(aborts) == 1 {
		require.Equal(t, http.MethodPost, aborts[0].Method)
		require.Equal(t, "/session/ses-live/abort", aborts[0].URL.Path)
		body, _ := io.ReadAll(aborts[0].Body)
		require.Empty(t, body, "abort request must carry no body")
	}
	mu.Unlock()

	assertStopTurnRetention(t, w, mgr, live, sseCtx, true)
}

func TestWorker_StopThenTerminatePreservesSiblingWrapper(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/session/ses-a/abort", r.URL.Path)
		_, _ = rw.Write([]byte("true"))
	}))
	t.Cleanup(server.Close)

	mgr := newRunningSingleton(t)
	mgr.mu.Lock()
	mgr.refs = 2
	mgr.mu.Unlock()
	workerA, connA := stopTurnTestWorker(t, server, mgr, "ses-a", "/tmp/a")
	workerB, connB := stopTurnTestWorker(t, server, mgr, "ses-b", "/tmp/b")
	mgr.Subscribe("ses-a")
	mgr.Subscribe("ses-b")
	ctxA, cancelA := context.WithCancel(context.Background())
	ctxB, cancelB := context.WithCancel(context.Background())
	workerA.Mu.Lock()
	workerA.sseCancel = cancelA
	workerA.Mu.Unlock()
	workerB.Mu.Lock()
	workerB.sseCancel = cancelB
	workerB.Mu.Unlock()

	require.NoError(t, workerA.StopCurrentTurn(context.Background()))
	require.NoError(t, workerA.Terminate(context.Background()))
	require.NoError(t, workerA.Kill(), "wrapper release must be idempotent")
	require.Error(t, ctxA.Err(), "stopped wrapper SSE context must be cancelled")
	_, open := <-connA.Recv()
	require.False(t, open, "terminated wrapper connection must close")

	mgr.mu.Lock()
	require.Equal(t, 1, mgr.refs, "only the stopped wrapper reference must be released")
	require.Equal(t, stateRunning, mgr.state, "terminating one wrapper must not stop the shared server")
	mgr.mu.Unlock()
	mgr.busMu.Lock()
	_, hasA := mgr.subscribers["ses-a"]
	_, hasB := mgr.subscribers["ses-b"]
	mgr.busMu.Unlock()
	require.False(t, hasA)
	require.True(t, hasB, "sibling wrapper subscription must remain active")
	require.Nil(t, ctxB.Err(), "sibling wrapper SSE context must remain active")
	require.Same(t, connB, workerB.Conn())

	require.NoError(t, workerB.Terminate(context.Background()))
}

func TestWorker_StopCurrentTurn_NoActiveConn(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		requests int
	)
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		rw.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	mgr := newRunningSingleton(t)
	w := New()
	w.singleton = mgr
	w.httpAddr = server.URL
	w.client = server.Client()

	sseCtx, sseCancel := context.WithCancel(context.Background())
	w.Mu.Lock()
	w.sseCancel = sseCancel
	w.Mu.Unlock()

	require.NoError(t, w.StopCurrentTurn(context.Background()))

	mu.Lock()
	require.Zero(t, requests, "no network access without an active conn")
	mu.Unlock()
	require.True(t, w.IsStopped(), "no active conn still marks the worker stopped")
	require.True(t, w.Conn() == nil)
	require.Empty(t, w.GetWorkerSessionID())
	mgr.mu.Lock()
	require.Equal(t, 1, mgr.refs, "StopCurrentTurn must not release the singleton ref")
	mgr.mu.Unlock()
	require.Nil(t, sseCtx.Err(), "StopCurrentTurn must not cancel the SSE subscription")
}

func TestWorker_StopCurrentTurn_AbortError500RetainsSession(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusInternalServerError)
		_, _ = rw.Write([]byte("abort failed"))
	}))
	t.Cleanup(server.Close)

	mgr := newRunningSingleton(t)
	w, live := stopTurnTestWorker(t, server, mgr, "ses-live", "/tmp/live")

	sseCtx, sseCancel := context.WithCancel(context.Background())
	w.Mu.Lock()
	w.sseCancel = sseCancel
	w.Mu.Unlock()

	err := w.StopCurrentTurn(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "opencodeserver: abort session")
	require.ErrorContains(t, err, "status 500")

	assertStopTurnRetention(t, w, mgr, live, sseCtx, false)
}

func TestWorker_StopCurrentTurn_AbortTimeout_UsesCallerDeadline(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/session/ses-live/abort", r.URL.Path)
		<-r.Context().Done() // hold until the caller deadline fires
	}))
	t.Cleanup(server.Close)

	mgr := newRunningSingleton(t)
	w, live := stopTurnTestWorker(t, server, mgr, "ses-live", "/tmp/live")

	sseCtx, sseCancel := context.WithCancel(context.Background())
	w.Mu.Lock()
	w.sseCancel = sseCancel
	w.Mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- w.StopCurrentTurn(ctx) }()

	var err error
	select {
	case err = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("StopCurrentTurn must honor the caller's shorter deadline")
	}
	elapsed := time.Since(start)

	require.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled),
		"abort must surface the deadline error, got: %v", err)
	require.Less(t, elapsed, time.Second, "caller deadline (50ms) must win over the 2s internal cap")

	assertStopTurnRetention(t, w, mgr, live, sseCtx, false)
}

func TestWorker_StopCurrentTurn_AbortTimeout_InternalCap2s(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/session/ses-live/abort", r.URL.Path)
		<-r.Context().Done() // hold until the internal 2s cap fires
	}))
	t.Cleanup(server.Close)

	mgr := newRunningSingleton(t)
	w, live := stopTurnTestWorker(t, server, mgr, "ses-live", "/tmp/live")

	sseCtx, sseCancel := context.WithCancel(context.Background())
	w.Mu.Lock()
	w.sseCancel = sseCancel
	w.Mu.Unlock()

	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- w.StopCurrentTurn(context.Background()) }()

	var err error
	select {
	case err = <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("StopCurrentTurn must bound the abort with an internal 2s cap")
	}
	elapsed := time.Since(start)

	require.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled),
		"abort must surface the deadline error, got: %v", err)
	require.GreaterOrEqual(t, elapsed, 1500*time.Millisecond, "abort must hit the ~2s internal cap, not return early")

	assertStopTurnRetention(t, w, mgr, live, sseCtx, false)
}

// TestWorker_Input_ClearsStoppedOnlyForPrimaryTurn verifies the user-stop
// marker is scoped to the current turn: interaction-response metadata
// (handled by DispatchMetadata) must NOT clear it, while the next primary
// content (an actual OCS message POST) must clear it before the send.
func TestWorker_Input_ClearsStoppedOnlyForPrimaryTurn(t *testing.T) {
	t.Parallel()

	t.Run("interaction response metadata keeps stopped", func(t *testing.T) {
		t.Parallel()

		var receivedPath string
		w, _ := newWorkerWithMockServer(t, func(rw http.ResponseWriter, r *http.Request) {
			receivedPath = r.URL.Path
			rw.WriteHeader(http.StatusOK)
		})
		w.MarkStopped()

		md := map[string]any{
			"permission_response": map[string]any{
				"request_id": "perm_stop_1",
				"allowed":    true,
			},
		}
		err := w.Input(context.Background(), "", md)
		require.NoError(t, err)
		require.Equal(t, "/permission/perm_stop_1/reply", receivedPath)
		require.True(t, w.IsStopped(), "handled metadata must not clear the stopped marker")
	})

	t.Run("next primary content clears stopped before send", func(t *testing.T) {
		t.Parallel()

		var receivedPath string
		w, _ := newWorkerWithMockServer(t, func(rw http.ResponseWriter, r *http.Request) {
			receivedPath = r.URL.Path
			rw.WriteHeader(http.StatusOK)
		})
		w.MarkStopped()

		err := w.Input(context.Background(), "hello again", nil)
		require.NoError(t, err)

		require.False(t, w.IsStopped(), "a new primary turn must clear the stopped marker")
		// The protocol fake observed the primary send (message POST).
		require.Equal(t, "/session/test-session/message", receivedPath)
	})
}

// TestForwardBusEvents_SuppressesTerminalWhileStopped is the regression test
// for the OCS double-terminal finding: while the user-stop marker is set (a
// stop was admitted), worker-emitted terminal envelopes (the converter's fresh
// Done/Error for the post-abort session.idle transition) must be suppressed —
// the gateway's synthetic done(stopped_by_user) is authoritative for that
// turn. A State event is used as an ordered barrier: it is non-droppable and
// forwarded, so once it arrives the suppressed Done/Error are guaranteed to
// have been processed.
func TestForwardBusEvents_SuppressesTerminalWhileStopped(t *testing.T) {
	t.Parallel()

	w := New()
	w.httpConn = &conn{
		sessionID: "s",
		recvCh:    make(chan *events.Envelope, 16),
		log:       w.Log,
	}

	busCh := make(chan *events.Envelope, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.forwardBusEvents(ctx, "s", busCh)

	w.MarkStopped()

	// Post-abort terminal envelopes arrive on the bus while stopped.
	busCh <- events.NewEnvelope(aep.NewID(), "s", 0, events.Done, events.DoneData{Success: true})
	busCh <- events.NewEnvelope(aep.NewID(), "s", 0, events.Error, events.ErrorData{Code: events.ErrCodeInternalError, Message: "aborted"})
	// Barrier: non-terminal, must be forwarded.
	busCh <- events.NewEnvelope(aep.NewID(), "s", 0, events.State, events.StateData{State: events.StateRunning})

	select {
	case env := <-w.httpConn.recvCh:
		require.Equal(t, events.State, env.Event.Type,
			"terminal events must be suppressed while stopped; got %s", env.Event.Type)
	case <-time.After(2 * time.Second):
		t.Fatal("barrier State event never forwarded")
	}

	// A new primary turn clears the marker; the next Done flows normally.
	w.BeginTurn()
	busCh <- events.NewEnvelope(aep.NewID(), "s", 0, events.Done, events.DoneData{Success: true})
	select {
	case env := <-w.httpConn.recvCh:
		require.Equal(t, events.Done, env.Event.Type, "Done must flow after BeginTurn clears the marker")
	case <-time.After(2 * time.Second):
		t.Fatal("Done never forwarded after BeginTurn")
	}
}

// TestOpenCodeServerWorker_InputSendFailureRestoresStopped verifies the
// capture-restore contract: when the protocol send fails, the new turn never
// started, so the previous turn's stopped marker must be preserved (the bridge
// crash fallback must not re-run a stopped turn).
func TestOpenCodeServerWorker_InputSendFailureRestoresStopped(t *testing.T) {
	t.Parallel()

	w, _ := newWorkerWithMockServer(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusInternalServerError)
	})
	w.MarkStopped()

	err := w.Input(context.Background(), "hello", nil)
	require.Error(t, err, "send failure must surface to the caller")
	require.True(t, w.IsStopped(), "a failed send must restore the stopped marker")
}

// TestOpenCodeServerWorker_StopAbortFailureUnmarks verifies that a FAILED stop
// attempt unmarks the worker: the gateway rolls back its stop fence and sends
// an error, the turn is still running, and its legitimate terminal event must
// flow normally rather than being suppressed forever.
func TestOpenCodeServerWorker_StopAbortFailureUnmarks(t *testing.T) {
	t.Parallel()

	w, _ := newWorkerWithMockServer(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusInternalServerError)
	})
	w.MarkStopped()

	err := w.StopCurrentTurn(context.Background())
	require.Error(t, err, "failed abort must surface to the gateway")
	require.False(t, w.IsStopped(), "a failed stop attempt must clear the marker")
}

// TestOpenCodeServerWorker_StopAbortSuccessKeepsMarker verifies a SUCCESSFUL
// stop keeps the marker set so post-abort terminal envelopes stay suppressed
// until the next turn.
func TestOpenCodeServerWorker_StopAbortSuccessKeepsMarker(t *testing.T) {
	t.Parallel()

	w, _ := newWorkerWithMockServer(t, func(rw http.ResponseWriter, r *http.Request) {
		_, _ = rw.Write([]byte("false")) // OCS: no active turn — still success for idempotent abort
	})
	w.MarkStopped()

	require.NoError(t, w.StopCurrentTurn(context.Background()))
	require.True(t, w.IsStopped(), "a successful stop keeps the marker set")
}

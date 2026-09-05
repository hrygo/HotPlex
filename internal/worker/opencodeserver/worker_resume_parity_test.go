package opencodeserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

type resumeRequestRecorder struct {
	mu       sync.Mutex
	requests []string
}

func (r *resumeRequestRecorder) record(req *http.Request) {
	r.mu.Lock()
	r.requests = append(r.requests, req.Method+" "+req.URL.Path)
	r.mu.Unlock()
}

func (r *resumeRequestRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.requests...)
}

func resumeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func newResumeTestWorker(t *testing.T, handler http.HandlerFunc) (*Worker, *SingletonProcessManager, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	mgr := NewSingletonProcessManager(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		config.OpenCodeServerConfig{IdleDrainPeriod: time.Hour},
	)
	mgr.mu.Lock()
	mgr.state = stateRunning
	mgr.httpAddr = server.URL
	mgr.client = server.Client()
	mgr.mu.Unlock()

	w := New()
	w.singleton = mgr
	t.Cleanup(func() {
		_ = w.Terminate(context.Background())
		mgr.Shutdown(context.Background())
		server.Close()
	})
	return w, mgr, server
}

func resumeSession() worker.SessionInfo {
	return worker.SessionInfo{
		SessionID:      "hotplex-resume-session",
		UserID:         "resume-user",
		PermissionMode: worker.PermissionModeBypass,
	}
}

func TestOpenCodeServerWorker_ResumeMissingRemoteFreshStartsWithBufferedNotice(t *testing.T) {
	recorder := &resumeRequestRecorder{}
	w, _, _ := newResumeTestWorker(t, func(rw http.ResponseWriter, req *http.Request) {
		recorder.record(req)
		switch {
		case req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/message"):
			rw.WriteHeader(http.StatusNotFound)
		case req.Method == http.MethodPost && req.URL.Path == "/session":
			resumeJSON(rw, http.StatusCreated, `{"id":"ocs-fresh-session"}`)
		case req.Method == http.MethodPatch && req.URL.Path == "/session/ocs-fresh-session":
			rw.WriteHeader(http.StatusOK)
		default:
			rw.WriteHeader(http.StatusNotFound)
		}
	})

	session := resumeSession()
	session.WorkerSessionID = "ocs-missing-session"
	err := w.Resume(context.Background(), session)

	require.ErrorIs(t, err, worker.ErrFellBackToFreshStart)
	require.Equal(t, "ocs-fresh-session", w.GetWorkerSessionID())
	require.Equal(t,
		[]string{"GET /session/ocs-missing-session/message", "POST /session", "PATCH /session/ocs-fresh-session"},
		recorder.snapshot(),
	)

	conn := w.Conn()
	require.NotNil(t, conn)
	select {
	case env := <-conn.Recv():
		require.Equal(t, session.SessionID, env.SessionID)
		require.Equal(t, events.Message, env.Event.Type)
		data, ok := env.Event.Data.(events.MessageData)
		require.True(t, ok)
		require.Contains(t, data.Content, "历史上下文未恢复")
		require.NotEqual(t, events.Error, env.Event.Type)
	default:
		t.Fatal("fresh resume did not buffer a context recovery notice")
	}
}

func TestOpenCodeServerWorker_ResumeWithoutWorkerSessionIDFreshStartsWithBufferedNotice(t *testing.T) {
	recorder := &resumeRequestRecorder{}
	w, _, _ := newResumeTestWorker(t, func(rw http.ResponseWriter, req *http.Request) {
		recorder.record(req)
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/session":
			resumeJSON(rw, http.StatusCreated, `{"id":"ocs-no-old-id-session"}`)
		case req.Method == http.MethodPatch && req.URL.Path == "/session/ocs-no-old-id-session":
			rw.WriteHeader(http.StatusOK)
		default:
			rw.WriteHeader(http.StatusNotFound)
		}
	})

	session := resumeSession()
	err := w.Resume(context.Background(), session)

	require.ErrorIs(t, err, worker.ErrFellBackToFreshStart)
	require.Equal(t, "ocs-no-old-id-session", w.GetWorkerSessionID())
	require.Equal(t,
		[]string{"POST /session", "PATCH /session/ocs-no-old-id-session"},
		recorder.snapshot(),
		"a missing persisted WorkerSessionID must skip remote lookup and still report a fresh resume",
	)

	conn := w.Conn()
	require.NotNil(t, conn)
	select {
	case env := <-conn.Recv():
		require.Equal(t, session.SessionID, env.SessionID)
		require.Equal(t, events.Message, env.Event.Type)
		data, ok := env.Event.Data.(events.MessageData)
		require.True(t, ok)
		require.Contains(t, data.Content, "历史上下文未恢复")
	default:
		t.Fatal("fresh resume without an old WorkerSessionID did not buffer a context recovery notice")
	}
}

func TestOpenCodeServerWorker_ResumeExistingRemoteReusesWithoutNotice(t *testing.T) {
	recorder := &resumeRequestRecorder{}
	w, _, _ := newResumeTestWorker(t, func(rw http.ResponseWriter, req *http.Request) {
		recorder.record(req)
		switch {
		case req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/message"):
			rw.WriteHeader(http.StatusOK)
		case req.Method == http.MethodPatch && req.URL.Path == "/session/ocs-existing-session":
			rw.WriteHeader(http.StatusOK)
		default:
			rw.WriteHeader(http.StatusNotFound)
		}
	})

	session := resumeSession()
	session.WorkerSessionID = "ocs-existing-session"
	err := w.Resume(context.Background(), session)

	require.NoError(t, err)
	require.Equal(t, "ocs-existing-session", w.GetWorkerSessionID())
	require.Equal(t,
		[]string{"GET /session/ocs-existing-session/message", "PATCH /session/ocs-existing-session"},
		recorder.snapshot(),
	)
	conn := w.Conn()
	require.NotNil(t, conn)
	select {
	case env := <-conn.Recv():
		t.Fatalf("existing-session resume must not buffer a fresh-start notice: %#v", env)
	default:
	}
}

func TestOpenCodeServerWorker_ResumeCheckFailureNeverCreatesFreshSession(t *testing.T) {
	recorder := &resumeRequestRecorder{}
	w, _, _ := newResumeTestWorker(t, func(rw http.ResponseWriter, req *http.Request) {
		recorder.record(req)
		if req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/message") {
			rw.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		rw.WriteHeader(http.StatusNotFound)
	})

	session := resumeSession()
	session.WorkerSessionID = "ocs-check-failure-session"
	err := w.Resume(context.Background(), session)

	require.ErrorIs(t, err, worker.ErrResumeCheckFailed)
	require.Nil(t, w.Conn())
	require.Equal(t, []string{"GET /session/ocs-check-failure-session/message"}, recorder.snapshot())
}

func TestOpenCodeServerWorker_ResumeNetworkCheckFailureNeverCreatesFreshSession(t *testing.T) {
	recorder := &resumeRequestRecorder{}
	w, _, server := newResumeTestWorker(t, func(rw http.ResponseWriter, req *http.Request) {
		recorder.record(req)
		rw.WriteHeader(http.StatusNotFound)
	})
	server.Close()

	session := resumeSession()
	session.WorkerSessionID = "ocs-network-failure-session"
	err := w.Resume(context.Background(), session)

	require.ErrorIs(t, err, worker.ErrResumeCheckFailed)
	require.Nil(t, w.Conn())
	require.Empty(t, recorder.snapshot(), "a network check failure cannot safely proceed to fresh creation")
}

func TestOpenCodeServerWorker_ResumeCreateFailureDoesNotEmitFreshSuccessNotice(t *testing.T) {
	recorder := &resumeRequestRecorder{}
	w, _, _ := newResumeTestWorker(t, func(rw http.ResponseWriter, req *http.Request) {
		recorder.record(req)
		switch {
		case req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/message"):
			rw.WriteHeader(http.StatusNotFound)
		case req.Method == http.MethodPost && req.URL.Path == "/session":
			rw.WriteHeader(http.StatusInternalServerError)
		default:
			rw.WriteHeader(http.StatusNotFound)
		}
	})

	session := resumeSession()
	session.WorkerSessionID = "ocs-create-failure-session"
	err := w.Resume(context.Background(), session)

	require.Error(t, err)
	require.NotErrorIs(t, err, worker.ErrFellBackToFreshStart)
	require.Nil(t, w.Conn())
	require.Equal(t,
		[]string{"GET /session/ocs-create-failure-session/message", "POST /session"},
		recorder.snapshot(),
	)
}

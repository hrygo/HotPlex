package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/execution"
)

// --- Fence mocks ---

type mockRuntimeExec struct {
	listFn  func(ctx context.Context, sessionID string, limit, offset int) ([]*execution.Record, error)
	applyFn func(ctx context.Context, req execution.FenceActionRequest) (*execution.Record, error)
}

func (m *mockRuntimeExec) ListFences(ctx context.Context, sessionID string, limit, offset int) ([]*execution.Record, error) {
	if m.listFn != nil {
		return m.listFn(ctx, sessionID, limit, offset)
	}
	return nil, nil
}

func (m *mockRuntimeExec) ApplyFenceDecision(ctx context.Context, req execution.FenceActionRequest) (*execution.Record, error) {
	if m.applyFn != nil {
		return m.applyFn(ctx, req)
	}
	return nil, execution.ErrNotFound
}

type mockRuntimeNotifier struct {
	calls []string // "sessionID/executionID" per call
}

func (m *mockRuntimeNotifier) NotifyExecutionAbandoned(_ context.Context, sessionID, executionID string) {
	m.calls = append(m.calls, sessionID+"/"+executionID)
}

func fencedRecord() *execution.Record {
	createdAt := int64(1722800000000)
	return &execution.Record{
		ExecutionID:    "exec-1",
		SessionID:      "sess-1",
		Status:         execution.StatusUnknown,
		RuntimeStatus:  execution.RuntimeUnknown,
		FenceReason:    "GATEWAY_LEASE_EXPIRED",
		FenceVersion:   3,
		FenceCreatedAt: &createdAt,
		UpdatedAt:      1722800001000,
	}
}

// --- ListFences ---

func TestHandleListFences_Forbidden(t *testing.T) {
	t.Parallel()
	api := newTestAPI()
	api.SetRuntimeExecution(&mockRuntimeExec{}, nil)
	w := httptest.NewRecorder()
	r := withScope(httptest.NewRequest(http.MethodGet, "/admin/executions/fences", nil), ScopeSessionRead)

	api.HandleListFences(w, r)

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleListFences_ProviderNotConfigured(t *testing.T) {
	t.Parallel()
	api := newTestAPI() // no SetRuntimeExecution
	w := httptest.NewRecorder()
	r := withScope(httptest.NewRequest(http.MethodGet, "/admin/executions/fences", nil), ScopeRuntimeRead)

	api.HandleListFences(w, r)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleListFences_Success(t *testing.T) {
	t.Parallel()
	var gotSession string
	var gotLimit, gotOffset int
	api := newTestAPI()
	api.SetRuntimeExecution(&mockRuntimeExec{
		listFn: func(_ context.Context, sessionID string, limit, offset int) ([]*execution.Record, error) {
			gotSession, gotLimit, gotOffset = sessionID, limit, offset
			return []*execution.Record{fencedRecord()}, nil
		},
	}, nil)
	w := httptest.NewRecorder()
	r := withScope(httptest.NewRequest(http.MethodGet, "/admin/executions/fences?session_id=sess-1&limit=999&offset=7", nil), ScopeRuntimeRead)

	api.HandleListFences(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "sess-1", gotSession)
	require.Equal(t, fenceListMaxLimit, gotLimit, "limit must clamp to 500")
	require.Equal(t, 7, gotOffset)

	var resp struct {
		Fences []FenceListItem `json:"fences"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Fences, 1)
	item := resp.Fences[0]
	require.Equal(t, "exec-1", item.ExecutionID)
	require.Equal(t, "sess-1", item.SessionID)
	require.Equal(t, string(execution.StatusUnknown), item.DeliveryStatus)
	require.Equal(t, string(execution.RuntimeUnknown), item.RuntimeStatus)
	require.Equal(t, "GATEWAY_LEASE_EXPIRED", item.FenceReason)
	require.Equal(t, int64(3), item.FenceVersion)

	// Secret-free projection: no payload hash or worker error fields.
	require.NotContains(t, w.Body.String(), "payload_hash")
	require.NotContains(t, w.Body.String(), "error_detail")
}

func TestHandleListFences_AdminWriteImpliesRuntimeRead(t *testing.T) {
	t.Parallel()
	api := newTestAPI()
	api.SetRuntimeExecution(&mockRuntimeExec{}, nil)
	w := httptest.NewRecorder()
	r := withScope(httptest.NewRequest(http.MethodGet, "/admin/executions/fences", nil), ScopeAdminWrite)

	api.HandleListFences(w, r)

	require.Equal(t, http.StatusOK, w.Code, "admin:write must imply runtime:read for existing admin tokens")
}

// --- FenceAction validation ---

func TestHandleFenceAction_Forbidden(t *testing.T) {
	t.Parallel()
	api := newTestAPI()
	api.SetRuntimeExecution(&mockRuntimeExec{}, nil)
	w := httptest.NewRecorder()
	r := withScope(httptest.NewRequest(http.MethodPost, "/admin/executions/exec-1/fence-action", strings.NewReader("{}")), ScopeRuntimeRead)
	r.SetPathValue("id", "exec-1")

	api.HandleFenceAction(w, r)

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleFenceAction_ProviderNotConfigured(t *testing.T) {
	t.Parallel()
	api := newTestAPI()
	w := httptest.NewRecorder()
	r := withScope(httptest.NewRequest(http.MethodPost, "/admin/executions/exec-1/fence-action", strings.NewReader("{}")), ScopeRuntimeWrite)
	r.SetPathValue("id", "exec-1")

	api.HandleFenceAction(w, r)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleFenceAction_BadRequests(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{"invalid json", "{not-json"},
		{"invalid decision", `{"decision":"force","expected_fence_version":3,"reason":"r"}`},
		{"missing version", `{"decision":"resolve","reason":"r"}`},
		{"negative version", `{"decision":"resolve","expected_fence_version":-1,"reason":"r"}`},
		{"empty reason", `{"decision":"resolve","expected_fence_version":3,"reason":"   "}`},
		{"reason too long", fmt.Sprintf(`{"decision":"resolve","expected_fence_version":3,"reason":"%s"}`, strings.Repeat("x", fenceReasonMaxLen+1))},
		{"evidence too long", fmt.Sprintf(`{"decision":"resolve","expected_fence_version":3,"reason":"r","evidence_ref":"%s"}`, strings.Repeat("e", fenceEvidenceMaxLen+1))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api := newTestAPI()
			api.SetRuntimeExecution(&mockRuntimeExec{
				applyFn: func(context.Context, execution.FenceActionRequest) (*execution.Record, error) {
					t.Fatal("store must not be reached on validation failure")
					return nil, nil
				},
			}, nil)
			w := httptest.NewRecorder()
			r := withScope(httptest.NewRequest(http.MethodPost, "/admin/executions/exec-1/fence-action", strings.NewReader(tc.body)), ScopeRuntimeWrite)
			r.SetPathValue("id", "exec-1")

			api.HandleFenceAction(w, r)

			require.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

// --- FenceAction outcomes ---

func fenceActionRequest(t *testing.T, decision, body string) *httptest.ResponseRecorder {
	t.Helper()
	notifier := &mockRuntimeNotifier{}
	rec := fencedRecord()
	api := newTestAPI()
	api.SetRuntimeExecution(&mockRuntimeExec{
		applyFn: func(_ context.Context, req execution.FenceActionRequest) (*execution.Record, error) {
			require.Equal(t, "exec-1", req.ExecutionID)
			require.Equal(t, int64(3), req.ExpectedFenceVersion)
			require.Equal(t, execution.FenceDecision(decision), req.Decision)
			if decision == string(execution.FenceDecisionAbandon) {
				rec.RuntimeStatus = execution.RuntimeFailed
				rec.RuntimeErrorCode = execution.RuntimeErrorCodeOperatorAbandoned
			}
			rec.FenceReason = ""
			return rec, nil
		},
	}, notifier)

	w := httptest.NewRecorder()
	r := withScope(httptest.NewRequest(http.MethodPost, "/admin/executions/exec-1/fence-action", strings.NewReader(body)), ScopeRuntimeWrite)
	r.SetPathValue("id", "exec-1")
	api.HandleFenceAction(w, r)
	return w
}

func TestHandleFenceAction_ResolveSuccess(t *testing.T) {
	t.Parallel()
	w := fenceActionRequest(t, "resolve", `{"decision":"resolve","expected_fence_version":3,"reason":"worker recovered","evidence_ref":"ticket-1"}`)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Decision  string        `json:"decision"`
		Execution FenceListItem `json:"execution"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, "resolve", resp.Decision)
	require.Equal(t, "exec-1", resp.Execution.ExecutionID)
	require.Empty(t, resp.Execution.FenceReason)
	require.Equal(t, string(execution.RuntimeUnknown), resp.Execution.RuntimeStatus)
}

func TestHandleFenceAction_AbandonSuccess(t *testing.T) {
	t.Parallel()
	notifier := &mockRuntimeNotifier{}
	rec := fencedRecord()
	api := newTestAPI()
	api.SetRuntimeExecution(&mockRuntimeExec{
		applyFn: func(_ context.Context, req execution.FenceActionRequest) (*execution.Record, error) {
			rec.RuntimeStatus = execution.RuntimeFailed
			rec.RuntimeErrorCode = execution.RuntimeErrorCodeOperatorAbandoned
			rec.FenceReason = ""
			return rec, nil
		},
	}, notifier)

	w := httptest.NewRecorder()
	r := withScope(httptest.NewRequest(http.MethodPost, "/admin/executions/exec-1/fence-action",
		strings.NewReader(`{"decision":"abandon","expected_fence_version":3,"reason":"operator decision"}`)), ScopeRuntimeWrite)
	r.SetPathValue("id", "exec-1")
	api.HandleFenceAction(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []string{"sess-1/exec-1"}, notifier.calls, "abandon must emit the runtime.execution.failed event")
	var resp struct {
		Decision  string        `json:"decision"`
		Execution FenceListItem `json:"execution"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, "abandon", resp.Decision)
	require.Equal(t, string(execution.RuntimeFailed), resp.Execution.RuntimeStatus)
}

func TestHandleFenceAction_Conflict(t *testing.T) {
	t.Parallel()
	api := newTestAPI()
	api.SetRuntimeExecution(&mockRuntimeExec{
		applyFn: func(context.Context, execution.FenceActionRequest) (*execution.Record, error) {
			return nil, fmt.Errorf("fence already cleared: %w", execution.ErrFenceConflict)
		},
	}, &mockRuntimeNotifier{})

	w := httptest.NewRecorder()
	r := withScope(httptest.NewRequest(http.MethodPost, "/admin/executions/exec-1/fence-action",
		strings.NewReader(`{"decision":"resolve","expected_fence_version":2,"reason":"r"}`)), ScopeRuntimeWrite)
	r.SetPathValue("id", "exec-1")
	api.HandleFenceAction(w, r)

	require.Equal(t, http.StatusConflict, w.Code)
	require.Contains(t, w.Body.String(), "FENCE_CONFLICT")
	require.Contains(t, w.Body.String(), "re-inspect", "409 must direct the operator to re-inspect, not blindly retry")
}

func TestHandleFenceAction_NotFound(t *testing.T) {
	t.Parallel()
	api := newTestAPI()
	api.SetRuntimeExecution(&mockRuntimeExec{}, nil)

	w := httptest.NewRecorder()
	r := withScope(httptest.NewRequest(http.MethodPost, "/admin/executions/missing/fence-action",
		strings.NewReader(`{"decision":"resolve","expected_fence_version":1,"reason":"r"}`)), ScopeRuntimeWrite)
	r.SetPathValue("id", "missing")
	api.HandleFenceAction(w, r)

	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), "FENCE_NOT_FOUND")
}

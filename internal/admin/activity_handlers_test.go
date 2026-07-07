package admin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/audit"
	"github.com/hrygo/hotplex/internal/dbutil"
)

// mockAuditStoreForHandler implements audit.Store for handler tests.
type mockAuditStoreForHandler struct {
	queryFn func(ctx context.Context, q audit.Query) ([]audit.UserActivity, error)
}

func (m *mockAuditStoreForHandler) BeginTx(ctx context.Context) (audit.Tx, error) {
	return nil, errors.New("not implemented")
}
func (m *mockAuditStoreForHandler) Query(ctx context.Context, q audit.Query) ([]audit.UserActivity, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, q)
	}
	return nil, nil
}
func (m *mockAuditStoreForHandler) QueryAsc(ctx context.Context, fromID int64, limit int) ([]audit.UserActivity, error) {
	return nil, nil // handler tests don't exercise the verifier path
}
func (m *mockAuditStoreForHandler) DeleteBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	return 0, nil
}
func (m *mockAuditStoreForHandler) SaveCheckpoint(ctx context.Context, c audit.Checkpoint) error {
	return nil
}
func (m *mockAuditStoreForHandler) LatestCheckpoint(ctx context.Context) (*audit.Checkpoint, error) {
	return nil, nil
}
func (m *mockAuditStoreForHandler) Close() error { return nil }
func (m *mockAuditStoreForHandler) Dialect() dbutil.Dialect {
	return dbutil.DialectSQLite
}

func newTestAPIWithActivity(overrides ...func(*Deps)) *AdminAPI {
	api := newTestAPI(overrides...)
	store := &mockAuditStoreForHandler{
		queryFn: func(ctx context.Context, q audit.Query) ([]audit.UserActivity, error) {
			return []audit.UserActivity{
				{
					ID:        1,
					Ts:        time.Now().UnixMilli(),
					UserID:    "u1",
					Action:    "auth.login",
					Outcome:   "success",
					IP:        "192.168.1.100",
					UserAgent: "Mozilla/5.0",
				},
			}, nil
		},
	}
	svc := NewActivityService(store, slog.Default())
	api.SetActivityService(svc)
	return api
}

func TestHandleUserActivity_Success(t *testing.T) {
	t.Parallel()
	api := newTestAPIWithActivity()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/users/u1/activity", nil)
	r = withScope(r, ScopeAdminRead)
	// Simulate path value
	r.SetPathValue("id", "u1")

	api.HandleUserActivity(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, "u1", resp["user_id"])
	require.Contains(t, resp, "rows")
}

func TestHandleUserActivity_Forbidden(t *testing.T) {
	t.Parallel()
	api := newTestAPIWithActivity()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/users/u1/activity", nil)
	r = withScope(r, ScopeSessionRead) // wrong scope
	r.SetPathValue("id", "u1")

	api.HandleUserActivity(w, r)

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleAdminActivity_Success(t *testing.T) {
	t.Parallel()
	api := newTestAPIWithActivity()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/activity?user_id=u1", nil)
	r = withScope(r, ScopeAdminRead)

	api.HandleAdminActivity(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Contains(t, resp, "rows")
}

func TestHandleAdminActivity_Forbidden(t *testing.T) {
	t.Parallel()
	api := newTestAPIWithActivity()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/activity", nil)
	r = withScope(r, ScopeSessionRead)

	api.HandleAdminActivity(w, r)

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleUserActivityExport_JSON(t *testing.T) {
	t.Parallel()
	api := newTestAPIWithActivity()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/users/u1/activity?format=json", nil)
	r = withScope(r, ScopeAdminRead)
	r.SetPathValue("id", "u1")

	api.HandleUserActivityExport(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "application/json")
	require.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
}

func TestHandleAdminActivityExport_CSV(t *testing.T) {
	t.Parallel()
	api := newTestAPIWithActivity()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/activity?format=csv&user_id=u1", nil)
	r = withScope(r, ScopeAdminRead)

	api.HandleAdminActivityExport(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "text/csv")
	require.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
}

func TestHandleActivityExport_IncludePII_RequiresAdminWrite(t *testing.T) {
	t.Parallel()
	api := newTestAPIWithActivity()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/activity?include_pii=true", nil)
	r = withScope(r, ScopeAdminRead) // has read but not write

	api.HandleAdminActivityExport(w, r)

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "admin:write")
}

func TestHandleActivityExport_IncludePII_WithAdminWrite(t *testing.T) {
	t.Parallel()
	api := newTestAPIWithActivity()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/activity?include_pii=true", nil)
	r = withScope(r, ScopeAdminWrite) // has write (implies read)

	api.HandleAdminActivityExport(w, r)

	require.Equal(t, http.StatusOK, w.Code)
}

func TestHandleActivityExport_UnknownFormat(t *testing.T) {
	t.Parallel()
	api := newTestAPIWithActivity()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/activity?format=xml", nil)
	r = withScope(r, ScopeAdminRead)

	api.HandleAdminActivityExport(w, r)

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleActivityExport_StoreError(t *testing.T) {
	t.Parallel()
	api := newTestAPI()
	store := &mockAuditStoreForHandler{
		queryFn: func(ctx context.Context, q audit.Query) ([]audit.UserActivity, error) {
			return nil, errors.New("db down")
		},
	}
	svc := NewActivityService(store, slog.Default())
	api.SetActivityService(svc)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/activity", nil)
	r = withScope(r, ScopeAdminRead)

	api.HandleAdminActivityExport(w, r)

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestParseActivityQuery(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/activity?from=2024-01-01T00:00:00Z&to=2024-12-31T23:59:59Z&action=auth.login&outcome=success&include_pii=true&limit=50&offset=10", nil)

	q := parseActivityQuery(r)

	require.Equal(t, "auth.login", q.Action)
	require.Equal(t, "success", q.Outcome)
	require.True(t, q.IncludePII)
	require.Equal(t, 50, q.Limit)
	require.Equal(t, 10, q.Offset)
	require.False(t, q.From.IsZero())
	require.False(t, q.To.IsZero())
}

func TestParseActivityQuery_Defaults(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/activity", nil)

	q := parseActivityQuery(r)

	require.Equal(t, 100, q.Limit) // default from web.ParsePagination
	require.Equal(t, 0, q.Offset)
	require.False(t, q.IncludePII)
}

func TestParseActivityQuery_InvalidDates(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/activity?from=invalid&to=alsobad", nil)

	q := parseActivityQuery(r)

	// Invalid dates should be ignored (zero time)
	require.True(t, q.From.IsZero())
	require.True(t, q.To.IsZero())
}

func TestHandleUserActivity_QueryError(t *testing.T) {
	t.Parallel()
	api := newTestAPI()
	store := &mockAuditStoreForHandler{
		queryFn: func(ctx context.Context, q audit.Query) ([]audit.UserActivity, error) {
			return nil, errors.New("query failed")
		},
	}
	svc := NewActivityService(store, slog.Default())
	api.SetActivityService(svc)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/users/u1/activity", nil)
	r = withScope(r, ScopeAdminRead)
	r.SetPathValue("id", "u1")

	api.HandleUserActivity(w, r)

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleAdminActivity_QueryError(t *testing.T) {
	t.Parallel()
	api := newTestAPI()
	store := &mockAuditStoreForHandler{
		queryFn: func(ctx context.Context, q audit.Query) ([]audit.UserActivity, error) {
			return nil, errors.New("query failed")
		},
	}
	svc := NewActivityService(store, slog.Default())
	api.SetActivityService(svc)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/activity", nil)
	r = withScope(r, ScopeAdminRead)

	api.HandleAdminActivity(w, r)

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleActivityExport_ExporterFromHeader(t *testing.T) {
	t.Parallel()
	api := newTestAPIWithActivity()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/activity?format=json", nil)
	r = withScope(r, ScopeAdminRead)
	r.Header.Set("X-Admin-Actor", "admin-123")

	api.HandleAdminActivityExport(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	// The exporter is logged but not returned in response; we verify no error
}

func TestHandleActivityExport_DefaultFormat(t *testing.T) {
	t.Parallel()
	api := newTestAPIWithActivity()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/activity", nil)
	r = withScope(r, ScopeAdminRead)

	api.HandleAdminActivityExport(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "application/json")
}

func TestHandleUserActivityExport_Forbidden(t *testing.T) {
	t.Parallel()
	api := newTestAPIWithActivity()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/users/u1/activity?format=json", nil)
	r = withScope(r, ScopeSessionRead)
	r.SetPathValue("id", "u1")

	api.HandleUserActivityExport(w, r)

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleActivityExport_EmptyUserID(t *testing.T) {
	t.Parallel()
	api := newTestAPIWithActivity()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/activity?format=json", nil)
	r = withScope(r, ScopeAdminRead)

	api.HandleAdminActivityExport(w, r)

	require.Equal(t, http.StatusOK, w.Code)
}

func TestHandleActivityExport_WithUserIDFilter(t *testing.T) {
	t.Parallel()
	var capturedQuery audit.Query
	api := newTestAPI()
	store := &mockAuditStoreForHandler{
		queryFn: func(ctx context.Context, q audit.Query) ([]audit.UserActivity, error) {
			capturedQuery = q
			return []audit.UserActivity{{ID: 1, UserID: "target"}}, nil
		},
	}
	svc := NewActivityService(store, slog.Default())
	api.SetActivityService(svc)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/activity?format=json&user_id=target", nil)
	r = withScope(r, ScopeAdminRead)

	api.HandleAdminActivityExport(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "target", capturedQuery.UserID)
}

func TestHandleUserActivity_WithPagination(t *testing.T) {
	t.Parallel()
	var capturedQuery audit.Query
	api := newTestAPI()
	store := &mockAuditStoreForHandler{
		queryFn: func(ctx context.Context, q audit.Query) ([]audit.UserActivity, error) {
			capturedQuery = q
			return []audit.UserActivity{{ID: 1}}, nil
		},
	}
	svc := NewActivityService(store, slog.Default())
	api.SetActivityService(svc)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/users/u1/activity?limit=25&offset=50", nil)
	r = withScope(r, ScopeAdminRead)
	r.SetPathValue("id", "u1")

	api.HandleUserActivity(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 25, capturedQuery.Limit)
	require.Equal(t, 50, capturedQuery.Offset)
}

func TestHandleUserActivity_WithFilters(t *testing.T) {
	t.Parallel()
	var capturedQuery audit.Query
	api := newTestAPI()
	store := &mockAuditStoreForHandler{
		queryFn: func(ctx context.Context, q audit.Query) ([]audit.UserActivity, error) {
			capturedQuery = q
			return []audit.UserActivity{{ID: 1}}, nil
		},
	}
	svc := NewActivityService(store, slog.Default())
	api.SetActivityService(svc)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/users/u1/activity?action=auth.login&outcome=success", nil)
	r = withScope(r, ScopeAdminRead)
	r.SetPathValue("id", "u1")

	api.HandleUserActivity(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "auth.login", capturedQuery.Action)
	require.Equal(t, "success", capturedQuery.Outcome)
	require.Equal(t, "u1", capturedQuery.UserID)
}

func TestHandleAdminActivity_ImpliedScope(t *testing.T) {
	t.Parallel()
	api := newTestAPIWithActivity()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/activity", nil)
	// admin:write implies admin:read
	r = withScope(r, ScopeAdminWrite)

	api.HandleAdminActivity(w, r)

	require.Equal(t, http.StatusOK, w.Code)
}

func TestHandleActivityExport_NilCollector(t *testing.T) {
	t.Parallel()
	api := newTestAPIWithActivity()
	// auditCollector is nil by default
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/activity?format=json", nil)
	r = withScope(r, ScopeAdminRead)

	api.HandleAdminActivityExport(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	// Should not panic even with nil collector
}

func TestHandleActivityExport_WithCollector(t *testing.T) {
	t.Parallel()
	api := newTestAPIWithActivity()
	// Create a minimal collector (won't actually enqueue in this test)
	store := &mockAuditStoreForHandler{}
	collector := audit.NewCollector(store, nil, nil, slog.Default(), audit.CollectorConfig{})
	api.SetAuditCollector(collector)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/activity?format=json", nil)
	r = withScope(r, ScopeAdminRead)

	api.HandleAdminActivityExport(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	_ = collector.Close(context.Background())
}

func TestParseActivityQuery_LimitClamp(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/activity?limit=5000", nil)

	q := parseActivityQuery(r)

	// web.ParsePagination clamps to MaxLimit (1000)
	require.Equal(t, 1000, q.Limit)
}

func TestHandleUserActivity_EmptyResult(t *testing.T) {
	t.Parallel()
	api := newTestAPI()
	store := &mockAuditStoreForHandler{
		queryFn: func(ctx context.Context, q audit.Query) ([]audit.UserActivity, error) {
			return []audit.UserActivity{}, nil
		},
	}
	svc := NewActivityService(store, slog.Default())
	api.SetActivityService(svc)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/users/u1/activity", nil)
	r = withScope(r, ScopeAdminRead)
	r.SetPathValue("id", "u1")

	api.HandleUserActivity(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	rows, ok := resp["rows"].([]any)
	require.True(t, ok)
	require.Empty(t, rows)
}

func TestHandleActivityExport_CSVFormat(t *testing.T) {
	t.Parallel()
	api := newTestAPIWithActivity()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/activity?format=csv", nil)
	r = withScope(r, ScopeAdminRead)

	api.HandleAdminActivityExport(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "text/csv")
	body := w.Body.String()
	require.True(t, strings.Contains(body, "id,ts,user_id"))
}

// ─── I1: meta-audit must fire on export failure (spec §5.8) ───────────────────

// capturingAuditStore is an audit.Store whose BeginTx returns a capturing Tx
// that records every appended row. Used to assert the export handler emits a
// system.audit_export row on BOTH success and failure paths.
type capturingAuditStore struct {
	mu       sync.Mutex
	recorded []audit.UserActivity
}

func (s *capturingAuditStore) BeginTx(context.Context) (audit.Tx, error) {
	return &capturingAuditTx{store: s}, nil
}
func (s *capturingAuditStore) Query(context.Context, audit.Query) ([]audit.UserActivity, error) {
	return nil, nil
}
func (s *capturingAuditStore) QueryAsc(context.Context, int64, int) ([]audit.UserActivity, error) {
	return nil, nil
}
func (s *capturingAuditStore) DeleteBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (s *capturingAuditStore) SaveCheckpoint(context.Context, audit.Checkpoint) error { return nil }
func (s *capturingAuditStore) LatestCheckpoint(context.Context) (*audit.Checkpoint, error) {
	return nil, nil
}
func (s *capturingAuditStore) Close() error            { return nil }
func (s *capturingAuditStore) Dialect() dbutil.Dialect { return dbutil.DialectSQLite }
func (s *capturingAuditStore) snapshot() []audit.UserActivity {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]audit.UserActivity, len(s.recorded))
	copy(out, s.recorded)
	return out
}

type capturingAuditTx struct {
	store *capturingAuditStore
}

func (t *capturingAuditTx) Append(_ context.Context, ua *audit.UserActivity) error {
	t.store.mu.Lock()
	t.store.recorded = append(t.store.recorded, *ua)
	t.store.mu.Unlock()
	return nil
}
func (t *capturingAuditTx) AppendBatch(ctx context.Context, uas []*audit.UserActivity) error {
	for _, ua := range uas {
		if err := t.Append(ctx, ua); err != nil {
			return err
		}
	}
	return nil
}
func (t *capturingAuditTx) SaveCheckpoint(context.Context, audit.Checkpoint) error { return nil }
func (t *capturingAuditTx) TailHash(context.Context) (string, error)               { return "", nil }
func (t *capturingAuditTx) LastRowBefore(context.Context, time.Time) (int64, string, error) {
	return 0, "", nil
}
func (t *capturingAuditTx) DeleteByIDLEQ(context.Context, int64) (int64, error) { return 0, nil }
func (t *capturingAuditTx) RowCount(context.Context) (int64, error)             { return 0, nil }
func (t *capturingAuditTx) Commit() error                                       { return nil }
func (t *capturingAuditTx) Rollback() error                                     { return nil }

// TestExport_FailureStillEmitsMetaAudit verifies the I1 fix: a failed export
// must still emit a system.audit_export row with outcome=failure. Before the
// fix the meta-audit only ran on the success path, leaving failed export
// attempts (attacker probing, failed bulk exfil) invisible in the audit log —
// a forensic gap for a system whose purpose is forensic visibility.
func TestExport_FailureStillEmitsMetaAudit(t *testing.T) {
	t.Parallel()
	api := newTestAPIWithActivity()
	capStore := &capturingAuditStore{}
	collector := audit.NewCollector(capStore, nil, nil, slog.Default(), audit.CollectorConfig{
		BatchSize:     1,
		BatchInterval: 5 * time.Millisecond,
	})
	collector.Start(context.Background())
	t.Cleanup(func() { _ = collector.Close(context.Background()) })
	api.SetAuditCollector(collector)

	w := httptest.NewRecorder()
	// Unknown format forces Export to error (audit_service.go:83).
	r := httptest.NewRequest(http.MethodGet, "/admin/activity?format=does-not-exist", nil)
	r = withScope(r, ScopeAdminRead)

	api.HandleAdminActivityExport(w, r)

	require.Equal(t, http.StatusInternalServerError, w.Code)

	// The meta-audit row must be present with outcome=failure. The collector
	// batches asynchronously, so poll until the row lands (no time.Sleep —
	// require.Eventually per AGENTS.md).
	require.Eventually(t, func() bool {
		for _, ua := range capStore.snapshot() {
			if ua.Action == audit.ActionSystemAuditExport && ua.Outcome == audit.OutcomeFailure {
				return true
			}
		}
		return false
	}, 2*time.Second, 5*time.Millisecond, "failed export must emit system.audit_export with outcome=failure")
}

// TestExport_SuccessEmitsMetaAudit confirms the success path still emits the
// meta-audit row (regression guard for the I1 refactor that moved Enqueue
// before the error return).
func TestExport_SuccessEmitsMetaAudit(t *testing.T) {
	t.Parallel()
	api := newTestAPIWithActivity()
	capStore := &capturingAuditStore{}
	collector := audit.NewCollector(capStore, nil, nil, slog.Default(), audit.CollectorConfig{
		BatchSize:     1,
		BatchInterval: 5 * time.Millisecond,
	})
	collector.Start(context.Background())
	t.Cleanup(func() { _ = collector.Close(context.Background()) })
	api.SetAuditCollector(collector)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/activity?format=json", nil)
	r = withScope(r, ScopeAdminRead)

	api.HandleAdminActivityExport(w, r)
	require.Equal(t, http.StatusOK, w.Code)

	require.Eventually(t, func() bool {
		for _, ua := range capStore.snapshot() {
			if ua.Action == audit.ActionSystemAuditExport && ua.Outcome == audit.OutcomeSuccess {
				return true
			}
		}
		return false
	}, 2*time.Second, 5*time.Millisecond, "successful export must emit system.audit_export with outcome=success")
}

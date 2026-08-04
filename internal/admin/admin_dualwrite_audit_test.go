package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/audit"
	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/internal/sqlutil"
)

// dualWriteSchema mirrors migration 023 (SQLite) for admin dual-write tests.
const dualWriteSchema = `
CREATE TABLE IF NOT EXISTS user_activity (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    ts            INTEGER NOT NULL,
    user_id       TEXT    NOT NULL,
    user_id_type  TEXT    NOT NULL,
    platform      TEXT    NOT NULL,
    session_id    TEXT,
    action        TEXT    NOT NULL,
    resource_type TEXT,
    resource_id   TEXT,
    outcome       TEXT    NOT NULL,
    detail_json   TEXT    NOT NULL,
    event_ref     TEXT,
    ip            TEXT,
    user_agent    TEXT,
    prev_hash     TEXT    NOT NULL,
    self_hash     TEXT    NOT NULL
);
CREATE TABLE IF NOT EXISTS audit_chain_checkpoints (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    pruned_at       INTEGER NOT NULL,
    last_self_hash  TEXT    NOT NULL,
    next_id         INTEGER NOT NULL
);
`

// newDualWriteCollector builds a started audit.Collector backed by SQLite.
// Returns the collector and a query helper that lists persisted rows.
func newDualWriteCollector(t *testing.T) (*audit.Collector, func() []audit.UserActivity) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "admin_dualwrite_test.db")
	db, err := sql.Open(sqlutil.DriverName, dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	_, err = db.Exec("PRAGMA journal_mode=WAL")
	require.NoError(t, err)
	_, err = db.Exec(dualWriteSchema)
	require.NoError(t, err)
	writeMu := sqlutil.NewWriteMu(sqlutil.DialectSQLite)
	store, err := audit.NewStore(db, dbutil.DialectSQLite, writeMu, slog.Default())
	require.NoError(t, err)
	spill, err := audit.OpenSpill(filepath.Join(t.TempDir(), "spill.wal"))
	require.NoError(t, err)
	c := audit.NewCollector(store, spill, nil, slog.Default(), audit.CollectorConfig{
		ChannelCap: 64, BatchSize: 10, BatchInterval: 5 * time.Millisecond,
	})
	c.Start(context.Background())
	t.Cleanup(func() { _ = c.Close(context.Background()) })

	query := func() []audit.UserActivity {
		rows, err := db.Query(`SELECT id, ts, user_id, user_id_type, platform,
			COALESCE(session_id,''), action, COALESCE(resource_type,''),
			COALESCE(resource_id,''), outcome, detail_json, COALESCE(event_ref,''),
			COALESCE(ip,''), COALESCE(user_agent,''), prev_hash, self_hash
			FROM user_activity ORDER BY id`)
		require.NoError(t, err)
		defer func() { _ = rows.Close() }()
		var out []audit.UserActivity
		for rows.Next() {
			var r audit.UserActivity
			require.NoError(t, rows.Scan(&r.ID, &r.Ts, &r.UserID, &r.UserIDType, &r.Platform,
				&r.SessionID, &r.Action, &r.ResourceType, &r.ResourceID, &r.Outcome,
				&r.DetailJSON, &r.EventRef, &r.IP, &r.UserAgent, &r.PrevHash, &r.SelfHash))
			out = append(out, r)
		}
		return out
	}
	return c, query
}

// captureSlogAudit swaps the package-level auditLogger to capture admin_audit
// slog records into a buffer. Tests MUST run non-parallel OR hold auditMu since
// the logger is package-global.
func captureSlogAudit(t *testing.T) *bytes.Buffer {
	t.Helper()
	auditMu.Lock()
	t.Cleanup(func() { auditMu.Unlock() })
	prev := currentAuditLogger()
	t.Cleanup(func() { auditLogger.Store(prev) })
	var buf bytes.Buffer
	SetAuditLogger(slog.New(slog.NewTextHandler(&buf, nil)))
	return &buf
}

// newDualWriteAPI builds a newTestAPI-equivalent AdminAPI with the default
// test-token config AND a wired audit collector. The default mockConfig gives
// "test-token" full scopes (including admin:write).
func newDualWriteAPI(t *testing.T) (*AdminAPI, func() []audit.UserActivity) {
	t.Helper()
	c, q := newDualWriteCollector(t)
	api := newTestAPI()
	api.SetAuditCollector(c)
	return api, q
}

// TestAdminDualWrite_WriteEmitsBoth verifies a successful admin write produces
// BOTH the legacy slog admin_audit record AND a tamper-evident user_activity
// row (spec §7 dual-write).
func TestAdminDualWrite_WriteEmitsBoth(t *testing.T) {
	// NOT parallel: captureSlogAudit swaps a package-global logger.
	logBuf := captureSlogAudit(t)
	api, query := newDualWriteAPI(t)

	// A POST that reaches the handler and returns 201 (slog result=ok).
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	wrapped := api.Middleware(okHandler)

	req := httptest.NewRequest(http.MethodPost, "/admin/bots", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	wrapped.ServeHTTP(httptest.NewRecorder(), req)

	// slog path: must contain admin_audit with action=bot.create result=ok.
	require.Eventually(t, func() bool { return logBuf.Len() > 0 }, time.Second, 5*time.Millisecond)
	out := logBuf.String()
	require.Contains(t, out, "admin_audit")
	require.Contains(t, out, "action="+AuditBotCreate)
	require.Contains(t, out, "result="+AuditResultOk)
	require.Contains(t, out, "actor_user_id=admin-token")

	// user_activity path: one row, admin.bot.create, success, resource_type=bot.
	require.Eventually(t, func() bool { return len(query()) >= 1 }, 2*time.Second, 5*time.Millisecond)
	rows := query()
	require.Len(t, rows, 1)
	r := rows[0]
	require.Equal(t, "admin."+AuditBotCreate, r.Action)
	require.Equal(t, audit.OutcomeSuccess, r.Outcome)
	require.Equal(t, "bot", r.ResourceType)
	require.Equal(t, audit.PlatformAdmin, r.Platform)
	// Bearer auth is a distinct service identity, not an anonymous actor.
	require.Equal(t, "admin-token", r.UserID)
	require.Equal(t, audit.UserIDTypeSystem, r.UserIDType)
	// detail_json must carry method/path/status + slog_action for dashboard migration.
	var d map[string]any
	require.NoError(t, json.Unmarshal([]byte(r.DetailJSON), &d))
	require.Equal(t, "POST", d["method"])
	require.Equal(t, "/admin/bots", d["path"])
	require.EqualValues(t, 201, d["status"])
	require.Equal(t, AuditBotCreate, d["slog_action"])
}

// TestAdminDualWrite_FailedWriteRecordsFailure verifies a handler returning
// 4xx/5xx yields outcome=failure in user_activity and result=failed in slog.
func TestAdminDualWrite_FailedWriteRecordsFailure(t *testing.T) {
	logBuf := captureSlogAudit(t)
	api, query := newDualWriteAPI(t)

	failHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})
	wrapped := api.Middleware(failHandler)

	req := httptest.NewRequest(http.MethodDelete, "/admin/cron/jobs/42", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	wrapped.ServeHTTP(httptest.NewRecorder(), req)

	require.Eventually(t, func() bool { return len(query()) >= 1 }, 2*time.Second, 5*time.Millisecond)
	r := query()[0]
	require.Equal(t, audit.OutcomeFailure, r.Outcome)
	require.Equal(t, "admin."+AuditCronDelete, r.Action)
	require.Equal(t, "cron", r.ResourceType)
	require.Contains(t, logBuf.String(), "result="+AuditResultFailed)
}

// TestAdminDualWrite_GetNotAudited verifies GET requests are NOT dual-written
// (only write methods trigger the audit defer per isWriteMethod).
func TestAdminDualWrite_GetNotAudited(t *testing.T) {
	logBuf := captureSlogAudit(t)
	api, query := newDualWriteAPI(t)

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := api.Middleware(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/admin/bots", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	wrapped.ServeHTTP(httptest.NewRecorder(), req)

	// No slog audit, no user_activity row.
	require.NotContains(t, logBuf.String(), "admin_audit")
	require.Never(t, func() bool { return len(query()) > 0 },
		200*time.Millisecond, 10*time.Millisecond,
		"no spurious flush should appear for GET requests")
}

// TestAdminDualWrite_NilCollectorSlogOnly verifies that when no collector is
// wired, the legacy slog path still fires (graceful degradation).
func TestAdminDualWrite_NilCollectorSlogOnly(t *testing.T) {
	logBuf := captureSlogAudit(t)
	api := newTestAPI() // auditCollector deliberately nil

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	wrapped := api.Middleware(okHandler)

	req := httptest.NewRequest(http.MethodPost, "/admin/api-keys", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	require.NotPanics(t, func() {
		wrapped.ServeHTTP(httptest.NewRecorder(), req)
	})
	require.Eventually(t, func() bool { return logBuf.Len() > 0 }, time.Second, 5*time.Millisecond)
	require.Contains(t, logBuf.String(), "admin_audit")
	require.Contains(t, logBuf.String(), "action="+AuditAPIKeyCreate)
}

// TestAdminDualWrite_ResourceTypeCoverage covers the action→resource_type
// derivation across the admin action namespace (table-driven).
func TestAdminDualWrite_ResourceTypeCoverage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		action string
		want   string
	}{
		{AuditBotCreate, "bot"},
		{AuditBotUpdate, "bot"},
		{AuditCronDelete, "cron"},
		{AuditCronTrigger, "cron"},
		{AuditAPIKeyUpdate, "apikey"},
		{AuditSessionDelete, "session"},
		{AuditGatewayRestart, "gateway"},
		{AuditConfigRollback, "config"},
		{AuditMemberStatusUpdate, "member"},
		{AuditInvitationCreate, "invitation"},
		{AuditWorkspacePermissionModeUpdate, "workspace"},
		{"weird-no-dot", "admin"}, // fallback
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, resourceTypeFromAction(tc.action))
		})
	}
}

// TestMapOutcome covers the slog→user_activity outcome translation.
func TestMapOutcome(t *testing.T) {
	t.Parallel()
	require.Equal(t, audit.OutcomeSuccess, mapOutcome(AuditResultOk))
	require.Equal(t, audit.OutcomeFailure, mapOutcome(AuditResultFailed))
	require.Equal(t, audit.OutcomeDenied, mapOutcome(AuditResultDenied))
	require.Equal(t, audit.OutcomeFailure, mapOutcome("unknown"), "unknown defaults to failure")
}

// TestAdminDualWrite_DiverseActions is a smoke test that several admin write
// routes all produce correctly-prefixed user_activity actions.
func TestAdminDualWrite_DiverseActions(t *testing.T) {
	logBuf := captureSlogAudit(t)
	api, query := newDualWriteAPI(t)
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := api.Middleware(okHandler)

	routes := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodPost, "/admin/bots", "admin.bot.create"},
		{http.MethodDelete, "/admin/api-keys/k1", "admin.apikey.delete"},
		{http.MethodPost, "/admin/cron/jobs", "admin.cron.create"},
		{http.MethodPatch, "/admin/workspaces/ws1", "admin.workspace.permission_mode.update"},
	}
	for _, rt := range routes {
		req := httptest.NewRequest(rt.method, rt.path, nil)
		req.Header.Set("Authorization", "Bearer test-token")
		wrapped.ServeHTTP(httptest.NewRecorder(), req)
	}
	_ = logBuf // slog captured but not asserted per-route here

	require.Eventually(t, func() bool { return len(query()) >= len(routes) }, 2*time.Second, 5*time.Millisecond)
	rows := query()
	actions := map[string]bool{}
	for _, r := range rows {
		actions[r.Action] = true
	}
	for _, rt := range routes {
		require.True(t, actions[rt.want], "missing action %s", rt.want)
	}
}

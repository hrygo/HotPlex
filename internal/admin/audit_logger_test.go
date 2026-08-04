package admin

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAdminAudit_FollowsDefaultLoggerWithoutOverride covers the root-cause
// fix for admin_audit records bypassing the configured log pipeline: the
// package captured slog.Default() at init time, so production admin_audit
// lines went through Go's pre-init default logger (text -> stderr) instead
// of the JSON/lumberjack handler installed later by initLogging. With no
// test override installed, AdminAudit must follow the CURRENT default.
func TestAdminAudit_FollowsDefaultLoggerWithoutOverride(t *testing.T) {
	prev := currentAuditLogger()
	auditLogger.Store(nil) // clear any test-installed override
	t.Cleanup(func() { auditLogger.Store(prev) })

	var buf bytes.Buffer
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(orig) })

	AdminAudit("actor-1", AuditBotCreate, "/admin/bots", AuditResultOk)

	out := buf.String()
	require.Contains(t, out, "admin_audit", "record must reach the current default logger")
	require.Contains(t, out, "actor-1", "actor field must be present")
	require.Contains(t, out, AuditBotCreate, "action field must be present")
}

// TestAdminAudit_ExplicitOverrideStillWins pins the test contract: an
// override installed via SetAuditLogger must keep working (existing tests
// capture admin_audit output this way).
func TestAdminAudit_ExplicitOverrideStillWins(t *testing.T) {
	prev := currentAuditLogger()
	t.Cleanup(func() { auditLogger.Store(prev) })

	var buf bytes.Buffer
	SetAuditLogger(slog.New(slog.NewTextHandler(&buf, nil)))

	AdminAudit("actor-2", AuditAPIKeyCreate, "/admin/api-keys", AuditResultOk)

	out := buf.String()
	require.Contains(t, out, "admin_audit", "explicit override logger must receive the record")
	require.Contains(t, out, "actor-2")
}

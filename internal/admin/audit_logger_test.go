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

// TestSetAuditLogger_NilClearsOverride pins the restore contract: tests
// previously "restored" with SetAuditLogger(slog.Default()), which with the
// atomic override pointer installs the default AS a permanent override and
// stops AdminAudit from following later slog.SetDefault changes. nil must
// clear the override so the default is followed again at call time.
func TestSetAuditLogger_NilClearsOverride(t *testing.T) {
	prev := currentAuditLogger()
	t.Cleanup(func() { auditLogger.Store(prev) })

	var buf bytes.Buffer
	SetAuditLogger(slog.New(slog.NewTextHandler(&buf, nil)))
	require.NotEqual(t, slog.Default(), currentAuditLogger(), "override must be active")

	SetAuditLogger(nil)
	require.Equal(t, slog.Default(), currentAuditLogger(),
		"nil must clear the override so AdminAudit follows the current default")
}

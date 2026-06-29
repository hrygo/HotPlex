package codexcli

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
)

func TestPermissionModeFromSession(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode     string
		sandbox  string
		approval string
		ok       bool
	}{
		{worker.PermissionModeReadOnly, "read-only", "untrusted", true},
		{worker.PermissionModeWorkspace, "workspace-write", "on-request", true},
		{worker.PermissionModeAutoEdit, "workspace-write", "never", true},
		{worker.PermissionModeBypass, "danger-full-access", "never", true},
		{"", "", "", false}, // empty → caller falls back to config defaults (YOLO)
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			t.Parallel()
			sb, ap, ok := permissionModeFromSession(tt.mode)
			require.Equal(t, tt.sandbox, sb)
			require.Equal(t, tt.approval, ap)
			require.Equal(t, tt.ok, ok)
		})
	}
}

// TestBuildThreadStartParams_PermissionCeiling: r3 #804 review P1 — a session
// PermissionMode (workspace override or bridge-injected default) is a CEILING on
// permissiveness. It must tighten the operator's cfg.Sandbox/ApprovalMode but never
// relax it, so injecting "workspace" cannot clobber an operator's read-only sandbox.
func TestBuildThreadStartParams_PermissionCeiling(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		permMode     string
		cfgSandbox   string
		cfgApproval  string
		wantSandbox  string
		wantApproval string
	}{
		{
			name:         "injected workspace must not escalate a restricted operator sandbox",
			permMode:     worker.PermissionModeWorkspace, // → workspace-write / on-request
			cfgSandbox:   "read-only",                    // operator: stricter than workspace-write
			cfgApproval:  "untrusted",
			wantSandbox:  "read-only", // operator floor honored (no escalation)
			wantApproval: "untrusted", // operator floor honored
		},
		{
			name:         "injected workspace tightens the default YOLO operator config",
			permMode:     worker.PermissionModeWorkspace,
			cfgSandbox:   "danger-full-access", // operator default (least restrictive)
			cfgApproval:  "never",
			wantSandbox:  "workspace-write", // tier wins (tightening)
			wantApproval: "on-request",      // tier wins
		},
		{
			name:         "empty session tier honors operator config verbatim",
			permMode:     "",
			cfgSandbox:   "read-only",
			cfgApproval:  "untrusted",
			wantSandbox:  "read-only",
			wantApproval: "untrusted",
		},
		{
			name:         "explicit bypass tier still cannot relax a restricted operator floor",
			permMode:     worker.PermissionModeBypass, // → danger-full-access / never
			cfgSandbox:   "workspace-write",           // operator: stricter than danger-full-access
			cfgApproval:  "on-failure",                // operator: stricter than never
			wantSandbox:  "workspace-write",           // operator floor honored
			wantApproval: "on-failure",                // operator floor honored
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			session := worker.SessionInfo{ProjectDir: "/tmp/proj", PermissionMode: tt.permMode}
			cfg := Config{Sandbox: tt.cfgSandbox, ApprovalMode: tt.cfgApproval}
			params := buildThreadStartParams(session, cfg)
			require.Equal(t, tt.wantSandbox, params["sandbox"], "sandbox")
			require.Equal(t, tt.wantApproval, params["approvalPolicy"], "approvalPolicy")
		})
	}
}

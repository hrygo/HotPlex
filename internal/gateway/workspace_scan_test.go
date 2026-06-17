package gateway

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/hrygo/hotplex/internal/session"
)

// scanTestStore implements WorkspaceOverridesReader for scan tests.
type scanTestStore struct {
	workspaces []*session.Workspace
	err        error
}

func (s *scanTestStore) GetWorkspaceByID(_ context.Context, _ string) (*session.Workspace, error) {
	return nil, nil
}

func (s *scanTestStore) ListAllWorkspaces(_ context.Context) ([]*session.Workspace, error) {
	return s.workspaces, s.err
}

func captureScanLog() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(h), &buf
}

func TestScanWorkspaceOverrides_NilStore_Noop(t *testing.T) {
	t.Parallel()
	log, buf := captureScanLog()
	ScanWorkspaceOverrides(context.Background(), nil, log)
	if buf.Len() > 0 {
		t.Fatalf("expected no output for nil store, got: %s", buf.String())
	}
}

func TestScanWorkspaceOverrides_AllValid_NoWarn(t *testing.T) {
	t.Parallel()
	store := &scanTestStore{
		workspaces: []*session.Workspace{
			{ID: "ws-1", AgentConfigOverrides: ""},
			{ID: "ws-2", AgentConfigOverrides: `{"SOUL.md":"ok"}`},
		},
	}
	log, buf := captureScanLog()
	ScanWorkspaceOverrides(context.Background(), store, log)
	output := buf.String()
	if strings.Contains(output, "level=WARN") {
		t.Fatalf("expected no warnings for valid overrides, got: %s", output)
	}
	if !strings.Contains(output, "all workspace overrides valid") {
		t.Fatalf("expected debug success message, got: %s", output)
	}
}

func TestScanWorkspaceOverrides_DirtyData_Warns(t *testing.T) {
	t.Parallel()
	store := &scanTestStore{
		workspaces: []*session.Workspace{
			{ID: "ws-clean", Name: "Clean", OwnerUserID: "u-1", AgentConfigOverrides: `{"SOUL.md":"ok"}`},
			{ID: "ws-dirty", Name: "Dirty", OwnerUserID: "u-2", AgentConfigOverrides: `{bad json`},
		},
	}
	log, buf := captureScanLog()
	ScanWorkspaceOverrides(context.Background(), store, log)
	output := buf.String()
	if !strings.Contains(output, "ws-dirty") {
		t.Fatalf("expected warning about ws-dirty, got: %s", output)
	}
	if !strings.Contains(output, "invalid agent_config_overrides detected") {
		t.Fatalf("expected summary warning, got: %s", output)
	}
	if strings.Contains(output, "ws-clean") {
		t.Fatalf("should not warn about valid workspace, got: %s", output)
	}
}

func TestScanWorkspaceOverrides_StoreError_WarnsOnce(t *testing.T) {
	t.Parallel()
	store := &scanTestStore{
		err: context.DeadlineExceeded,
	}
	log, buf := captureScanLog()
	ScanWorkspaceOverrides(context.Background(), store, log)
	output := buf.String()
	if !strings.Contains(output, "startup workspace overrides scan failed") {
		t.Fatalf("expected scan failure warning, got: %s", output)
	}
}

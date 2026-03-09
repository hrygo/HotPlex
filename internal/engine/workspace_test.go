package engine

import (
	"context"
	"path/filepath"
	"testing"

	"log/slog"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceManager_CreateAndGet(t *testing.T) {
	// Create temp dir for audit
	tmpDir := t.TempDir()
	auditDir := filepath.Join(tmpDir, "audit")

	logger := slog.Default()
	wm := NewWorkspaceManager(logger, auditDir, DefaultResourceQuota())
	defer wm.Shutdown()

	ctx := context.Background()

	// Create workspace
	cfg := WorkspaceConfig{
		ID:        "test-workspace-1",
		Name:      "Test Workspace",
		RootPath:  filepath.Join(tmpDir, "workspaces", "test-workspace-1"),
		CreatedBy: "test-user",
		Quota:     DefaultResourceQuota(),
	}

	ws, err := wm.CreateWorkspace(ctx, cfg)
	require.NoError(t, err)
	assert.NotNil(t, ws)
	assert.Equal(t, "test-workspace-1", ws.Config.ID)
	assert.Equal(t, WorkspaceStatusActive, ws.Status)

	// Get workspace
	ws2, ok := wm.GetWorkspace(ctx, "test-workspace-1")
	require.True(t, ok)
	assert.Equal(t, ws.Config.ID, ws2.Config.ID)
}

func TestWorkspaceManager_DuplicateWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	auditDir := filepath.Join(tmpDir, "audit")

	logger := slog.Default()
	wm := NewWorkspaceManager(logger, auditDir, DefaultResourceQuota())
	defer wm.Shutdown()

	ctx := context.Background()

	cfg := WorkspaceConfig{
		ID:        "test-workspace",
		Name:      "Test",
		RootPath:  filepath.Join(tmpDir, "workspaces", "test"),
		CreatedBy: "test-user",
	}

	_, err := wm.CreateWorkspace(ctx, cfg)
	require.NoError(t, err)

	// Duplicate should fail
	_, err = wm.CreateWorkspace(ctx, cfg)
	assert.Error(t, err)
}

func TestWorkspaceManager_DeleteWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	auditDir := filepath.Join(tmpDir, "audit")

	logger := slog.Default()
	wm := NewWorkspaceManager(logger, auditDir, DefaultResourceQuota())
	defer wm.Shutdown()

	ctx := context.Background()

	cfg := WorkspaceConfig{
		ID:        "test-workspace-delete",
		Name:      "Test Delete",
		RootPath:  filepath.Join(tmpDir, "workspaces", "delete"),
		CreatedBy: "test-user",
	}

	_, err := wm.CreateWorkspace(ctx, cfg)
	require.NoError(t, err)

	err = wm.DeleteWorkspace(ctx, "test-workspace-delete")
	require.NoError(t, err)

	// Verify deleted
	_, ok := wm.GetWorkspace(ctx, "test-workspace-delete")
	assert.False(t, ok)
}

func TestWorkspaceManager_DeleteWithActiveSession(t *testing.T) {
	tmpDir := t.TempDir()
	auditDir := filepath.Join(tmpDir, "audit")

	logger := slog.Default()
	wm := NewWorkspaceManager(logger, auditDir, DefaultResourceQuota())
	defer wm.Shutdown()

	ctx := context.Background()

	cfg := WorkspaceConfig{
		ID:        "test-workspace-sessions",
		Name:      "Test With Sessions",
		RootPath:  filepath.Join(tmpDir, "workspaces", "sessions"),
		CreatedBy: "test-user",
	}

	ws, err := wm.CreateWorkspace(ctx, cfg)
	require.NoError(t, err)

	// Add a mock session
	mockSession := &Session{
		ID:                "session-1",
		ProviderSessionID:  "provider-session-1",
		Status:            SessionStatusReady,
		statusChange:      make(chan SessionStatus, 10),
	}
	ws.sessions["session-1"] = mockSession

	// Delete should fail
	err = wm.DeleteWorkspace(ctx, "test-workspace-sessions")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "active sessions")
}

func TestWorkspaceManager_ListWorkspaces(t *testing.T) {
	tmpDir := t.TempDir()
	auditDir := filepath.Join(tmpDir, "audit")

	logger := slog.Default()
	wm := NewWorkspaceManager(logger, auditDir, DefaultResourceQuota())
	defer wm.Shutdown()

	ctx := context.Background()

	// Create multiple workspaces
	for i := 0; i < 3; i++ {
		cfg := WorkspaceConfig{
			ID:        string(rune('0' + i)),
			Name:      "Workspace",
			RootPath:  filepath.Join(tmpDir, "workspaces", string(rune('0'+i))),
			CreatedBy: "test-user",
		}
		_, err := wm.CreateWorkspace(ctx, cfg)
		require.NoError(t, err)
	}

	list := wm.ListWorkspaces(ctx)
	assert.Len(t, list, 3)
}

func TestWorkspaceManager_UpdateQuota(t *testing.T) {
	tmpDir := t.TempDir()
	auditDir := filepath.Join(tmpDir, "audit")

	logger := slog.Default()
	wm := NewWorkspaceManager(logger, auditDir, DefaultResourceQuota())
	defer wm.Shutdown()

	ctx := context.Background()

	cfg := WorkspaceConfig{
		ID:        "test-quota",
		Name:      "Test Quota",
		RootPath:  filepath.Join(tmpDir, "workspaces", "quota"),
		CreatedBy: "test-user",
	}

	ws, err := wm.CreateWorkspace(ctx, cfg)
	require.NoError(t, err)
	_ = ws // ws will be retrieved after quota update

	newQuota := ResourceQuota{
		MemoryLimit:       4 * 1024 * 1024 * 1024, // 4GB
		CPUPercent:        90,
		MaxProcesses:      100,
		DiskIOBytesPerSec: 200 * 1024 * 1024,
		MaxSessions:       20,
		MaxWorkspaceSize:   20 * 1024 * 1024 * 1024,
	}

	err = wm.UpdateQuota(ctx, "test-quota", newQuota)
	require.NoError(t, err)

	ws, _ = wm.GetWorkspace(ctx, "test-quota")
	assert.Equal(t, newQuota.MemoryLimit, ws.Config.Quota.MemoryLimit)
	assert.Equal(t, newQuota.CPUPercent, ws.Config.Quota.CPUPercent)
}

func TestWorkspaceManager_GetUsage(t *testing.T) {
	tmpDir := t.TempDir()
	auditDir := filepath.Join(tmpDir, "audit")

	logger := slog.Default()
	wm := NewWorkspaceManager(logger, auditDir, DefaultResourceQuota())
	defer wm.Shutdown()

	ctx := context.Background()

	cfg := WorkspaceConfig{
		ID:        "test-usage",
		Name:      "Test Usage",
		RootPath:  filepath.Join(tmpDir, "workspaces", "usage"),
		CreatedBy: "test-user",
	}

	ws, err := wm.CreateWorkspace(ctx, cfg)
	require.NoError(t, err)

	// Add mock sessions
	for i := 0; i < 3; i++ {
		sess := &Session{
			ID:               "session-" + string(rune('0'+i)),
			ProviderSessionID: "provider-session-" + string(rune('0'+i)),
			Status:           SessionStatusReady,
			statusChange:     make(chan SessionStatus, 10),
		}
		ws.sessions[sess.ID] = sess
	}

	usage, err := wm.GetUsage(ctx, "test-usage")
	require.NoError(t, err)
	assert.Equal(t, 3, usage.ActiveSessions)
}

func TestWorkspaceManager_ValidatePath(t *testing.T) {
	tmpDir := t.TempDir()
	auditDir := filepath.Join(tmpDir, "audit")

	logger := slog.Default()
	wm := NewWorkspaceManager(logger, auditDir, DefaultResourceQuota())
	defer wm.Shutdown()

	ctx := context.Background()

	workspaceRoot := filepath.Join(tmpDir, "workspaces", "valid-path")
	cfg := WorkspaceConfig{
		ID:        "test-path-validation",
		Name:      "Test Path",
		RootPath:  workspaceRoot,
		CreatedBy: "test-user",
		AllowedPaths: []string{
			"/tmp/allowed",
		},
	}

	_, err := wm.CreateWorkspace(ctx, cfg)
	require.NoError(t, err)

	// Valid path within workspace
	valid, err := wm.ValidatePath(ctx, "test-path-validation", filepath.Join(workspaceRoot, "file.txt"))
	require.NoError(t, err)
	assert.True(t, valid)

	// Invalid path outside workspace
	valid, err = wm.ValidatePath(ctx, "test-path-validation", "/etc/passwd")
	require.NoError(t, err)
	assert.False(t, valid)

	// Path traversal attempt
	valid, err = wm.ValidatePath(ctx, "test-path-validation", filepath.Join(workspaceRoot, "..", "..", "etc", "passwd"))
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestWorkspaceManager_RegisterSession(t *testing.T) {
	tmpDir := t.TempDir()
	auditDir := filepath.Join(tmpDir, "audit")

	logger := slog.Default()
	wm := NewWorkspaceManager(logger, auditDir, DefaultResourceQuota())
	defer wm.Shutdown()

	ctx := context.Background()

	cfg := WorkspaceConfig{
		ID:        "test-register-session",
		Name:      "Test Register",
		RootPath:  filepath.Join(tmpDir, "workspaces", "register"),
		CreatedBy: "test-user",
		Quota: ResourceQuota{
			MaxSessions: 2,
		},
	}

	_, err := wm.CreateWorkspace(ctx, cfg)
	require.NoError(t, err)

	sess := &Session{
		ID:                "new-session",
		ProviderSessionID: "provider-new-session",
		Status:            SessionStatusReady,
		statusChange:      make(chan SessionStatus, 10),
	}

	err = wm.RegisterSession(ctx, "test-register-session", sess)
	require.NoError(t, err)

	ws, _ := wm.GetWorkspace(ctx, "test-register-session")
	assert.Len(t, ws.sessions, 1)
	assert.Equal(t, WorkspaceStatusActive, ws.Status)
}

func TestWorkspaceManager_SessionLimit(t *testing.T) {
	tmpDir := t.TempDir()
	auditDir := filepath.Join(tmpDir, "audit")

	logger := slog.Default()
	wm := NewWorkspaceManager(logger, auditDir, DefaultResourceQuota())
	defer wm.Shutdown()

	ctx := context.Background()

	cfg := WorkspaceConfig{
		ID:        "test-session-limit",
		Name:      "Test Limit",
		RootPath:  filepath.Join(tmpDir, "workspaces", "limit"),
		CreatedBy: "test-user",
		Quota: ResourceQuota{
			MaxSessions: 1,
		},
	}

	_, err := wm.CreateWorkspace(ctx, cfg)
	require.NoError(t, err)

	// Register first session
	sess1 := &Session{
		ID:                "session-1",
		ProviderSessionID: "provider-session-1",
		Status:            SessionStatusReady,
		statusChange:      make(chan SessionStatus, 10),
	}
	err = wm.RegisterSession(ctx, "test-session-limit", sess1)
	require.NoError(t, err)

	// Register second session should fail
	sess2 := &Session{
		ID:                "session-2",
		ProviderSessionID: "provider-session-2",
		Status:            SessionStatusReady,
		statusChange:      make(chan SessionStatus, 10),
	}
	err = wm.RegisterSession(ctx, "test-session-limit", sess2)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "session limit")
}

func TestWorkspaceConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     WorkspaceConfig
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: WorkspaceConfig{
				ID:        "valid",
				Name:      "Valid",
				RootPath:  "/tmp/test",
				CreatedBy: "user",
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			cfg: WorkspaceConfig{
				Name:     "Valid",
				RootPath: "/tmp/test",
				CreatedBy: "user",
			},
			wantErr: true,
		},
		{
			name: "missing Name",
			cfg: WorkspaceConfig{
				ID:       "valid",
				RootPath: "/tmp/test",
				CreatedBy: "user",
			},
			wantErr: true,
		},
		{
			name: "missing RootPath",
			cfg: WorkspaceConfig{
				ID:        "valid",
				Name:      "Valid",
				CreatedBy: "user",
			},
			wantErr: true,
		},
		{
			name: "missing CreatedBy",
			cfg: WorkspaceConfig{
				ID:       "valid",
				Name:     "Valid",
				RootPath: "/tmp/test",
			},
			wantErr: true,
		},
		{
			name: "relative path",
			cfg: WorkspaceConfig{
				ID:        "valid",
				Name:      "Valid",
				RootPath:  "relative/path",
				CreatedBy: "user",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestResourceQuota_Defaults(t *testing.T) {
	quota := DefaultResourceQuota()

	assert.Equal(t, int64(2*1024*1024*1024), quota.MemoryLimit)
	assert.Equal(t, 80, quota.CPUPercent)
	assert.Equal(t, 50, quota.MaxProcesses)
	assert.Equal(t, int64(100*1024*1024), quota.DiskIOBytesPerSec)
	assert.Equal(t, 10, quota.MaxSessions)
	assert.Equal(t, int64(10*1024*1024*1024), quota.MaxWorkspaceSize)
}

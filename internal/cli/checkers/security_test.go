package checkers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cli"
)

// Tests using t.Setenv or modifying configPath cannot use t.Parallel because:
//   - t.Setenv is incompatible with t.Parallel in Go's testing framework
//   - configPath is a package-level mutable variable subject to data races

func TestAdminToken_Empty(t *testing.T) {
	t.Setenv("ADMIN_TOKEN", "")
	t.Setenv("HOTPLEX_ADMIN_TOKEN_1", "")
	defer resetConfigPath()
	SetConfigPath("")

	c := adminTokenChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusFail, d.Status)
	require.Contains(t, d.Message, "empty")
}

func TestAdminToken_WeakDefault(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{"admin", "admin"},
		{"default", "default"},
		{"password", "password"},
		{"changeme", "changeme"},
		{"ADMIN uppercase", "ADMIN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ADMIN_TOKEN", tt.token)

			c := adminTokenChecker{}
			d := c.Check(context.Background())

			require.Equal(t, cli.StatusFail, d.Status)
			require.Contains(t, d.Message, "weak default")
		})
	}
}

func TestAdminToken_Strong(t *testing.T) {
	t.Setenv("ADMIN_TOKEN", "a1b2c3d4e5f6a1b2c3d4e5f6")

	c := adminTokenChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusPass, d.Status)
}

func TestAdminToken_FromHotplexEnv(t *testing.T) {
	t.Setenv("ADMIN_TOKEN", "")
	t.Setenv("HOTPLEX_ADMIN_TOKEN_1", "strong-token-from-env")

	c := adminTokenChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusPass, d.Status)
}

func TestFilePerms(t *testing.T) {
	c := filePermsChecker{}
	d := c.Check(context.Background())

	require.Equal(t, "security.file_permissions", d.Name)
	require.Equal(t, "security", d.Category)
	require.Contains(t, []cli.Status{cli.StatusPass, cli.StatusWarn}, d.Status)
}

func TestEnvInGit(t *testing.T) {
	c := envInGitChecker{}
	d := c.Check(context.Background())

	require.Equal(t, "security.env_in_git", d.Name)
	require.Equal(t, "security", d.Category)
	require.Contains(t, []cli.Status{cli.StatusPass, cli.StatusFail}, d.Status)
}

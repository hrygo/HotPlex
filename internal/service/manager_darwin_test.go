//go:build darwin

package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDarwinManagerStatusRequiresLiveLaunchctlPID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	plistPath := filepath.Join(home, "Library", "LaunchAgents", launchdLabel("hotplex", LevelUser)+".plist")
	require.NoError(t, os.MkdirAll(filepath.Dir(plistPath), 0o755))
	require.NoError(t, os.WriteFile(plistPath, []byte("plist"), 0o600))

	tests := []struct {
		name      string
		launchctl string
		processUp bool
		wantRun   bool
		wantPID   int
	}{
		{
			name:      "live process",
			launchctl: `"PID" = 1234;`,
			processUp: true,
			wantRun:   true,
			wantPID:   1234,
		},
		{
			name:      "stale process",
			launchctl: `"PID" = 1234;`,
			wantRun:   false,
		},
		{
			name:      "loaded without process",
			launchctl: `"LastExitStatus" = 0;`,
			wantRun:   false,
		},
		{
			name:      "zero process id",
			launchctl: `"PID" = 0;`,
			wantRun:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &darwinManager{
				run: mockCommandRunner{
					combinedOutputFn: func(name string, args ...string) ([]byte, error) {
						require.Equal(t, "launchctl", name)
						require.Equal(t, []string{"list", launchdLabel("hotplex", LevelUser)}, args)
						return []byte(tt.launchctl), nil
					},
				},
				processAlive: func(pid int) bool {
					return tt.processUp && pid == 1234
				},
			}

			status, err := manager.Status("hotplex", LevelUser)
			require.NoError(t, err)
			require.Equal(t, tt.wantRun, status.Running)
			require.Equal(t, tt.wantPID, status.PID)
		})
	}
}

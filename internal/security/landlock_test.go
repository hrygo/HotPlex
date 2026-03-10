package security

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========================================
// Landlock Tests
// ========================================

func TestIsLandlockSupported(t *testing.T) {
	// This test checks if Landlock is supported on the system
	// It may skip on systems without Landlock support
	supported := IsLandlockSupported()
	t.Logf("Landlock supported: %v", supported)
}

func TestLandlockEnforcerConfig(t *testing.T) {
	config := DefaultLandlockConfig()
	assert.NotNil(t, config.Logger)
	assert.False(t, config.AllowExec)
	assert.True(t, config.AllowRead)
}

func TestLandlockEnforcerCreation(t *testing.T) {
	config := DefaultLandlockConfig()
	config.AllowedDirs = []string{"/tmp", "/home/user"}
	config.Logger = testLogger(t)

	enforcer, err := NewLandlockEnforcer(config)
	require.NoError(t, err)
	assert.NotNil(t, enforcer)

	// Don't call Enforce() in tests as it requires actual kernel support
	// and may fail in containers or restricted environments
}

func TestPathTraversalPrevention(t *testing.T) {
	logger := testLogger(t)
	allowedRoots := []string{"/tmp", "/home/user"}

	ptp := NewPathTraversalPrevention(allowedRoots, logger)

	tests := []struct {
		name        string
		path        string
		shouldFail bool
	}{
		{"normal path", "/tmp/file.txt", false},
		{"subdirectory", "/tmp/subdir/file.txt", false},
		{"traversal attempt", "/tmp/../../../etc/passwd", true},
		{"url encoded", "/tmp/%2e%2e%2fpasswd", true},
		{"outside root", "/var/log/secret", true},
		{"user home", "/home/user/file.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ptp.CheckPath(tt.path)
			if tt.shouldFail {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestLandlockCheckPath(t *testing.T) {
	config := DefaultLandlockConfig()
	config.AllowedDirs = []string{"/tmp", "/home/user"}
	config.Logger = testLogger(t)

	enforcer, err := NewLandlockEnforcer(config)
	require.NoError(t, err)

	tests := []struct {
		name        string
		path        string
		shouldFail  bool
	}{
		{"allowed path", "/tmp/test.txt", false},
		{"subdirectory", "/tmp/dir/file.txt", false},
		{"outside allowed", "/var/log", true},
		{"user path", "/home/user/file.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := enforcer.CheckPath(tt.path)
			if tt.shouldFail {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDeniedExtensions(t *testing.T) {
	config := DefaultLandlockConfig()
	config.AllowedDirs = []string{"/tmp"}
	config.DeniedExtensions = []string{".so", ".exe", ".dll"}
	config.Logger = testLogger(t)

	enforcer, err := NewLandlockEnforcer(config)
	require.NoError(t, err)

	// Allowed extension should pass
	err = enforcer.CheckPath("/tmp/script.sh")
	assert.NoError(t, err)

	// Denied extension should fail
	err = enforcer.CheckPath("/tmp/malicious.so")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "extension denied")
}

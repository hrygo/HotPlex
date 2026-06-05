//go:build darwin || linux

package config

import (
	"os"
	"path/filepath"
)

// TempBaseDir returns the base directory for temporary HotPlex files.
// Uses os.TempDir() to respect TMPDIR overrides instead of hardcoding /tmp.
func TempBaseDir() string { return filepath.Join(os.TempDir(), "hotplex") }

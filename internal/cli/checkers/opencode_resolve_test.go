package checkers

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyExecutable(t *testing.T) {
	t.Parallel()

	// Resolving whether OCS is launched via a wrapper (.cmd/.bat shim that maps
	// the child's native exit status to a generic code) vs a native binary is
	// central to issue #900's Windows diagnostics.
	tests := []struct {
		name string
		path string
		want string
	}{
		{"windows exe", `C:\opt\opencode.exe`, "native"},
		{"posix no extension", "/usr/local/bin/opencode", "native"},
		{"empty path", "", "native"},
		{"windows cmd wrapper", `C:\node\opencode.cmd`, "wrapper"},
		{"windows bat wrapper", `C:\node\opencode.bat`, "wrapper"},
		{"powershell wrapper", `C:\node\opencode.ps1`, "wrapper"},
		{"posix shell shim", "/usr/local/bin/opencode.sh", "wrapper"},
		{"unknown extension", "/usr/local/bin/opencode.bin", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, classifyExecutable(tt.path))
		})
	}
}

package proc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatExitCode(t *testing.T) {
	t.Parallel()

	// FormatExitCode reports the exit code as its honest signed Go int (so POSIX
	// signal death -1 stays -1, not a huge unsigned value) plus the uint32 bit
	// pattern as 8-digit hex (the reliable cross-platform identifier for Windows
	// NT statuses like 0xC0000142, which arrive as the positive int 3221225794
	// via ProcessState.ExitCode() on Windows).
	tests := []struct {
		name    string
		code    int
		wantDec string
		wantHex string
	}{
		{"zero", 0, "0", "0x00000000"},
		{"normal exit 1", 1, "1", "0x00000001"},
		{"SIGTERM 143", 143, "143", "0x0000008F"},
		{"POSIX signal death -1", -1, "-1", "0xFFFFFFFF"},
		{"STATUS_DLL_INIT_FAILED positive int", 3221225794, "3221225794", "0xC0000142"},
		{"STATUS_DLL_INIT_FAILED sign-extended", -1073741502, "-1073741502", "0xC0000142"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dec, hex := FormatExitCode(tt.code)
			require.Equal(t, tt.wantDec, dec, "decimal mismatch")
			require.Equal(t, tt.wantHex, hex, "hex mismatch")
		})
	}
}

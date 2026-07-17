package proc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatExitCode(t *testing.T) {
	t.Parallel()

	// Windows NT status codes arrive as the raw GetExitCodeProcess DWORD.
	// Depending on the Go runtime path, the same 0xC0000142 may surface as a
	// positive int (3221225474) or a sign-extended negative int (-1073741502).
	// FormatExitCode must normalize both to the same unsigned decimal + hex so
	// operators read STATUS_DLL_INIT_FAILED consistently regardless of sign.
	tests := []struct {
		name    string
		code    int
		wantDec string
		wantHex string
	}{
		{"zero", 0, "0", "0x00000000"},
		{"normal exit 1", 1, "1", "0x00000001"},
		{"SIGTERM 143", 143, "143", "0x0000008F"},
		{"killed sentinel -1", -1, "4294967295", "0xFFFFFFFF"},
		{"STATUS_DLL_INIT_FAILED positive int", 3221225794, "3221225794", "0xC0000142"},
		{"STATUS_DLL_INIT_FAILED negative int", -1073741502, "3221225794", "0xC0000142"},
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

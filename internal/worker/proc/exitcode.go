package proc

import (
	"fmt"
	"strconv"
)

// FormatExitCode renders a worker process exit code as its honest signed Go
// int (decimal) plus the uint32 bit pattern as zero-padded hexadecimal.
//
// The signed decimal preserves POSIX semantics where -1 means signal death;
// distorting it to the unsigned 4294967295 would hide that. The uint32 hex is
// the reliable cross-platform identifier for Windows NT statuses (e.g.
// 0xC0000142 STATUS_DLL_INIT_FAILED, which arrives as the positive int
// 3221225794 via ProcessState.ExitCode() on Windows), since identical bit
// patterns render the same hex regardless of int sign.
func FormatExitCode(code int) (decimal, hex string) {
	return strconv.Itoa(code), fmt.Sprintf("0x%08X", uint32(code))
}

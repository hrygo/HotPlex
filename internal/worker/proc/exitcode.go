package proc

import (
	"fmt"
	"strconv"
)

// FormatExitCode renders a worker process exit code as both unsigned decimal
// and zero-padded hexadecimal. Windows NT status codes (e.g. 0xC0000142
// STATUS_DLL_INIT_FAILED) may surface as either a positive int or a
// sign-extended negative int depending on the Go runtime path; both forms
// normalize to the same unsigned representation so operators read one value.
func FormatExitCode(code int) (decimal, hex string) {
	u := uint32(code)
	return strconv.FormatUint(uint64(u), 10), fmt.Sprintf("0x%08X", u)
}

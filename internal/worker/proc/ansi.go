package proc

import "strings"

// StripANSI removes ANSI escape sequences from a single line of terminal
// output so worker stderr/stdout content can be logged without control
// character noise (e.g. codex rmcp color codes like "\x1b[31m").
//
// Recognized sequences:
//   - CSI: ESC [ ... final byte in 0x40-0x7E (covers SGR color codes)
//   - OSC: ESC ] ... BEL (0x07) or ESC \ (ST) (e.g. window title)
//   - Fe two-byte escapes: ESC followed by a byte in 0x40-0x5F
//   - A stray ESC is dropped but the following regular character is kept
//
// The fast path returns the input unchanged when no ESC byte is present,
// so the common case (plain log lines) does no allocation.
func StripANSI(line string) string {
	if !strings.ContainsRune(line, 0x1b) {
		return line
	}

	var b strings.Builder
	b.Grow(len(line))
	i := 0
	for i < len(line) {
		if line[i] != 0x1b {
			b.WriteByte(line[i])
			i++
			continue
		}
		// Escape sequence starts at i; find where it ends.
		if i+1 >= len(line) {
			// Trailing lone ESC: drop it.
			i++
			continue
		}
		next := line[i+1]
		switch {
		case next == '[':
			// CSI: consume until a final byte in 0x40-0x7E (inclusive).
			j := i + 2
			for j < len(line) && (line[j] < 0x40 || line[j] > 0x7e) {
				j++
			}
			if j < len(line) {
				j++ // consume the final byte
			}
			i = j
		case next == ']':
			// OSC: consume until BEL or ST (ESC \).
			j := i + 2
			for j < len(line) {
				if line[j] == 0x07 {
					j++
					break
				}
				if line[j] == 0x1b && j+1 < len(line) && line[j+1] == '\\' {
					j += 2
					break
				}
				j++
			}
			i = j
		case next >= 0x40 && next <= 0x5f:
			// Fe two-byte escape: drop both bytes.
			i += 2
		default:
			// Stray ESC before a regular character: drop the ESC, keep the char.
			i++
		}
	}
	return b.String()
}

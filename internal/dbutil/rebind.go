package dbutil

import (
	"fmt"
	"strings"
)

type rebindState int

const (
	stateNormal rebindState = iota
	stateSingleQuote
	stateDoubleQuote
	stateDollarQuote
	stateLineComment
	stateBlockComment
)

// rebind converts ? placeholders to $N positional parameters.
// It implements a state machine that skips ? inside string literals,
// quoted identifiers, comments, and dollar quotes.
//
// Special cases:
//   - ” (escaped single quote inside string) stays in SingleQuote
//   - "" (escaped double quote inside identifier) stays in DoubleQuote
//   - $$ or $tag$ enters DollarQuote; $tag$ exits it
//   - -- starts LineComment; newline exits it
//   - /* starts BlockComment; */ exits it
//   - \? (backslash-escaped question mark) is preserved
func rebind(query string) string {
	if !strings.ContainsRune(query, '?') {
		return query
	}

	var buf strings.Builder
	buf.Grow(len(query) + 32)

	n := 0
	i := 0
	state := stateNormal
	var dollarTag string

	for i < len(query) {
		ch := query[i]

		switch state {
		case stateNormal:
			switch {
			case ch == '\'':
				state = stateSingleQuote
				buf.WriteByte(ch)
				i++

			case ch == '"':
				state = stateDoubleQuote
				buf.WriteByte(ch)
				i++

			case ch == '-' && i+1 < len(query) && query[i+1] == '-':
				state = stateLineComment
				buf.WriteString("--")
				i += 2

			case ch == '/' && i+1 < len(query) && query[i+1] == '*':
				state = stateBlockComment
				buf.WriteString("/*")
				i += 2

			case ch == '$':
				tag, consumed := parseDollarQuoteStart(query, i)
				if consumed > 0 {
					state = stateDollarQuote
					dollarTag = tag
					buf.WriteString(query[i : i+consumed])
					i += consumed
				} else {
					buf.WriteByte(ch)
					i++
				}

			case ch == '\\' && i+1 < len(query) && query[i+1] == '?':
				buf.WriteByte('\\')
				buf.WriteByte('?')
				i += 2

			case ch == '?':
				n++
				fmt.Fprintf(&buf, "$%d", n)
				i++

			default:
				buf.WriteByte(ch)
				i++
			}

		case stateSingleQuote:
			buf.WriteByte(ch)
			if ch == '\'' {
				if i+1 < len(query) && query[i+1] == '\'' {
					buf.WriteByte('\'')
					i += 2
				} else {
					state = stateNormal
					i++
				}
			} else {
				i++
			}

		case stateDoubleQuote:
			buf.WriteByte(ch)
			if ch == '"' {
				if i+1 < len(query) && query[i+1] == '"' {
					buf.WriteByte('"')
					i += 2
				} else {
					state = stateNormal
					i++
				}
			} else {
				i++
			}

		case stateDollarQuote:
			buf.WriteByte(ch)
			if ch == '$' {
				if checkDollarQuoteEnd(query, i, dollarTag) {
					_, tagLen := dollarQuoteTagLen(query[i+1:])
					buf.WriteString(query[i+1 : i+1+tagLen+1])
					i += 2 + tagLen
					state = stateNormal
					dollarTag = ""
				} else {
					i++
				}
			} else {
				i++
			}

		case stateLineComment:
			buf.WriteByte(ch)
			if ch == '\n' {
				state = stateNormal
			}
			i++

		case stateBlockComment:
			buf.WriteByte(ch)
			if ch == '*' && i+1 < len(query) && query[i+1] == '/' {
				buf.WriteByte('/')
				i += 2
				state = stateNormal
			} else {
				i++
			}
		}
	}

	return buf.String()
}

// parseDollarQuoteStart checks if position i is the start of a dollar-quoted string.
// Returns the tag (empty for $$) and how many characters the opening $tag$ consumes.
// Returns consumed=0 if position i is not a dollar quote start.
func parseDollarQuoteStart(s string, i int) (tag string, consumed int) {
	if i >= len(s) || s[i] != '$' {
		return "", 0
	}
	tag, tagLen := dollarQuoteTagLen(s[i+1:])
	if tagLen < 0 {
		return "", 0
	}
	// consumed = 1 (opening $) + tagLen + 1 (closing $)
	return tag, 1 + tagLen + 1
}

// dollarQuoteTagLen reads the tag from a dollar-quote suffix.
// s is the content after the opening $.
// Returns the tag string and its length.
// Returns tagLen=-1 if not a valid dollar quote.
func dollarQuoteTagLen(s string) (string, int) {
	if s == "" {
		return "", -1
	}
	// Read tag characters (must be valid identifier chars)
	j := 0
	for j < len(s) && isDollarTagChar(s[j]) {
		j++
	}
	if j >= len(s) || s[j] != '$' {
		return "", -1
	}
	return s[:j], j
}

// isDollarTagChar returns true if ch is valid in a dollar quote tag.
// Tags must be composed of identifier characters per PostgreSQL spec.
func isDollarTagChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '_'
}

// checkDollarQuoteEnd checks if the sequence starting at i ($ + tag + $)
// matches the expected tag for closing a dollar quote.
func checkDollarQuoteEnd(s string, i int, tag string) bool {
	if i >= len(s) || s[i] != '$' {
		return false
	}
	// Need at least $ + tag + $ = 2 + len(tag) chars
	if i+1+len(tag) >= len(s) {
		return false
	}
	if s[i+1:i+1+len(tag)] != tag {
		return false
	}
	if s[i+1+len(tag)] != '$' {
		return false
	}
	return true
}

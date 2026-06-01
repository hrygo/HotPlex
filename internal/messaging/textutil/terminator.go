// Package textutil hosts platform-agnostic predicates over streaming text
// chunks (delta content, partial tokens, etc.). Adapters in feishu/, slack/,
// yuanxin/ etc. call these helpers instead of re-deriving the same
// per-byte / per-rune checks — the constants and UTF-8 traps below are
// non-obvious and easy to get wrong from scratch.
package textutil

import "unicode/utf8"

// EndsWithSentenceTerminator reports whether s ends on a sentence
// terminator: ASCII (. ? !) or CJK fullwidth (。 ？ ！). A trailing
// half-width cluster such as "?!" or "!!" is accepted — we only care
// that the final rune is a terminator.
//
// Hot-path: no []rune conversion, single DecodeLastRuneInString for the
// non-ASCII branch. DecodeLastRuneInString is alloc-free in the Go
// stdlib for both ASCII and CJK inputs (verified by benchmark).
//
// Why a byte-level fast path is wrong: '。' (U+3002), '？' (U+FF1F),
// and '！' (U+FF01) have DIFFERENT trailing bytes in UTF-8 (0x82,
// 0x9F, 0x81 respectively). There is no single byte that catches all
// three fullwidth terminators. An earlier version of this helper had a
// "0x82 fast path" that only matched '。' and silently dropped '？' and
// '！' — the exact bug this extraction was made to prevent from
// recurring across platform adapters.
func EndsWithSentenceTerminator(s string) bool {
	if s == "" {
		return false
	}
	last := s[len(s)-1]
	if last == '.' || last == '?' || last == '!' {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s)
	return r == '。' || r == '？' || r == '！'
}

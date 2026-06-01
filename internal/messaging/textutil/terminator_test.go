package textutil

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEndsWithSentenceTerminator(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		text string
		want bool
	}{
		// Empty → no separator.
		{"empty", "", false},
		// CJK word-split mid-stream chunks: must NOT add \n.
		// Regression: original Feishu bug appended \n\n between every
		// MessageDelta, which split CJK words across chunk boundaries
		// (e.g. "赫尔" + "墨斯" rendered as separate lines).
		{"cjk first half of word", "赫尔", false},
		{"cjk second half of word", "墨斯", false},
		{"cjk mid sentence", "墨斯的", false},
		{"chinese comma", "赫尔墨斯，", false},
		// ASCII sentence terminators → add \n.
		{"ascii period", "Hello world.", true},
		{"ascii question", "Are you sure?", true},
		{"ascii exclamation", "Got it!", true},
		// ASCII terminator clusters → add \n.
		{"ascii question-period", "really?!", true},
		{"ascii bang-period", "yes!.", true},
		{"ascii double question", "why??", true},
		{"ascii double bang", "wow!!", true},
		{"ascii interrobang", "what!?", true},
		// CJK fullwidth terminators → add \n.
		{"cjk period", "你好世界。", true},
		{"cjk question", "你好吗？", true},
		{"cjk exclamation", "太棒了！", true},
		// Whitespace and other punctuation: do not trigger.
		{"space suffix", "trailing space ", false},
		{"semicolon", "item;", false},
		{"chinese semicolon", "项；", false},
		{"chinese colon", "注：", false},
		// CJK char ending in 0x82 that is NOT a terminator: 0x82 is
		// also the trailing byte of other 3-byte UTF-8 chars. Make sure
		// we decode the rune and don't false-positive.
		{"cjk non-terminator 0x82", "啊", false},
		{"chinese comma only", "你，", false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := EndsWithSentenceTerminator(tt.text)
			require.Equal(t, tt.want, got, "EndsWithSentenceTerminator(%q)", tt.text)
		})
	}
}

// BenchmarkEndsWithSentenceTerminator measures the per-delta decision
// cost on representative streaming chunks. Every MessageDelta on every
// platform adapter should funnel through this helper, so it sits on the
// hot path of every streaming turn. The constant-time implementation
// must not allocate per call.
//
// The shapes (CJK mid-word 200 runes, 1k ASCII, CJK-with-terminator,
// short mid-CJK) intentionally differ from the correctness test cases
// above: this benchmark measures perf axes (length × terminator
// presence × UTF-8 decode branch), not the correctness matrix.
func BenchmarkEndsWithSentenceTerminator(b *testing.B) {
	cases := []struct {
		name string
		text string
	}{
		{"cjk_mid_200", strings.Repeat("赫", 199) + "尔"},
		{"ascii_long_1k", strings.Repeat("a", 999) + "."},
		{"cjk_end_50", strings.Repeat("中", 49) + "。"},
		{"cjk_short_2", "赫尔"},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = EndsWithSentenceTerminator(c.text)
			}
		})
	}
}

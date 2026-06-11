package textutil

import "unicode/utf8"

// paraBreakThreshold is the cumulative character count after which a
// paragraph break (\n\n) is inserted at the next sentence terminator.
// 150 runes ≈ 8–10 lines on mobile, optimal for CJK reading rhythm.
const paraBreakThreshold = 150

// ParagraphBreaker tracks cumulative character count across streaming
// deltas and decides when to inject a paragraph break.
type ParagraphBreaker struct {
	count int
}

// Add accumulates the rune count of the latest delta and returns true
// when a paragraph break should be inserted: the cumulative count
// exceeds paraBreakThreshold AND the delta ends with a sentence
// terminator. On break the internal counter resets to zero.
func (pb *ParagraphBreaker) Add(delta string) bool {
	pb.count += utf8.RuneCountInString(delta)
	if pb.count > paraBreakThreshold && EndsWithSentenceTerminator(delta) {
		pb.count = 0
		return true
	}
	return false
}

// Reset resets the cumulative counter (e.g. on Done/Error events).
func (pb *ParagraphBreaker) Reset() {
	pb.count = 0
}

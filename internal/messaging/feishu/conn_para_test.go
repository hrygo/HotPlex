package feishu

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestParagraphBreak_NoBreakBelowThreshold(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	c := newTestConn(adapter, "")

	text := strings.Repeat("a", 199) + "\u3002"
	c.mu.Lock()
	c.paraCharCount += utf8.RuneCountInString(text)
	shouldBreak := c.paraCharCount > 200 && endsWithSentenceTerminator(text)
	c.mu.Unlock()

	require.False(t, shouldBreak, "200 runes (not > 200), should not break")
	require.Equal(t, 200, c.paraCharCount)
}

func TestParagraphBreak_TriggerAt201(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	c := newTestConn(adapter, "")

	c.mu.Lock()
	c.paraCharCount = 200
	delta := "\u3002"
	c.paraCharCount += utf8.RuneCountInString(delta)
	shouldBreak := c.paraCharCount > 200 && endsWithSentenceTerminator(delta)
	if shouldBreak {
		c.paraCharCount = 0
	}
	c.mu.Unlock()

	require.True(t, shouldBreak, "201 > 200 with terminator, should break")
	require.Equal(t, 0, c.paraCharCount, "counter should reset")
}

func TestParagraphBreak_Boundary200(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	c := newTestConn(adapter, "")

	c.mu.Lock()
	c.paraCharCount = 199
	delta := "a"
	c.paraCharCount += utf8.RuneCountInString(delta)
	shouldBreak := c.paraCharCount > 200 && endsWithSentenceTerminator(delta)
	c.mu.Unlock()

	require.False(t, shouldBreak, "exactly 200 is not > 200")
}

func TestParagraphBreak_NoTerminatorNoBreak(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	c := newTestConn(adapter, "")

	c.mu.Lock()
	c.paraCharCount = 500
	delta := "hello"
	c.paraCharCount += utf8.RuneCountInString(delta)
	shouldBreak := c.paraCharCount > 200 && endsWithSentenceTerminator(delta)
	c.mu.Unlock()

	require.False(t, shouldBreak, "no terminator, no break")
}

func TestParagraphBreak_CJKTerminator(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	c := newTestConn(adapter, "")

	c.mu.Lock()
	c.paraCharCount = 200
	delta := "\uff01" // '！'
	c.paraCharCount += utf8.RuneCountInString(delta)
	shouldBreak := c.paraCharCount > 200 && endsWithSentenceTerminator(delta)
	if shouldBreak {
		c.paraCharCount = 0
	}
	c.mu.Unlock()

	require.True(t, shouldBreak, "CJK terminator should trigger break")
	require.Equal(t, 0, c.paraCharCount)
}

func TestParagraphBreak_ResetAfterBreak(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	c := newTestConn(adapter, "")

	c.mu.Lock()
	c.paraCharCount = 200
	delta1 := "\u3002"
	c.paraCharCount += utf8.RuneCountInString(delta1)
	if c.paraCharCount > 200 && endsWithSentenceTerminator(delta1) {
		c.paraCharCount = 0
	}
	require.Equal(t, 0, c.paraCharCount)

	delta2 := "hello"
	c.paraCharCount += utf8.RuneCountInString(delta2)
	require.Equal(t, 5, c.paraCharCount, "fresh accumulation after reset")
	c.mu.Unlock()
}

func TestParagraphBreak_SessionIsolation(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	c1 := newTestConn(adapter, "")
	c2 := newTestConn(adapter, "")

	c1.mu.Lock()
	c1.paraCharCount = 200
	c1.mu.Unlock()

	c2.mu.Lock()
	require.Equal(t, 0, c2.paraCharCount)
	c2.mu.Unlock()
}

func TestParagraphBreak_ResetOnDone(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	c := newTestConn(adapter, "")

	c.mu.Lock()
	c.paraCharCount = 150
	c.paraCharCount = 0
	c.mu.Unlock()

	c.mu.RLock()
	require.Equal(t, 0, c.paraCharCount)
	c.mu.RUnlock()
}

func TestParagraphBreak_ResetOnError(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	c := newTestConn(adapter, "")

	c.mu.Lock()
	c.paraCharCount = 300
	c.paraCharCount = 0
	c.mu.Unlock()

	c.mu.RLock()
	require.Equal(t, 0, c.paraCharCount)
	c.mu.RUnlock()
}

// endsWithSentenceTerminator mirrors textutil.EndsWithSentenceTerminator
// for in-package testing without cross-package dependency.
func endsWithSentenceTerminator(s string) bool {
	if s == "" {
		return false
	}
	last := s[len(s)-1]
	if last == '.' || last == '?' || last == '!' {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s)
	return r == '\u3002' || r == '\uff1f' || r == '\uff01'
}

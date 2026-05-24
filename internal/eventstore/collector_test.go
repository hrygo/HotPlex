package eventstore

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

func TestCollector_CaptureDeltaString(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	c.CaptureDeltaString("s1", 4, "Hello")
	c.CaptureDeltaString("s1", 5, " world")
	c.Capture("s1", 6, events.MessageEnd, nil, "outbound", SourceNormal)

	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	require.Len(t, page.Events, 1)
	require.Equal(t, int64(4), page.Events[0].Seq)
	require.Equal(t, string(events.Message), page.Events[0].Type)

	var data map[string]any
	require.NoError(t, json.Unmarshal(page.Events[0].Data, &data))
	require.Equal(t, "Hello world", data["content"])
	require.Equal(t, float64(2), data["merged_count"])
}

func TestCollector_CaptureDeltaStringSizeFlush(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	// First chunk: 3000 bytes < 4096 threshold
	c.CaptureDeltaString("s1", 1, strings.Repeat("a", 3000))
	// Second chunk: 3000+1100=4100 >= 4096 → immediate flush
	c.CaptureDeltaString("s1", 2, strings.Repeat("b", 1100))

	// Done triggers flush of any remaining (none in this case)
	c.Capture("s1", 3, events.Done, json.RawMessage(`{}`), "outbound", SourceNormal)
	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	// Size-flushed Message + Done
	require.Len(t, page.Events, 2)

	require.Equal(t, string(events.Message), page.Events[0].Type)
	require.Equal(t, int64(1), page.Events[0].Seq)

	var data map[string]any
	require.NoError(t, json.Unmarshal(page.Events[0].Data, &data))
	require.Equal(t, float64(2), data["merged_count"])
	content, _ := data["content"].(string)
	require.Len(t, content, 4100)
}

func TestCollector_MessageEndFlushWithoutStore(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	c.CaptureDeltaString("s1", 3, "Hello")
	c.CaptureDeltaString("s1", 4, " world")
	// MessageEnd triggers flush but is NOT stored
	c.Capture("s1", 5, events.MessageEnd, json.RawMessage(`{}`), "outbound", SourceNormal)
	c.Capture("s1", 6, events.Done, json.RawMessage(`{}`), "outbound", SourceNormal)

	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	// Message (flushed deltas) + Done. MessageEnd NOT stored.
	require.Len(t, page.Events, 2)
	require.Equal(t, string(events.Message), page.Events[0].Type)
	require.Equal(t, int64(3), page.Events[0].Seq)
	require.Equal(t, string(events.Done), page.Events[1].Type)
}

func TestCollector_ResetSession(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	c.CaptureDeltaString("s1", 1, "old content to discard")

	// Simulate retry
	c.ResetSession("s1")

	// New content after retry
	c.CaptureDeltaString("s1", 10, "new content")
	c.Capture("s1", 11, events.MessageEnd, nil, "outbound", SourceNormal)

	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	require.Len(t, page.Events, 1)

	var data map[string]any
	require.NoError(t, json.Unmarshal(page.Events[0].Data, &data))
	require.Equal(t, "new content", data["content"])
	require.Equal(t, float64(1), data["merged_count"])
}

func TestCollector_CreatedAtUsesFirstSeenAt(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	before := time.Now()
	c.CaptureDeltaString("s1", 1, "first")
	time.Sleep(50 * time.Millisecond)
	c.CaptureDeltaString("s1", 2, "second")
	// Flush well after first delta
	time.Sleep(50 * time.Millisecond)
	c.Capture("s1", 3, events.MessageEnd, nil, "outbound", SourceNormal)
	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	require.Len(t, page.Events, 1)

	createdAt := time.UnixMilli(page.Events[0].CreatedAt)
	// created_at should be close to before (first delta), not to flush time
	require.WithinDuration(t, before, createdAt, 50*time.Millisecond)
}

func TestCollector_ReplaySeqOrdering(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	// Full turn: Input → State → Delta×2 → MessageEnd → ToolCall → Delta×2 → MessageEnd → Done
	c.Capture("s1", 1, events.Input, json.RawMessage(`{"content":"do it"}`), "inbound", SourceNormal)
	c.Capture("s1", 2, events.State, json.RawMessage(`{"state":"running"}`), "outbound", SourceNormal)

	c.CaptureDeltaString("s1", 4, "Hello")
	c.CaptureDeltaString("s1", 5, " world")
	c.Capture("s1", 6, events.MessageEnd, nil, "outbound", SourceNormal)

	c.Capture("s1", 7, events.ToolCall, json.RawMessage(`{"name":"read"}`), "outbound", SourceNormal)

	c.CaptureDeltaString("s1", 9, "Result")
	c.CaptureDeltaString("s1", 10, " done")
	c.Capture("s1", 11, events.MessageEnd, nil, "outbound", SourceNormal)

	c.Capture("s1", 12, events.Done, json.RawMessage(`{}`), "outbound", SourceNormal)
	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)

	// Input(1) State(2) Message(4) ToolCall(7) Message(9) Done(12)
	require.Len(t, page.Events, 6)

	seqs := make([]int64, len(page.Events))
	types := make([]string, len(page.Events))
	for i, e := range page.Events {
		seqs[i] = e.Seq
		types[i] = e.Type
	}

	require.Equal(t, []int64{1, 2, 4, 7, 9, 12}, seqs)
	require.Equal(t, []string{
		string(events.Input), string(events.State), string(events.Message),
		string(events.ToolCall), string(events.Message), string(events.Done),
	}, types)

	// Input (question) always before Messages (answer)
	require.Less(t, seqs[0], seqs[2])
	require.Less(t, seqs[0], seqs[4])
}

func TestCollector_ConcurrentFlushNoLoss(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	const goroutines = 10
	const deltasPer = 100
	done := make(chan struct{})

	for g := range goroutines {
		go func(g int) {
			defer func() { done <- struct{}{} }()
			for d := range deltasPer {
				c.CaptureDeltaString("s1", int64(g*deltasPer+d+1), "x")
			}
		}(g)
	}
	for range goroutines {
		<-done
	}

	c.Capture("s1", int64(goroutines*deltasPer+1), events.Done, json.RawMessage(`{}`), "outbound", SourceNormal)
	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 1000)
	require.NoError(t, err)

	totalChars := 0
	for _, e := range page.Events {
		if e.Type != string(events.Message) {
			continue
		}
		var data map[string]any
		require.NoError(t, json.Unmarshal(e.Data, &data))
		content, _ := data["content"].(string)
		totalChars += len(content)
	}
	require.Equal(t, goroutines*deltasPer, totalChars)
}

func TestCollector_TimerFlush(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timer test in short mode")
	}

	store := newTestStore(t)
	c := NewCollector(store, slog.Default())
	defer func() { _ = c.Close() }()

	// Accumulate small content (< 4096) so size trigger won't fire
	c.CaptureDeltaString("s1", 1, "chunk1")
	c.CaptureDeltaString("s1", 2, "chunk2")

	// Wait for timer trigger (deltaFlushInterval = 2s + ticker margin)
	time.Sleep(deltaFlushInterval + 200*time.Millisecond)

	// Verify Message was written by timer flush (without Close)
	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	require.Len(t, page.Events, 1)
	require.Equal(t, string(events.Message), page.Events[0].Type)

	var data map[string]any
	require.NoError(t, json.Unmarshal(page.Events[0].Data, &data))
	require.Equal(t, "chunk1chunk2", data["content"])
}

func TestCollector_ResetSessionEmptyFlush(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	// No deltas accumulated, reset should be no-op
	c.ResetSession("s1")
	c.Capture("s1", 1, events.Done, json.RawMessage(`{}`), "outbound", SourceNormal)
	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	// Only Done, no Message
	require.Len(t, page.Events, 1)
	require.Equal(t, string(events.Done), page.Events[0].Type)
}

func TestCollector_DroppedEvents(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	// Fill the channel buffer to capacity (writer goroutine will drain these
	// into batches, so we need to backpressure the writer).
	// Use a blocking BeginTx to stall the writer so the channel stays full.
	blockTx := make(chan struct{})
	stallStore := &stallingEventStore{EventStore: store, block: blockTx}
	c.store = stallStore

	// Send enough events to saturate the channel beyond its capacity.
	// The writer goroutine may consume some before blocking, so send 2× capacity.
	const totalEvents = collectorChanCap * 2
	for i := range totalEvents {
		c.Capture("s1", int64(i), events.Done, json.RawMessage(`{}`), "outbound", SourceNormal)
	}

	// Unblock the writer and close cleanly.
	close(blockTx)
	require.NoError(t, c.Close())

	require.Greater(t, c.DroppedEvents(), int64(0), "expected some events to be dropped")
}

// stallingEventStore wraps an EventStore and blocks BeginTx until block chan is closed.
type stallingEventStore struct {
	EventStore
	block chan struct{}
}

func (s *stallingEventStore) BeginTx(ctx context.Context) (EventTx, error) {
	select {
	case <-s.block:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return s.EventStore.BeginTx(ctx)
}

func TestCollector_CaptureReasoningString(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	c.CaptureReasoningString("s1", 1, "Let me think")
	c.CaptureReasoningString("s1", 2, " about this")
	c.Capture("s1", 3, events.Done, json.RawMessage(`{}`), "outbound", SourceNormal)

	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	require.Len(t, page.Events, 2) // Reasoning + Done

	require.Equal(t, string(events.Reasoning), page.Events[0].Type)
	require.Equal(t, int64(1), page.Events[0].Seq)

	var data map[string]any
	require.NoError(t, json.Unmarshal(page.Events[0].Data, &data))
	require.Equal(t, "Let me think about this", data["content"])
	require.Equal(t, float64(2), data["merged_count"])
}

func TestCollector_ReasoningSizeFlush(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	c.CaptureReasoningString("s1", 1, strings.Repeat("x", 3000))
	c.CaptureReasoningString("s1", 2, strings.Repeat("y", 1100))

	c.Capture("s1", 3, events.Done, json.RawMessage(`{}`), "outbound", SourceNormal)
	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	require.Len(t, page.Events, 2)

	require.Equal(t, string(events.Reasoning), page.Events[0].Type)

	var data map[string]any
	require.NoError(t, json.Unmarshal(page.Events[0].Data, &data))
	require.Equal(t, float64(2), data["merged_count"])
	content, _ := data["content"].(string)
	require.Len(t, content, 4100)
}

func TestCollector_ReasoningDeltaInterleave(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	// Reasoning first, then delta — both should flush at boundaries
	c.CaptureReasoningString("s1", 1, "thinking...")
	c.CaptureDeltaString("s1", 2, "Hello")                               // flushes reasoning
	c.Capture("s1", 3, events.MessageEnd, nil, "outbound", SourceNormal) // flushes delta

	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	require.Len(t, page.Events, 2) // Reasoning + Message

	require.Equal(t, string(events.Reasoning), page.Events[0].Type)
	require.Equal(t, string(events.Message), page.Events[1].Type)

	var rData map[string]any
	require.NoError(t, json.Unmarshal(page.Events[0].Data, &rData))
	require.Equal(t, "thinking...", rData["content"])
}

func TestCollector_ReasoningTimerFlush(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timer test in short mode")
	}

	store := newTestStore(t)
	c := NewCollector(store, slog.Default())
	defer func() { _ = c.Close() }()

	c.CaptureReasoningString("s1", 1, "think1")
	c.CaptureReasoningString("s1", 2, "think2")

	time.Sleep(deltaFlushInterval + 200*time.Millisecond)

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	require.Len(t, page.Events, 1)
	require.Equal(t, string(events.Reasoning), page.Events[0].Type)

	var data map[string]any
	require.NoError(t, json.Unmarshal(page.Events[0].Data, &data))
	require.Equal(t, "think1think2", data["content"])
}

func TestCollector_CaptureReasoningViaCapture(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	c.Capture("s1", 1, events.Reasoning, json.RawMessage(`{"content":"think"}`), "outbound", SourceNormal)
	c.Capture("s1", 2, events.Reasoning, json.RawMessage(`{"content":" more"}`), "outbound", SourceNormal)
	c.Capture("s1", 3, events.Done, json.RawMessage(`{}`), "outbound", SourceNormal)

	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	require.Len(t, page.Events, 2) // Reasoning + Done

	require.Equal(t, string(events.Reasoning), page.Events[0].Type)
	require.Equal(t, int64(1), page.Events[0].Seq)

	var data map[string]any
	require.NoError(t, json.Unmarshal(page.Events[0].Data, &data))
	require.Equal(t, "think more", data["content"])
	require.Equal(t, float64(2), data["merged_count"])
}

func TestCollector_CaptureReasoningStringEmptyContent(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	c.CaptureReasoningString("s1", 1, "")
	c.Capture("s1", 2, events.Done, json.RawMessage(`{}`), "outbound", SourceNormal)

	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	// Only Done — empty content accumulator flushed but has count=1 with empty string
	require.Len(t, page.Events, 2)
	require.Equal(t, string(events.Reasoning), page.Events[0].Type)
	require.Equal(t, string(events.Done), page.Events[1].Type)

	var data map[string]any
	require.NoError(t, json.Unmarshal(page.Events[0].Data, &data))
	require.Equal(t, "", data["content"])
}

func TestCollector_Flush(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())
	defer func() { _ = c.Close() }()

	// Send events without waiting for timer flush.
	c.Capture("s1", 1, events.Input, json.RawMessage(`{"content":"hello"}`), "inbound", SourceNormal)
	c.Capture("s1", 2, events.Done, json.RawMessage(`{}`), "outbound", SourceNormal)

	// Flush must make events queryable immediately (no 1s timer wait).
	c.Flush()

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	require.Len(t, page.Events, 2)
	require.Equal(t, string(events.Input), page.Events[0].Type)
	require.Equal(t, string(events.Done), page.Events[1].Type)
}

func TestCollector_FlushAfterClose(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	require.NoError(t, c.Close())

	// Flush on a closed collector must return immediately without panic.
	c.Flush()
}

func TestCollector_FlushConcurrent(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())
	defer func() { _ = c.Close() }()

	const flushers = 5
	done := make(chan struct{})
	for i := range flushers {
		go func() {
			defer func() { done <- struct{}{} }()
			c.Capture("s1", int64(i+1), events.Done, json.RawMessage(`{}`), "outbound", SourceNormal)
			c.Flush()
		}()
	}
	for range flushers {
		<-done
	}

	// All events must be persisted.
	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	require.Len(t, page.Events, flushers)
}

func TestCollector_FlushTurn(t *testing.T) {
	store := newTestStoreWithTurnsTable(t)
	c := NewCollector(store, slog.Default())
	defer func() { _ = c.Close() }()

	// Capture a turn (the exact pattern used by cron delivery).
	c.CaptureTurn(&TurnWriteRequest{
		SessionID:  "s1",
		Generation: 1,
		TurnNum:    1,
		Role:       "assistant",
		Content:    "cron result",
		Source:     SourceNormal,
		CreatedAt:  time.Now().UnixMilli(),
	})

	c.Flush()

	turns, err := store.QueryTurns(context.Background(), "s1", 1, 0)
	require.NoError(t, err)
	require.Len(t, turns, 1)
	require.Equal(t, "cron result", turns[0].Content)
}

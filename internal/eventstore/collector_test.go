package eventstore

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

// panicFirstTxStore wraps an EventStore and panics on the first BeginTx call,
// to exercise runWriter's panic-recovery restart (issue #879 problem 1).
type panicFirstTxStore struct {
	EventStore
	failed atomic.Bool
}

func (s *panicFirstTxStore) BeginTx(ctx context.Context) (EventTx, error) {
	if !s.failed.Swap(true) {
		panic("simulated runWriter panic during flushBatch")
	}
	return s.EventStore.BeginTx(ctx)
}

func TestCollector_RunWriterPanicRecovery(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ps := &panicFirstTxStore{EventStore: store}
	// Short flush interval so the first batch flushes (and panics) quickly.
	c := NewCollectorWithIntervals(ps, slog.Default(), 50*time.Millisecond, 3*time.Second)
	t.Cleanup(func() { _ = c.Close() })

	// seq 1 lands in a batch; the next ticker flush calls BeginTx → panic.
	c.Capture("s1", 1, events.Message, json.RawMessage(`{}`), "outbound", SourceNormal)

	// Wait until the panic has fired (BeginTx was reached → failed flipped).
	require.Eventually(t, func() bool { return ps.failed.Load() },
		2*time.Second, 20*time.Millisecond, "flush never ran (BeginTx not called)")

	// After the goroutine restarts, a NEW event must still persist — proving the
	// writer survived instead of permanently dying. (seq 1 was in the panicked
	// batch and is lost, the pre-existing behavior on any flush error.)
	c.Capture("s1", 2, events.Done, json.RawMessage(`{}`), "outbound", SourceNormal)
	require.Eventually(t, func() bool {
		page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
		if err != nil || len(page.Events) == 0 {
			return false
		}
		for _, e := range page.Events {
			if e.Seq == 2 {
				return true
			}
		}
		return false
	}, 3*time.Second, 50*time.Millisecond, "runWriter did not recover from panic")
}

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
	time.Sleep(10 * time.Millisecond)
	c.CaptureDeltaString("s1", 2, "second")
	// Flush well after first delta
	time.Sleep(10 * time.Millisecond)
	c.Capture("s1", 3, events.MessageEnd, nil, "outbound", SourceNormal)
	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	require.Len(t, page.Events, 1)

	createdAt := time.UnixMilli(page.Events[0].CreatedAt)
	// created_at should be close to before (first delta), not to flush time
	require.WithinDuration(t, before, createdAt, 30*time.Millisecond)
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
	c := NewCollectorWithIntervals(store, slog.Default(), 50*time.Millisecond, 100*time.Millisecond)
	defer func() { _ = c.Close() }()

	// Accumulate small content (< 4096) so size trigger won't fire
	c.CaptureDeltaString("s1", 1, "chunk1")
	c.CaptureDeltaString("s1", 2, "chunk2")

	// Wait for timer trigger (deltaFlush + ticker margin)
	require.Eventually(t, func() bool {
		page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
		if err != nil || len(page.Events) != 1 {
			return false
		}
		if page.Events[0].Type != string(events.Message) {
			return false
		}
		var data map[string]any
		if err := json.Unmarshal(page.Events[0].Data, &data); err != nil {
			return false
		}
		return data["content"] == "chunk1chunk2"
	}, 500*time.Millisecond, 50*time.Millisecond, "expected flushed message event")
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

func TestCollector_SizeFlushPreservesReasoningMessageOrder(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	c.CaptureReasoningString("s1", 1, "thinking")
	c.CaptureDeltaString("s1", 2, strings.Repeat("m", deltaFlushSize))
	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	require.Len(t, page.Events, 2)
	require.Equal(t, []string{string(events.Reasoning), string(events.Message)},
		[]string{page.Events[0].Type, page.Events[1].Type})
	require.Less(t, page.Events[0].ID, page.Events[1].ID)
}

func TestCollector_TimerFlushOrdersCoexistingAccumulators(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	c := NewCollectorWithIntervals(store, slog.Default(), 10*time.Millisecond, 10*time.Millisecond)

	// Seed both maps to exercise the timer's defensive ordering path. Normal
	// capture cross-flushes on type switches, but both maps can coexist across
	// code upgrades or a capture racing a timer boundary.
	rAcc := newDeltaAccumulator(events.Reasoning)
	rAcc.appendRaw(1, "thinking")
	dAcc := newDeltaAccumulator(events.Message)
	dAcc.appendRaw(2, "answer")
	rAcc.firstSeenAt = time.Now().Add(-time.Second)
	dAcc.firstSeenAt = time.Now().Add(-time.Second)
	c.accumMu.Lock()
	c.reasoningAccum["s1"] = rAcc
	c.accum["s1"] = dAcc
	c.accumMu.Unlock()

	require.Eventually(t, func() bool {
		page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
		return err == nil && len(page.Events) == 2
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	require.Equal(t, []string{string(events.Reasoning), string(events.Message)},
		[]string{page.Events[0].Type, page.Events[1].Type})
}

func TestCollector_BatchFailureIsolatesBadEvent(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	_, err := store.db.Exec(`CREATE UNIQUE INDEX events_session_seq_test
		ON events(session_id, seq)`)
	require.NoError(t, err)
	require.NoError(t, store.Append(context.Background(), &StoredEvent{
		SessionID: "s1", Seq: 1, Type: string(events.Done), Data: raw(`{}`),
		Direction: "outbound", Source: SourceNormal, CreatedAt: time.Now().UnixMilli(),
	}))

	c := NewCollector(store, slog.Default())
	c.Capture("s1", 1, events.Done, raw(`{}`), "outbound", SourceNormal)
	c.Capture("s1", 2, events.Done, raw(`{}`), "outbound", SourceNormal)
	require.NoError(t, c.Flush())
	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	require.Len(t, page.Events, 2)
	require.Equal(t, []int64{1, 2}, []int64{page.Events[0].Seq, page.Events[1].Seq})
}

func TestCollector_ReasoningTimerFlush(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timer test in short mode")
	}

	store := newTestStore(t)
	c := NewCollectorWithIntervals(store, slog.Default(), 50*time.Millisecond, 100*time.Millisecond)
	defer func() { _ = c.Close() }()

	c.CaptureReasoningString("s1", 1, "think1")
	c.CaptureReasoningString("s1", 2, "think2")

	require.Eventually(t, func() bool {
		page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
		if err != nil || len(page.Events) != 1 {
			return false
		}
		if page.Events[0].Type != string(events.Reasoning) {
			return false
		}
		var data map[string]any
		if err := json.Unmarshal(page.Events[0].Data, &data); err != nil {
			return false
		}
		return data["content"] == "think1think2"
	}, 500*time.Millisecond, 50*time.Millisecond, "expected flushed reasoning event")
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

func TestCollector_FlushSessionFlushesPendingDelta(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())
	t.Cleanup(func() { _ = c.Close() })

	c.CaptureDeltaString("s1", 1, "pending")
	require.NoError(t, c.FlushSession("s1"))

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 10)
	require.NoError(t, err)
	require.Len(t, page.Events, 1)
	require.Equal(t, int64(1), page.Events[0].Seq)
}

func TestCollector_FlushSessionAndThenSerializesProducer(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())
	t.Cleanup(func() { _ = c.Close() })

	producerDone := make(chan struct{})
	require.NoError(t, c.FlushSessionAndThen("s1", func() {
		go func() {
			c.CaptureDeltaString("s1", 2, "after-release")
			close(producerDone)
		}()
		select {
		case <-producerDone:
			t.Error("producer entered while session drain callback held the lock")
		default:
		}
	}))
	require.Eventually(t, func() bool {
		select {
		case <-producerDone:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
}

func TestCollector_CaptureWithSeqSerializesAllocationAndRelease(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())
	t.Cleanup(func() { _ = c.Close() })

	allocated := make(chan struct{})
	allowCapture := make(chan struct{})
	producerDone := make(chan struct{})
	go func() {
		c.CaptureWithSeq(
			"s1",
			func() int64 {
				close(allocated)
				<-allowCapture
				return 7
			},
			events.Done,
			json.RawMessage(`{"success":true}`),
			"outbound",
			SourceNormal,
			nil,
		)
		close(producerDone)
	}()
	<-allocated

	released := make(chan struct{})
	flushDone := make(chan error, 1)
	go func() {
		flushDone <- c.FlushSessionAndThen("s1", func() { close(released) })
	}()
	select {
	case <-released:
		t.Fatal("session released while a producer held an allocated seq")
	default:
	}

	close(allowCapture)
	require.Eventually(t, func() bool {
		select {
		case <-producerDone:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, <-flushDone)
	select {
	case <-released:
	default:
		t.Fatal("release callback did not run after the producer drained")
	}

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 10)
	require.NoError(t, err)
	require.Len(t, page.Events, 1)
	require.Equal(t, int64(7), page.Events[0].Seq)
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

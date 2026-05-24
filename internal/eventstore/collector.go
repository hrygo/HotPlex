package eventstore

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hrygo/hotplex/pkg/events"
)

const (
	collectorChanCap       = 2048
	collectorBatchMax      = 100
	collectorFlushInterval = 1 * time.Second

	deltaFlushSize     = 4096 // bytes — flush accumulator when content exceeds this
	deltaFlushInterval = 3 * time.Second
)

// StorableTypes is the set of AEP event types eligible for event storage replay.
var StorableTypes = map[events.Kind]bool{
	events.Init:                true,
	events.Error:               true,
	events.State:               true,
	events.Input:               true,
	events.Done:                true,
	events.Message:             true,
	events.ToolCall:            true,
	events.ToolResult:          true,
	events.Reasoning:           true,
	events.Step:                true,
	events.PermissionRequest:   true,
	events.PermissionResponse:  true,
	events.QuestionRequest:     true,
	events.QuestionResponse:    true,
	events.ElicitationRequest:  true,
	events.ElicitationResponse: true,
	events.ContextUsage:        true,
	events.Control:             true,
}

// IsStorable returns true if the event type should be persisted for replay.
func IsStorable(eventType events.Kind) bool {
	return StorableTypes[eventType]
}

// captureRequest is a pending write for the background batch writer.
// event, turn, and flush are mutually exclusive: exactly one is set.
type captureRequest struct {
	event *StoredEvent
	turn  *TurnWriteRequest
	flush chan struct{} // non-nil: flush marker; closed by runWriter after batch commit
}

func (r *captureRequest) sessionID() string {
	if r.event != nil {
		return r.event.SessionID
	}
	if r.turn != nil {
		return r.turn.SessionID
	}
	return ""
}

func kindOf(req *captureRequest) string {
	if req.turn != nil {
		return "turn"
	}
	if req.flush != nil {
		return "flush"
	}
	return "event"
}

// Collector captures AEP events, merges message.delta streams, and writes
// them asynchronously to the underlying EventStore.
//
// Delta accumulation uses three flush triggers:
//   - Size: content exceeds deltaFlushSize (hot path, synchronous)
//   - Timer: accumulator age exceeds deltaFlushInterval (runWriter ticker)
//   - Event: MessageEnd or next storable event (hot path, synchronous)
type Collector struct {
	store    EventStore
	captureC chan *captureRequest
	closeC   chan struct{}
	closeWg  sync.WaitGroup
	log      *slog.Logger

	accumMu        sync.Mutex
	accum          map[string]*deltaAccumulator // sessionID → active delta accumulator
	reasoningAccum map[string]*deltaAccumulator // sessionID → active reasoning accumulator
	dropped        atomic.Int64
}

// NewCollector creates a Collector that writes events to store.
func NewCollector(store EventStore, log *slog.Logger) *Collector {
	c := &Collector{
		store:          store,
		captureC:       make(chan *captureRequest, collectorChanCap),
		closeC:         make(chan struct{}),
		log:            log.With("component", "eventstore-collector"),
		accum:          make(map[string]*deltaAccumulator),
		reasoningAccum: make(map[string]*deltaAccumulator),
	}
	c.closeWg.Add(1)
	go c.runWriter()
	return c
}

// Capture sends an event to the collector for async persistence.
// MessageDelta and Reasoning events are accumulated in-memory and merged
// on flush trigger (MessageEnd, next storable event, size/timer threshold).
func (c *Collector) Capture(sessionID string, seq int64, eventType events.Kind, data json.RawMessage, direction, source string) {
	if eventType == events.MessageDelta {
		c.flushAndAccumulate(sessionID, seq, false, data)
		return
	}

	if eventType == events.Reasoning {
		c.flushAndAccumulate(sessionID, seq, true, data)
		return
	}

	// MessageEnd triggers flush of both accumulators but is not stored itself.
	if eventType == events.MessageEnd {
		c.flushBoth(sessionID)
		return
	}

	if !IsStorable(eventType) {
		return
	}

	c.flushBoth(sessionID)

	req := &captureRequest{event: &StoredEvent{
		SessionID: sessionID,
		Seq:       seq,
		Type:      string(eventType),
		Data:      data,
		Direction: direction,
		Source:    source,
		CreatedAt: time.Now().UnixMilli(),
	}}
	c.send(req)
}

// flushAndAccumulate holds accumMu once to cross-flush the other accumulator and
// accumulate the incoming event. isReasoning selects which map to use.
func (c *Collector) flushAndAccumulate(sessionID string, seq int64, isReasoning bool, data json.RawMessage) {
	c.accumMu.Lock()

	// Cross-flush the other accumulator under the same lock.
	var flushed *deltaAccumulator
	if isReasoning {
		flushed = c.accum[sessionID]
		delete(c.accum, sessionID)
	} else {
		flushed = c.reasoningAccum[sessionID]
		delete(c.reasoningAccum, sessionID)
	}

	// Accumulate into target map.
	var acc *deltaAccumulator
	if isReasoning {
		acc = c.getOrCreateReasoningAccum(sessionID)
	} else {
		acc = c.getOrCreateAccum(sessionID)
	}
	if acc != nil {
		acc.append(seq, data)
	}
	c.accumMu.Unlock()

	if flushed != nil && flushed.count > 0 {
		c.send(flushed.toRequest(sessionID))
	}
}

// flushBoth flushes both delta and reasoning accumulators under a single lock.
func (c *Collector) flushBoth(sessionID string) {
	c.accumMu.Lock()
	dAcc := c.accum[sessionID]
	delete(c.accum, sessionID)
	rAcc := c.reasoningAccum[sessionID]
	delete(c.reasoningAccum, sessionID)
	c.accumMu.Unlock()

	if dAcc != nil && dAcc.count > 0 {
		c.send(dAcc.toRequest(sessionID))
	}
	if rAcc != nil && rAcc.count > 0 {
		c.send(rAcc.toRequest(sessionID))
	}
}

// getOrCreateAccum returns the delta accumulator for sessionID, creating one if needed.
// Caller must hold c.accumMu. Returns nil if the collector is closed.
func (c *Collector) getOrCreateAccum(sessionID string) *deltaAccumulator {
	if c.accum == nil {
		return nil
	}
	acc := c.accum[sessionID]
	if acc == nil {
		acc = newDeltaAccumulator(events.Message)
		c.accum[sessionID] = acc
	}
	return acc
}

// getOrCreateReasoningAccum returns the reasoning accumulator for sessionID.
// Caller must hold c.accumMu. Returns nil if the collector is closed.
func (c *Collector) getOrCreateReasoningAccum(sessionID string) *deltaAccumulator {
	if c.reasoningAccum == nil {
		return nil
	}
	acc := c.reasoningAccum[sessionID]
	if acc == nil {
		acc = newDeltaAccumulator(events.Reasoning)
		c.reasoningAccum[sessionID] = acc
	}
	return acc
}

// CaptureReasoningString accumulates a reasoning content string directly,
// skipping the json.Marshal/Unmarshal round-trip of Capture.
// Flushes immediately when accumulated content exceeds deltaFlushSize.
func (c *Collector) CaptureReasoningString(sessionID string, seq int64, content string) {
	c.accumMu.Lock()
	acc := c.getOrCreateReasoningAccum(sessionID)
	if acc == nil {
		c.accumMu.Unlock()
		return
	}
	acc.appendRaw(seq, content)

	if acc.content.Len() >= deltaFlushSize {
		delete(c.reasoningAccum, sessionID)
		c.accumMu.Unlock()
		c.send(acc.toRequest(sessionID))
		return
	}
	c.accumMu.Unlock()
}

func (c *Collector) send(req *captureRequest) {
	select {
	case c.captureC <- req:
	default:
		c.dropped.Add(1)
		c.log.Warn("eventstore: capture channel full, dropping",
			"session_id", req.sessionID(),
			"kind", kindOf(req),
		)
	}
}

// CaptureTurn sends a turn write request to the collector for async persistence.
func (c *Collector) CaptureTurn(turn *TurnWriteRequest) {
	if turn == nil {
		return
	}
	c.send(&captureRequest{turn: turn})
}

// Flush blocks until all pending writes in the capture channel have been
// committed to the underlying store. Safe for concurrent use.
// Returns immediately if the collector is closed.
func (c *Collector) Flush() {
	// Fast path: return immediately if already closed.
	select {
	case <-c.closeC:
		return
	default:
	}

	done := make(chan struct{})
	select {
	case c.captureC <- &captureRequest{flush: done}:
	case <-c.closeC:
		return
	case <-time.After(5 * time.Second):
		c.log.Warn("eventstore: flush send timeout")
		return
	}
	select {
	case <-done:
	case <-c.closeC:
	case <-time.After(5 * time.Second):
		c.log.Warn("eventstore: flush wait timeout")
	}
}

// Close drains the capture channel and flushes remaining events.
func (c *Collector) Close() error {
	// Swap accumulator maps under lock, flush outside to avoid deadlock.
	c.accumMu.Lock()
	pending := c.accum
	pendingReasoning := c.reasoningAccum
	c.accum = nil
	c.reasoningAccum = nil
	c.accumMu.Unlock()

	for sid, acc := range pending {
		if acc.count > 0 {
			c.send(acc.toRequest(sid))
		}
	}
	for sid, acc := range pendingReasoning {
		if acc.count > 0 {
			c.send(acc.toRequest(sid))
		}
	}

	close(c.closeC)
	c.closeWg.Wait()
	return nil
}

// DroppedEvents returns the total number of events dropped due to a full channel.
func (c *Collector) DroppedEvents() int64 {
	return c.dropped.Load()
}

func (c *Collector) runWriter() {
	defer c.closeWg.Done()

	ticker := time.NewTicker(collectorFlushInterval)
	defer ticker.Stop()

	var batch []*captureRequest
	flush := func() {
		if len(batch) == 0 {
			return
		}
		c.flushBatch(batch)
		batch = batch[:0]
	}

	for {
		select {
		case <-c.closeC:
			for {
				select {
				case req := <-c.captureC:
					if req.flush != nil {
						flush()
						close(req.flush)
						continue
					}
					batch = append(batch, req)
				default:
					flush()
					if d := c.dropped.Load(); d > 0 {
						c.log.Warn("eventstore: events dropped during lifetime", "count", d)
					}
					return
				}
			}
		case req := <-c.captureC:
			if req.flush != nil {
				flush()
				close(req.flush)
				continue
			}
			batch = append(batch, req)
			if len(batch) >= collectorBatchMax {
				flush()
			}
		case <-ticker.C:
			c.flushTimedOutAccumulators(&batch)
			flush()
		}
	}
}

// flushTimedOutAccumulators scans all accumulators and flushes those whose
// age exceeds deltaFlushInterval. Bypasses captureC to avoid deadlock in
// runWriter (which both reads from and would write to captureC).
func (c *Collector) flushTimedOutAccumulators(batch *[]*captureRequest) {
	now := time.Now()
	c.accumMu.Lock()
	for sid, acc := range c.accum {
		if now.Sub(acc.firstSeenAt) >= deltaFlushInterval {
			delete(c.accum, sid)
			*batch = append(*batch, acc.toRequest(sid))
		}
	}
	for sid, acc := range c.reasoningAccum {
		if now.Sub(acc.firstSeenAt) >= deltaFlushInterval {
			delete(c.reasoningAccum, sid)
			*batch = append(*batch, acc.toRequest(sid))
		}
	}
	c.accumMu.Unlock()
}

func (c *Collector) flushBatch(batch []*captureRequest) {
	if len(batch) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := c.store.BeginTx(ctx)
	if err != nil {
		c.log.Error("eventstore: batch tx begin", "err", err)
		return
	}

	done := false
	defer func() {
		if !done {
			_ = tx.Rollback()
		}
	}()

	for _, req := range batch {
		var err error
		switch {
		case req.turn != nil:
			err = tx.AppendTurn(ctx, req.turn)
		case req.event != nil:
			err = tx.Append(ctx, req.event)
		}
		if err != nil {
			c.log.Warn("eventstore: batch append failed",
				"session_id", req.sessionID(),
				"kind", kindOf(req),
				"err", err,
			)
			return // SQLite: failed statement aborts tx, remaining appends would also fail.
		}
	}

	if err := tx.Commit(); err != nil {
		c.log.Error("eventstore: batch commit", "err", err)
	}
	done = true
}

// CaptureDeltaString accumulates a message.delta content string directly,
// skipping the json.Marshal/Unmarshal round-trip of Capture.
// Flushes immediately when accumulated content exceeds deltaFlushSize.
func (c *Collector) CaptureDeltaString(sessionID string, seq int64, content string) {
	c.accumMu.Lock()
	acc := c.getOrCreateAccum(sessionID)
	if acc == nil {
		c.accumMu.Unlock()
		return
	}
	acc.appendRaw(seq, content)

	if acc.content.Len() >= deltaFlushSize {
		delete(c.accum, sessionID)
		c.accumMu.Unlock()
		c.send(acc.toRequest(sessionID))
		return
	}
	c.accumMu.Unlock()
}

// ResetSession discards any accumulated delta and reasoning content for the given session.
func (c *Collector) ResetSession(sessionID string) {
	c.accumMu.Lock()
	delete(c.accum, sessionID)
	delete(c.reasoningAccum, sessionID)
	c.accumMu.Unlock()
}

// deltaAccumulator merges streaming content (message.delta or reasoning) in-memory.
type deltaAccumulator struct {
	eventType   events.Kind
	content     strings.Builder
	seq         int64
	firstSeq    int64
	lastSeq     int64
	count       int
	firstSeenAt time.Time
}

func newDeltaAccumulator(eventType events.Kind) *deltaAccumulator {
	return &deltaAccumulator{eventType: eventType}
}

func (a *deltaAccumulator) append(seq int64, data json.RawMessage) {
	var delta struct {
		Content string `json:"content"`
	}
	_ = json.Unmarshal(data, &delta)

	a.appendRaw(seq, delta.Content)
}

func (a *deltaAccumulator) appendRaw(seq int64, content string) {
	a.content.WriteString(content)
	a.lastSeq = seq
	if a.count == 0 {
		a.firstSeq = seq
		a.seq = seq
		a.firstSeenAt = time.Now()
	}
	a.count++
}

func (a *deltaAccumulator) toRequest(sessionID string) *captureRequest {
	mergedData, _ := json.Marshal(map[string]any{
		"content":      a.content.String(),
		"merged_count": a.count,
		"seq_range":    []int64{a.firstSeq, a.lastSeq},
	})
	return &captureRequest{event: &StoredEvent{
		SessionID: sessionID,
		Seq:       a.seq,
		Type:      string(a.eventType),
		Data:      mergedData,
		Direction: "outbound",
		Source:    SourceNormal,
		CreatedAt: a.firstSeenAt.UnixMilli(),
	}}
}

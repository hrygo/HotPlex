package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/observability"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// pcEntry wraps a PlatformConn with an async writeLoop goroutine and delta
// coalescing. It satisfies the SessionWriter interface.
//
// WriteCtx sends envelopes to a buffered channel; a dedicated writeLoop
// goroutine reads from the channel, coalesces consecutive droppable events
// (message.delta / raw), and forwards merged or individual envelopes to the
// underlying PlatformConn. This decouples Hub.Run() from blocking platform
// HTTP API calls.
type pcEntry struct {
	pc      messaging.PlatformConn
	cfg     pcEntryConfig
	ch      chan platformWrite
	closeCh chan struct{} // signals Close() was called
	done    chan struct{}
	closed  atomic.Bool // fast-path check to avoid send-on-closed-channel
	closeMu sync.Once
	log     *slog.Logger
	ctx     context.Context
}

// platformWrite carries a queued platform envelope and, for terminal events,
// a one-shot completion channel. The channel is buffered so a caller that
// times out cannot strand the write loop while it reports its eventual result.
type platformWrite struct {
	env    *events.Envelope
	ctx    context.Context
	result chan<- error
}

type pcEntryConfig struct {
	WriteBuffer     int
	DropThreshold   int
	CoalesceIntvl   time.Duration
	CoalesceSize    int
	TerminalTimeout time.Duration
}

const defaultPlatformTerminalTimeout = 5 * time.Second

func defaultPCEntryConfig(cfg *config.Config) pcEntryConfig {
	c := pcEntryConfig{
		WriteBuffer:     cfg.Gateway.PlatformWriteBuffer,
		DropThreshold:   cfg.Gateway.PlatformDropThreshold,
		CoalesceIntvl:   cfg.Gateway.DeltaCoalesceInterval,
		CoalesceSize:    cfg.Gateway.DeltaCoalesceSize,
		TerminalTimeout: defaultPlatformTerminalTimeout,
	}
	if c.WriteBuffer <= 0 {
		c.WriteBuffer = 64
	}
	if c.DropThreshold <= 0 {
		c.DropThreshold = 56
	}
	if c.CoalesceIntvl <= 0 {
		c.CoalesceIntvl = 120 * time.Millisecond
	}
	if c.CoalesceSize <= 0 {
		c.CoalesceSize = 200
	}
	if c.TerminalTimeout <= 0 {
		c.TerminalTimeout = defaultPlatformTerminalTimeout
	}
	return c
}

func newPCEntry(ctx context.Context, pc messaging.PlatformConn, cfg pcEntryConfig, log *slog.Logger) *pcEntry {
	if cfg.TerminalTimeout <= 0 {
		cfg.TerminalTimeout = defaultPlatformTerminalTimeout
	}
	e := &pcEntry{
		pc:      pc,
		ch:      make(chan platformWrite, cfg.WriteBuffer),
		closeCh: make(chan struct{}),
		done:    make(chan struct{}),
		cfg:     cfg,
		log:     log,
		ctx:     ctx,
	}
	go e.writeLoop()
	return e
}

// RouteWrite writes an envelope through the Hub routing path.
// pcEntry already handles droppable semantics in WriteCtx, so this delegates directly.
func (e *pcEntry) RouteWrite(ctx context.Context, env *events.Envelope) error {
	observability.GatewayMessages().Add(ctx, 1, metric.WithAttributes(attribute.String("direction", "outgoing"), attribute.String("event_type", string(env.Event.Type))))
	return e.WriteCtx(ctx, env)
}

// RouteWriteData decodes pre-encoded bytes and delegates to RouteWrite.
// pcEntry internally works with envelopes (not raw bytes), so it cannot
// benefit from the pre-encoding optimization. This fallback ensures
// pcEntry satisfies the SessionWriter interface.
func (e *pcEntry) RouteWriteData(data []byte, eventType events.Kind) error {
	env, err := aep.DecodeLine(data)
	if err != nil {
		return err
	}
	return e.RouteWrite(context.Background(), env)
}

// PreferEnvelope returns true: pcEntry needs the original envelope to preserve
// json:"-" fields (e.g. OwnerID) that EncodeJSON omits from pre-encoded bytes.
func (e *pcEntry) PreferEnvelope() bool { return true }

// WriteCtx enqueues env and, for terminal events, waits up to the configured
// terminal timeout for the write loop to report the actual platform result.
func (e *pcEntry) WriteCtx(ctx context.Context, env *events.Envelope) (err error) {
	if !isTerminalPlatformEvent(env.Event.Type) {
		return e.EnqueueWrite(ctx, env, nil)
	}
	terminalCtx, cancel := context.WithTimeout(ctx, e.cfg.TerminalTimeout)
	defer cancel()
	ack := make(chan error, 1)
	if err := e.EnqueueWrite(terminalCtx, env, ack); err != nil {
		return err
	}
	select {
	case err := <-ack:
		return err
	case <-terminalCtx.Done():
		return fmt.Errorf("platform conn terminal write timeout: %w", terminalCtx.Err())
	case <-e.closeCh:
		return errors.New("platform conn closed")
	case <-e.done:
		return errors.New("platform conn closed")
	}
}

// EnqueueWrite admits env into the write queue and returns without waiting
// for the platform write result. For terminal events the write loop reports
// the real result on result (buffered, one-shot); for all other events result
// is ignored. EnqueueWrite never blocks on the write itself, so it is safe to
// call from the Hub's single router goroutine.
func (e *pcEntry) EnqueueWrite(ctx context.Context, env *events.Envelope, result chan<- error) (err error) {
	// Recover from send-on-closed-channel panic caused by the TOCTOU window
	// between closed.Load() and the channel send. The atomic guard narrows
	// this window to nanoseconds, but recover() eliminates it entirely.
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("platform conn closed")
		}
	}()

	// Fast-path: skip channel send if already closed.
	if e.closed.Load() {
		return errors.New("platform conn closed")
	}

	write := platformWrite{env: env, ctx: ctx}
	if isTerminalPlatformEvent(env.Event.Type) {
		terminalCtx, cancel := context.WithTimeout(ctx, e.cfg.TerminalTimeout)
		write.ctx = terminalCtx
		write.result = result
		ctx = terminalCtx
		if result != nil {
			// Budget guard: when the write loop cannot report within the
			// terminal budget (e.g. a platform write that ignores its
			// context), complete the caller here. cancel() is deferred to
			// the guard so terminalCtx stays live for the write loop until
			// the budget itself expires; the goroutine is bounded by
			// TerminalTimeout, so it cannot leak. A result already delivered
			// by the write loop is dropped by the buffered send.
			go func() {
				defer cancel()
				select {
				case <-terminalCtx.Done():
					select {
					case result <- fmt.Errorf("platform conn terminal write timeout: %w", terminalCtx.Err()):
					default:
					}
				case <-e.done:
				}
			}()
		} else {
			cancel()
		}
	}
	if isDroppable(env.Event.Type) {
		if len(e.ch) >= e.cfg.DropThreshold {
			observability.GatewayPlatformDropped().Add(ctx, 1, metric.WithAttributes(attribute.String("event_type", string(env.Event.Type))))
			return nil
		}
		select {
		case e.ch <- write:
			return nil
		default:
			observability.GatewayPlatformDropped().Add(ctx, 1, metric.WithAttributes(attribute.String("event_type", string(env.Event.Type))))
			return nil
		}
	}

	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	select {
	case e.ch <- write:
		return nil
	case <-writeCtx.Done():
		return fmt.Errorf("platform conn write timeout: buffer full")
	case <-e.closeCh:
		return errors.New("platform conn closed")
	case <-e.done:
		return errors.New("platform conn closed")
	}
}

// Close signals writeLoop to drain pending deltas and exit, waits for
// completion, then closes the underlying PlatformConn.
func (e *pcEntry) Close() error {
	var err error
	e.closeMu.Do(func() {
		e.closed.Store(true) // set before closing channel to prevent WriteCtx races
		close(e.closeCh)
		close(e.ch) // signal writeLoop to drain and exit
		<-e.done
		err = e.pc.Close()
	})
	return err
}

// writeLoop reads envelopes from the channel, coalesces consecutive droppable
// events into merged deltas, and forwards them to the underlying PlatformConn.
func (e *pcEntry) writeLoop() {
	defer close(e.done)
	defer func() {
		if r := recover(); r != nil {
			e.log.Error("pcEntry writeLoop panic", "panic", r, "stack", string(debug.Stack()))
		}
	}()

	var db strings.Builder
	var timer *time.Timer
	var timerCh <-chan time.Time
	var pendingSID string // tracks SessionID for pending coalesced deltas
	var runeCount int

	flush := func(sid string) {
		if db.Len() == 0 {
			return
		}
		merged := &events.Envelope{
			Version:   events.Version,
			ID:        aep.NewID(),
			SessionID: sid,
			Event: events.Event{
				Type: events.MessageDelta,
				Data: events.MessageDeltaData{
					Content: db.String(),
				},
			},
		}
		observability.GatewayDeltaFlush().Add(e.ctx, 1)
		db.Reset()
		runeCount = 0
		if timer != nil {
			timer.Stop()
			timerCh = nil
		}
		e.writeOne(platformWrite{env: merged, ctx: e.ctx})
	}

	for {
		select {
		case write, ok := <-e.ch:
			if !ok {
				flush(pendingSID)
				return
			}
			env := write.env

			if isDroppable(env.Event.Type) {
				content := extractDeltaContent(env)
				if db.Len() == 0 {
					pendingSID = env.SessionID
				}
				db.WriteString(content)
				runeCount += utf8.RuneCountInString(content)
				observability.GatewayDeltaCoalesced().Add(e.ctx, 1)

				if runeCount >= e.cfg.CoalesceSize {
					flush(pendingSID)
				} else if timer == nil {
					timer = time.NewTimer(e.cfg.CoalesceIntvl)
					timerCh = timer.C
				} else {
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					timer.Reset(e.cfg.CoalesceIntvl)
				}
			} else {
				flush(pendingSID)
				e.writeOne(write)
			}

		case <-timerCh:
			flush(pendingSID)
		}
	}
}

func (e *pcEntry) writeOne(write platformWrite) {
	ctx := write.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if !isTerminalPlatformEvent(write.env.Event.Type) {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	err := e.pc.WriteCtx(ctx, write.env)
	if write.result != nil {
		select {
		case write.result <- err:
		default:
		}
	}
	if err != nil {
		e.log.Warn("platform async write failed",
			"event_type", write.env.Event.Type,
			"session_id", write.env.SessionID,
			"err", err)
	}
}

func isTerminalPlatformEvent(kind events.Kind) bool {
	return kind == events.Done || kind == events.Error
}

func extractDeltaContent(env *events.Envelope) string {
	switch env.Event.Type {
	case events.MessageDelta:
		// Struct type: Clone() preserves the original typed struct.
		if d, ok := env.Event.Data.(events.MessageDeltaData); ok {
			return d.Content
		}
		// Map type: JSON unmarshal path (e.g., from older Clone or direct decode).
		if m, ok := env.Event.Data.(map[string]any); ok {
			if c, _ := m["content"].(string); c != "" {
				return c
			}
		}
	case events.Raw:
		if d, ok := env.Event.Data.(events.RawData); ok {
			if m, ok := d.Raw.(map[string]any); ok {
				if t, _ := m["text"].(string); t != "" {
					return t
				}
			}
		}
	}
	if s, ok := env.Event.Data.(string); ok {
		return s
	}
	return ""
}

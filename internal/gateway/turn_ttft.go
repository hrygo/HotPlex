package gateway

import (
	"context"
	"sync"
	"time"

	"github.com/hrygo/hotplex/internal/observability"
	"github.com/hrygo/hotplex/internal/worker"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// turnTTFTTracker correlates one accepted input with its first worker output.
// It is process-local by design: identifiers correlate events only and are
// never exported as metric attributes or persisted with input content.
type turnTTFTTracker struct {
	mu        sync.Mutex
	bySession map[string]*turnTTFTRecord
}

type turnTTFTRecord struct {
	executionID       string
	workerType        worker.WorkerType
	receivedAt        time.Time
	durableAcceptedAt time.Time
	workerAcceptedAt  time.Time
	firstOutputAt     time.Time
	firstOutputText   bool
	firstTextAt       time.Time
	ttftEmitted       bool
	firstTextEmitted  bool
}

type turnTTFTSample struct {
	workerType worker.WorkerType
	duration   time.Duration
}

func newTurnTTFTTracker() *turnTTFTTracker {
	return &turnTTFTTracker{bySession: make(map[string]*turnTTFTRecord)}
}

func (t *turnTTFTTracker) begin(sessionID, executionID string, workerType worker.WorkerType, receivedAt time.Time) {
	if sessionID == "" || executionID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.bySession[sessionID] = &turnTTFTRecord{
		executionID: executionID,
		workerType:  workerType,
		receivedAt:  receivedAt,
	}
}

func (t *turnTTFTTracker) durableAccepted(sessionID, executionID string, at time.Time) (turnTTFTSample, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	record := t.bySession[sessionID]
	if record == nil || record.executionID != executionID || !record.durableAcceptedAt.IsZero() {
		return turnTTFTSample{}, false
	}
	record.durableAcceptedAt = at
	return turnTTFTSample{workerType: record.workerType, duration: at.Sub(record.receivedAt)}, true
}

func (t *turnTTFTTracker) setWorkerType(sessionID, executionID string, workerType worker.WorkerType) (turnTTFTSample, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	record := t.bySession[sessionID]
	if record != nil && record.executionID == executionID {
		record.workerType = workerType
		if !record.durableAcceptedAt.IsZero() {
			return turnTTFTSample{workerType: workerType, duration: record.durableAcceptedAt.Sub(record.receivedAt)}, true
		}
	}
	return turnTTFTSample{}, false
}

func (t *turnTTFTTracker) workerAccepted(sessionID, executionID string, at time.Time) (turnTTFTSample, turnTTFTOutputs, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	record := t.bySession[sessionID]
	if record == nil || record.executionID != executionID || !record.workerAcceptedAt.IsZero() {
		return turnTTFTSample{}, turnTTFTOutputs{}, false
	}
	record.workerAcceptedAt = at
	start := record.durableAcceptedAt
	if start.IsZero() {
		start = record.receivedAt
	}
	return turnTTFTSample{workerType: record.workerType, duration: at.Sub(start)}, t.readyOutputs(record), true
}

type turnTTFTOutputs struct {
	ttft             *turnTTFTSample
	firstText        *turnTTFTSample
	firstOutputStage *turnTTFTSample
	firstOutputText  bool
}

func (t *turnTTFTTracker) readyOutputs(record *turnTTFTRecord) turnTTFTOutputs {
	if record.workerAcceptedAt.IsZero() {
		return turnTTFTOutputs{}
	}
	outputs := turnTTFTOutputs{}
	if !record.firstOutputAt.IsZero() && !record.ttftEmitted {
		ttft := turnTTFTSample{workerType: record.workerType, duration: record.firstOutputAt.Sub(record.receivedAt)}
		stage := turnTTFTSample{workerType: record.workerType, duration: record.firstOutputAt.Sub(record.workerAcceptedAt)}
		record.ttftEmitted = true
		outputs.ttft = &ttft
		outputs.firstOutputStage = &stage
		outputs.firstOutputText = record.firstOutputText
	}
	if !record.firstTextAt.IsZero() && !record.firstTextEmitted {
		firstText := turnTTFTSample{workerType: record.workerType, duration: record.firstTextAt.Sub(record.receivedAt)}
		record.firstTextEmitted = true
		outputs.firstText = &firstText
	}
	return outputs
}

func (t *turnTTFTTracker) firstOutput(sessionID string, text bool, at time.Time) turnTTFTOutputs {
	t.mu.Lock()
	defer t.mu.Unlock()
	record := t.bySession[sessionID]
	if record == nil {
		return turnTTFTOutputs{}
	}
	if record.firstOutputAt.IsZero() {
		record.firstOutputAt = at
		record.firstOutputText = text
	}
	if text && record.firstTextAt.IsZero() {
		record.firstTextAt = at
	}
	return t.readyOutputs(record)
}

func (t *turnTTFTTracker) finish(sessionID string) (worker.WorkerType, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	record := t.bySession[sessionID]
	if record == nil {
		return "", false
	}
	delete(t.bySession, sessionID)
	return record.workerType, record.firstOutputAt.IsZero()
}

func (b *Bridge) beginTurnTTFT(sessionID, executionID string, workerType worker.WorkerType, receivedAt time.Time) {
	if b.turnTTFT != nil {
		b.turnTTFT.begin(sessionID, executionID, workerType, receivedAt)
	}
}

func (b *Bridge) markTurnDurablyAccepted(sessionID, executionID string) {
	if b.turnTTFT == nil {
		return
	}
	_, _ = b.turnTTFT.durableAccepted(sessionID, executionID, time.Now())
}

func (b *Bridge) setTurnTTFTWorkerType(sessionID, executionID string, workerType worker.WorkerType) {
	if b.turnTTFT == nil {
		return
	}
	if sample, ok := b.turnTTFT.setWorkerType(sessionID, executionID, workerType); ok {
		observability.TurnStageDuration().Record(context.Background(), sample.duration.Seconds(),
			metric.WithAttributes(
				attribute.String("worker_type", string(sample.workerType)),
				attribute.String("stage", "admission"),
			))
	}
}

func (b *Bridge) markTurnWorkerAccepted(sessionID, executionID string) {
	if b.turnTTFT == nil {
		return
	}
	if sample, outputs, ok := b.turnTTFT.workerAccepted(sessionID, executionID, time.Now()); ok {
		observability.TurnStageDuration().Record(context.Background(), sample.duration.Seconds(),
			metric.WithAttributes(
				attribute.String("worker_type", string(sample.workerType)),
				attribute.String("stage", "dispatch"),
			))
		b.recordTurnTTFTOutputs(outputs)
	}
}

func (b *Bridge) recordTurnFirstOutput(sessionID string, text bool) {
	if b.turnTTFT == nil {
		return
	}
	b.recordTurnTTFTOutputs(b.turnTTFT.firstOutput(sessionID, text, time.Now()))
}

func (b *Bridge) recordTurnTTFTOutputs(outputs turnTTFTOutputs) {
	if outputs.ttft != nil {
		outputKind := "reasoning"
		if outputs.firstOutputText {
			outputKind = "text"
		}
		observability.TurnTTFT().Record(context.Background(), outputs.ttft.duration.Seconds(),
			metric.WithAttributes(
				attribute.String("worker_type", string(outputs.ttft.workerType)),
				attribute.String("first_output", outputKind),
			))
		observability.TurnStageDuration().Record(context.Background(), outputs.firstOutputStage.duration.Seconds(),
			metric.WithAttributes(
				attribute.String("worker_type", string(outputs.firstOutputStage.workerType)),
				attribute.String("stage", "first_output"),
			))
	}
	if outputs.firstText != nil {
		observability.TurnFirstTextLatency().Record(context.Background(), outputs.firstText.duration.Seconds(),
			metric.WithAttributes(attribute.String("worker_type", string(outputs.firstText.workerType))))
	}
}

func (b *Bridge) finishTurnTTFT(sessionID, terminalStatus string) {
	if b.turnTTFT == nil {
		return
	}
	workerType, withoutOutput := b.turnTTFT.finish(sessionID)
	if withoutOutput {
		observability.TurnWithoutOutput().Add(context.Background(), 1,
			metric.WithAttributes(
				attribute.String("worker_type", string(workerType)),
				attribute.String("terminal_status", terminalStatus),
			))
	}
}

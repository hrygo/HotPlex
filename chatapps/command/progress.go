package command

import (
	"fmt"
	"sync"
	"time"

	"github.com/hrygo/hotplex/event"
	"github.com/hrygo/hotplex/provider"
)

// ProgressEmitter sends progress events during command execution
type ProgressEmitter struct {
	command   string
	callback  event.Callback
	steps     []ProgressStep
	startTime time.Time
	mu        sync.Mutex
}

// NewProgressEmitter creates a new progress emitter
func NewProgressEmitter(command string, callback event.Callback, steps []ProgressStep) *ProgressEmitter {
	return &ProgressEmitter{
		command:   command,
		callback:  callback,
		steps:     steps,
		startTime: time.Now(),
	}
}

// Start sends initial progress with all steps in pending state
func (e *ProgressEmitter) Start(title string) error {
	return e.emitProgress(title)
}

// UpdateStep updates a specific step and emits progress
func (e *ProgressEmitter) UpdateStep(stepIndex int, status, message string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if stepIndex < 0 || stepIndex >= len(e.steps) {
		return fmt.Errorf("invalid step index: %d", stepIndex)
	}

	e.steps[stepIndex].Status = status
	if message != "" {
		e.steps[stepIndex].Message = message
	}

	return nil
}

// Running marks a step as running
func (e *ProgressEmitter) Running(stepIndex int) error {
	return e.UpdateStep(stepIndex, "running", "")
}

// Success marks a step as successful
func (e *ProgressEmitter) Success(stepIndex int, message string) error {
	return e.UpdateStep(stepIndex, "success", message)
}

// Error marks a step as failed
func (e *ProgressEmitter) Error(stepIndex int, message string) error {
	return e.UpdateStep(stepIndex, "error", message)
}

// Emit sends the current progress state
func (e *ProgressEmitter) Emit(title string) error {
	return e.emitProgress(title)
}

// Complete sends the final completion event
func (e *ProgressEmitter) Complete(message string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Mark all pending steps as success
	for i := range e.steps {
		if e.steps[i].Status == "pending" || e.steps[i].Status == "running" {
			e.steps[i].Status = "success"
		}
	}

	// Create event data with steps
	meta := &event.EventMeta{
		Status:          "completed",
		TotalDurationMs: time.Since(e.startTime).Milliseconds(),
		OutputSummary:   message,
	}

	return e.callback(string(provider.EventTypeCommandComplete),
		event.NewEventWithMeta(string(provider.EventTypeCommandComplete), message, meta))
}

// GetSteps returns current steps (for external use)
func (e *ProgressEmitter) GetSteps() []ProgressStep {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := make([]ProgressStep, len(e.steps))
	copy(result, e.steps)
	return result
}

func (e *ProgressEmitter) emitProgress(title string) error {
	e.mu.Lock()
	stepsCopy := make([]ProgressStep, len(e.steps))
	copy(stepsCopy, e.steps)
	e.mu.Unlock()

	// Calculate progress percentage
	completed := 0
	currentStep := 0
	for i, s := range stepsCopy {
		if s.Status == "success" {
			completed++
		}
		if s.Status == "running" {
			currentStep = i + 1
		}
	}
	progress := int32(0)
	if len(stepsCopy) > 0 {
		progress = int32(completed * 100 / len(stepsCopy))
	}

	meta := &event.EventMeta{
		Status:          "running",
		Progress:        progress,
		TotalSteps:      int32(len(stepsCopy)),
		CurrentStep:     int32(currentStep),
		TotalDurationMs: time.Since(e.startTime).Milliseconds(),
		OutputSummary:   title,
	}

	return e.callback(string(provider.EventTypeCommandProgress),
		event.NewEventWithMeta(string(provider.EventTypeCommandProgress), title, meta))
}

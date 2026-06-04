package cron

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/hrygo/hotplex/internal/observability"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/pkg/events"
)

// AttachedSessionRouter is the narrow interface for session callback execution.
// Implemented by an adapter in cmd/hotplex/ that bridges Bridge + SessionManager.
type AttachedSessionRouter interface {
	// GetSessionInfo returns session metadata for callback dispatch.
	GetSessionInfo(ctx context.Context, id string) (*session.SessionInfo, error)

	// ResumeAndInput resumes a dormant session and injects the callback prompt.
	ResumeAndInput(ctx context.Context, sessionID string, workDir string, prompt string, metadata map[string]any) error

	// InjectInput sends a prompt to an already-running session's worker.
	InjectInput(ctx context.Context, sessionID string, prompt string, metadata map[string]any) error
}

// AttachedSessionHandler dispatches callback prompts into existing sessions.
type AttachedSessionHandler struct {
	log    *slog.Logger
	router AttachedSessionRouter
}

// NewAttachedSessionHandler creates a new callback handler.
func NewAttachedSessionHandler(log *slog.Logger, router AttachedSessionRouter) *AttachedSessionHandler {
	return &AttachedSessionHandler{
		log:    log.With("component", "cron_attached"),
		router: router,
	}
}

// Execute dispatches a callback into the target session.
// Returns nil on successful injection (fire-and-forget), or an error if dispatch fails.
//
// Design note: This is intentionally fire-and-forget. Success is recorded at
// injection time, not at completion. If the target session fails after injection,
// the cron job still shows StatusSuccess and RunCount is incremented. This avoids
// the complexity of cross-session state observation. The metrics label "success"
// reflects successful prompt dispatch, not successful execution outcome.
func (h *AttachedSessionHandler) Execute(ctx context.Context, job *CronJob) error {
	sid := job.Payload.TargetSessionID

	info, err := h.router.GetSessionInfo(ctx, sid)
	if err != nil {
		observability.CronAttached().Add(ctx, 1, metric.WithAttributes(attribute.String("result", "session_not_found")))
		return fmt.Errorf("callback: session %s not found: %w", sid, err)
	}

	prompt := formatJobPrompt(job, time.Now())
	metadata := map[string]any{
		"source":   "cron_attached",
		"cron_job": job.ID,
	}

	switch info.State {
	case events.StateRunning:
		if err := h.router.InjectInput(ctx, sid, prompt, metadata); err != nil {
			observability.CronAttached().Add(ctx, 1, metric.WithAttributes(attribute.String("result", "inject_failed")))
			return fmt.Errorf("callback: inject into running session: %w", err)
		}
		h.log.Info("callback: injected into running session",
			"session_id", sid, "job_id", job.ID)

	case events.StateIdle, events.StateTerminated:
		if err := h.router.ResumeAndInput(ctx, sid, info.WorkDir, prompt, metadata); err != nil {
			observability.CronAttached().Add(ctx, 1, metric.WithAttributes(attribute.String("result", "resume_failed")))
			return fmt.Errorf("callback: resume session %s: %w", sid, err)
		}
		h.log.Info("callback: resumed and injected",
			"session_id", sid, "job_id", job.ID, "from_state", info.State)

	case events.StateDeleted:
		observability.CronAttached().Add(ctx, 1, metric.WithAttributes(attribute.String("result", "session_not_found")))
		return fmt.Errorf("callback: session %s is deleted, aborting", sid)

	case events.StateCreated:
		observability.CronAttached().Add(ctx, 1, metric.WithAttributes(attribute.String("result", "session_not_found")))
		return fmt.Errorf("callback: session %s is in CREATED state (never started), aborting", sid)

	default:
		observability.CronAttached().Add(ctx, 1, metric.WithAttributes(attribute.String("result", "session_not_found")))
		return fmt.Errorf("callback: session %s in unexpected state %s", sid, info.State)
	}

	observability.CronAttached().Add(ctx, 1, metric.WithAttributes(attribute.String("result", "success")))
	return nil
}

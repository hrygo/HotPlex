package gateway

import (
	"context"
	"errors"
	"fmt"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

func (h *Handler) sendErrorf(ctx context.Context, env *events.Envelope, code events.ErrorCode, format string, args ...any) error {
	err := events.NewEnvelope(aep.NewID(), env.SessionID, h.hub.NextSeq(env.SessionID), events.Error, events.ErrorData{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	})
	_ = h.hub.SendToSession(ctx, err) // best-effort; always return the error
	return fmt.Errorf("%s: %s", code, fmt.Sprintf(format, args...))
}

// classifyWorkerError converts worker errors into AEP error codes.
// Worker process death (ErrKindUnavailable) maps to ErrCodeSessionTerminated
// so clients can reconnect rather than treating them as transient internal errors.
// Timeout errors (ErrKindTimeout) are not treated as fatal — the worker is still alive.
func classifyWorkerError(err error) events.ErrorCode {
	we, ok := errors.AsType[*worker.WorkerError](err)
	if ok {
		switch we.Kind {
		case worker.ErrKindUnavailable:
			return events.ErrCodeSessionTerminated
		case worker.ErrKindTimeout:
			return events.ErrCodeInternalError
		}
	}
	return events.ErrCodeInternalError
}

package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

func (h *Handler) sendErrorf(ctx context.Context, env *events.Envelope, code events.ErrorCode, format string, args ...any) error {
	err := events.NewEnvelope(aep.NewID(), env.SessionID, 0, events.Error, events.ErrorData{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	})
	if h.hub != nil {
		_ = h.hub.SendToSession(ctx, err) // best-effort; always return the error
	}
	return fmt.Errorf("%s: %s", code, fmt.Sprintf(format, args...))
}

// classifyWorkerError converts worker errors into AEP error codes.
// Worker process death (ErrKindUnavailable) maps to ErrCodeSessionTerminated
// so clients can reconnect rather than treating them as transient internal errors.
// Timeout errors (ErrKindTimeout) are not treated as fatal — the worker is still alive.
// ErrNotImplemented maps to ErrCodeNotSupported for unimplemented worker capabilities.
func classifyWorkerError(err error) events.ErrorCode {
	if errors.Is(err, base.ErrInvalidSchema) {
		return events.ErrCodeInvalidMessage
	}
	if errors.Is(err, worker.ErrInvalidPermissionMode) ||
		errors.Is(err, worker.ErrPermissionEscalation) ||
		errors.Is(err, worker.ErrPermissionCeilingUnset) {
		return events.ErrCodeInvalidMessage
	}
	if isCapabilityRejection(err) {
		return events.ErrCodeNotSupported
	}
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

// isCapabilityRejection recognizes the stable error vocabulary emitted by
// Worker adapters when a command is unavailable or disabled. Adapters wrap
// vendor-specific errors at different layers, so this keeps the wire-level
// classification consistent without exposing those implementation details to
// clients. Runtime availability errors are checked first above and do not use
// this vocabulary.
func isCapabilityRejection(err error) bool {
	if errors.Is(err, worker.ErrNotImplemented) ||
		errors.Is(err, worker.ErrSkillNotSupported) {
		return true
	}

	message := strings.ToLower(err.Error())
	for _, phrase := range []string{
		"not implemented",
		"not supported",
		"unsupported",
		"not enabled",
	} {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}

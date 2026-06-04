package observability

import "go.opentelemetry.io/otel/attribute"

// Shared attribute key constants used across all domain packages.
// These avoid stringly-typed attribute.Set() calls and ensure consistency.

// ─── Session attributes ─────────────────────────────────────────────

const (
	AttrSessionID = "session_id"
	AttrState     = "state"  // created, running, idle
	AttrReason    = "reason" // idle_timeout, max_lifetime, client_kill, admin_kill, zombie, crash
)

// ─── Worker attributes ──────────────────────────────────────────────

const (
	AttrWorkerType = "worker_type"
	AttrResult     = "result" // success, failed
	AttrExitCode   = "exit_code"
	AttrErrorType  = "error_type"
)

// ─── Gateway attributes ─────────────────────────────────────────────

const (
	AttrDirection = "direction" // incoming, outgoing, s2c, c2s
	AttrEventType = "event_type"
	AttrPriority  = "priority"
	AttrErrorCode = "error_code"
	AttrPhase     = "phase" // close_old, ensure_card
)

// ─── ACP attributes ─────────────────────────────────────────────────

const (
	AttrType    = "type"    // input, output, cached_read, cached_write, thought
	AttrKind    = "kind"    // read, edit, delete, execute, search, other
	AttrOutcome = "outcome" // approved, denied, timeout
)

// ─── Cron attributes ────────────────────────────────────────────────

const (
	AttrJobName = "job_name"
)

// ─── Brain / LLM attributes ─────────────────────────────────────────

const (
	AttrModel    = "model"
	AttrScenario = "scenario"
	AttrStrategy = "strategy"
)

// ─── Pool attributes ────────────────────────────────────────────────

const (
	AttrPoolResult = "result" // success, pool_exhausted, user_quota_exceeded, memory_exceeded, toctou_retry
)

// ─── Retry attributes ───────────────────────────────────────────────

// AttrReason is reused for retry attempt labels (llm_error, etc.).

// ─── Tracing attributes ─────────────────────────────────────────────

const (
	AttrSeq = "seq"
)

// ─── Span names ─────────────────────────────────────────────────────

const (
	SpanConnInit         = "conn.init"
	SpanConnRecv         = "conn.recv"
	SpanHubSendToSession = "hub.send_to_session"
	SpanHubBroadcast     = "hub.broadcast"
	SpanSessionStart     = "session.start"
	SpanWorkerExec       = "worker.exec"
	SpanBrainLLMRequest  = "brain.llm.request"
)

// KVs returns a slice of attribute.KeyValue for a single key-value pair.
// Convenience function for use with metric.WithAttributes().
func KV(k, v string) attribute.KeyValue {
	return attribute.String(k, v)
}

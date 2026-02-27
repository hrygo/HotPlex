package logging

// Field names for structured logging (all snake_case for JSON compatibility).
const (
	// Session identification fields
	FieldSessionID         = "session_id"
	FieldProviderSessionID = "provider_session_id"

	// Platform and user identification fields
	FieldPlatform  = "platform"
	FieldNamespace = "namespace"
	FieldUserID    = "user_id"
	FieldChannelID = "channel_id"
	FieldThreadID  = "thread_id"
	FieldRequestID = "request_id"

	// Performance and metrics fields
	FieldDurationMs = "duration_ms"
	FieldLatencyMs  = "latency_ms"
	FieldCostUsd    = "cost_usd"

	// Operation and error fields
	FieldOperation  = "operation"
	FieldError      = "error"
	FieldReason     = "reason"
	FieldEventType  = "event_type"
	FieldStatusType = "status_type"

	// Content fields
	FieldContentLength = "content_length"
	FieldInputLen      = "input_len"
	FieldOutputLen     = "output_len"
	FieldToolName      = "tool_name"
)

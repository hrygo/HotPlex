package observability

// keys.go is the single source of truth for the shared telemetry semantic keys
// — the string key names that correlate a request across HotPlex's THREE
// observability planes: AEP event metadata (Envelope.Metadata), slog
// structured-log fields, and OpenTelemetry span/metric attributes.
//
// Centralizing them here kills the scattered string literals that had grown
// across the gateway (worker_type appeared 40+×, session_id 60+×, execution_id
// 12×, trace_id 3× with no constant at all) and makes the cardinality contract
// explicit and machine-checkable.
//
// # Cardinality contract
//
// Keys split into two tiers by label-set impact:
//
//   - LOW-cardinality keys (trace/span ids; bounded enums like worker_type,
//     platform, status, reason) are metric-safe: use freely as metric labels,
//     slog fields, span attributes, and AEP metadata.
//   - HIGH-cardinality keys (agent_id, user_id, workspace_id, execution_id,
//     session_id) MUST NEVER be metric labels — a per-request/per-session id as
//     a label explodes the label set and starves Prometheus. They appear only
//     as span attributes, slog fields, AEP metadata, or audit detail. This
//     mirrors the rule already documented for turn.ttft in
//     docs/reference/metrics.md.
//
// MetricSafeKeys is the allowlist enforcing the contract; keys_metric_test.go
// asserts no high-cardinality key is in it.
//
// # Relationship to agentspec.MetadataKey*
//
// The identity keys (KeyAgentID, KeyUserID, KeyWorkspaceID, KeyWorkerType,
// KeyPlatform) share their string values with agentspec.MetadataKey* (#848).
// keys_consistency_test.go asserts the two never drift, so the packages remain
// a single de-facto source of truth WITHOUT forcing the lightweight, pure
// agentspec package (stdlib + uuid only) to import observability's otel/prom
// dependencies.

// --- low-cardinality correlation identifiers (metric-safe) ---

const (
	// KeyTraceID is the W3C TraceContext trace id, injected into AEP metadata
	// by Hub.SendToSession so a downstream consumer can correlate an event to
	// its trace. Low cardinality.
	KeyTraceID = "trace_id"
	// KeySpanID is the OTel span id, injected alongside KeyTraceID. Low
	// cardinality.
	KeySpanID = "span_id"
)

// HighCardinalityKeys lists the correlation identifiers that are per-session or
// per-request: they are HIGH cardinality and MUST NOT be used as metric labels.
// They are valid as span attributes, slog fields, AEP metadata, and audit
// detail_json keys. Callers building metric attributes must consult
// MetricSafeKeys rather than reaching for these.
const (
	KeyAgentID     = "agent_id"     // agentspec.AgentIdentity.AgentID (#848)
	KeyUserID      = "user_id"      // agentspec.AgentIdentity.UserID
	KeyWorkspaceID = "workspace_id" // multitenancy anchor
	KeyExecutionID = "execution_id" // per-input execution id (#878)
	KeySessionID   = "session_id"   // gateway session id
)

// --- bounded enum labels (metric-safe) ---

const (
	KeyWorkerType     = "worker_type"     // claude_code|codex_cli|opencode_server|acp
	KeyPlatform       = "platform"        // webchat|feishu|slack|yuanxin|cron
	KeyEventType      = "event_type"      // AEP event Kind
	KeyDirection      = "direction"       // inbound|outbound
	KeyReason         = "reason"          // termination/failure reason enum
	KeyStatus         = "status"          // generic status enum
	KeyErrorType      = "error_type"      // normalized error category
	KeyExitCode       = "exit_code"       // normalized worker exit class
	KeyRawExitCode    = "raw_exit_code"   // OS-level exit code
	KeyDeliveryStatus = "delivery_status" // execution delivery outcome enum
	KeyRuntimeStatus  = "runtime_status"  // execution runtime outcome enum
	KeyTerminalStatus = "terminal_status" // turn terminal class
	KeyStage          = "stage"           // turn pipeline stage
	KeyFirstOutput    = "first_output"    // turn first-output flag
	KeyTool           = "tool"            // tool name (bounded by registry)
	KeyResult         = "result"          // generic result enum
	KeyAnonymous      = "anonymous"       // unauthenticated-session flag
	KeyConnID         = "conn_id"         // WS connection id
	KeyPriority       = "priority"        // event priority
	KeySeq            = "seq"             // per-session sequence number
)

// MetricSafeKeys is the allowlist of keys permitted as Prometheus/OTel metric
// labels. It contains only LOW-cardinality, bounded-enum keys. Any key not in
// this map — notably every entry of HighCardinalityKeys — must stay off metric
// instruments. keys_metric_test.go guards this set against accidental addition
// of a high-cardinality key.
//
// This is intentionally an explicit allowlist (not "everything not in
// HighCardinalityKeys"): adding a new metric label is a conscious act that
// should also update metrics.md.
var MetricSafeKeys = map[string]struct{}{
	KeyWorkerType:     {},
	KeyPlatform:       {},
	KeyEventType:      {},
	KeyDirection:      {},
	KeyReason:         {},
	KeyStatus:         {},
	KeyErrorType:      {},
	KeyExitCode:       {},
	KeyRawExitCode:    {},
	KeyDeliveryStatus: {},
	KeyRuntimeStatus:  {},
	KeyTerminalStatus: {},
	KeyStage:          {},
	KeyFirstOutput:    {},
	KeyTool:           {},
	KeyResult:         {},
	KeyAnonymous:      {},
	KeyConnID:         {},
	KeyPriority:       {},
	KeySeq:            {},

	// trace_id/span_id are technically low-cardinality within a sampling window
	// but are NOT useful as metric labels (they don't partition a useful
	// distribution); deliberately omitted to keep metric label sets bounded.
}

// IsMetricSafe reports whether key is permitted as a metric label. New code
// building metric attributes should guard with this rather than assuming.
func IsMetricSafe(key string) bool {
	_, ok := MetricSafeKeys[key]
	return ok
}

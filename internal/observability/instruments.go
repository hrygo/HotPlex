package observability

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// All instruments are lazily initialized on first use via sync.Once per instrument.
// This avoids the need to pass a context at package init time.

// ─── Session Instruments ────────────────────────────────────────────

var (
	sessionActive     metric.Int64ObservableGauge
	sessionActiveInit sync.Once

	sessionCreated     metric.Int64Counter
	sessionCreatedInit sync.Once

	sessionTerminated     metric.Int64Counter
	sessionTerminatedInit sync.Once

	sessionDeleted     metric.Int64Counter
	sessionDeletedInit sync.Once

	sessionStartAttempts     metric.Int64Counter
	sessionStartAttemptsInit sync.Once

	sessionStartErrors     metric.Int64Counter
	sessionStartErrorsInit sync.Once

	sessionStartDuration     metric.Float64Histogram
	sessionStartDurationInit sync.Once
)

func SessionActive() metric.Int64ObservableGauge {
	sessionActiveInit.Do(func() {
		sessionActive, _ = Meter().Int64ObservableGauge(
			"hotplex.session.active",
			metric.WithDescription("Number of active sessions by state"),
		)
	})
	return sessionActive
}

func SessionCreated() metric.Int64Counter {
	sessionCreatedInit.Do(func() {
		sessionCreated, _ = Meter().Int64Counter(
			"hotplex.session.created",
			metric.WithDescription("Total sessions created"),
		)
	})
	return sessionCreated
}

func SessionTerminated() metric.Int64Counter {
	sessionTerminatedInit.Do(func() {
		sessionTerminated, _ = Meter().Int64Counter(
			"hotplex.session.terminated",
			metric.WithDescription("Total sessions terminated by reason"),
		)
	})
	return sessionTerminated
}

func SessionDeleted() metric.Int64Counter {
	sessionDeletedInit.Do(func() {
		sessionDeleted, _ = Meter().Int64Counter(
			"hotplex.session.deleted",
			metric.WithDescription("Total sessions deleted by retention GC"),
		)
	})
	return sessionDeleted
}

func SessionStartAttempts() metric.Int64Counter {
	sessionStartAttemptsInit.Do(func() {
		sessionStartAttempts, _ = Meter().Int64Counter(
			"hotplex.session.start.attempts",
			metric.WithDescription("Total session start attempts"),
		)
	})
	return sessionStartAttempts
}

func SessionStartErrors() metric.Int64Counter {
	sessionStartErrorsInit.Do(func() {
		sessionStartErrors, _ = Meter().Int64Counter(
			"hotplex.session.start.errors",
			metric.WithDescription("Total session start errors"),
		)
	})
	return sessionStartErrors
}

func SessionStartDuration() metric.Float64Histogram {
	sessionStartDurationInit.Do(func() {
		sessionStartDuration, _ = Meter().Float64Histogram(
			"hotplex.session.start.duration",
			metric.WithDescription("Duration of session start in seconds"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(0.5, 1, 2, 5, 10, 30, 60),
		)
	})
	return sessionStartDuration
}

// ─── Worker Instruments ─────────────────────────────────────────────

var (
	workerRunning     metric.Int64ObservableGauge
	workerRunningInit sync.Once

	workerStarts     metric.Int64Counter
	workerStartsInit sync.Once

	workerExecDuration     metric.Float64Histogram
	workerExecDurationInit sync.Once

	workerCrashes     metric.Int64Counter
	workerCrashesInit sync.Once

	workerMemory     metric.Int64ObservableGauge
	workerMemoryInit sync.Once

	workerCreationDuration     metric.Float64Histogram
	workerCreationDurationInit sync.Once
)

func WorkerRunning() metric.Int64ObservableGauge {
	workerRunningInit.Do(func() {
		workerRunning, _ = Meter().Int64ObservableGauge(
			"hotplex.worker.running",
			metric.WithDescription("Number of currently running workers"),
		)
	})
	return workerRunning
}

func WorkerStarts() metric.Int64Counter {
	workerStartsInit.Do(func() {
		workerStarts, _ = Meter().Int64Counter(
			"hotplex.worker.starts",
			metric.WithDescription("Total worker starts"),
		)
	})
	return workerStarts
}

func WorkerExecDuration() metric.Float64Histogram {
	workerExecDurationInit.Do(func() {
		workerExecDuration, _ = Meter().Float64Histogram(
			"hotplex.worker.execution.duration",
			metric.WithDescription("Worker execution duration in seconds"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(1, 5, 15, 30, 60, 120, 300, 600, 1800),
		)
	})
	return workerExecDuration
}

func WorkerCrashes() metric.Int64Counter {
	workerCrashesInit.Do(func() {
		workerCrashes, _ = Meter().Int64Counter(
			"hotplex.worker.crashes",
			metric.WithDescription("Total worker crashes"),
		)
	})
	return workerCrashes
}

func WorkerMemory() metric.Int64ObservableGauge {
	workerMemoryInit.Do(func() {
		workerMemory, _ = Meter().Int64ObservableGauge(
			"hotplex.worker.memory.bytes",
			metric.WithDescription("Estimated worker memory by worker_type"),
			metric.WithUnit("By"),
		)
	})
	return workerMemory
}

func WorkerCreationDuration() metric.Float64Histogram {
	workerCreationDurationInit.Do(func() {
		workerCreationDuration, _ = Meter().Float64Histogram(
			"hotplex.worker.creation.duration",
			metric.WithDescription("Duration of worker creation in seconds"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(0.5, 1, 2, 5, 10, 30, 60),
		)
	})
	return workerCreationDuration
}

// ─── Gateway Instruments ────────────────────────────────────────────

var (
	gatewayConnections     metric.Int64ObservableGauge
	gatewayConnectionsInit sync.Once

	gatewayMessages     metric.Int64Counter
	gatewayMessagesInit sync.Once

	gatewayEvents     metric.Int64Counter
	gatewayEventsInit sync.Once

	gatewayDeltasDropped     metric.Int64Counter
	gatewayDeltasDroppedInit sync.Once

	gatewayPlatformDropped     metric.Int64Counter
	gatewayPlatformDroppedInit sync.Once

	gatewayNoSubscribersDropped     metric.Int64Counter
	gatewayNoSubscribersDroppedInit sync.Once

	gatewayDeltaCoalesced     metric.Int64Counter
	gatewayDeltaCoalescedInit sync.Once

	gatewayDeltaFlush     metric.Int64Counter
	gatewayDeltaFlushInit sync.Once

	gatewayErrors     metric.Int64Counter
	gatewayErrorsInit sync.Once

	gatewayInitHandshakeDuration     metric.Float64Histogram
	gatewayInitHandshakeDurationInit sync.Once
)

func GatewayConnections() metric.Int64ObservableGauge {
	gatewayConnectionsInit.Do(func() {
		gatewayConnections, _ = Meter().Int64ObservableGauge(
			"hotplex.gateway.connections",
			metric.WithDescription("Current WebSocket connections"),
		)
	})
	return gatewayConnections
}

func GatewayMessages() metric.Int64Counter {
	gatewayMessagesInit.Do(func() {
		gatewayMessages, _ = Meter().Int64Counter(
			"hotplex.gateway.messages",
			metric.WithDescription("Total messages by direction and event type"),
		)
	})
	return gatewayMessages
}

func GatewayEvents() metric.Int64Counter {
	gatewayEventsInit.Do(func() {
		gatewayEvents, _ = Meter().Int64Counter(
			"hotplex.gateway.events",
			metric.WithDescription("Total pass-through events by type and direction"),
		)
	})
	return gatewayEvents
}

func GatewayDeltasDropped() metric.Int64Counter {
	gatewayDeltasDroppedInit.Do(func() {
		gatewayDeltasDropped, _ = Meter().Int64Counter(
			"hotplex.gateway.deltas.dropped",
			metric.WithDescription("Delta events dropped due to backpressure"),
		)
	})
	return gatewayDeltasDropped
}

func GatewayPlatformDropped() metric.Int64Counter {
	gatewayPlatformDroppedInit.Do(func() {
		gatewayPlatformDropped, _ = Meter().Int64Counter(
			"hotplex.gateway.platform.dropped",
			metric.WithDescription("Events dropped at platform buffer level"),
		)
	})
	return gatewayPlatformDropped
}

func GatewayNoSubscribersDropped() metric.Int64Counter {
	gatewayNoSubscribersDroppedInit.Do(func() {
		gatewayNoSubscribersDropped, _ = Meter().Int64Counter(
			"hotplex.gateway.no_subscribers.dropped",
			metric.WithDescription("Events dropped due to no subscribers"),
		)
	})
	return gatewayNoSubscribersDropped
}

func GatewayDeltaCoalesced() metric.Int64Counter {
	gatewayDeltaCoalescedInit.Do(func() {
		gatewayDeltaCoalesced, _ = Meter().Int64Counter(
			"hotplex.gateway.delta.coalesced",
			metric.WithDescription("Delta events merged by coalescer"),
		)
	})
	return gatewayDeltaCoalesced
}

func GatewayDeltaFlush() metric.Int64Counter {
	gatewayDeltaFlushInit.Do(func() {
		gatewayDeltaFlush, _ = Meter().Int64Counter(
			"hotplex.gateway.delta.flush",
			metric.WithDescription("Merged delta flushes sent to platform"),
		)
	})
	return gatewayDeltaFlush
}

func GatewayErrors() metric.Int64Counter {
	gatewayErrorsInit.Do(func() {
		gatewayErrors, _ = Meter().Int64Counter(
			"hotplex.gateway.errors",
			metric.WithDescription("Gateway errors by error code"),
		)
	})
	return gatewayErrors
}

func GatewayInitHandshakeDuration() metric.Float64Histogram {
	gatewayInitHandshakeDurationInit.Do(func() {
		gatewayInitHandshakeDuration, _ = Meter().Float64Histogram(
			"hotplex.gateway.init.handshake.duration",
			metric.WithDescription("WebSocket init handshake duration in seconds"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(0.1, 0.25, 0.5, 1, 2, 5),
		)
	})
	return gatewayInitHandshakeDuration
}

// ─── Pool Instruments ───────────────────────────────────────────────

var (
	poolAcquire     metric.Int64Counter
	poolAcquireInit sync.Once

	poolReleaseErrors     metric.Int64Counter
	poolReleaseErrorsInit sync.Once

	poolUtilization     metric.Float64ObservableGauge
	poolUtilizationInit sync.Once
)

func PoolAcquire() metric.Int64Counter {
	poolAcquireInit.Do(func() {
		poolAcquire, _ = Meter().Int64Counter(
			"hotplex.pool.acquire",
			metric.WithDescription("Pool acquire attempts by result"),
		)
	})
	return poolAcquire
}

func PoolReleaseErrors() metric.Int64Counter {
	poolReleaseErrorsInit.Do(func() {
		poolReleaseErrors, _ = Meter().Int64Counter(
			"hotplex.pool.release.errors",
			metric.WithDescription("Pool release errors (double-release bugs)"),
		)
	})
	return poolReleaseErrors
}

func PoolUtilization() metric.Float64ObservableGauge {
	poolUtilizationInit.Do(func() {
		poolUtilization, _ = Meter().Float64ObservableGauge(
			"hotplex.pool.utilization",
			metric.WithDescription("Pool utilization ratio (0-1)"),
		)
	})
	return poolUtilization
}

// ─── Cron Instruments ───────────────────────────────────────────────

var (
	cronFires     metric.Int64Counter
	cronFiresInit sync.Once

	cronErrors     metric.Int64Counter
	cronErrorsInit sync.Once

	cronDuration     metric.Float64Histogram
	cronDurationInit sync.Once

	cronAttached     metric.Int64Counter
	cronAttachedInit sync.Once
)

func CronFires() metric.Int64Counter {
	cronFiresInit.Do(func() {
		cronFires, _ = Meter().Int64Counter(
			"hotplex.cron.fires",
			metric.WithDescription("Cron job fire attempts"),
		)
	})
	return cronFires
}

func CronErrors() metric.Int64Counter {
	cronErrorsInit.Do(func() {
		cronErrors, _ = Meter().Int64Counter(
			"hotplex.cron.errors",
			metric.WithDescription("Cron job execution errors"),
		)
	})
	return cronErrors
}

func CronDuration() metric.Float64Histogram {
	cronDurationInit.Do(func() {
		cronDuration, _ = Meter().Float64Histogram(
			"hotplex.cron.duration",
			metric.WithDescription("Cron job execution duration in seconds"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(1, 5, 15, 30, 60, 120, 300, 600, 1800),
		)
	})
	return cronDuration
}

func CronAttached() metric.Int64Counter {
	cronAttachedInit.Do(func() {
		cronAttached, _ = Meter().Int64Counter(
			"hotplex.cron.attached",
			metric.WithDescription("Session callback attempts by result"),
		)
	})
	return cronAttached
}

// ─── Streaming Card Instruments ─────────────────────────────────────

var (
	streamingCardRotations     metric.Int64Counter
	streamingCardRotationsInit sync.Once

	streamingCardRotationFailures     metric.Int64Counter
	streamingCardRotationFailuresInit sync.Once

	streamingCardFlushFallbacks     metric.Int64Counter
	streamingCardFlushFallbacksInit sync.Once
)

func StreamingCardRotations() metric.Int64Counter {
	streamingCardRotationsInit.Do(func() {
		streamingCardRotations, _ = Meter().Int64Counter(
			"hotplex.streaming.card.rotations",
			metric.WithDescription("Streaming card TTL rotations"),
		)
	})
	return streamingCardRotations
}

func StreamingCardRotationFailures() metric.Int64Counter {
	streamingCardRotationFailuresInit.Do(func() {
		streamingCardRotationFailures, _ = Meter().Int64Counter(
			"hotplex.streaming.card.rotation_failures",
			metric.WithDescription("Streaming card rotation failures by phase"),
		)
	})
	return streamingCardRotationFailures
}

func StreamingCardFlushFallbacks() metric.Int64Counter {
	streamingCardFlushFallbacksInit.Do(func() {
		streamingCardFlushFallbacks, _ = Meter().Int64Counter(
			"hotplex.streaming.card.flush_fallbacks",
			metric.WithDescription("CardKit-to-IM Patch degradations"),
		)
	})
	return streamingCardFlushFallbacks
}

// ─── ACP Instruments ────────────────────────────────────────────────

var (
	acpPromptTokens     metric.Int64Counter
	acpPromptTokensInit sync.Once

	acpToolCalls     metric.Int64Counter
	acpToolCallsInit sync.Once

	acpPermissionRequests     metric.Int64Counter
	acpPermissionRequestsInit sync.Once

	acpHandshakeDuration     metric.Float64Histogram
	acpHandshakeDurationInit sync.Once
)

func ACPPromptTokens() metric.Int64Counter {
	acpPromptTokensInit.Do(func() {
		acpPromptTokens, _ = Meter().Int64Counter(
			"hotplex.acp.prompt_tokens",
			metric.WithDescription("ACP token usage by type"),
		)
	})
	return acpPromptTokens
}

func ACPToolCalls() metric.Int64Counter {
	acpToolCallsInit.Do(func() {
		acpToolCalls, _ = Meter().Int64Counter(
			"hotplex.acp.tool_calls",
			metric.WithDescription("ACP tool calls by kind"),
		)
	})
	return acpToolCalls
}

func ACPPermissionRequests() metric.Int64Counter {
	acpPermissionRequestsInit.Do(func() {
		acpPermissionRequests, _ = Meter().Int64Counter(
			"hotplex.acp.permission_requests",
			metric.WithDescription("ACP permission requests by outcome"),
		)
	})
	return acpPermissionRequests
}

func ACPHandshakeDuration() metric.Float64Histogram {
	acpHandshakeDurationInit.Do(func() {
		acpHandshakeDuration, _ = Meter().Float64Histogram(
			"hotplex.acp.handshake.duration",
			metric.WithDescription("ACP agent initialize handshake duration in seconds"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(0.1, 0.25, 0.5, 1, 2, 5, 10),
		)
	})
	return acpHandshakeDuration
}

// ─── Retry Instruments ──────────────────────────────────────────────

var (
	retryAttempts     metric.Int64Counter
	retryAttemptsInit sync.Once

	retryExhaustion     metric.Int64Counter
	retryExhaustionInit sync.Once
)

func RetryAttempts() metric.Int64Counter {
	retryAttemptsInit.Do(func() {
		retryAttempts, _ = Meter().Int64Counter(
			"hotplex.retry.attempts",
			metric.WithDescription("Auto-retry attempts by reason"),
		)
	})
	return retryAttempts
}

func RetryExhaustion() metric.Int64Counter {
	retryExhaustionInit.Do(func() {
		retryExhaustion, _ = Meter().Int64Counter(
			"hotplex.retry.exhaustion",
			metric.WithDescription("Retry exhaustion events"),
		)
	})
	return retryExhaustion
}

// ─── Helpers ────────────────────────────────────────────────────────

// CtxAttr returns a context and attribute set for use with instrument calls.
func CtxAttr(ctx context.Context, kvs ...attribute.KeyValue) (context.Context, metric.MeasurementOption) {
	return ctx, metric.WithAttributes(kvs...)
}

package observability

import (
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel/metric"
)

var instrumentLog *slog.Logger

func setInstrumentLogger(l *slog.Logger) {
	instrumentLog = l
}

func warnInstrument(metricName string, err error) {
	if instrumentLog != nil {
		instrumentLog.Warn("observability: instrument creation failed", "metric", metricName, "err", err)
	}
}

// ─── Session Instruments ────────────────────────────────────────────

var (
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

func SessionCreated() metric.Int64Counter {
	sessionCreatedInit.Do(func() {
		var err error
		sessionCreated, err = Meter().Int64Counter(
			"hotplex.session.created",
			metric.WithDescription("Total sessions created"),
		)
		if err != nil {
			warnInstrument("hotplex.session.created", err)
		}
	})
	return sessionCreated
}

func SessionTerminated() metric.Int64Counter {
	sessionTerminatedInit.Do(func() {
		var err error
		sessionTerminated, err = Meter().Int64Counter(
			"hotplex.session.terminated",
			metric.WithDescription("Total sessions terminated by reason"),
		)
		if err != nil {
			warnInstrument("hotplex.session.terminated", err)
		}
	})
	return sessionTerminated
}

func SessionDeleted() metric.Int64Counter {
	sessionDeletedInit.Do(func() {
		var err error
		sessionDeleted, err = Meter().Int64Counter(
			"hotplex.session.deleted",
			metric.WithDescription("Total sessions deleted by retention GC"),
		)
		if err != nil {
			warnInstrument("hotplex.session.deleted", err)
		}
	})
	return sessionDeleted
}

func SessionStartAttempts() metric.Int64Counter {
	sessionStartAttemptsInit.Do(func() {
		var err error
		sessionStartAttempts, err = Meter().Int64Counter(
			"hotplex.session.start.attempts",
			metric.WithDescription("Total session start attempts"),
		)
		if err != nil {
			warnInstrument("hotplex.session.start.attempts", err)
		}
	})
	return sessionStartAttempts
}

func SessionStartErrors() metric.Int64Counter {
	sessionStartErrorsInit.Do(func() {
		var err error
		sessionStartErrors, err = Meter().Int64Counter(
			"hotplex.session.start.errors",
			metric.WithDescription("Total session start errors"),
		)
		if err != nil {
			warnInstrument("hotplex.session.start.errors", err)
		}
	})
	return sessionStartErrors
}

func SessionStartDuration() metric.Float64Histogram {
	sessionStartDurationInit.Do(func() {
		var err error
		sessionStartDuration, err = Meter().Float64Histogram(
			"hotplex.session.start.duration",
			metric.WithDescription("Duration of session start in seconds"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(0.5, 1, 2, 5, 10, 30, 60),
		)
		if err != nil {
			warnInstrument("hotplex.session.start.duration", err)
		}
	})
	return sessionStartDuration
}

// ─── Worker Instruments ─────────────────────────────────────────────

var (
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

func WorkerStarts() metric.Int64Counter {
	workerStartsInit.Do(func() {
		var err error
		workerStarts, err = Meter().Int64Counter(
			"hotplex.worker.starts",
			metric.WithDescription("Total worker starts"),
		)
		if err != nil {
			warnInstrument("hotplex.worker.starts", err)
		}
	})
	return workerStarts
}

func WorkerExecDuration() metric.Float64Histogram {
	workerExecDurationInit.Do(func() {
		var err error
		workerExecDuration, err = Meter().Float64Histogram(
			"hotplex.worker.execution.duration",
			metric.WithDescription("Worker execution duration in seconds"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(1, 5, 15, 30, 60, 120, 300, 600, 1800),
		)
		if err != nil {
			warnInstrument("hotplex.worker.execution.duration", err)
		}
	})
	return workerExecDuration
}

func WorkerCrashes() metric.Int64Counter {
	workerCrashesInit.Do(func() {
		var err error
		workerCrashes, err = Meter().Int64Counter(
			"hotplex.worker.crashes",
			metric.WithDescription("Total worker crashes"),
		)
		if err != nil {
			warnInstrument("hotplex.worker.crashes", err)
		}
	})
	return workerCrashes
}

func WorkerMemory() metric.Int64ObservableGauge {
	workerMemoryInit.Do(func() {
		var err error
		workerMemory, err = Meter().Int64ObservableGauge(
			"hotplex.worker.memory.bytes",
			metric.WithDescription("Estimated worker memory by worker_type"),
			metric.WithUnit("By"),
		)
		if err != nil {
			warnInstrument("hotplex.worker.memory.bytes", err)
		}
	})
	return workerMemory
}

func WorkerCreationDuration() metric.Float64Histogram {
	workerCreationDurationInit.Do(func() {
		var err error
		workerCreationDuration, err = Meter().Float64Histogram(
			"hotplex.worker.creation.duration",
			metric.WithDescription("Duration of worker creation in seconds"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(0.5, 1, 2, 5, 10, 30, 60),
		)
		if err != nil {
			warnInstrument("hotplex.worker.creation.duration", err)
		}
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
		var err error
		gatewayConnections, err = Meter().Int64ObservableGauge(
			"hotplex.gateway.connections",
			metric.WithDescription("Current WebSocket connections"),
		)
		if err != nil {
			warnInstrument("hotplex.gateway.connections", err)
		}
	})
	return gatewayConnections
}

func GatewayMessages() metric.Int64Counter {
	gatewayMessagesInit.Do(func() {
		var err error
		gatewayMessages, err = Meter().Int64Counter(
			"hotplex.gateway.messages",
			metric.WithDescription("Total messages by direction and event type"),
		)
		if err != nil {
			warnInstrument("hotplex.gateway.messages", err)
		}
	})
	return gatewayMessages
}

func GatewayEvents() metric.Int64Counter {
	gatewayEventsInit.Do(func() {
		var err error
		gatewayEvents, err = Meter().Int64Counter(
			"hotplex.gateway.events",
			metric.WithDescription("Total pass-through events by type and direction"),
		)
		if err != nil {
			warnInstrument("hotplex.gateway.events", err)
		}
	})
	return gatewayEvents
}

func GatewayDeltasDropped() metric.Int64Counter {
	gatewayDeltasDroppedInit.Do(func() {
		var err error
		gatewayDeltasDropped, err = Meter().Int64Counter(
			"hotplex.gateway.deltas.dropped",
			metric.WithDescription("Delta events dropped due to backpressure"),
		)
		if err != nil {
			warnInstrument("hotplex.gateway.deltas.dropped", err)
		}
	})
	return gatewayDeltasDropped
}

func GatewayPlatformDropped() metric.Int64Counter {
	gatewayPlatformDroppedInit.Do(func() {
		var err error
		gatewayPlatformDropped, err = Meter().Int64Counter(
			"hotplex.gateway.platform.dropped",
			metric.WithDescription("Events dropped at platform buffer level"),
		)
		if err != nil {
			warnInstrument("hotplex.gateway.platform.dropped", err)
		}
	})
	return gatewayPlatformDropped
}

func GatewayNoSubscribersDropped() metric.Int64Counter {
	gatewayNoSubscribersDroppedInit.Do(func() {
		var err error
		gatewayNoSubscribersDropped, err = Meter().Int64Counter(
			"hotplex.gateway.no_subscribers.dropped",
			metric.WithDescription("Events dropped due to no subscribers"),
		)
		if err != nil {
			warnInstrument("hotplex.gateway.no_subscribers.dropped", err)
		}
	})
	return gatewayNoSubscribersDropped
}

func GatewayDeltaCoalesced() metric.Int64Counter {
	gatewayDeltaCoalescedInit.Do(func() {
		var err error
		gatewayDeltaCoalesced, err = Meter().Int64Counter(
			"hotplex.gateway.delta.coalesced",
			metric.WithDescription("Delta events merged by coalescer"),
		)
		if err != nil {
			warnInstrument("hotplex.gateway.delta.coalesced", err)
		}
	})
	return gatewayDeltaCoalesced
}

func GatewayDeltaFlush() metric.Int64Counter {
	gatewayDeltaFlushInit.Do(func() {
		var err error
		gatewayDeltaFlush, err = Meter().Int64Counter(
			"hotplex.gateway.delta.flush",
			metric.WithDescription("Merged delta flushes sent to platform"),
		)
		if err != nil {
			warnInstrument("hotplex.gateway.delta.flush", err)
		}
	})
	return gatewayDeltaFlush
}

func GatewayErrors() metric.Int64Counter {
	gatewayErrorsInit.Do(func() {
		var err error
		gatewayErrors, err = Meter().Int64Counter(
			"hotplex.gateway.errors",
			metric.WithDescription("Gateway errors by error code"),
		)
		if err != nil {
			warnInstrument("hotplex.gateway.errors", err)
		}
	})
	return gatewayErrors
}

func GatewayInitHandshakeDuration() metric.Float64Histogram {
	gatewayInitHandshakeDurationInit.Do(func() {
		var err error
		gatewayInitHandshakeDuration, err = Meter().Float64Histogram(
			"hotplex.gateway.init.handshake.duration",
			metric.WithDescription("WebSocket init handshake duration in seconds"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(0.1, 0.25, 0.5, 1, 2, 5),
		)
		if err != nil {
			warnInstrument("hotplex.gateway.init.handshake.duration", err)
		}
	})
	return gatewayInitHandshakeDuration
}

// ─── Pool Instruments ───────────────────────────────────────────────

var (
	poolAcquire     metric.Int64Counter
	poolAcquireInit sync.Once

	poolReleaseErrors     metric.Int64Counter
	poolReleaseErrorsInit sync.Once
)

func PoolAcquire() metric.Int64Counter {
	poolAcquireInit.Do(func() {
		var err error
		poolAcquire, err = Meter().Int64Counter(
			"hotplex.pool.acquire",
			metric.WithDescription("Pool acquire attempts by result"),
		)
		if err != nil {
			warnInstrument("hotplex.pool.acquire", err)
		}
	})
	return poolAcquire
}

func PoolReleaseErrors() metric.Int64Counter {
	poolReleaseErrorsInit.Do(func() {
		var err error
		poolReleaseErrors, err = Meter().Int64Counter(
			"hotplex.pool.release.errors",
			metric.WithDescription("Pool release errors (double-release bugs)"),
		)
		if err != nil {
			warnInstrument("hotplex.pool.release.errors", err)
		}
	})
	return poolReleaseErrors
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
		var err error
		cronFires, err = Meter().Int64Counter(
			"hotplex.cron.fires",
			metric.WithDescription("Cron job fire attempts"),
		)
		if err != nil {
			warnInstrument("hotplex.cron.fires", err)
		}
	})
	return cronFires
}

func CronErrors() metric.Int64Counter {
	cronErrorsInit.Do(func() {
		var err error
		cronErrors, err = Meter().Int64Counter(
			"hotplex.cron.errors",
			metric.WithDescription("Cron job execution errors"),
		)
		if err != nil {
			warnInstrument("hotplex.cron.errors", err)
		}
	})
	return cronErrors
}

func CronDuration() metric.Float64Histogram {
	cronDurationInit.Do(func() {
		var err error
		cronDuration, err = Meter().Float64Histogram(
			"hotplex.cron.duration",
			metric.WithDescription("Cron job execution duration in seconds"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(1, 5, 15, 30, 60, 120, 300, 600, 1800),
		)
		if err != nil {
			warnInstrument("hotplex.cron.duration", err)
		}
	})
	return cronDuration
}

func CronAttached() metric.Int64Counter {
	cronAttachedInit.Do(func() {
		var err error
		cronAttached, err = Meter().Int64Counter(
			"hotplex.cron.attached",
			metric.WithDescription("Session callback attempts by result"),
		)
		if err != nil {
			warnInstrument("hotplex.cron.attached", err)
		}
	})
	return cronAttached
}

var (
	cronDeliveryRetry     metric.Int64Counter
	cronDeliveryRetryInit sync.Once
)

func CronDeliveryRetry() metric.Int64Counter {
	cronDeliveryRetryInit.Do(func() {
		var err error
		cronDeliveryRetry, err = Meter().Int64Counter(
			"hotplex.cron.delivery.result",
			metric.WithDescription("Cron delivery retry attempts by status (success|exhausted|permanent)"),
		)
		if err != nil {
			warnInstrument("hotplex.cron.delivery.result", err)
		}
	})
	return cronDeliveryRetry
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
		var err error
		streamingCardRotations, err = Meter().Int64Counter(
			"hotplex.streaming.card.rotations",
			metric.WithDescription("Streaming card TTL rotations"),
		)
		if err != nil {
			warnInstrument("hotplex.streaming.card.rotations", err)
		}
	})
	return streamingCardRotations
}

func StreamingCardRotationFailures() metric.Int64Counter {
	streamingCardRotationFailuresInit.Do(func() {
		var err error
		streamingCardRotationFailures, err = Meter().Int64Counter(
			"hotplex.streaming.card.rotation_failures",
			metric.WithDescription("Streaming card rotation failures by phase"),
		)
		if err != nil {
			warnInstrument("hotplex.streaming.card.rotation_failures", err)
		}
	})
	return streamingCardRotationFailures
}

func StreamingCardFlushFallbacks() metric.Int64Counter {
	streamingCardFlushFallbacksInit.Do(func() {
		var err error
		streamingCardFlushFallbacks, err = Meter().Int64Counter(
			"hotplex.streaming.card.flush_fallbacks",
			metric.WithDescription("CardKit-to-IM Patch degradations"),
		)
		if err != nil {
			warnInstrument("hotplex.streaming.card.flush_fallbacks", err)
		}
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
		var err error
		acpPromptTokens, err = Meter().Int64Counter(
			"hotplex.acp.prompt_tokens",
			metric.WithDescription("ACP token usage by type"),
		)
		if err != nil {
			warnInstrument("hotplex.acp.prompt_tokens", err)
		}
	})
	return acpPromptTokens
}

func ACPToolCalls() metric.Int64Counter {
	acpToolCallsInit.Do(func() {
		var err error
		acpToolCalls, err = Meter().Int64Counter(
			"hotplex.acp.tool_calls",
			metric.WithDescription("ACP tool calls by kind"),
		)
		if err != nil {
			warnInstrument("hotplex.acp.tool_calls", err)
		}
	})
	return acpToolCalls
}

func ACPPermissionRequests() metric.Int64Counter {
	acpPermissionRequestsInit.Do(func() {
		var err error
		acpPermissionRequests, err = Meter().Int64Counter(
			"hotplex.acp.permission_requests",
			metric.WithDescription("ACP permission requests by outcome"),
		)
		if err != nil {
			warnInstrument("hotplex.acp.permission_requests", err)
		}
	})
	return acpPermissionRequests
}

func ACPHandshakeDuration() metric.Float64Histogram {
	acpHandshakeDurationInit.Do(func() {
		var err error
		acpHandshakeDuration, err = Meter().Float64Histogram(
			"hotplex.acp.handshake.duration",
			metric.WithDescription("ACP agent initialize handshake duration in seconds"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(0.1, 0.25, 0.5, 1, 2, 5, 10),
		)
		if err != nil {
			warnInstrument("hotplex.acp.handshake.duration", err)
		}
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
		var err error
		retryAttempts, err = Meter().Int64Counter(
			"hotplex.retry.attempts",
			metric.WithDescription("Auto-retry attempts by reason"),
		)
		if err != nil {
			warnInstrument("hotplex.retry.attempts", err)
		}
	})
	return retryAttempts
}

func RetryExhaustion() metric.Int64Counter {
	retryExhaustionInit.Do(func() {
		var err error
		retryExhaustion, err = Meter().Int64Counter(
			"hotplex.retry.exhaustion",
			metric.WithDescription("Retry exhaustion events"),
		)
		if err != nil {
			warnInstrument("hotplex.retry.exhaustion", err)
		}
	})
	return retryExhaustion
}

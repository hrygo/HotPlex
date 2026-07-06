# Audit P2 Review Fixes Design

## Goal

Close all findings from the review of issue #833 P2 follow-ups without weakening audit durability, gateway responsiveness, or backward compatibility.

## Scope

The change covers credential-safe audit serialization, tool-input redaction, sink isolation and lifecycle, dual-write coverage for both admin API families, effective retention reload semantics, config-change coverage, and service-identity attribution.

## Design

### Credential-safe serialization

- Add one recursive sanitizer for JSON-like values. Keys matching credential concepts such as password, secret, token, API key, authorization, cookie, and private key are replaced with `[REDACTED]`.
- Sanitize every tool input before producing either full content, preview, or SHA-256. The hash is computed from sanitized JSON so it cannot become an offline oracle for a captured secret.
- Supplement key-based redaction with value-pattern masking for bearer tokens, prefixed keys, URL userinfo, cookies, and PEM private keys.
- Normalize tool names with `strings.ToLower`; sensitive tools are matched in canonical lowercase form.
- Config-change audit compares a sanitized copy of sink configuration. Secret changes remain observable as a redacted value change marker without storing plaintext.

### Sink isolation and lifecycle

- Replace per-event goroutine fan-out and the shared blocking semaphore with one bounded queue and one worker per configured sink.
- Collector commits enqueue immutable audit events to each sink queue using a non-blocking send. A full queue records the sink failure metric and drops only that external delivery; the tamper-evident database row remains durable.
- Worker calls use `SinkTimeout` and recover panics at the extension boundary so a third-party sink cannot terminate the gateway.
- Add an optional `Close(context.Context) error` lifecycle interface. Collector shutdown stops accepting new sink work, drains queued events within the provided deadline, waits for workers, then closes lifecycle-aware sinks.
- WebhookSink uses the same context-aware close contract and drains its private delivery queue before cancellation. Close is idempotent and bounded by the caller context.

### Admin dual-write

- Inject the audit collector into `UserAdminHandlers` from `routes.go`.
- Centralize cookie-admin audit emission so every write attempt records both legacy slog and `user_activity` with success, failure, or denied outcome.
- Include authentication denial and validation failure paths, not only storage failures.
- Preserve bearer administration as a distinct system/service actor instead of mapping `admin-token` to anonymous.

### Retention and config audit

- Treat `audit.full_content_retention` as static/restart-required because the event GC captures its TTL at startup. This matches actual runtime behavior and prevents a false “applied” signal.
- Add `audit.full_content_retention` to `auditConfigDiff` so attempted changes are still recorded in the tamper-evident trail.

## Compatibility

- Existing `sinks.Sink` implementations remain source-compatible; lifecycle support is optional.
- Existing YAML fields and defaults do not change.
- External sink delivery remains best-effort and non-blocking. Database audit persistence remains the source of truth.
- `full_content_retention` changes now explicitly require restart; startup behavior is unchanged.

## Testing

- Assert sink secrets and representative tool credentials never occur in `detail_json`.
- Cover lowercase sensitive tool names and sanitized previews/hashes.
- Use blocking and panicking sinks to prove collector progress, panic containment, shutdown draining, and bounded timeout behavior.
- Verify WebhookSink drains and exits on close.
- Cover `/api/admin/*` success, validation failure, storage failure, and denial dual-write paths.
- Verify `full_content_retention` is classified static and appears in credential-safe config-change audit records.
- Run focused `go test -count=1 -race`, then `make fmt`, `make lint`, and `make check`.

## Completion Criteria

All nine review findings have regression tests, relevant race tests pass, lint and complete quality gates pass, and the non-main branch is committed and pushed without bypassing hooks.

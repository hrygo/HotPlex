// Package sinks provides AlertSink implementations for the user behavior audit
// system (spec §5.6, issue #833).
//
// # Extension contract
//
// Register is the public extension point. Third-party code registers a custom
// sink from an init() function:
//
//	import "github.com/hrygo/hotplex/internal/audit/sinks"
//
//	func init() {
//	    sinks.Register("mycorp_slack", func(cfg map[string]any, log *slog.Logger) (sinks.Sink, error) {
//	        // parse cfg, construct client, return a Sink impl
//	        return &mySlackSink{...}, nil
//	    })
//	}
//
// A registered sink is activated by adding it to audit.sinks in the config:
//
//	audit:
//	  sinks:
//	    - name: mycorp_slack
//	      type: mycorp_slack
//	      config:
//	        webhook_url: https://hooks.slack.com/services/...
//
// # Implementation requirements (spec §5.6)
//
// Every Sink implementation MUST satisfy:
//
//  1. Non-blocking: OnAlertEvent MUST return quickly (well under the collector's
//     5s fan-out timeout). Use an internal goroutine + bounded queue; never do
//     synchronous network I/O on the caller's goroutine.
//  2. No panics: a panic inside OnAlertEvent would crash the collector's fan-out
//     goroutine and silently stop delivery to ALL sinks. Recover internally if
//     you call arbitrary code.
//  3. Errors are non-fatal: a returned error is logged and counted in the
//     hotplex.audit.sink_failures metric but does NOT affect the main audit
//     write path. The event was already persisted to the tamper-evident table
//     before the sink sees it.
//  4. Backpressure: when the sink's internal queue is full, DROP the event and
//     increment hotplex.audit.sink_failures. Never block the collector — a slow
//     sink must not stall the audit write path (spec §5.6, AGENTS.md backpressure).
//
// # Built-in sinks
//
//	noop  — discards every event (default; lets the system run sinkless).
//	log   — structured slog at INFO (development/debugging).
//	webhook — HTTP POST with HMAC-SHA256 signing (SIEM/SOC integration).
//
// See WebhookSink for a reference implementation of the contract above.
//
// # Why AlertEvent is separate from audit.AuditEvent
//
// This package intentionally does NOT import the parent audit package. The
// AlertEvent type here mirrors audit.AuditEvent field-for-field; the bridge
// between the two (audit.AlertSink → sinks.Sink) is performed in cmd/hotplex
// via a small sinkAdapter. This keeps the dependency direction one-way: cmd/
// hotplex imports both, and sinks stays a leaf package that third-party code
// can import without pulling in the collector.
package sinks

# WebChat staged feedback and TTFT observability

## Goal

Remove the apparent "black hole" between a WebChat send action and the first
worker event, while collecting server-authoritative time-to-first-token (TTFT)
data that can guide later latency work. This implements GitHub issue #894.

## Scope

- Create one local assistant placeholder synchronously for every submitted
  input, then move it through localized `thinking`, `accepted`, and output
  states without changing its message ID.
- Reuse the existing `input.ack` protocol: a delivered acknowledgement changes
  the placeholder to accepted; an unknown acknowledgement never claims
  success.
- Record one TTFT sample per Gateway turn, separately identifying the first
  reasoning and first text output and retaining bounded stage data.
- Document dashboard queries and review thresholds.

Cold-start acceleration and new AEP progress events are explicitly out of
scope. The existing input acknowledgement contract remains unchanged.

## Frontend design

The runtime adapter will hold the pending placeholder identity and its input
correlation key. `onNew` appends the user message and the streaming assistant
placeholder in the same synchronous update. The placeholder has no content;
the presentation layer renders the localized stage text from metadata rather
than putting a visible status string into model content.

`input.ack`, `message.start`, reasoning, and text-delta handlers locate that
placeholder and update it in place. The first worker-originated output moves
the placeholder into output state. Acknowledgements remain idempotent by
client-message/execution identity and cannot regress an output state.

`done`, an error, an unknown delivery outcome, and terminal reconnect paths
end or remove the placeholder so it cannot remain streaming indefinitely.
Output that arrives before its acknowledgement owns the visible state; a late
acknowledgement does not create another message.

Both locale trees receive identical keys for the `thinking` and `accepted`
states.

## Server observability design

Gateway maintains a short-lived per-turn record keyed by execution ID. It
captures timestamps at input receipt, durable acceptance, Worker.Input
success, first reasoning, first text, and terminal completion. The bridge
forwarder supplies first-output timestamps; the input handler supplies ingress
and dispatch timestamps.

The primary `hotplex.turn.ttft` histogram measures Gateway receipt to the
first user-visible worker output. Its bounded dimensions distinguish output
kind (`reasoning` or `text`), worker type, and cold/warm classification. A
separate bounded stage metric or structured trace records the durations for
admission, dispatch, and first output. Completed turns without output increase
a separate counter by terminal class and never write a successful TTFT sample.

No session ID, execution ID, user ID, workspace ID, prompt, metadata value, or
raw worker error appears as a metric label.

## Error handling and lifecycle

The tracker records only the first event of each phase, making multiple deltas
and reconnect replays harmless. Terminal handling removes the record after
emitting its single applicable result. If the record is absent, normal event
forwarding continues and no synthetic sample is emitted.

## Testing and documentation

Frontend tests cover delta-first, message-start-first, reasoning-first,
delivered/unknown/duplicate acknowledgements, output-before-ack, error, and
reconnect behavior. Gateway tests cover a single sample across repeated deltas,
reasoning-before-text, error/completion before output, and bounded attributes.

`docs/reference/metrics.md` documents the metrics, sample PromQL for p50/p95/
p99 TTFT, and review thresholds. A p95 regression or sustained p99 breach is
an investigation trigger; cold-path optimization is deferred until production
stage data identifies it as material.

# Service Lifecycle Broadcast Design

Date: 2026-08-24
Status: approved for implementation

## Objective

Notify every currently connected external messaging conversation when the HotPlex Gateway is about to stop, and notify the same conversations after the next successful Gateway startup.

The user-visible messages are fixed for the first version:

- stopping: `⚠️ HotPlex 服务即将停止。`
- started: `✅ HotPlex 服务已启动。`

This feature is a best-effort operational notice. Delivery failure must never prevent or materially delay service shutdown or startup.

## Scope

In scope:

- controlled Gateway shutdown caused by `SIGINT`, `SIGTERM`, or the existing `stopCh` path;
- restart paths that eventually use controlled shutdown, including `hotplex service restart`, `hotplex gateway restart --detached`, and the Admin restart callback;
- Slack, Feishu, and Yuanxin adapters;
- all external messaging sessions that have an active platform connection when shutdown begins;
- restoring exactly that recipient set after the next Gateway startup;
- multi-Bot routing and recipient deduplication.

Out of scope:

- WebChat notifications; WebChat already observes disconnect and reconnect directly;
- notifications for crashes, `SIGKILL`, power loss, or startup failure before adapters are ready;
- broadcasting to historical conversations that were not connected at shutdown time;
- configuring custom message text or fixed operational channels;
- guaranteed delivery, persistent retry queues, or delivery receipts;
- AEP protocol changes, database migrations, or new Admin API fields.

## Current constraints

The Gateway starts messaging adapters in `startMessagingAdapters` and registers each running Bot in `messaging.DefaultBotRegistry`. Shutdown currently closes the Hub before closing adapters, so the stopping notice must run before context cancellation and before `shutdownGateway` begins.

Platform adapters already support direct outbound delivery through the cron-specific `CronResultSender` contract, but lifecycle code must not depend on cron terminology. In addition, routing only by platform is ambiguous when more than one Bot is configured for Slack or Feishu.

`session.SessionInfo` already persists `Platform`, `BotID`, `BotName`, and `PlatformKey`. Those fields are sufficient to restore a recipient after restart. The Hub can identify whether a session currently has a connection, while the session manager can provide the corresponding routing metadata.

## Design principles

1. Notify only conversations that are connected when controlled shutdown begins.
2. Use the same recipient snapshot for the stopping and started notices.
3. Persist only opaque session IDs across the process boundary; do not copy channel metadata, credentials, or Yuanxin secrets into a lifecycle file.
4. Select the exact Bot that owns each session; never choose the first adapter for a platform.
5. Make delivery bounded and best-effort so lifecycle operations remain authoritative.
6. Consume the startup snapshot at most once to avoid duplicate broadcasts after repeated startup failures.
7. Keep the change additive: no database, configuration, AEP, or external API migration.

## Recipient discovery and deduplication

Before controlled shutdown, the Gateway builds a recipient snapshot from in-memory runtime state:

1. call `SessionManager.ListActive()`;
2. keep sessions whose `Platform` is Slack, Feishu, or Yuanxin;
3. keep only sessions for which `Hub.HasActiveConn(sessionID)` is true;
4. discard sessions without the routing keys required by their platform;
5. deduplicate logical targets and retain one representative `session_id` for each target.

The deduplication key is secret-free and includes the Bot identity:

| Platform | Deduplication fields |
|---|---|
| Slack | platform, Bot name or Bot ID, team ID, channel ID, thread timestamp |
| Feishu | platform, Bot name or Bot ID, chat ID, thread timestamp |
| Yuanxin | platform, Bot name or Bot ID, message ID, reply user codes, system ID |

The user ID is deliberately excluded. Multiple users can have separate HotPlex sessions in the same group, channel, or thread; including the user ID would send duplicate lifecycle messages to the same visible conversation.

If no eligible target exists, the Gateway removes any stale pending snapshot and continues shutdown without sending a notice.

## Cross-process snapshot

The snapshot is stored at:

```text
$HOTPLEX_HOME/.pids/gateway.lifecycle-broadcast.json
```

Its schema is intentionally narrow:

```json
{
  "version": 1,
  "created_at": "2026-08-24T12:00:00Z",
  "session_ids": ["session-uuid"]
}
```

Requirements:

- write a temporary file in the same directory, set owner-only permissions where supported, then atomically rename it into place;
- read through a 64 KiB size limit, then validate the version, timestamp, list size, and session ID syntax;
- reject a snapshot containing more session IDs than the normalized `pool.max_size` value;
- treat malformed snapshots or snapshots older than 24 hours as non-fatal, log a sanitized warning, and remove them;
- never persist `PlatformKey`, raw channel metadata, message text, tokens, or credentials in this file.

The snapshot is written before the stopping broadcast. This preserves the recipient set even when the old process is terminated while a platform send is in flight.

## Generic outbound contract

Add an internal, platform-neutral capability beside the existing adapter interface:

```go
type ProactiveMessageSender interface {
	SendProactiveMessage(ctx context.Context, text string, platformKey map[string]string) error
}
```

Slack, Feishu, and Yuanxin implement this method with their existing sanitized outbound paths. Existing `SendCronResult` methods remain for compatibility and delegate to the generic method. This is additive and avoids making lifecycle broadcasting depend on cron semantics.

Adapter selection uses the runtime Bot registry:

1. resolve by `(platform, BotName)` when `BotName` is present;
2. otherwise resolve the running entry with the matching `BotID`;
3. only for legacy single-Bot sessions with neither value, use the sole running Bot for that platform;
4. if selection is missing or ambiguous, skip the target and log the routing failure.

No caller may route by platform alone in a multi-Bot configuration.

## Lifecycle flow

### Controlled shutdown

The signal loop distinguishes controlled shutdown from a server startup/runtime error. On `SIGINT`, `SIGTERM`, or `stopCh`:

1. collect and deduplicate active external messaging targets;
2. atomically persist their representative session IDs;
3. broadcast the stopping message with a five-second lifecycle-owned timeout and at most eight concurrent sends;
4. log the aggregate result;
5. cancel the Gateway context and execute the existing ordered shutdown.

`SIGHUP` remains a reload operation and emits no lifecycle notice. A server error proceeds directly to shutdown because the Gateway cannot truthfully advertise an intentional service stop.

The stopping broadcast is synchronous only within its bounded timeout. Individual target failures do not stop remaining sends and never change the shutdown result.

### Startup

After stores, Hub, bridges, messaging adapters, HTTP routes, and server goroutines have been initialized, the Gateway consumes the pending snapshot:

1. atomically rename the pending file to a process-owned `.claimed` path so another startup cannot consume it concurrently;
2. parse and validate the snapshot;
3. resolve each session through `SessionManager.Get` to recover platform, Bot, and routing metadata;
4. deduplicate again defensively;
5. route through the selected `ProactiveMessageSender`;
6. broadcast the started message with a five-second lifecycle-owned timeout and at most eight concurrent sends;
7. remove the claimed snapshot and log the aggregate result.

Claim-before-send gives at-most-once startup attempts. A crash after claim can lose the started notice, but it cannot cause repeated messages on every subsequent boot. This trade-off matches the feature's best-effort contract and protects users from notification spam.

A normal cold start without a pending snapshot emits no broadcast. A controlled stop followed by a later manual start still completes the stop/start notification pair, provided the snapshot has not exceeded its validity window.

## Failure handling

All failures are fail-open with respect to service lifecycle:

| Failure | Behavior |
|---|---|
| Snapshot write fails | Log warning, attempt stopping notice, continue shutdown; no started notice is possible |
| Target session disappears | Skip target and continue |
| Bot is disabled or fails startup | Skip target and continue |
| Platform API rejects a send | Log sanitized error and continue |
| Overall delivery timeout expires | Cancel outstanding sends and continue lifecycle |
| Snapshot is malformed or stale | Remove it, log warning, do not broadcast |
| Process crashes before controlled shutdown | No snapshot and no lifecycle broadcast |

Logs use structured snake_case fields such as `phase`, `platform`, `bot_name`, `target_count`, `sent_count`, `failed_count`, and `duration_ms`. Logs must not include message-channel secrets or complete `PlatformKey` maps.

No persistent retry queue or new metric is required in the first version. Existing service logs are sufficient to diagnose best-effort notification failures.

## Component boundaries

The implementation should keep orchestration close to Gateway lifecycle wiring while isolating pure logic for tests:

- `internal/messaging/platform_interfaces.go`: additive proactive sender contract;
- Slack, Feishu, and Yuanxin adapters: generic proactive-send implementations, with cron delivery delegating to them;
- a focused lifecycle broadcaster module: target validation, deduplication, Bot resolution, snapshot read/write/claim, and bounded fan-out;
- `cmd/hotplex/gateway_run.go`: invoke the broadcaster at the two lifecycle seams only;
- `cmd/hotplex/messaging_init.go`: no new routing policy; the existing Bot registry remains the runtime source of adapter identity.

The broadcaster accepts narrow interfaces for session lookup, active-connection checks, and Bot resolution. Tests can therefore use fakes without constructing a complete Gateway.

## Test strategy

Unit tests cover:

- target filtering for Slack, Feishu, Yuanxin, and WebChat;
- deduplication across multiple user sessions in the same visible conversation;
- preservation of distinct Slack or Feishu threads;
- exact Bot selection in multi-Bot configurations;
- legacy single-Bot fallback and ambiguous-Bot rejection;
- snapshot atomic write, owner-only permissions where supported, validation, stale cleanup, and claim-once behavior;
- absence of platform keys, credentials, and raw message metadata in the snapshot;
- proactive sender sanitization and `SendCronResult` delegation for all three adapters;
- per-target failure isolation and overall timeout handling.

Gateway lifecycle tests cover:

- stopping snapshot and broadcast occur before context cancellation and Hub shutdown;
- `SIGINT`, `SIGTERM`, and `stopCh` produce a stopping notice;
- `SIGHUP` and server errors do not produce a lifecycle notice;
- a startup with a valid snapshot sends one started notice per deduplicated target;
- a cold start sends no started notice;
- a claimed snapshot is not retried on the next startup;
- notification failures do not change the Gateway's stop or startup result.

Focused adapter tests verify the required platform keys and outbound API calls. Race-enabled tests cover snapshot consumption and simultaneous lifecycle signals without using `time.Sleep` for synchronization.

## Documentation impact

Implementation updates should document the best-effort broadcast behavior in the service lifecycle and CLI references. Release notes should state that controlled service stop/restart notifies currently connected external messaging conversations. No configuration reference change is needed.

## Rollout and rollback

The feature is enabled unconditionally because it has no configuration or schema dependency. Rollout requires one Gateway binary containing both snapshot production and consumption.

Rollback is safe:

- an older binary ignores the lifecycle snapshot file;
- the file contains no credentials and may be removed manually if necessary;
- adapter interfaces are additive;
- no database or AEP rollback is required.

## Acceptance criteria

1. A controlled Gateway stop sends exactly one stopping notice to each connected Slack, Feishu, or Yuanxin visible conversation, subject to best-effort platform delivery.
2. The next successful Gateway startup sends exactly one started notice to the same deduplicated recipient set.
3. Multiple HotPlex sessions for different users in the same visible conversation do not produce duplicate notices.
4. Multi-Bot deployments always send through the Bot that owns the session.
5. WebChat, historical disconnected sessions, cold starts, reloads, crashes, and server errors do not produce lifecycle broadcasts.
6. Snapshot files contain only version, timestamp, and opaque session IDs; no routing metadata or credentials are persisted.
7. Broadcast failures and timeouts never prevent or change the outcome of service stop, restart, or startup.
8. Existing cron delivery behavior remains compatible after introduction of the generic proactive sender.
9. No database migration, AEP change, or operator configuration is required.

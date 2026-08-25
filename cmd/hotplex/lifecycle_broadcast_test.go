package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/session"
)

func TestLifecycleTargets_FilterAndDeduplicate(t *testing.T) {
	t.Parallel()

	slackID := uuid.NewString()
	sessions := []*session.SessionInfo{
		{
			ID:       slackID,
			Platform: "slack",
			BotName:  "ops",
			UserID:   "user-1",
			PlatformKey: map[string]string{
				"team_id":    "team-1",
				"channel_id": "channel-1",
				"thread_ts":  "thread-1",
				"user_id":    "user-1",
			},
		},
		{
			ID:       uuid.NewString(),
			Platform: "slack",
			BotName:  "ops",
			UserID:   "user-2",
			PlatformKey: map[string]string{
				"team_id":    "team-1",
				"channel_id": "channel-1",
				"thread_ts":  "thread-1",
				"user_id":    "user-2",
			},
		},
		{
			ID:       uuid.NewString(),
			Platform: "slack",
			BotName:  "ops",
			PlatformKey: map[string]string{
				"team_id":    "team-1",
				"channel_id": "channel-1",
				"thread_ts":  "thread-2",
			},
		},
		{
			ID:       uuid.NewString(),
			Platform: "feishu",
			BotName:  "assistant",
			PlatformKey: map[string]string{
				"chat_id": "chat-disconnected",
			},
		},
		{
			ID:       uuid.NewString(),
			Platform: "webchat",
			PlatformKey: map[string]string{
				"channel_id": "web-1",
			},
		},
		{
			ID:       uuid.NewString(),
			Platform: "yuanxin",
			BotID:    "yuanxin-app",
			PlatformKey: map[string]string{
				"message_id":       "message-1",
				"reply_user_codes": "user-code",
				"sys_id":           "sys-1",
			},
		},
	}
	active := map[string]bool{
		sessions[0].ID: true,
		sessions[1].ID: true,
		sessions[2].ID: true,
		sessions[4].ID: true,
		sessions[5].ID: true,
	}

	targets := collectLifecycleTargets(sessions, func(id string) bool { return active[id] })

	require.Len(t, targets, 3)
	keys := make(map[string]bool, len(targets))
	for _, target := range targets {
		key, ok := lifecycleTargetKey(target)
		require.True(t, ok)
		keys[key] = true
	}
	require.Len(t, keys, 3)
	require.Contains(t, []string{sessions[0].ID, sessions[1].ID}, targets[0].ID)
	targets[0].PlatformKey["copy_probe"] = "set-on-copy"
	for _, original := range sessions {
		if original.ID == targets[0].ID {
			require.NotContains(t, original.PlatformKey, "copy_probe")
		}
	}
}

func TestLifecycleSnapshot_SaveClaimAndComplete(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "gateway.lifecycle-broadcast.json")
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	store := newLifecycleSnapshotStore(path, 4, func() time.Time { return now })
	ids := []string{uuid.NewString(), uuid.NewString()}

	require.NoError(t, store.Save(ids))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &fields))
	require.ElementsMatch(t, []string{"version", "created_at", "session_ids"}, mapKeys(fields))
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(path)
		require.NoError(t, statErr)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	temps, err := filepath.Glob(filepath.Join(dir, ".gateway.lifecycle-broadcast.json.tmp-*"))
	require.NoError(t, err)
	require.Empty(t, temps)

	snapshot, claimedPath, err := store.Claim()
	require.NoError(t, err)
	require.Equal(t, ids, snapshot.SessionIDs)
	require.NoFileExists(t, path)
	require.FileExists(t, claimedPath)

	second, secondPath, err := store.Claim()
	require.NoError(t, err)
	require.Nil(t, second)
	require.Empty(t, secondPath)

	require.NoError(t, store.CompleteClaim(claimedPath))
	require.NoFileExists(t, claimedPath)
}

func TestLifecycleSnapshot_EmptySaveRemovesPending(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gateway.lifecycle-broadcast.json")
	store := newLifecycleSnapshotStore(path, 4, time.Now)
	require.NoError(t, store.Save([]string{uuid.NewString()}))
	require.NoError(t, os.WriteFile(path+lifecycleSnapshotClaimedSuffix, []byte("stale"), 0o600))

	require.NoError(t, store.Save(nil))

	require.NoFileExists(t, path)
	require.NoFileExists(t, path+lifecycleSnapshotClaimedSuffix)
}

func TestLifecycleSnapshot_RejectsInvalidInput(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	validID := uuid.NewString()
	tests := []struct {
		name string
		body string
	}{
		{
			name: "too large",
			body: strings.Repeat("x", lifecycleSnapshotMaxBytes+1),
		},
		{
			name: "unknown field",
			body: `{"version":1,"created_at":"2026-08-24T12:00:00Z","session_ids":["` + validID + `"],"platform_key":{"secret":"x"}}`,
		},
		{
			name: "wrong version",
			body: `{"version":2,"created_at":"2026-08-24T12:00:00Z","session_ids":["` + validID + `"]}`,
		},
		{
			name: "stale",
			body: `{"version":1,"created_at":"2026-08-23T11:59:59Z","session_ids":["` + validID + `"]}`,
		},
		{
			name: "invalid uuid",
			body: `{"version":1,"created_at":"2026-08-24T12:00:00Z","session_ids":["not-a-uuid"]}`,
		},
		{
			name: "too many ids",
			body: `{"version":1,"created_at":"2026-08-24T12:00:00Z","session_ids":["` + validID + `","` + uuid.NewString() + `"]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "gateway.lifecycle-broadcast.json")
			require.NoError(t, os.WriteFile(path, []byte(tt.body), 0o600))
			store := newLifecycleSnapshotStore(path, 1, func() time.Time { return now })

			snapshot, claimedPath, err := store.Claim()

			require.Error(t, err)
			require.Nil(t, snapshot)
			require.Empty(t, claimedPath)
			require.NoFileExists(t, path)
		})
	}
}

func TestLifecycleRouting_ResolvesOwningBot(t *testing.T) {
	t.Parallel()

	named := &lifecycleFakeAdapter{platform: messaging.PlatformSlack, botName: "named", botID: "bot-named"}
	byID := &lifecycleFakeAdapter{platform: messaging.PlatformSlack, botName: "other", botID: "bot-id"}
	registry := &lifecycleFakeBotRegistry{entries: []*messaging.BotEntry{
		{Name: "named", Platform: messaging.PlatformSlack, BotID: "bot-named", Status: messaging.BotStatusRunning, Adapter: named},
		{Name: "other", Platform: messaging.PlatformSlack, BotID: "bot-id", Status: messaging.BotStatusRunning, Adapter: byID},
	}}

	tests := []struct {
		name string
		si   *session.SessionInfo
		want messaging.ProactiveMessageSender
	}{
		{
			name: "bot name",
			si:   &session.SessionInfo{Platform: "slack", BotName: "named"},
			want: named,
		},
		{
			name: "bot id",
			si:   &session.SessionInfo{Platform: "slack", BotID: "bot-id"},
			want: byID,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveLifecycleSender(registry, tt.si)
			require.NoError(t, err)
			require.Same(t, tt.want, got)
		})
	}
}

func TestLifecycleRouting_UsesSoleRunningBotForLegacySession(t *testing.T) {
	t.Parallel()

	adapter := &lifecycleFakeAdapter{platform: messaging.PlatformFeishu, botName: "sole", botID: "bot-1"}
	registry := &lifecycleFakeBotRegistry{entries: []*messaging.BotEntry{
		{Name: "sole", Platform: messaging.PlatformFeishu, BotID: "bot-1", Status: messaging.BotStatusRunning, Adapter: adapter},
		{Name: "stopped", Platform: messaging.PlatformFeishu, BotID: "bot-2", Status: messaging.BotStatusStopped, Adapter: &lifecycleFakeAdapter{}},
	}}

	got, err := resolveLifecycleSender(registry, &session.SessionInfo{Platform: "feishu"})

	require.NoError(t, err)
	require.Same(t, adapter, got)
}

func TestLifecycleRouting_RejectsAmbiguousOrUnavailableBot(t *testing.T) {
	t.Parallel()

	proactive1 := &lifecycleFakeAdapter{platform: messaging.PlatformSlack, botName: "one", botID: "bot-1"}
	proactive2 := &lifecycleFakeAdapter{platform: messaging.PlatformSlack, botName: "two", botID: "bot-2"}
	registry := &lifecycleFakeBotRegistry{entries: []*messaging.BotEntry{
		{Name: "one", Platform: messaging.PlatformSlack, BotID: "bot-1", Status: messaging.BotStatusRunning, Adapter: proactive1},
		{Name: "two", Platform: messaging.PlatformSlack, BotID: "bot-2", Status: messaging.BotStatusRunning, Adapter: proactive2},
		{Name: "stopped", Platform: messaging.PlatformSlack, BotID: "bot-3", Status: messaging.BotStatusStopped, Adapter: &lifecycleFakeAdapter{}},
		{Name: "legacy", Platform: messaging.PlatformFeishu, Status: messaging.BotStatusRunning, Adapter: &lifecycleNonProactiveAdapter{}},
	}}

	tests := []struct {
		name string
		si   *session.SessionInfo
	}{
		{name: "ambiguous", si: &session.SessionInfo{Platform: "slack"}},
		{name: "named missing", si: &session.SessionInfo{Platform: "slack", BotName: "missing"}},
		{name: "stopped id", si: &session.SessionInfo{Platform: "slack", BotID: "bot-3"}},
		{name: "non proactive", si: &session.SessionInfo{Platform: "feishu"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveLifecycleSender(registry, tt.si)
			require.Error(t, err)
			require.Nil(t, got)
		})
	}
}

func TestLifecycleBroadcast_StoppingSnapshotsBeforeSend(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gateway.lifecycle-broadcast.json")
	si := lifecycleSlackSession("ops", "channel-1")
	snapshotSeen := make(chan bool, 1)
	sentText := make(chan string, 1)
	adapter := &lifecycleFakeAdapter{
		platform: messaging.PlatformSlack,
		botName:  "ops",
		botID:    "bot-1",
		send: func(_ context.Context, text string, _ map[string]string) error {
			_, err := os.Stat(path)
			snapshotSeen <- err == nil
			sentText <- text
			return nil
		},
	}
	b := newTestLifecycleBroadcaster(path, []*session.SessionInfo{si}, adapter)

	summary := b.BroadcastStopping()

	require.True(t, <-snapshotSeen)
	require.Equal(t, lifecycleStoppingMessage, <-sentText)
	require.Equal(t, lifecycleBroadcastSummary{Phase: lifecyclePhaseStopping, TargetCount: 1, SentCount: 1}, summary)
	require.FileExists(t, path)
}

func TestLifecycleBroadcast_ConstructorUsesGatewayDependencies(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	b := newLifecycleBroadcaster(&GatewayDeps{
		Log: logger,
		Config: &config.Config{
			Pool: config.PoolConfig{MaxSize: 17},
		},
	})

	require.Same(t, logger, b.log)
	require.Equal(t, 17, b.snapshots.maxTargets)
	require.Equal(t, lifecycleBroadcastTimeout, b.timeout)
	require.Equal(t, lifecycleBroadcastConcurrency, b.concurrency)
}

func TestLifecycleBroadcast_StartedClaimsOnceAndRestoresSession(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gateway.lifecycle-broadcast.json")
	si := lifecycleSlackSession("ops", "channel-1")
	sentText := make(chan string, 2)
	adapter := &lifecycleFakeAdapter{
		platform: messaging.PlatformSlack,
		botName:  "ops",
		botID:    "bot-1",
		send: func(_ context.Context, text string, _ map[string]string) error {
			sentText <- text
			return nil
		},
	}
	b := newTestLifecycleBroadcaster(path, nil, adapter)
	b.sessions.(*lifecycleFakeSessions).byID[si.ID] = si
	require.NoError(t, b.snapshots.Save([]string{si.ID}))

	summary := b.BroadcastStarted()
	second := b.BroadcastStarted()

	require.Equal(t, lifecycleStartedMessage, <-sentText)
	require.Equal(t, lifecycleBroadcastSummary{Phase: lifecyclePhaseStarted, TargetCount: 1, SentCount: 1}, summary)
	require.Equal(t, lifecycleBroadcastSummary{Phase: lifecyclePhaseStarted}, second)
	require.NoFileExists(t, path)
	require.NoFileExists(t, path+lifecycleSnapshotClaimedSuffix)
	select {
	case duplicate := <-sentText:
		require.Failf(t, "duplicate startup message", "got %q", duplicate)
	default:
	}
}

func TestLifecycleBroadcast_IsolatesTargetFailure(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gateway.lifecycle-broadcast.json")
	sessions := []*session.SessionInfo{
		lifecycleSlackSession("ops", "channel-ok"),
		lifecycleSlackSession("ops", "channel-fail"),
	}
	adapter := &lifecycleFakeAdapter{
		platform: messaging.PlatformSlack,
		botName:  "ops",
		botID:    "bot-1",
		send: func(_ context.Context, _ string, platformKey map[string]string) error {
			if platformKey["channel_id"] == "channel-fail" {
				return errors.New("platform rejected send")
			}
			return nil
		},
	}
	b := newTestLifecycleBroadcaster(path, sessions, adapter)

	summary := b.BroadcastStopping()

	require.Equal(t, 2, summary.TargetCount)
	require.Equal(t, 1, summary.SentCount)
	require.Equal(t, 1, summary.FailedCount)
}

func TestLifecycleBroadcast_RestartReceiptCoversNoSessionTarget(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	var audit bytes.Buffer
	sentText := make(chan string, 2)
	adapter := &lifecycleFakeAdapter{
		platform: messaging.PlatformFeishu,
		botName:  "ops",
		botID:    "bot-1",
		send: func(_ context.Context, text string, _ map[string]string) error {
			sentText <- text
			return nil
		},
	}
	b := newTestLifecycleBroadcaster(filepath.Join(dir, "snapshot.json"), nil, adapter)
	b.log = slog.New(slog.NewJSONHandler(&audit, nil))
	b.receipts = newRestartReceiptStore(filepath.Join(dir, "receipt.json"))
	receipt := &gatewayRestartReceipt{
		SchemaVersion: gatewayRestartReceiptSchemaVersion,
		RequestID:     "req_0123456789abcdef0123456789abcdef",
		Platform:      string(messaging.PlatformFeishu),
		Actor:         "ou_actor",
		BotName:       "ops",
		PlatformKey:   map[string]string{"chat_id": "oc_chat", "message_id": "om_message"},
		RequestedAt:   time.Date(2026, 8, 25, 1, 2, 3, 0, time.UTC),
		OldVersion:    "v1.42.1",
		OldPID:        4321,
	}
	require.NoError(t, b.receipts.Write(receipt))
	b.buildInfo = BuildInfo{Version: "v1.43.0", BuildTime: "2026-08-25", OS: "darwin", Arch: "arm64"}

	stopping := b.BroadcastStopping()
	require.Equal(t, 1, stopping.TargetCount)
	require.Equal(t, 1, stopping.SentCount)
	stoppingText := <-sentText
	require.Contains(t, stoppingText, "Gateway 即将重启")
	require.Contains(t, stoppingText, receipt.RequestID)

	started := b.BroadcastStarted()
	require.Equal(t, 1, started.TargetCount)
	require.Equal(t, 1, started.SentCount)
	startedText := <-sentText
	require.Contains(t, startedText, "上一版本")
	require.Contains(t, startedText, receipt.RequestID)
	require.Contains(t, audit.String(), `"actor":"ou_actor"`)
	_, err := os.Stat(b.receipts.path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestLifecycleBroadcast_RestartReceiptSurvivesSendFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	adapter := &lifecycleFakeAdapter{
		platform: messaging.PlatformFeishu,
		botName:  "ops",
		botID:    "bot-1",
		send: func(context.Context, string, map[string]string) error {
			return errors.New("send failed")
		},
	}
	b := newTestLifecycleBroadcaster(filepath.Join(dir, "snapshot.json"), nil, adapter)
	b.receipts = newRestartReceiptStore(filepath.Join(dir, "receipt.json"))
	receipt := &gatewayRestartReceipt{
		SchemaVersion: gatewayRestartReceiptSchemaVersion,
		RequestID:     "req_0123456789abcdef0123456789abcdef",
		Platform:      string(messaging.PlatformFeishu),
		BotName:       "ops",
		PlatformKey:   map[string]string{"chat_id": "oc_chat"},
		RequestedAt:   time.Now().UTC(),
		OldVersion:    "v1.42.1",
		OldPID:        4321,
	}
	require.NoError(t, b.receipts.Write(receipt))

	summary := b.BroadcastStarted()
	require.Equal(t, 1, summary.FailedCount)
	remaining, err := b.receipts.Read()
	require.NoError(t, err)
	require.Equal(t, receipt.RequestID, remaining.RequestID)
}

func TestLifecycleBroadcast_RestartReceiptCompletesDespiteUnrelatedTargetFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sessionInfo := &session.SessionInfo{
		ID:       uuid.NewString(),
		Platform: string(messaging.PlatformFeishu),
		BotName:  "ops",
		BotID:    "bot-1",
		PlatformKey: map[string]string{
			"chat_id": "oc_unrelated_failure",
		},
	}
	adapter := &lifecycleFakeAdapter{
		platform: messaging.PlatformFeishu,
		botName:  "ops",
		botID:    "bot-1",
		send: func(_ context.Context, _ string, platformKey map[string]string) error {
			if platformKey["chat_id"] == "oc_unrelated_failure" {
				return errors.New("unrelated target failed")
			}
			return nil
		},
	}
	b := newTestLifecycleBroadcaster(filepath.Join(dir, "snapshot.json"), []*session.SessionInfo{sessionInfo}, adapter)
	b.receipts = newRestartReceiptStore(filepath.Join(dir, "receipt.json"))
	receipt := testGatewayRestartReceipt("req_0123456789abcdef0123456789abcdef")
	receipt.PlatformKey = map[string]string{"chat_id": "oc_receipt"}
	require.NoError(t, b.receipts.Write(receipt))
	require.NoError(t, b.snapshots.Save([]string{sessionInfo.ID}))

	summary := b.BroadcastStarted()
	require.Equal(t, 1, summary.SentCount)
	require.Equal(t, 1, summary.FailedCount)
	require.NoFileExists(t, b.receipts.path)
}

func TestLifecycleBroadcast_RetriesRestartReceiptWithBoundedAttempts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	var attempts atomic.Int32
	adapter := &lifecycleFakeAdapter{
		platform: messaging.PlatformFeishu,
		botName:  "ops",
		botID:    "bot-1",
		send: func(context.Context, string, map[string]string) error {
			if attempts.Add(1) < lifecycleRestartReceiptMaxAttempts {
				return errors.New("transient send failure")
			}
			return nil
		},
	}
	b := newTestLifecycleBroadcaster(filepath.Join(dir, "snapshot.json"), nil, adapter)
	b.receipts = newRestartReceiptStore(filepath.Join(dir, "receipt.json"))
	receipt := testGatewayRestartReceipt("req_0123456789abcdef0123456789abcdef")
	require.NoError(t, b.receipts.Write(receipt))

	summary := b.BroadcastStarted()
	require.Equal(t, int32(lifecycleRestartReceiptMaxAttempts), attempts.Load())
	require.Equal(t, 1, summary.SentCount)
	require.Zero(t, summary.FailedCount)
	require.NoFileExists(t, b.receipts.path)
}

func TestLifecycleBroadcast_TimeoutDoesNotBlock(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gateway.lifecycle-broadcast.json")
	adapter := &lifecycleFakeAdapter{
		platform: messaging.PlatformSlack,
		botName:  "ops",
		botID:    "bot-1",
		send: func(ctx context.Context, _ string, _ map[string]string) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	b := newTestLifecycleBroadcaster(path, []*session.SessionInfo{lifecycleSlackSession("ops", "channel-1")}, adapter)
	b.timeout = 20 * time.Millisecond

	startedAt := time.Now()
	summary := b.BroadcastStopping()

	require.Less(t, time.Since(startedAt), time.Second)
	require.Equal(t, 1, summary.FailedCount)
}

func TestLifecycleBroadcast_TimeoutDoesNotWaitForSenderCancellation(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gateway.lifecycle-broadcast.json")
	release := make(chan struct{})
	defer close(release)
	adapter := &lifecycleFakeAdapter{
		platform: messaging.PlatformSlack,
		botName:  "ops",
		botID:    "bot-1",
		send: func(context.Context, string, map[string]string) error {
			<-release
			return nil
		},
	}
	b := newTestLifecycleBroadcaster(path, []*session.SessionInfo{lifecycleSlackSession("ops", "channel-1")}, adapter)
	b.timeout = 20 * time.Millisecond
	done := make(chan lifecycleBroadcastSummary, 1)
	go func() { done <- b.BroadcastStopping() }()

	select {
	case summary := <-done:
		require.Equal(t, 1, summary.FailedCount)
	case <-time.After(250 * time.Millisecond):
		require.Fail(t, "broadcast waited for a sender that ignored context cancellation")
	}
}

func TestLifecycleBroadcast_LimitsConcurrentSends(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gateway.lifecycle-broadcast.json")
	sessions := make([]*session.SessionInfo, 0, 16)
	for i := range 16 {
		sessions = append(sessions, lifecycleSlackSession("ops", "channel-"+string(rune('a'+i))))
	}
	started := make(chan struct{}, len(sessions))
	release := make(chan struct{})
	var current atomic.Int32
	var maximum atomic.Int32
	adapter := &lifecycleFakeAdapter{
		platform: messaging.PlatformSlack,
		botName:  "ops",
		botID:    "bot-1",
		send: func(ctx context.Context, _ string, _ map[string]string) error {
			active := current.Add(1)
			defer current.Add(-1)
			for {
				observed := maximum.Load()
				if active <= observed || maximum.CompareAndSwap(observed, active) {
					break
				}
			}
			started <- struct{}{}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	b := newTestLifecycleBroadcaster(path, sessions, adapter)
	done := make(chan lifecycleBroadcastSummary, 1)
	go func() { done <- b.BroadcastStopping() }()

	for range lifecycleBroadcastConcurrency {
		select {
		case <-started:
		case <-time.After(time.Second):
			require.FailNow(t, "timed out waiting for bounded sends")
		}
	}
	require.Equal(t, int32(lifecycleBroadcastConcurrency), maximum.Load())
	select {
	case <-started:
		require.FailNow(t, "send concurrency exceeded limit")
	default:
	}
	close(release)

	summary := <-done
	require.Equal(t, len(sessions), summary.SentCount)
	require.LessOrEqual(t, maximum.Load(), int32(lifecycleBroadcastConcurrency))
}

func TestGatewayLifecycle_ControlledShutdownOrder(t *testing.T) {
	t.Parallel()

	order := make([]string, 0, 3)
	notifier := lifecycleStopRecorder{record: func() { order = append(order, "broadcast") }}

	runGatewayControlledShutdown(
		notifier,
		func() { order = append(order, "cancel") },
		func() { order = append(order, "shutdown") },
	)

	require.Equal(t, []string{"broadcast", "cancel", "shutdown"}, order)
}

func TestGatewayLifecycle_HTTPServerBindFailureIsSynchronous(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("address already in use")
	serverErr := make(chan error, 1)
	err := startGatewayHTTPServer(
		&http.Server{Addr: "127.0.0.1:1"},
		serverErr,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		"gateway: server failed",
		func(string, string) (net.Listener, error) { return nil, wantErr },
	)

	require.ErrorIs(t, err, wantErr)
	select {
	case asyncErr := <-serverErr:
		require.Failf(t, "bind failure was reported asynchronously", "got %v", asyncErr)
	default:
	}
}

type lifecycleFakeBotRegistry struct {
	entries []*messaging.BotEntry
}

func (r *lifecycleFakeBotRegistry) Get(platform messaging.PlatformType, name string) (*messaging.BotEntry, bool) {
	for _, entry := range r.entries {
		if entry.Platform == platform && entry.Name == name {
			return entry, true
		}
	}
	return nil, false
}

func (r *lifecycleFakeBotRegistry) ListByPlatform(platform messaging.PlatformType) []*messaging.BotEntry {
	var entries []*messaging.BotEntry
	for _, entry := range r.entries {
		if entry.Platform == platform {
			entries = append(entries, entry)
		}
	}
	return entries
}

type lifecycleFakeAdapter struct {
	platform messaging.PlatformType
	botID    string
	botName  string
	send     func(context.Context, string, map[string]string) error
}

func (a *lifecycleFakeAdapter) Platform() messaging.PlatformType { return a.platform }
func (a *lifecycleFakeAdapter) Start(context.Context) error      { return nil }
func (a *lifecycleFakeAdapter) HandleTextMessage(context.Context, string, string, string, string, string, string) error {
	return nil
}
func (a *lifecycleFakeAdapter) Close(context.Context) error                 { return nil }
func (a *lifecycleFakeAdapter) ConfigureWith(messaging.AdapterConfig) error { return nil }
func (a *lifecycleFakeAdapter) GetBotID() string                            { return a.botID }
func (a *lifecycleFakeAdapter) GetBotName() string                          { return a.botName }
func (a *lifecycleFakeAdapter) GetInjectExclude() []string                  { return nil }

func (a *lifecycleFakeAdapter) SendProactiveMessage(ctx context.Context, text string, platformKey map[string]string) error {
	if a.send != nil {
		return a.send(ctx, text, platformKey)
	}
	return nil
}

type lifecycleNonProactiveAdapter struct {
	messaging.PlatformAdapterInterface
}

type lifecycleStopRecorder struct {
	record func()
}

func (r lifecycleStopRecorder) BroadcastStopping() lifecycleBroadcastSummary {
	if r.record != nil {
		r.record()
	}
	return lifecycleBroadcastSummary{Phase: lifecyclePhaseStopping}
}

type lifecycleFakeSessions struct {
	active []*session.SessionInfo
	byID   map[string]*session.SessionInfo
}

func (s *lifecycleFakeSessions) ListActive() []*session.SessionInfo {
	return s.active
}

func (s *lifecycleFakeSessions) Get(_ context.Context, id string) (*session.SessionInfo, error) {
	si, ok := s.byID[id]
	if !ok {
		return nil, session.ErrSessionNotFound
	}
	return si, nil
}

type lifecycleFakeConnections struct {
	active map[string]bool
}

func (c *lifecycleFakeConnections) HasActiveConn(id string) bool {
	return c.active[id]
}

func newTestLifecycleBroadcaster(path string, active []*session.SessionInfo, adapter *lifecycleFakeAdapter) *lifecycleBroadcaster {
	byID := make(map[string]*session.SessionInfo, len(active))
	connections := make(map[string]bool, len(active))
	for _, si := range active {
		byID[si.ID] = si
		connections[si.ID] = true
	}
	return &lifecycleBroadcaster{
		log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		sessions:    &lifecycleFakeSessions{active: active, byID: byID},
		connections: &lifecycleFakeConnections{active: connections},
		bots: &lifecycleFakeBotRegistry{entries: []*messaging.BotEntry{
			{Name: adapter.botName, Platform: adapter.platform, BotID: adapter.botID, Status: messaging.BotStatusRunning, Adapter: adapter},
		}},
		snapshots:   newLifecycleSnapshotStore(path, 32, time.Now),
		timeout:     lifecycleBroadcastTimeout,
		concurrency: lifecycleBroadcastConcurrency,
	}
}

func lifecycleSlackSession(botName, channelID string) *session.SessionInfo {
	return &session.SessionInfo{
		ID:       uuid.NewString(),
		Platform: string(messaging.PlatformSlack),
		BotName:  botName,
		BotID:    "bot-1",
		PlatformKey: map[string]string{
			"team_id":    "team-1",
			"channel_id": channelID,
		},
	}
}

func mapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func TestLifecycleTargets_RejectMissingRoutingKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		platform string
		key      map[string]string
	}{
		{name: "slack", platform: "slack", key: map[string]string{"team_id": "team"}},
		{name: "feishu", platform: "feishu", key: map[string]string{"thread_ts": "thread"}},
		{name: "yuanxin", platform: "yuanxin", key: map[string]string{"sys_id": "sys"}},
		{name: "webchat", platform: "webchat", key: map[string]string{"channel_id": "channel"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, ok := lifecycleTargetKey(&session.SessionInfo{
				ID:          uuid.NewString(),
				Platform:    tt.platform,
				BotID:       "bot",
				PlatformKey: tt.key,
			})
			require.False(t, ok)
		})
	}
}

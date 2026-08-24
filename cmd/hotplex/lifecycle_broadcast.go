package main

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/session"
)

const (
	lifecycleTargetSeparator      = "\x00"
	lifecycleStoppingMessage      = "⚠️ HotPlex 服务即将停止。"
	lifecycleStartedMessage       = "✅ HotPlex 服务已启动。"
	lifecyclePhaseStopping        = "stopping"
	lifecyclePhaseStarted         = "started"
	lifecycleBroadcastTimeout     = 5 * time.Second
	lifecycleBroadcastConcurrency = 8
)

type lifecycleBotRegistry interface {
	Get(platform messaging.PlatformType, name string) (*messaging.BotEntry, bool)
	ListByPlatform(platform messaging.PlatformType) []*messaging.BotEntry
}

type lifecycleSessionSource interface {
	ListActive() []*session.SessionInfo
	Get(ctx context.Context, id string) (*session.SessionInfo, error)
}

type lifecycleConnectionChecker interface {
	HasActiveConn(sessionID string) bool
}

type lifecycleStopNotifier interface {
	BroadcastStopping() lifecycleBroadcastSummary
}

type lifecycleBroadcastSummary struct {
	Phase       string
	TargetCount int
	SentCount   int
	FailedCount int
}

type lifecycleBroadcaster struct {
	log         *slog.Logger
	sessions    lifecycleSessionSource
	connections lifecycleConnectionChecker
	bots        lifecycleBotRegistry
	snapshots   *lifecycleSnapshotStore
	timeout     time.Duration
	concurrency int
}

func newLifecycleBroadcaster(deps *GatewayDeps) *lifecycleBroadcaster {
	maxTargets := 1
	if deps != nil && deps.Config != nil && deps.Config.Pool.MaxSize > 0 {
		maxTargets = deps.Config.Pool.MaxSize
	}
	home := config.HotplexHome()
	b := &lifecycleBroadcaster{
		log:         slog.Default(),
		bots:        messaging.DefaultBotRegistry(),
		snapshots:   newLifecycleSnapshotStore(filepath.Join(home, ".pids", lifecycleSnapshotFilename), maxTargets, time.Now),
		timeout:     lifecycleBroadcastTimeout,
		concurrency: lifecycleBroadcastConcurrency,
	}
	if deps != nil {
		if deps.Log != nil {
			b.log = deps.Log
		}
		b.sessions = deps.SessionMgr
		b.connections = deps.Hub
	}
	return b
}

func runGatewayControlledShutdown(notifier lifecycleStopNotifier, cancel, shutdown func()) {
	if notifier != nil {
		notifier.BroadcastStopping()
	}
	if cancel != nil {
		cancel()
	}
	if shutdown != nil {
		shutdown()
	}
}

func (b *lifecycleBroadcaster) BroadcastStopping() lifecycleBroadcastSummary {
	summary := lifecycleBroadcastSummary{Phase: lifecyclePhaseStopping}
	if b == nil || b.sessions == nil || b.connections == nil || b.snapshots == nil {
		return summary
	}
	targets := collectLifecycleTargets(b.sessions.ListActive(), b.connections.HasActiveConn)
	summary.TargetCount = len(targets)
	ids := make([]string, 0, len(targets))
	for _, target := range targets {
		ids = append(ids, target.ID)
	}
	if err := b.snapshots.Save(ids); err != nil {
		b.logger().Warn("lifecycle broadcast: snapshot save failed", "phase", lifecyclePhaseStopping, "err", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), b.effectiveTimeout())
	defer cancel()
	return b.broadcast(ctx, lifecycleStoppingMessage, targets, summary)
}

func (b *lifecycleBroadcaster) BroadcastStarted() lifecycleBroadcastSummary {
	summary := lifecycleBroadcastSummary{Phase: lifecyclePhaseStarted}
	if b == nil || b.sessions == nil || b.snapshots == nil {
		return summary
	}
	snapshot, claimedPath, err := b.snapshots.Claim()
	if err != nil {
		b.logger().Warn("lifecycle broadcast: snapshot claim failed", "phase", lifecyclePhaseStarted, "err", err)
		return summary
	}
	if snapshot == nil {
		return summary
	}
	defer func() {
		if removeErr := b.snapshots.CompleteClaim(claimedPath); removeErr != nil {
			b.logger().Warn("lifecycle broadcast: snapshot cleanup failed", "phase", lifecyclePhaseStarted, "err", removeErr)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), b.effectiveTimeout())
	defer cancel()
	restored := make([]*session.SessionInfo, 0, len(snapshot.SessionIDs))
	summary.TargetCount = len(snapshot.SessionIDs)
	for _, id := range snapshot.SessionIDs {
		si, getErr := b.sessions.Get(ctx, id)
		if getErr != nil {
			summary.FailedCount++
			b.logger().Warn("lifecycle broadcast: session restore failed", "phase", lifecyclePhaseStarted, "session_id", id)
			continue
		}
		restored = append(restored, si)
	}
	targets := collectLifecycleTargets(restored, func(string) bool { return true })
	summary.TargetCount = summary.FailedCount + len(targets)
	return b.broadcast(ctx, lifecycleStartedMessage, targets, summary)
}

func (b *lifecycleBroadcaster) broadcast(
	ctx context.Context,
	text string,
	targets []*session.SessionInfo,
	summary lifecycleBroadcastSummary,
) lifecycleBroadcastSummary {
	startedAt := time.Now()
	if len(targets) == 0 {
		b.logSummary(summary, time.Since(startedAt))
		return summary
	}
	limit := b.concurrency
	if limit <= 0 {
		limit = 1
	}
	semaphore := make(chan struct{}, limit)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, target := range targets {
		target := target
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				mu.Lock()
				summary.FailedCount++
				mu.Unlock()
				return
			}

			sender, err := resolveLifecycleSender(b.bots, target)
			if err == nil {
				err = sender.SendProactiveMessage(ctx, text, target.PlatformKey)
			}
			mu.Lock()
			if err != nil {
				summary.FailedCount++
			} else {
				summary.SentCount++
			}
			mu.Unlock()
			if err != nil {
				b.logger().Warn("lifecycle broadcast: target send failed",
					"phase", summary.Phase,
					"platform", target.Platform,
					"bot_name", target.BotName,
					"session_id", target.ID,
					"error_kind", "send_failed")
			}
		}()
	}
	wg.Wait()
	b.logSummary(summary, time.Since(startedAt))
	return summary
}

func (b *lifecycleBroadcaster) effectiveTimeout() time.Duration {
	if b.timeout <= 0 {
		return lifecycleBroadcastTimeout
	}
	return b.timeout
}

func (b *lifecycleBroadcaster) logger() *slog.Logger {
	if b != nil && b.log != nil {
		return b.log
	}
	return slog.Default()
}

func (b *lifecycleBroadcaster) logSummary(summary lifecycleBroadcastSummary, duration time.Duration) {
	b.logger().Info("lifecycle broadcast: phase completed",
		"phase", summary.Phase,
		"target_count", summary.TargetCount,
		"sent_count", summary.SentCount,
		"failed_count", summary.FailedCount,
		"duration_ms", duration.Milliseconds())
}

func resolveLifecycleSender(registry lifecycleBotRegistry, si *session.SessionInfo) (messaging.ProactiveMessageSender, error) {
	if registry == nil || si == nil || si.Platform == "" {
		return nil, fmt.Errorf("lifecycle broadcast: missing bot routing context")
	}
	platform := messaging.PlatformType(si.Platform)
	if si.BotName != "" {
		entry, ok := registry.Get(platform, si.BotName)
		if !ok {
			return nil, fmt.Errorf("lifecycle broadcast: bot not found")
		}
		return lifecycleSenderFromEntry(entry)
	}

	entries := registry.ListByPlatform(platform)
	if si.BotID != "" {
		matches := make([]*messaging.BotEntry, 0, 1)
		for _, entry := range entries {
			if entry != nil && entry.Status == messaging.BotStatusRunning && entry.BotID == si.BotID {
				matches = append(matches, entry)
			}
		}
		if len(matches) != 1 {
			return nil, fmt.Errorf("lifecycle broadcast: bot id unavailable or ambiguous")
		}
		return lifecycleSenderFromEntry(matches[0])
	}

	running := make([]*messaging.BotEntry, 0, len(entries))
	for _, entry := range entries {
		if entry != nil && entry.Status == messaging.BotStatusRunning {
			running = append(running, entry)
		}
	}
	if len(running) != 1 {
		return nil, fmt.Errorf("lifecycle broadcast: legacy bot routing unavailable or ambiguous")
	}
	return lifecycleSenderFromEntry(running[0])
}

func lifecycleSenderFromEntry(entry *messaging.BotEntry) (messaging.ProactiveMessageSender, error) {
	if entry == nil || entry.Status != messaging.BotStatusRunning || entry.Adapter == nil {
		return nil, fmt.Errorf("lifecycle broadcast: bot is not running")
	}
	sender, ok := entry.Adapter.(messaging.ProactiveMessageSender)
	if !ok {
		return nil, fmt.Errorf("lifecycle broadcast: bot does not support proactive messages")
	}
	return sender, nil
}

func lifecycleTargetKey(si *session.SessionInfo) (string, bool) {
	if si == nil || si.ID == "" {
		return "", false
	}
	botIdentity := si.BotName
	if botIdentity == "" {
		botIdentity = si.BotID
	}
	fields := []string{si.Platform, botIdentity}
	switch messaging.PlatformType(si.Platform) {
	case messaging.PlatformSlack:
		channelID := si.PlatformKey["channel_id"]
		if channelID == "" {
			return "", false
		}
		fields = append(fields, si.PlatformKey["team_id"], channelID, si.PlatformKey["thread_ts"])
	case messaging.PlatformFeishu:
		chatID := si.PlatformKey["chat_id"]
		if chatID == "" {
			return "", false
		}
		fields = append(fields, chatID, si.PlatformKey["thread_ts"])
	case messaging.PlatformYuanxin:
		messageID := si.PlatformKey["message_id"]
		if messageID == "" {
			return "", false
		}
		fields = append(fields, messageID, si.PlatformKey["reply_user_codes"], si.PlatformKey["sys_id"])
	default:
		return "", false
	}
	return strings.Join(fields, lifecycleTargetSeparator), true
}

func collectLifecycleTargets(sessions []*session.SessionInfo, hasActive func(string) bool) []*session.SessionInfo {
	targets := make([]*session.SessionInfo, 0, len(sessions))
	seen := make(map[string]struct{}, len(sessions))
	for _, si := range sessions {
		if si == nil || hasActive == nil || !hasActive(si.ID) {
			continue
		}
		key, ok := lifecycleTargetKey(si)
		if !ok {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		copyInfo := *si
		copyInfo.PlatformKey = maps.Clone(si.PlatformKey)
		targets = append(targets, &copyInfo)
	}
	return targets
}

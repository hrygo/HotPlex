package main

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/session"
)

const (
	lifecycleTargetSeparator           = "\x00"
	lifecycleStoppingMessage           = "⚠️ HotPlex 服务即将停止。"
	lifecycleStartedMessage            = "✅ HotPlex 服务已启动。"
	lifecyclePhaseStopping             = "stopping"
	lifecyclePhaseStarted              = "started"
	lifecycleBroadcastTimeout          = 5 * time.Second
	lifecycleBroadcastConcurrency      = 8
	lifecycleRestartReceiptMaxAttempts = 3
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

type lifecycleSendResult struct {
	target *session.SessionInfo
	err    error
}

type lifecycleBroadcaster struct {
	log         *slog.Logger
	sessions    lifecycleSessionSource
	connections lifecycleConnectionChecker
	bots        lifecycleBotRegistry
	snapshots   *lifecycleSnapshotStore
	receipts    *restartReceiptStore
	buildInfo   BuildInfo
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
		receipts:    newRestartReceiptStore(gatewayRestartReceiptPath()),
		buildInfo:   newBuildInfo(),
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
	if b == nil {
		return summary
	}
	receipt := b.readRestartReceipt()
	if receipt != nil {
		b.logger().Info("lifecycle broadcast: restart stopping", "request_id", receipt.RequestID)
	}
	var targets []*session.SessionInfo
	if b.sessions != nil && b.connections != nil {
		targets = collectLifecycleTargets(b.sessions.ListActive(), b.connections.HasActiveConn)
		if b.snapshots != nil {
			ids := make([]string, 0, len(targets))
			for _, target := range targets {
				ids = append(ids, target.ID)
			}
			if err := b.snapshots.Save(ids); err != nil {
				b.logger().Warn("lifecycle broadcast: snapshot save failed", "phase", lifecyclePhaseStopping, "err", err)
			}
		}
	}
	if explicit := lifecycleTargetFromRestartReceipt(receipt); explicit != nil {
		targets = mergeLifecycleTargets([]*session.SessionInfo{explicit}, targets)
	}
	summary.TargetCount = len(targets)
	ctx, cancel := context.WithTimeout(context.Background(), b.effectiveTimeout())
	defer cancel()
	return b.broadcast(ctx, lifecycleStoppingMessageFor(b.buildInfo, receipt), targets, summary)
}

func (b *lifecycleBroadcaster) BroadcastStarted() lifecycleBroadcastSummary {
	summary := lifecycleBroadcastSummary{Phase: lifecyclePhaseStarted}
	if b == nil {
		return summary
	}
	receipt := b.readRestartReceipt()
	if receipt != nil {
		b.logger().Info("lifecycle broadcast: restart started", "request_id", receipt.RequestID)
	}
	var snapshot *lifecycleSnapshot
	var claimedPath string
	if b.snapshots != nil {
		var err error
		snapshot, claimedPath, err = b.snapshots.Claim()
		if err != nil {
			b.logger().Warn("lifecycle broadcast: snapshot claim failed", "phase", lifecyclePhaseStarted, "err", err)
			if receipt == nil {
				return summary
			}
			snapshot = nil
			claimedPath = ""
		}
	}
	if snapshot == nil && receipt == nil {
		return summary
	}
	defer func() {
		if b.snapshots != nil {
			if removeErr := b.snapshots.CompleteClaim(claimedPath); removeErr != nil {
				b.logger().Warn("lifecycle broadcast: snapshot cleanup failed", "phase", lifecyclePhaseStarted, "err", removeErr)
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), b.effectiveTimeout())
	defer cancel()
	var restored []*session.SessionInfo
	if snapshot != nil && b.sessions != nil {
		restored = make([]*session.SessionInfo, 0, len(snapshot.SessionIDs))
		for _, id := range snapshot.SessionIDs {
			si, getErr := b.sessions.Get(ctx, id)
			if getErr != nil {
				summary.FailedCount++
				b.logger().Warn("lifecycle broadcast: session restore failed", "phase", lifecyclePhaseStarted, "session_id", id)
				continue
			}
			restored = append(restored, si)
		}
	}
	targets := collectLifecycleTargets(restored, func(string) bool { return true })
	explicit := lifecycleTargetFromRestartReceipt(receipt)
	explicitKey, explicitValid := lifecycleTargetKey(explicit)
	if explicitValid {
		targets = mergeLifecycleTargets([]*session.SessionInfo{explicit}, targets)
	}
	summary.TargetCount = summary.FailedCount + len(targets)
	if receipt != nil && !explicitValid {
		summary.TargetCount++
		summary.FailedCount++
	}

	message := lifecycleStartedMessageFor(b.buildInfo, receipt)
	receiptDelivered := false
	if explicitValid {
		remaining := make([]*session.SessionInfo, 0, len(targets)-1)
		for _, target := range targets {
			key, ok := lifecycleTargetKey(target)
			if ok && key == explicitKey {
				continue
			}
			remaining = append(remaining, target)
		}
		if err := sendRestartStartedReceipt(ctx, b.bots, b.buildInfo, receipt); err != nil {
			summary.FailedCount++
			b.logger().Warn("lifecycle broadcast: target send failed",
				"phase", lifecyclePhaseStarted,
				"platform", explicit.Platform,
				"bot_name", explicit.BotName,
				"session_id", explicit.ID,
				"error_kind", "send_failed")
			logGatewayRestartAudit(b.logger(), gatewayRestartAuditRecord{
				RequestID:  receipt.RequestID,
				Source:     receipt.Platform,
				Actor:      receipt.Actor,
				BotName:    receipt.BotName,
				ChatID:     receipt.PlatformKey["chat_id"],
				Result:     "failed",
				OldPID:     receipt.OldPID,
				NewPID:     os.Getpid(),
				OldVersion: receipt.OldVersion,
				NewVersion: b.buildInfo.Version,
			})
		} else {
			summary.SentCount++
			receiptDelivered = true
			logGatewayRestartAudit(b.logger(), gatewayRestartAuditRecord{
				RequestID:  receipt.RequestID,
				Source:     receipt.Platform,
				Actor:      receipt.Actor,
				BotName:    receipt.BotName,
				ChatID:     receipt.PlatformKey["chat_id"],
				Result:     "started",
				OldPID:     receipt.OldPID,
				NewPID:     os.Getpid(),
				OldVersion: receipt.OldVersion,
				NewVersion: b.buildInfo.Version,
			})
		}
		targets = remaining
	}
	summary = b.broadcast(ctx, message, targets, summary)
	if receiptDelivered && b.receipts != nil {
		if err := b.receipts.Complete(receipt.RequestID); err != nil {
			b.logger().Warn("lifecycle broadcast: restart receipt completion failed", "phase", lifecyclePhaseStarted, "error_kind", "receipt_complete_failed")
		}
	}
	return summary
}

func (b *lifecycleBroadcaster) readRestartReceipt() *gatewayRestartReceipt {
	if b == nil || b.receipts == nil {
		return nil
	}
	receipt, err := b.receipts.Read()
	if err == nil {
		return receipt
	}
	if _, quarantineErr := b.receipts.Quarantine(); quarantineErr != nil {
		b.logger().Warn("lifecycle broadcast: invalid restart receipt", "phase", "restart", "error_kind", "receipt_invalid")
	}
	return nil
}

func lifecycleTargetFromRestartReceipt(receipt *gatewayRestartReceipt) *session.SessionInfo {
	if receipt == nil || receipt.Platform == "" {
		return nil
	}
	return &session.SessionInfo{
		ID:          "restart:" + receipt.RequestID,
		Platform:    receipt.Platform,
		BotName:     receipt.BotName,
		PlatformKey: maps.Clone(receipt.PlatformKey),
	}
}

func mergeLifecycleTargets(groups ...[]*session.SessionInfo) []*session.SessionInfo {
	var merged []*session.SessionInfo
	seen := make(map[string]struct{})
	for _, group := range groups {
		for _, si := range group {
			if si == nil {
				continue
			}
			key, ok := lifecycleTargetKey(si)
			if !ok && strings.HasPrefix(si.ID, "restart:") {
				key = "restart" + lifecycleTargetSeparator + si.ID
				ok = true
			}
			if !ok {
				continue
			}
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			copyInfo := *si
			copyInfo.PlatformKey = maps.Clone(si.PlatformKey)
			merged = append(merged, &copyInfo)
		}
	}
	return merged
}

func lifecycleStoppingMessageFor(info BuildInfo, receipt *gatewayRestartReceipt) string {
	if receipt == nil {
		return lifecycleStoppingMessage
	}
	version := info.Version
	if version == "" {
		version = receipt.OldVersion
	}
	return fmt.Sprintf("⚠️ HotPlex Gateway 即将重启。\n版本: %s\nBuild: %s\nPID: %d\n系统: %s/%s\n原因: Feishu /gateway restart\n请求 ID: %s\n请求时间: %s",
		version, info.BuildTime, receipt.OldPID, info.OS, info.Arch, receipt.RequestID, receipt.RequestedAt.UTC().Format(time.RFC3339))
}

func lifecycleStartedMessageFor(info BuildInfo, receipt *gatewayRestartReceipt) string {
	if receipt == nil {
		return lifecycleStartedMessage
	}
	previousVersion := receipt.OldVersion
	if previousVersion == "" {
		previousVersion = "unknown"
	}
	return fmt.Sprintf("✅ HotPlex Gateway 已启动。\n版本: %s\nBuild: %s\nPID: %d\n系统: %s/%s\n上一版本: %s\n上一 PID: %d\n请求 ID: %s\n请求时间: %s",
		info.Version, info.BuildTime, os.Getpid(), info.OS, info.Arch, previousVersion, receipt.OldPID, receipt.RequestID, receipt.RequestedAt.UTC().Format(time.RFC3339))
}

func sendRestartStartedReceipt(
	ctx context.Context,
	registry lifecycleBotRegistry,
	info BuildInfo,
	receipt *gatewayRestartReceipt,
) error {
	target := lifecycleTargetFromRestartReceipt(receipt)
	if _, ok := lifecycleTargetKey(target); !ok {
		return fmt.Errorf("lifecycle broadcast: invalid restart receipt target")
	}
	sender, err := resolveLifecycleSender(registry, target)
	if err != nil {
		return err
	}
	message := lifecycleStartedMessageFor(info, receipt)
	var sendErr error
	for range lifecycleRestartReceiptMaxAttempts {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if sendErr = sender.SendProactiveMessage(ctx, message, target.PlatformKey); sendErr == nil {
			return nil
		}
	}
	return sendErr
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
	if limit > len(targets) {
		limit = len(targets)
	}
	jobs := make(chan *session.SessionInfo, len(targets))
	results := make(chan lifecycleSendResult, len(targets))
	for _, target := range targets {
		jobs <- target
	}
	close(jobs)
	for range limit {
		go func() {
			for {
				if ctx.Err() != nil {
					return
				}
				select {
				case <-ctx.Done():
					return
				case target, ok := <-jobs:
					if !ok {
						return
					}
					sender, err := resolveLifecycleSender(b.bots, target)
					if err == nil {
						err = sender.SendProactiveMessage(ctx, text, target.PlatformKey)
					}
					results <- lifecycleSendResult{target: target, err: err}
				}
			}
		}()
	}

	pending := len(targets)
	for pending > 0 {
		select {
		case result := <-results:
			pending--
			if result.err != nil {
				summary.FailedCount++
			} else {
				summary.SentCount++
			}
			if result.err != nil {
				b.logger().Warn("lifecycle broadcast: target send failed",
					"phase", summary.Phase,
					"platform", result.target.Platform,
					"bot_name", result.target.BotName,
					"session_id", result.target.ID,
					"error_kind", "send_failed")
			}
		case <-ctx.Done():
			summary.FailedCount += pending
			b.logSummary(summary, time.Since(startedAt))
			return summary
		}
	}
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

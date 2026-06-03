package groupchat

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/pkg/events"
)

// BridgeStarter is the narrow interface from the gateway Bridge for session creation.
// Mirrors cron.BridgeStarter.
type BridgeStarter interface {
	StartSession(ctx context.Context, id, userID, botID string, wt worker.WorkerType, allowedTools []string, workDir, platform string, platformKey map[string]string, title, clientKey string, injectExclude ...string) error
}

// SessionStateChecker polls session state for completion detection.
type SessionStateChecker interface {
	Get(ctx context.Context, id string) (*session.SessionInfo, error)
	GetWorker(id string) worker.Worker
	Transition(ctx context.Context, id string, to events.SessionState) error
}

// BotLookup resolves bot names to entries with bridge info.
type BotLookup interface {
	GetByName(name string) (BotEntry, bool)
}

// BotEntry is a minimal bot info struct to avoid importing messaging package.
type BotEntry struct {
	Name       string
	BotID      string
	WorkerType string
}

// ResponseSender posts a bot's turn response to the platform thread.
type ResponseSender interface {
	SendTurnResponse(ctx context.Context, platform, channelID, threadTS, botName, content string, turnNum int) error
	SendControlMessage(ctx context.Context, platform, channelID, threadTS, message string) error
}

// Manager orchestrates group chat sessions.
type Manager struct {
	log      *slog.Logger
	cfg      Config
	store    Store
	bridge   BridgeStarter
	sm       SessionStateChecker
	bots     BotLookup
	sender   ResponseSender
	guard    *TerminateCheck
	selector *RoundRobinSelector

	mu     sync.Mutex
	active map[string]*groupRun // groupID → running context
}

type groupRun struct {
	gs     *GroupSession
	cancel context.CancelFunc
	done   chan struct{}
}

// NewManager creates the group chat manager.
func NewManager(log *slog.Logger, cfg Config, store Store, bridge BridgeStarter, sm SessionStateChecker, bots BotLookup, sender ResponseSender) *Manager {
	return &Manager{
		log:      log.With("component", "groupchat_manager"),
		cfg:      cfg,
		store:    store,
		bridge:   bridge,
		sm:       sm,
		bots:     bots,
		sender:   sender,
		guard:    &TerminateCheck{MaxTurns: cfg.MaxTurns, CostLimitUSD: cfg.CostLimitUSD, MaxConsecutiveTMO: 2},
		selector: &RoundRobinSelector{},
		active:   make(map[string]*groupRun),
	}
}

// StartDiscussion creates and begins a group chat.
func (m *Manager) StartDiscussion(ctx context.Context, ownerID, platform, channelID, threadTS string, botNames []string, topic string) (string, error) {
	// 1. Validate participant count.
	if len(botNames) < 2 {
		return "", fmt.Errorf("groupchat: need at least 2 bots, got %d", len(botNames))
	}

	// 2. Resolve bot names to IDs.
	var botIDs []string
	botNamesMap := make(map[string]string)
	for _, name := range botNames {
		be, ok := m.bots.GetByName(name)
		if !ok {
			return "", fmt.Errorf("groupchat: bot %q not found", name)
		}
		botIDs = append(botIDs, be.BotID)
		botNamesMap[be.BotID] = be.Name
	}

	// 3. Check quotas.
	if err := m.checkQuotas(ctx, ownerID); err != nil {
		return "", err
	}

	// 4. Create group session.
	gs := NewGroupSession(topic, platform, channelID, threadTS, ownerID, botIDs, m.cfg)
	gs.Initiator = ownerID
	gs.BotNames = botNamesMap

	if err := m.store.CreateGroup(ctx, gs); err != nil {
		return "", fmt.Errorf("groupchat: create session: %w", err)
	}

	// 5. Launch turn loop in background.
	runCtx, cancel := context.WithCancel(ctx)
	run := &groupRun{gs: gs, cancel: cancel, done: make(chan struct{})}

	m.mu.Lock()
	m.active[gs.ID] = run
	m.mu.Unlock()

	go m.runTurnLoop(runCtx, run)

	m.log.Info("groupchat: discussion started",
		"group_id", gs.ID, "topic", topic, "bots", botNames, "owner", ownerID)

	return gs.ID, nil
}

// StopDiscussion terminates an active group chat.
func (m *Manager) StopDiscussion(ctx context.Context, groupID string) error {
	m.mu.Lock()
	run, ok := m.active[groupID]
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("groupchat: no active discussion %s", groupID)
	}

	run.cancel()
	return nil
}

// StopAll terminates all active discussions (gateway shutdown).
func (m *Manager) StopAll(ctx context.Context) {
	m.mu.Lock()
	runs := make([]*groupRun, 0, len(m.active))
	for _, run := range m.active {
		runs = append(runs, run)
	}
	m.mu.Unlock()

	for _, run := range runs {
		run.cancel()
	}

	// Wait for all goroutines to finish.
	for _, run := range runs {
		<-run.done
	}

	// Mark all active sessions as gateway_restart.
	for _, run := range runs {
		_ = m.store.UpdateGroupState(ctx, run.gs.ID, GroupStateGatewayRestart, EndGatewayRestart)
	}

	m.log.Info("groupchat: all discussions stopped", "count", len(runs))
}

// GetActiveForChannel returns the active group session for a given channel+thread, or nil.
func (m *Manager) GetActiveForChannel(channelID, threadTS string) *GroupSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, run := range m.active {
		if run.gs.ChannelID == channelID && run.gs.ThreadTS == threadTS {
			return run.gs
		}
	}
	return nil
}

// RepairRunningSessions marks stale active sessions after gateway restart.
func (m *Manager) RepairRunningSessions(ctx context.Context) {
	active, err := m.store.ListActive(ctx)
	if err != nil {
		m.log.Error("groupchat: repair: list active failed", "err", err)
		return
	}
	for _, gs := range active {
		m.log.Warn("groupchat: repairing stale session", "group_id", gs.ID)
		_ = m.store.UpdateGroupState(ctx, gs.ID, GroupStateGatewayRestart, EndGatewayRestart)
		_ = m.store.RecordAudit(ctx, &AuditEvent{
			EventType: "gateway_restart",
			SessionID: gs.ID,
			Detail:    "stale session marked on startup",
			CreatedAt: time.Now(),
		})
	}
}

// ---------------------------------------------------------------------------
// Turn Loop
// ---------------------------------------------------------------------------

func (m *Manager) runTurnLoop(ctx context.Context, run *groupRun) {
	defer close(run.done)
	defer m.cleanup(run)

	gs := run.gs

	// Record start audit.
	_ = m.store.RecordAudit(ctx, &AuditEvent{
		EventType: "discussion_start", SessionID: gs.ID, Initiator: gs.OwnerID,
		Detail: fmt.Sprintf("topic=%q bots=%v", gs.Topic, gs.BotIDs), CreatedAt: time.Now(),
	})

	// Send confirmation to platform.
	_ = m.sender.SendControlMessage(ctx, gs.Platform, gs.ChannelID, gs.ThreadTS,
		fmt.Sprintf("🤝 讨论开始：%s（参与者：%s）", gs.Topic, formatBotList(gs)))

	transcript := ""
	participants := make([]string, len(gs.BotIDs))
	copy(participants, gs.BotIDs)

	for turnNum := 1; ; turnNum++ {
		// Check cancellation.
		select {
		case <-ctx.Done():
			m.endDiscussion(ctx, gs, EndUserStopped)
			return
		default:
		}

		// Check termination conditions.
		turns, _ := m.store.ListTurns(ctx, gs.ID)
		if reason := m.guard.ShouldTerminate(ctx, gs, turns); reason != "" {
			m.endDiscussion(ctx, gs, reason)
			return
		}

		// Select next speaker.
		if len(participants) == 0 {
			m.endDiscussion(ctx, gs, EndError)
			return
		}
		speakerID := m.selector.Next(turnNum, participants)
		speakerName := gs.BotNames[speakerID]

		// Execute turn.
		result := m.executeTurn(ctx, gs, speakerID, speakerName, turnNum, transcript)

		// Handle SKIP.
		if result.Skipped {
			m.log.Debug("groupchat: bot skipped", "group_id", gs.ID, "bot", speakerName, "turn", turnNum)
			_ = m.store.AppendTurn(ctx, result)
			gs.TurnCount = turnNum
			_ = m.store.UpdateGroupCost(ctx, gs.ID, gs.TurnCount, gs.CostAccumulated)
			continue
		}

		// Handle error.
		if result.Err != nil {
			m.log.Error("groupchat: turn error", "group_id", gs.ID, "bot", speakerName, "turn", turnNum, "err", result.Err)

			// Check consecutive timeout eviction.
			if result.TimeoutCount > 0 {
				turns, _ = m.store.ListTurns(ctx, gs.ID)
				if m.guard.ShouldEvictBot(speakerID, turns) {
					_ = m.sender.SendControlMessage(ctx, gs.Platform, gs.ChannelID, gs.ThreadTS,
						fmt.Sprintf("🚫 @%s 已从讨论中移除（连续超时）", speakerName))
					participants = RemoveFromParticipants(participants, speakerID)
					_ = m.store.RecordAudit(ctx, &AuditEvent{
						EventType: "bot_evicted", SessionID: gs.ID, BotID: speakerID,
						TurnNum: turnNum, Detail: "consecutive timeout", CreatedAt: time.Now(),
					})
					continue
				}
				_ = m.sender.SendControlMessage(ctx, gs.Platform, gs.ChannelID, gs.ThreadTS,
					fmt.Sprintf("⏱️ @%s %ds 无回复 → 跳过本轮", speakerName, gs.TurnTimeoutSec))
			}
			_ = m.store.AppendTurn(ctx, result)
			gs.TurnCount = turnNum
			_ = m.store.UpdateGroupCost(ctx, gs.ID, gs.TurnCount, gs.CostAccumulated)
			continue
		}

		// Sanitize content for inter-bot safety.
		safeContent, sanitizeReason := SanitizeContent(result.Content, m.cfg.MaxTurnContentLength)
		result.Sanitized = sanitizeReason != ""
		result.SanitizeReason = sanitizeReason

		if sanitizeReason != "" {
			_ = m.sender.SendControlMessage(ctx, gs.Platform, gs.ChannelID, gs.ThreadTS,
				fmt.Sprintf("🛡️ @%s 回复被安全过滤器处理 → %s", speakerName, sanitizeReason))
		}

		// Send response to platform.
		_ = m.sender.SendTurnResponse(ctx, gs.Platform, gs.ChannelID, gs.ThreadTS, speakerName, safeContent, turnNum)

		// Record turn.
		result.Content = safeContent
		_ = m.store.AppendTurn(ctx, result)
		gs.TurnCount = turnNum
		gs.CostAccumulated += result.CostUSD
		_ = m.store.UpdateGroupCost(ctx, gs.ID, gs.TurnCount, gs.CostAccumulated)

		// Update transcript for next turn's context.
		transcript += fmt.Sprintf("\n## %s:\n%s\n", speakerName, safeContent)

		// Inter-turn cooldown.
		select {
		case <-time.After(m.cfg.Cooldown()):
		case <-ctx.Done():
			m.endDiscussion(ctx, gs, EndUserStopped)
			return
		}
	}
}

// turnResult captures the outcome of a single bot turn.
type turnResult = TurnRecord

func (m *Manager) executeTurn(ctx context.Context, gs *GroupSession, botID, botName string, turnNum int, transcript string) *turnResult {
	now := time.Now()
	result := &turnResult{
		ID:        uuid.NewString(),
		GroupID:   gs.ID,
		BotID:     botID,
		BotName:   botName,
		TurnNum:   turnNum,
		CreatedAt: now,
	}

	// Derive sub-session key.
	subSessionID := session.DeriveGroupSessionKey(gs.ID, botID, turnNum)

	// Look up bot to get worker type.
	be, ok := m.bots.GetByName(botName)
	if !ok {
		result.Err = fmt.Errorf("bot %q not found", botName)
		return result
	}
	wt := worker.WorkerType(be.WorkerType)
	if wt == "" {
		wt = worker.TypeClaudeCode
	}

	// Start or resume the sub-session.
	platformKey := map[string]string{
		"group_chat_id": gs.ID,
		"bot_id":        botID,
		"channel_id":    gs.ChannelID,
	}
	if gs.ThreadTS != "" {
		platformKey["thread_ts"] = gs.ThreadTS
	}

	title := fmt.Sprintf("groupchat:%s:turn%d", gs.Topic, turnNum)

	if err := m.bridge.StartSession(ctx, subSessionID, gs.OwnerID, botID,
		wt, nil, "", gs.Platform, platformKey, title, ""); err != nil {
		result.Err = fmt.Errorf("start sub-session: %w", err)
		return result
	}

	// Get the worker.
	w := m.sm.GetWorker(subSessionID)
	if w == nil {
		result.Err = fmt.Errorf("worker not found after start")
		return result
	}

	// Build the prompt.
	prompt := m.buildTurnPrompt(botName, gs.Topic, transcript)

	// Send input.
	if err := w.Input(ctx, prompt, nil); err != nil {
		result.Err = fmt.Errorf("input: %w", err)
		return result
	}

	// Signal EOF so --print mode workers exit after processing.
	if ci, ok := w.(interface{ CloseInput() error }); ok {
		_ = ci.CloseInput()
	}

	// Wait for completion with timeout.
	timeout := time.Duration(gs.TurnTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	if err := m.waitForCompletion(ctx, subSessionID, timeout); err != nil {
		result.TimeoutCount = 1
		result.Err = fmt.Errorf("timeout: %w", err)
		return result
	}

	// Extract response from the session's event store turns.
	content := m.extractResponse(ctx, subSessionID)
	result.Content = content

	// Check SKIP.
	if IsSkipResponse(content) {
		result.Skipped = true
		result.Content = ""
	}

	// Terminate the sub-session to free the worker.
	termCtx, cancel := context.WithTimeout(context.Background(), base.GracefulShutdownTimeout)
	defer cancel()
	if termErr := m.sm.Transition(termCtx, subSessionID, events.StateTerminated); termErr != nil {
		m.log.Warn("groupchat: failed to terminate sub-session", "session_id", subSessionID, "err", termErr)
	}

	return result
}

func (m *Manager) buildTurnPrompt(botName, topic, transcript string) string {
	var b strings.Builder

	// System context.
	b.WriteString("你正在参与一个多人讨论。以下是讨论信息：\n\n")
	fmt.Fprintf(&b, "## 话题：%s\n", topic)

	// Add transcript (limited).
	if transcript != "" {
		ctxLen := m.cfg.MaxTotalContextLength
		t := transcript
		if ctxLen > 0 && len(t) > ctxLen {
			// Keep the most recent context. Use rune-aware truncation to
			// avoid splitting multi-byte UTF-8 sequences (CJK characters).
			runes := []rune(t)
			if len(runes) > ctxLen {
				t = string(runes[len(runes)-ctxLen:])
			}
		}
		b.WriteString("\n## 讨论历史：\n")
		b.WriteString(t)
	}

	// Bot-specific instructions.
	fmt.Fprintf(&b, "\n## 你的角色：%s\n", botName)
	b.WriteString("\n请基于以上讨论内容发表你的观点。")
	b.WriteString("如果无补充，回复 SKIP。\n")
	b.WriteString("如需修改代码，输出 unified diff 作为建议，前缀 '🔒 SUGGESTED CHANGE:'\n")

	return b.String()
}

func (m *Manager) waitForCompletion(ctx context.Context, sessionID string, timeout time.Duration) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Initial check.
	si, err := m.sm.Get(timeoutCtx, sessionID)
	if err == nil && si.State != events.StateRunning && si.State != events.StateCreated {
		return nil
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("turn timeout: %w", timeoutCtx.Err())
		case <-ticker.C:
			si, err := m.sm.Get(timeoutCtx, sessionID)
			if err != nil {
				m.log.Warn("groupchat: session state check failed", "session_id", sessionID, "err", err)
				continue
			}
			if si.State != events.StateRunning && si.State != events.StateCreated {
				return nil
			}
		}
	}
}

// extractResponse retrieves the last assistant turn text from the sub-session.
// For v1, we poll session state and return a placeholder; the actual content
// extraction uses the event store turns query if available, or falls back to
// the worker's last output.
func (m *Manager) extractResponse(_ context.Context, _ string) string {
	// The forwardEvents goroutine already captured the response in the event store.
	// For v1, we query the session's turns from the bridge's accumulator.
	// Since we don't have direct access to the accumulator, we use the session's
	// last known state as a signal that the worker completed.
	// The actual content will be delivered via the Hub to any subscribed PlatformConns.
	// For the groupchat manager, we rely on the fact that the worker's output
	// has been forwarded to the hub, and the platform adapter will render it.
	//
	// For now, return empty content and rely on the platform adapter's
	// existing rendering pipeline to show the response.
	// The groupchat manager's primary job is orchestration, not content extraction.
	//
	// TODO: Phase 2 — extract content from event store for context building.
	return ""
}

func (m *Manager) endDiscussion(ctx context.Context, gs *GroupSession, reason EndReason) {
	_ = m.store.UpdateGroupState(ctx, gs.ID, GroupStateCompleted, reason)
	_ = m.store.RecordAudit(ctx, &AuditEvent{
		EventType: "discussion_end", SessionID: gs.ID,
		Detail:    fmt.Sprintf("reason=%s turns=%d cost=%.4f", reason, gs.TurnCount, gs.CostAccumulated),
		CreatedAt: time.Now(),
	})

	var endMsg string
	switch reason {
	case EndMaxTurns:
		endMsg = fmt.Sprintf("🛑 已达最大轮次 %d → 终止", gs.MaxTurns)
	case EndCostLimit:
		endMsg = fmt.Sprintf("💰 成本超 $%.2f → 终止", gs.CostLimitUSD)
	case EndAllSkip:
		endMsg = "💤 所有 Bot 跳过 → 讨论结束"
	case EndUserStopped:
		endMsg = "✋ 用户手动终止"
	case EndGatewayRestart:
		endMsg = "🔄 网关重启 → 讨论终止"
	default:
		endMsg = fmt.Sprintf("讨论结束（%s）", reason)
	}

	_ = m.sender.SendControlMessage(ctx, gs.Platform, gs.ChannelID, gs.ThreadTS, endMsg)
	m.log.Info("groupchat: discussion ended", "group_id", gs.ID, "reason", reason, "turns", gs.TurnCount)
}

func (m *Manager) cleanup(run *groupRun) {
	m.mu.Lock()
	delete(m.active, run.gs.ID)
	m.mu.Unlock()
}

func (m *Manager) checkQuotas(ctx context.Context, ownerID string) error {
	globalCount, err := m.store.CountActive(ctx)
	if err != nil {
		return fmt.Errorf("groupchat: check global quota: %w", err)
	}
	if globalCount >= m.cfg.MaxGroupSessions {
		return fmt.Errorf("groupchat: global limit reached (%d/%d)", globalCount, m.cfg.MaxGroupSessions)
	}

	userCount, err := m.store.CountActiveByOwner(ctx, ownerID)
	if err != nil {
		return fmt.Errorf("groupchat: check user quota: %w", err)
	}
	if userCount >= m.cfg.MaxSessionsPerUser {
		return fmt.Errorf("groupchat: user limit reached (%d/%d)", userCount, m.cfg.MaxSessionsPerUser)
	}

	return nil
}

func formatBotList(gs *GroupSession) string {
	names := make([]string, 0, len(gs.BotIDs))
	for _, id := range gs.BotIDs {
		if n, ok := gs.BotNames[id]; ok {
			names = append(names, "@"+n)
		}
	}
	return strings.Join(names, ", ")
}

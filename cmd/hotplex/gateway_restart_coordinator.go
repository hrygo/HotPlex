package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"strings"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/messaging"
)

const (
	gatewayRestartHelpText       = "用法：/gateway restart（仅授权的 Feishu OpenID 可用）"
	gatewayRestartDeniedText     = "❌ 你没有权限执行 Gateway 重启。"
	gatewayRestartConflictText   = "⚠️ Gateway 正在重启，请稍候。"
	gatewayRestartAcceptedText   = "✅ Gateway 重启请求已受理。"
	gatewayRestartScheduleFailed = "❌ Gateway 重启调度失败，请查看 Gateway 日志。"
)

var errRestartReceiptPending = errors.New("gateway restart receipt delivery is pending")

type restartReceiptPendingError struct {
	RequestID string
}

func (e *restartReceiptPendingError) Error() string {
	if e.RequestID == "" {
		return errRestartReceiptPending.Error()
	}
	return fmt.Sprintf("%s (request_id=%s)", errRestartReceiptPending, e.RequestID)
}

func (e *restartReceiptPendingError) Unwrap() error {
	return errRestartReceiptPending
}

type gatewayRestartRequest struct {
	Platform      string
	Actor         string
	BotName       string
	ChatID        string
	PlatformKey   map[string]string
	RequestedAt   time.Time
	ConfigPath    string
	ConfigChanged bool
	DevMode       bool
	Daemon        bool
}

type gatewayRestartTicket struct {
	RequestID  string
	Lease      *restartLease
	Receipt    *gatewayRestartReceipt
	Instance   *gatewayInstance
	Source     string
	Actor      string
	BotName    string
	ChatID     string
	OldVersion string
	ConfigPath string
	DevMode    bool
	Daemon     bool
}

type gatewayRestartAuditRecord struct {
	RequestID  string
	Source     string
	Actor      string
	BotName    string
	ChatID     string
	Result     string
	OldPID     int
	HelperPID  int
	NewPID     int
	OldVersion string
	NewVersion string
}

type gatewayRestartCoordinator struct {
	log          *slog.Logger
	configStore  *config.ConfigStore
	configPath   string
	devMode      bool
	leaseStore   *restartLeaseStore
	receipts     *restartReceiptStore
	findInstance func() (*gatewayInstance, error)
	spawnHelper  func(*gatewayRestartTicket) (int, error)
	allowFeishu  func(botName, actorID string) bool
	retryReceipt func(context.Context, *gatewayRestartReceipt) error
	now          func() time.Time
}

func newGatewayRestartCoordinator(log *slog.Logger, configStore *config.ConfigStore, configPath string, devMode bool) *gatewayRestartCoordinator {
	if log == nil {
		log = slog.Default()
	}
	c := &gatewayRestartCoordinator{
		log:          log,
		configStore:  configStore,
		configPath:   configPath,
		devMode:      devMode,
		leaseStore:   newRestartLeaseStore(restartMarkerPath(), time.Now, nil),
		receipts:     newRestartReceiptStore(gatewayRestartReceiptPath()),
		findInstance: findRunningGateway,
		now:          time.Now,
	}
	c.spawnHelper = c.spawnRestartHelper
	c.allowFeishu = c.feishuRestartAllowed
	c.retryReceipt = func(ctx context.Context, receipt *gatewayRestartReceipt) error {
		return sendRestartStartedReceipt(ctx, messaging.DefaultBotRegistry(), newBuildInfo(), receipt)
	}
	return c
}

func (c *gatewayRestartCoordinator) Prepare(ctx context.Context, request gatewayRestartRequest) (*gatewayRestartTicket, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	if c == nil || c.leaseStore == nil {
		return nil, errors.New("gateway restart coordinator is not configured")
	}
	find := c.findInstance
	if find == nil {
		find = findRunningGateway
	}
	instance, err := find()
	if err != nil {
		return nil, fmt.Errorf("discover gateway for restart: %w", err)
	}
	if instance == nil || instance.PID <= 0 {
		return nil, errors.New("discover gateway for restart: invalid gateway instance")
	}

	lease, err := c.leaseStore.Acquire(os.Getpid())
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(request.Platform, string(messaging.PlatformFeishu)) {
		if err := c.retryPendingReceipt(ctx); err != nil {
			_ = c.leaseStore.Release(lease.RequestID)
			return nil, err
		}
	}

	configPath := request.ConfigPath
	if configPath == "" {
		configPath = c.configPath
	}
	configPath = resolveRestartConfig(configPath, request.ConfigChanged, instance.ConfigPath)
	devMode := c.devMode || request.DevMode || instance.DevMode
	ticket := &gatewayRestartTicket{
		RequestID:  lease.RequestID,
		Lease:      lease,
		Instance:   instance,
		Source:     request.Platform,
		Actor:      request.Actor,
		BotName:    request.BotName,
		ChatID:     request.ChatID,
		OldVersion: versionString(),
		ConfigPath: configPath,
		DevMode:    devMode,
		Daemon:     request.Daemon,
	}

	if strings.EqualFold(request.Platform, string(messaging.PlatformFeishu)) {
		requestedAt := request.RequestedAt
		if requestedAt.IsZero() {
			requestedAt = c.currentTime()
		}
		ticket.Receipt = &gatewayRestartReceipt{
			SchemaVersion: gatewayRestartReceiptSchemaVersion,
			RequestID:     lease.RequestID,
			Platform:      string(messaging.PlatformFeishu),
			Actor:         request.Actor,
			BotName:       request.BotName,
			PlatformKey:   cloneStringMap(request.PlatformKey),
			RequestedAt:   requestedAt.UTC(),
			OldVersion:    ticket.OldVersion,
			OldPID:        instance.PID,
		}
		if c.receipts == nil {
			_ = c.leaseStore.Release(lease.RequestID)
			return nil, errors.New("gateway restart receipt store is not configured")
		}
		if err := c.receipts.Write(ticket.Receipt); err != nil {
			_ = c.leaseStore.Release(lease.RequestID)
			return nil, fmt.Errorf("write gateway restart receipt: %w", err)
		}
	}

	c.logRestartAudit(gatewayRestartAuditRecord{
		RequestID:  ticket.RequestID,
		Source:     ticket.Source,
		Actor:      ticket.Actor,
		BotName:    ticket.BotName,
		ChatID:     ticket.ChatID,
		Result:     "prepared",
		OldPID:     instance.PID,
		OldVersion: ticket.OldVersion,
	})
	return ticket, nil
}

func (c *gatewayRestartCoordinator) Commit(ticket *gatewayRestartTicket) error {
	if c == nil || c.leaseStore == nil || ticket == nil || ticket.Lease == nil || ticket.RequestID == "" {
		return errors.New("gateway restart commit: invalid ticket")
	}
	spawn := c.spawnHelper
	if spawn == nil {
		spawn = c.spawnRestartHelper
	}
	helperPID, err := spawn(ticket)
	if err != nil {
		c.logTicketAudit(ticket, "failed", 0)
		_ = c.Abort(ticket)
		return fmt.Errorf("gateway restart commit: %w", err)
	}
	if helperPID <= 0 {
		c.logTicketAudit(ticket, "failed", 0)
		_ = c.Abort(ticket)
		return errors.New("gateway restart commit: helper returned invalid PID")
	}
	if err := c.leaseStore.Update(ticket.RequestID, func(lease *restartLease) error {
		lease.Phase = restartLeaseHelperStarted
		lease.HelperPID = helperPID
		return nil
	}); err != nil {
		terminateRestartHelper(helperPID)
		c.logTicketAudit(ticket, "failed", 0)
		_ = c.Abort(ticket)
		return fmt.Errorf("gateway restart commit: record helper: %w", err)
	}
	c.logTicketAudit(ticket, "helper_started", helperPID)
	return nil
}

func (c *gatewayRestartCoordinator) Abort(ticket *gatewayRestartTicket) error {
	if c == nil || ticket == nil || ticket.RequestID == "" {
		return nil
	}
	return abortRestartArtifacts(c.leaseStore, c.receipts, ticket.RequestID)
}

func abortRestartArtifacts(leaseStore *restartLeaseStore, receipts *restartReceiptStore, requestID string) error {
	if requestID == "" {
		return nil
	}
	var errs []error
	if receipts != nil {
		if err := receipts.Complete(requestID); err != nil && !errors.Is(err, errRestartReceiptTicketMismatch) {
			errs = append(errs, err)
		}
	}
	if leaseStore != nil {
		if err := leaseStore.Release(requestID); err != nil && !errors.Is(err, errRestartLeaseTicketMismatch) {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// CompleteReady releases the restart lease only after the new Gateway has
// registered adapters and started its HTTP listeners. The lifecycle receipt is
// intentionally kept until BroadcastStarted confirms delivery to its explicit
// restart target.
func (c *gatewayRestartCoordinator) CompleteReady() error {
	if c == nil || c.leaseStore == nil {
		return nil
	}
	lease, err := c.leaseStore.Read()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if lease.Phase != restartLeaseWaitingForReady {
		return nil
	}
	return c.leaseStore.Release(lease.RequestID)
}

func (c *gatewayRestartCoordinator) HandleGatewayCommand(ctx context.Context, command messaging.GatewayCommand, request messaging.GatewayRestartRequest) error {
	reply := func(replyCtx context.Context, text string) error {
		if request.Reply == nil {
			return nil
		}
		if err := request.Reply(replyCtx, text); err != nil {
			c.logger().Warn("gateway restart reply failed", "platform", "feishu", "bot_name", request.BotName, "error_kind", "reply_failed")
			return err
		}
		return nil
	}

	if command.Kind != messaging.GatewayCommandRestart {
		return reply(ctx, gatewayRestartHelpText)
	}
	allow := c.allowFeishu
	if allow == nil || !allow(request.BotName, request.ActorID) {
		c.logRestartAudit(gatewayRestartAuditRecord{
			Source:  string(messaging.PlatformFeishu),
			Actor:   request.ActorID,
			BotName: request.BotName,
			ChatID:  request.ChatID,
			Result:  "denied",
		})
		_ = reply(ctx, gatewayRestartDeniedText)
		return nil
	}

	ticket, err := c.Prepare(ctx, gatewayRestartRequest{
		Platform:    string(messaging.PlatformFeishu),
		Actor:       request.ActorID,
		BotName:     request.BotName,
		ChatID:      request.ChatID,
		PlatformKey: request.PlatformKey,
	})
	if err != nil {
		requestID, conflict := restartConflictRequestID(err)
		if conflict {
			c.logRestartAudit(gatewayRestartAuditRecord{
				RequestID: requestID,
				Source:    string(messaging.PlatformFeishu),
				Actor:     request.ActorID,
				BotName:   request.BotName,
				ChatID:    request.ChatID,
				Result:    "conflict",
			})
			_ = reply(ctx, fmt.Sprintf("%s 请求 ID: %s", gatewayRestartConflictText, requestID))
			return nil
		}
		c.logRestartAudit(gatewayRestartAuditRecord{
			Source:  string(messaging.PlatformFeishu),
			Actor:   request.ActorID,
			BotName: request.BotName,
			ChatID:  request.ChatID,
			Result:  "failed",
		})
		c.logger().Warn("gateway restart prepare failed", "platform", "feishu", "bot_name", request.BotName, "error_kind", "prepare_failed")
		_ = reply(ctx, gatewayRestartScheduleFailed)
		return nil
	}
	if err := reply(ctx, gatewayRestartAcceptedText); err != nil {
		c.logTicketAudit(ticket, "failed", 0)
		_ = c.Abort(ticket)
		return err
	}

	go func() {
		if err := c.Commit(ticket); err != nil {
			c.logger().Error("gateway restart commit failed", "request_id", ticket.RequestID, "error_kind", "commit_failed")
			_ = reply(context.Background(), gatewayRestartScheduleFailed)
		}
	}()
	return nil
}

func (c *gatewayRestartCoordinator) feishuRestartAllowed(botName, actorID string) bool {
	if c == nil || c.configStore == nil || actorID == "" {
		return false
	}
	cfg := c.configStore.Load()
	if cfg == nil {
		return false
	}
	var botAllow []string
	for i := range cfg.Messaging.Feishu.Bots {
		if cfg.Messaging.Feishu.Bots[i].Name == botName {
			botAllow = cfg.Messaging.Feishu.Bots[i].GatewayRestartAllowFrom
			break
		}
	}
	for _, allowed := range config.ResolveGatewayRestartAllowFrom(cfg.Messaging.Feishu.GatewayRestartAllowFrom, botAllow) {
		if allowed == actorID {
			return true
		}
	}
	return false
}

func (c *gatewayRestartCoordinator) retryPendingReceipt(ctx context.Context) error {
	if c == nil || c.receipts == nil {
		return nil
	}
	receipt, err := c.receipts.Read()
	if err != nil {
		_, _ = c.receipts.Quarantine()
		return fmt.Errorf("read pending gateway restart receipt: %w", err)
	}
	if receipt == nil {
		return nil
	}
	retry := c.retryReceipt
	if retry == nil {
		return &restartReceiptPendingError{RequestID: receipt.RequestID}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := retry(ctx, receipt); err != nil {
		return &restartReceiptPendingError{RequestID: receipt.RequestID}
	}
	if err := c.receipts.Complete(receipt.RequestID); err != nil {
		return fmt.Errorf("complete pending gateway restart receipt: %w", err)
	}
	info := newBuildInfo()
	logGatewayRestartAudit(c.logger(), gatewayRestartAuditRecord{
		RequestID:  receipt.RequestID,
		Source:     receipt.Platform,
		Actor:      receipt.Actor,
		BotName:    receipt.BotName,
		ChatID:     receipt.PlatformKey["chat_id"],
		Result:     "started",
		OldPID:     receipt.OldPID,
		NewPID:     os.Getpid(),
		OldVersion: receipt.OldVersion,
		NewVersion: info.Version,
	})
	return nil
}

func restartConflictRequestID(err error) (string, bool) {
	var leaseConflict *restartLeaseConflictError
	if errors.As(err, &leaseConflict) {
		return leaseConflict.RequestID, true
	}
	var receiptPending *restartReceiptPendingError
	if errors.As(err, &receiptPending) {
		return receiptPending.RequestID, true
	}
	return "", false
}

func (c *gatewayRestartCoordinator) currentTime() time.Time {
	if c != nil && c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *gatewayRestartCoordinator) logger() *slog.Logger {
	if c != nil && c.log != nil {
		return c.log
	}
	return slog.Default()
}

func (c *gatewayRestartCoordinator) logTicketAudit(ticket *gatewayRestartTicket, result string, helperPID int) {
	if ticket == nil {
		return
	}
	oldPID := 0
	if ticket.Instance != nil {
		oldPID = ticket.Instance.PID
	}
	c.logRestartAudit(gatewayRestartAuditRecord{
		RequestID:  ticket.RequestID,
		Source:     ticket.Source,
		Actor:      ticket.Actor,
		BotName:    ticket.BotName,
		ChatID:     ticket.ChatID,
		Result:     result,
		OldPID:     oldPID,
		HelperPID:  helperPID,
		OldVersion: ticket.OldVersion,
	})
}

func (c *gatewayRestartCoordinator) logRestartAudit(record gatewayRestartAuditRecord) {
	logGatewayRestartAudit(c.logger(), record)
}

func logGatewayRestartAudit(log *slog.Logger, record gatewayRestartAuditRecord) {
	if log == nil {
		log = slog.Default()
	}
	log.Info("gateway restart audit",
		"action", "gateway.restart",
		"request_id", record.RequestID,
		"source", record.Source,
		"actor", record.Actor,
		"bot_name", record.BotName,
		"chat_id", record.ChatID,
		"result", record.Result,
		"old_pid", record.OldPID,
		"helper_pid", record.HelperPID,
		"new_pid", record.NewPID,
		"old_version", record.OldVersion,
		"new_version", record.NewVersion,
	)
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	return maps.Clone(values)
}

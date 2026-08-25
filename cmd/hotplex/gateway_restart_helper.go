package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/service"
	"github.com/hrygo/hotplex/internal/worker/proc"
)

func newRestartHelperCmd() *cobra.Command {
	var oldPID int
	var source string
	var configPath string
	var level string
	var requestID string
	var devMode, daemon bool

	cmd := &cobra.Command{
		Use:    "_restart-helper",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRestartHelper(oldPID, source, configPath, level, requestID, devMode, daemon)
		},
	}
	cmd.Flags().IntVar(&oldPID, "old-pid", 0, "PID of the old gateway process")
	cmd.Flags().StringVar(&source, "source", "pid", "discovery source (pid|service)")
	cmd.Flags().StringVar(&configPath, "config", "", "config file path")
	cmd.Flags().StringVar(&level, "level", "", "service level (user|system)")
	cmd.Flags().StringVar(&requestID, "request-id", "", "restart request ID")
	cmd.Flags().BoolVar(&devMode, "dev", false, "development mode")
	cmd.Flags().BoolVarP(&daemon, "daemon", "d", false, "restart as daemon")
	return cmd
}

func (c *gatewayRestartCoordinator) spawnRestartHelper(ticket *gatewayRestartTicket) (int, error) {
	if ticket == nil || ticket.Instance == nil || ticket.RequestID == "" {
		return 0, fmt.Errorf("spawn restart helper: invalid ticket")
	}
	self, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("resolve executable: %w", err)
	}

	args := []string{
		"gateway", "_restart-helper",
		"--old-pid", fmt.Sprintf("%d", ticket.Instance.PID),
		"--source", string(ticket.Instance.Source),
		"--request-id", ticket.RequestID,
	}
	if ticket.ConfigPath != "" {
		args = append(args, "--config", ticket.ConfigPath)
	}
	if ticket.Instance.Level != "" {
		args = append(args, "--level", string(ticket.Instance.Level))
	}
	if ticket.DevMode {
		args = append(args, "--dev")
	}
	if ticket.Daemon {
		args = append(args, "-d")
	}

	logDir := filepath.Join(config.HotplexHome(), "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return 0, fmt.Errorf("create log dir: %w", err)
	}
	logPath := filepath.Join(logDir, "gateway-restart.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, fmt.Errorf("open restart log: %w", err)
	}
	defer func() { _ = logFile.Close() }()

	helperCmd := exec.Command(self, args...)
	helperCmd.Stdout = logFile
	helperCmd.Stderr = logFile
	helperCmd.Stdin = nil
	helperCmd.SysProcAttr = restartHelperSysProcAttr()

	if err := helperCmd.Start(); err != nil {
		return 0, fmt.Errorf("spawn restart helper: %w", err)
	}

	helperPID := helperCmd.Process.Pid
	_ = helperCmd.Process.Release()
	fmt.Fprintf(os.Stderr, "gateway: restart helper spawned (PID %d, log: %s)\n", helperPID, logPath)
	return helperPID, nil
}

func terminateRestartHelper(pid int) {
	if pid > 0 {
		_ = proc.ForceKillProcess(pid)
	}
}

func runRestartHelper(oldPID int, source, configPath, levelStr, requestID string, devMode, daemon bool) error {
	if requestID == "" {
		return fmt.Errorf("restart helper: missing request ID")
	}

	logDir := filepath.Join(config.HotplexHome(), "logs")
	logPath := filepath.Join(logDir, "gateway-restart.log")
	leaseStore := newRestartLeaseStore(restartMarkerPath(), time.Now, nil)
	receiptStore := newRestartReceiptStore(gatewayRestartReceiptPath())
	if err := waitForRestartHelperHandoff(leaseStore, requestID); err != nil {
		appendRestartLog(logPath, "restart lease handoff failed: %s\n", err)
		return restartHelperFailure(leaseStore, receiptStore, requestID, oldPID, fmt.Errorf("restart helper: lease handoff: %w", err))
	}
	if err := leaseStore.Update(requestID, func(lease *restartLease) error {
		lease.Phase = restartLeaseWaitingForReady
		lease.HelperPID = os.Getpid()
		return nil
	}); err != nil {
		appendRestartLog(logPath, "restart lease update failed: %s\n", err)
		return restartHelperFailure(leaseStore, receiptStore, requestID, oldPID, fmt.Errorf("restart helper: update lease: %w", err))
	}

	switch source {
	case "service":
		var lvl service.Level
		switch levelStr {
		case "system":
			lvl = service.LevelSystem
		default:
			lvl = service.LevelUser
		}
		if err := service.NewManager().Restart("hotplex", lvl); err != nil {
			appendRestartLog(logPath, "service restart failed: %s\n", err)
			return restartHelperFailure(leaseStore, receiptStore, requestID, oldPID, fmt.Errorf("service restart: %w", err))
		}
		appendRestartLog(logPath, "service restart completed\n")

	default: // "pid"
		appendRestartLog(logPath, "stopping old gateway (PID %d)\n", oldPID)

		if err := proc.Terminate(oldPID); err != nil {
			appendRestartLog(logPath, "terminate failed: %s, force killing\n", err)
			_ = proc.ForceKillProcess(oldPID)
		}
		waitForProcessExit(oldPID, 30*time.Second)

		if proc.IsProcessAlive(oldPID) == nil {
			appendRestartLog(logPath, "process %d still alive after timeout, force killing\n", oldPID)
			_ = proc.ForceKillProcess(oldPID)
			time.Sleep(500 * time.Millisecond)
		}

		removeGatewayState()
		appendRestartLog(logPath, "old gateway stopped, starting new instance\n")

		if daemon {
			if err := startDaemon(configPath, devMode); err != nil {
				appendRestartLog(logPath, "daemon start failed: %s\n", err)
				return err
			}
			appendRestartLog(logPath, "new gateway started as daemon\n")
			return nil
		}

		if err := writeGatewayState(configPath, devMode); err != nil {
			appendRestartLog(logPath, "warning: could not write PID file: %s\n", err)
		}
		if err := runGateway(configPath, devMode, nil); err != nil {
			removeGatewayState()
			appendRestartLog(logPath, "gateway run failed: %s\n", err)
			return err
		}
		appendRestartLog(logPath, "new gateway started\n")
	}

	return nil
}

func restartHelperFailure(
	leaseStore *restartLeaseStore,
	receiptStore *restartReceiptStore,
	requestID string,
	oldPID int,
	cause error,
) error {
	if cause == nil {
		return nil
	}
	record := gatewayRestartAuditRecord{
		RequestID: requestID,
		Result:    "failed",
		OldPID:    oldPID,
	}
	if receiptStore != nil {
		if receipt, err := receiptStore.Read(); err == nil && receipt != nil && receipt.RequestID == requestID {
			record.Source = receipt.Platform
			record.Actor = receipt.Actor
			record.BotName = receipt.BotName
			record.ChatID = receipt.PlatformKey["chat_id"]
			record.OldVersion = receipt.OldVersion
		}
	}
	logGatewayRestartAudit(slog.Default(), record)
	if leaseStore == nil || leaseStore.processAlive == nil || !leaseStore.processAlive(oldPID) {
		return cause
	}
	if err := abortRestartArtifacts(leaseStore, receiptStore, requestID); err != nil {
		return errors.Join(cause, fmt.Errorf("cleanup restart artifacts: %w", err))
	}
	return cause
}

func waitForRestartHelperHandoff(store *restartLeaseStore, requestID string) error {
	if store == nil || requestID == "" {
		return fmt.Errorf("invalid restart lease handoff")
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for {
		lease, err := store.Read()
		if err != nil {
			return err
		}
		if lease.RequestID != requestID {
			return errRestartLeaseTicketMismatch
		}
		if lease.Phase == restartLeaseHelperStarted || lease.Phase == restartLeaseWaitingForReady {
			return nil
		}
		select {
		case <-ticker.C:
		case <-timeout.C:
			return fmt.Errorf("timed out waiting for prepared restart lease handoff")
		}
	}
}

func appendRestartLog(path, format string, args ...interface{}) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "[%s] ", time.Now().Format(time.RFC3339))
	_, _ = fmt.Fprintf(f, format, args...)
}

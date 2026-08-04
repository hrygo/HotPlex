package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/audit"
	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/sqlutil"
)

func newAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Audit log chain operations",
	}
	cmd.AddCommand(newAuditVerifyCmd())
	return cmd
}

func newAuditVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify the audit hash chain integrity (read-only)",
		Long: `Verify the user_activity hash chain end to end, from the latest
checkpoint (or genesis) through every surviving row. Read-only: no rows
are modified.

Every break is reported in one pass (id, timestamp, platform, action,
expected vs actual prev_hash). A prev_hash_mismatch means a row was
deleted or modified outside the checkpoint-anchored GC path — since
migration 030 unauthorized deletes are rejected by the DB trigger, so a
fresh break now indicates tampering or a corrupted backup.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(cmd.Flag("config").Value.String(), false)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			log := slog.New(slog.NewTextHandler(os.Stderr, nil))
			dialect := dbutil.ParseDialect(cfg.DB.Driver)

			writeMu := sqlutil.NewWriteMu(string(dialect))
			var store audit.Store
			switch dialect {
			case dbutil.DialectPostgres:
				pgDB, err := dbutil.Open(dbutil.DialectPostgres, &cfg.DB)
				if err != nil {
					return fmt.Errorf("open pg db: %w", err)
				}
				defer func() { _ = pgDB.Close() }()
				store, err = audit.NewStore(pgDB.DB, dialect, nil, log)
				if err != nil {
					return fmt.Errorf("open audit store: %w", err)
				}
			default:
				s, err := session.NewSQLiteStore(context.Background(), cfg, writeMu)
				if err != nil {
					return fmt.Errorf("open session store: %w", err)
				}
				defer func() { _ = s.Close() }()
				store, err = audit.NewStore(s.DB(), dbutil.DialectSQLite, writeMu, log)
				if err != nil {
					return fmt.Errorf("open audit store: %w", err)
				}
			}
			defer func() { _ = store.Close() }()

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
			defer cancel()

			v := audit.NewVerifier(store, audit.VerifierConfig{}, log)
			result, err := v.VerifyOnce(ctx)
			if err != nil {
				return fmt.Errorf("audit verify: %w", err)
			}

			if result.BrokenID == 0 {
				fmt.Printf("audit chain OK: %d rows checked, no breaks\n", result.RowsChecked)
				return nil
			}

			fmt.Printf("audit chain BROKEN: %d break(s) found across %d rows checked\n",
				len(result.BrokenRows), result.RowsChecked)
			for _, b := range result.BrokenRows {
				fmt.Printf("  id=%d ts=%s platform=%s action=%s outcome=%s reason=%s expected_prev_hash=%s actual_prev_hash=%s\n",
					b.ID, time.UnixMilli(b.Ts).Format(time.RFC3339),
					b.Platform, b.Action, b.Outcome, b.Reason,
					b.ExpectedPrevHash, b.ActualPrevHash)
			}
			if result.BrokenID > 0 && len(result.BrokenRows) == 0 {
				fmt.Printf("  (first break id=%d reason=%s; rows were not fully decorated)\n",
					result.BrokenID, result.Reason)
			}
			fmt.Printf("advice: %s\n", audit.BreakAdvice(result.Reason))
			return nil
		},
	}
	cmd.PersistentFlags().String("config", "", "配置文件路径（默认 ~/.hotplex/config.yaml）")
	return cmd
}

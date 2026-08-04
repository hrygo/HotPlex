package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/audit"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/dbutil"
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
are modified and no migrations are applied.

Every break found is reported in one pass (up to 50), with per-reason
diagnostics: expected/actual prev_hash for chain-gap breaks
(prev_hash_mismatch), expected/actual self_hash for tamper breaks
(self_hash_mismatch). A prev_hash_mismatch means a row was deleted or
modified outside the checkpoint-anchored GC path — since migration 030
unauthorized deletes are rejected by the DB trigger, so a fresh break
now indicates tampering or a corrupted backup.

Exits non-zero when the chain is broken (CI/cron gate).`,
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
				// Open the SQLite file directly — no migration runner, so the
				// command never mutates the database (read-only contract).
				db, err := sql.Open(sqlutil.DriverName, cfg.DB.SQLite.Path)
				if err != nil {
					return fmt.Errorf("open sqlite db: %w", err)
				}
				defer func() { _ = db.Close() }()
				db.SetMaxOpenConns(1)
				if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
					return fmt.Errorf("set sqlite busy_timeout: %w", err)
				}
				if _, err := db.Exec("PRAGMA query_only=ON"); err != nil {
					return fmt.Errorf("set sqlite query_only: %w", err)
				}
				store, err = audit.NewStore(db, dbutil.DialectSQLite, writeMu, log)
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
				ts := time.UnixMilli(b.Ts).Format(time.RFC3339)
				if strings.HasPrefix(b.Reason, "prev_hash_mismatch") {
					fmt.Printf("  id=%d ts=%s platform=%s action=%s outcome=%s reason=%s expected_prev_hash=%s actual_prev_hash=%s\n",
						b.ID, ts, b.Platform, b.Action, b.Outcome, b.Reason,
						b.ExpectedPrevHash, b.ActualPrevHash)
				} else {
					fmt.Printf("  id=%d ts=%s platform=%s action=%s outcome=%s reason=%s expected_self_hash=%s actual_self_hash=%s\n",
						b.ID, ts, b.Platform, b.Action, b.Outcome, b.Reason,
						b.ExpectedSelfHash, b.ActualSelfHash)
				}
			}
			if result.BrokenID > 0 && len(result.BrokenRows) == 0 {
				fmt.Printf("  (first break id=%d reason=%s; rows were not fully decorated)\n",
					result.BrokenID, result.Reason)
			}
			fmt.Printf("advice: %s\n", audit.BreakAdvice(result.Reason))
			// Non-zero exit so CI/cron callers can gate on chain integrity.
			return fmt.Errorf("audit chain broken: %d break(s) found across %d rows checked",
				len(result.BrokenRows), result.RowsChecked)
		},
	}
	cmd.PersistentFlags().String("config", config.DefaultConfigPath, "配置文件路径（默认 ~/.hotplex/config.yaml）")
	return cmd
}

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
	cmd.AddCommand(newAuditRebaseCmd())
	return cmd
}

// openAuditStoreCLI opens the audit store for a CLI command. When queryOnly
// is true the SQLite file is opened read-only (PRAGMA query_only=ON) so the
// command can never mutate the database; write commands (rebase) pass false.
// The returned close function closes both the raw DB handle and the store.
func openAuditStoreCLI(cfg *config.Config, log *slog.Logger, queryOnly bool) (audit.Store, func(), error) {
	dialect := dbutil.ParseDialect(cfg.DB.Driver)
	writeMu := sqlutil.NewWriteMu(string(dialect))
	switch dialect {
	case dbutil.DialectPostgres:
		pgDB, err := dbutil.Open(dbutil.DialectPostgres, &cfg.DB)
		if err != nil {
			return nil, nil, fmt.Errorf("open pg db: %w", err)
		}
		store, err := audit.NewStore(pgDB.DB, dialect, nil, log)
		if err != nil {
			_ = pgDB.Close()
			return nil, nil, fmt.Errorf("open audit store: %w", err)
		}
		return store, func() { _ = pgDB.Close(); _ = store.Close() }, nil
	default:
		// Open the SQLite file directly — no migration runner, so the
		// command never mutates the schema.
		db, err := sql.Open(sqlutil.DriverName, cfg.DB.SQLite.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("open sqlite db: %w", err)
		}
		db.SetMaxOpenConns(1)
		if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
			_ = db.Close()
			return nil, nil, fmt.Errorf("set sqlite busy_timeout: %w", err)
		}
		if queryOnly {
			if _, err := db.Exec("PRAGMA query_only=ON"); err != nil {
				_ = db.Close()
				return nil, nil, fmt.Errorf("set sqlite query_only: %w", err)
			}
		}
		store, err := audit.NewStore(db, dbutil.DialectSQLite, writeMu, log)
		if err != nil {
			_ = db.Close()
			return nil, nil, fmt.Errorf("open audit store: %w", err)
		}
		return store, func() { _ = store.Close() }, nil
	}
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
			store, closeStore, err := openAuditStoreCLI(cfg, log, true)
			if err != nil {
				return err
			}
			defer closeStore()

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
	cmd.PersistentFlags().String("config", config.DefaultConfigPath(), "配置文件路径（默认 $HOTPLEX_HOME/config.yaml，未设置时为 ~/.hotplex/config.yaml）")
	return cmd
}

func newAuditRebaseCmd() *cobra.Command {
	var (
		nextID  int64
		confirm bool
	)
	cmd := &cobra.Command{
		Use:   "rebase",
		Short: "Re-anchor the audit hash chain at a surviving row (repair)",
		Long: `Repair a broken audit hash chain by re-anchoring it at the row with
the given id. The row's stored prev_hash becomes the new checkpoint anchor
and verify continues from this row; rows before the anchor are retained but
no longer chain-verified.

This is the only legitimate repair for a chain broken by a historical
manual DELETE — migrations 023/030 reject row UPDATE and un-anchored DELETE,
so the broken row cannot be edited or removed in place. The rebase writes
one checkpoint row and is irreversible: re-run with --confirm to apply.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(cmd.Flag("config").Value.String(), false)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			log := slog.New(slog.NewTextHandler(os.Stderr, nil))
			store, closeStore, err := openAuditStoreCLI(cfg, log, false)
			if err != nil {
				return err
			}
			defer closeStore()

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
			defer cancel()

			// Preview the target row before doing anything: the anchor must be
			// an id with a surviving row, never silently shifted elsewhere.
			rows, err := store.QueryAsc(ctx, nextID, 1)
			if err != nil {
				return fmt.Errorf("audit rebase: query target row: %w", err)
			}
			if len(rows) == 0 || rows[0].ID != nextID {
				return fmt.Errorf("%w: id=%d", audit.ErrRebaseRowNotFound, nextID)
			}
			row := rows[0]
			fmt.Printf("rebase target: id=%d ts=%s platform=%s action=%s outcome=%s\n",
				row.ID, time.UnixMilli(row.Ts).Format(time.RFC3339), row.Platform, row.Action, row.Outcome)
			fmt.Printf("new checkpoint: next_id=%d anchor_hash=%s\n", nextID, row.PrevHash)
			if !confirm {
				return fmt.Errorf("audit rebase is irreversible; re-run with --confirm")
			}

			if _, err := audit.Rebase(ctx, store, nextID); err != nil {
				return err
			}
			// SaveCheckpoint does not return the inserted row id; re-read the
			// latest checkpoint to report the real id.
			latest, err := store.LatestCheckpoint(ctx)
			if err != nil {
				return fmt.Errorf("audit rebase: read checkpoint: %w", err)
			}
			fmt.Printf("checkpoint written: id=%d next_id=%d anchor_hash=%s\n",
				latest.ID, latest.NextID, latest.LastSelfHash)

			// Report the repaired state in the same pass.
			result, err := audit.NewVerifier(store, audit.VerifierConfig{}, log).VerifyOnce(ctx)
			if err != nil {
				return fmt.Errorf("audit verify after rebase: %w", err)
			}
			if result.BrokenID == 0 {
				fmt.Printf("audit chain OK after rebase: %d rows checked, no breaks\n", result.RowsChecked)
				return nil
			}
			fmt.Printf("audit chain still BROKEN after rebase: %d break(s) found across %d rows checked\n",
				len(result.BrokenRows), result.RowsChecked)
			return fmt.Errorf("audit chain broken after rebase: %d break(s) found across %d rows checked",
				len(result.BrokenRows), result.RowsChecked)
		},
	}
	cmd.Flags().Int64Var(&nextID, "next-id", 0, "id of the first row to keep in the chain (its prev_hash becomes the anchor)")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "required to apply the irreversible rebase")
	if err := cmd.MarkFlagRequired("next-id"); err != nil {
		panic(err)
	}
	cmd.PersistentFlags().String("config", config.DefaultConfigPath(), "配置文件路径（默认 $HOTPLEX_HOME/config.yaml，未设置时为 ~/.hotplex/config.yaml）")
	return cmd
}

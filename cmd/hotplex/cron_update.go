package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/agentconfig"
	croncli "github.com/hrygo/hotplex/internal/cli/cron"
	"github.com/hrygo/hotplex/internal/cron"
)

func newCronUpdateCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "update <id|name>",
		Short: "Update a cron job",
		Long: `Update an existing cron job. Only specified flags are modified.

Schedule format:
  --schedule "cron:*/5 * * * *"
  --schedule "every:30m"
  --schedule "at:2026-01-01T00:00:00Z"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("requires an <id|name> argument.\nSee 'hotplex cron update --help' for usage")
			}
			return withStore(context.Background(), configPath, func(store croncli.Store) error {
				job, err := croncli.ResolveJob(store, context.Background(), args[0])
				if err != nil {
					return err
				}

				if changed, err := applyFlags(cmd, job); err != nil {
					return err
				} else if changed {
					if err := cron.ValidateJob(job); err != nil {
						return err
					}

					job.UpdatedAtMs = time.Now().UnixMilli()

					if cmd.Flags().Changed("schedule") {
						next, err := cron.NextRun(job.Schedule, time.Now())
						if err != nil {
							return fmt.Errorf("compute next run: %w", err)
						}
						job.State.NextRunAtMs = next.UnixMilli()
					}

					if err := store.Update(context.Background(), job); err != nil {
						return fmt.Errorf("update job: %w", err)
					}

					warnIfGatewayNotNotified(croncli.NotifyGateway())
					fmt.Printf("Updated job %s (%s)\n", job.ID, job.Name)
				} else {
					fmt.Println("No changes specified.")
				}
				return nil
			})
		},
	}
	configFlag(cmd, &configPath)
	cmd.Flags().String("schedule", "", "schedule expression")
	cmd.Flags().StringP("message", "m", "", "prompt message")
	cmd.Flags().String("description", "", "job description")
	cmd.Flags().String("work-dir", "", "working directory")
	cmd.Flags().String("bot-id", "", "bot ID")
	cmd.Flags().String("bot-name", "", "bot name for agent config resolution")
	cmd.Flags().String("owner-id", "", "owner ID")
	cmd.Flags().Int("timeout", 0, "execution timeout in seconds")
	cmd.Flags().String("allowed-tools", "", "comma-separated tool list")
	cmd.Flags().Bool("enabled", true, "enable or disable the job")
	cmd.Flags().Bool("delete-after-run", false, "delete one-shot job after execution")
	cmd.Flags().Bool("silent", false, "suppress result delivery (self-maintenance tasks)")
	cmd.Flags().Int("max-retries", 0, "max retries for failed one-shot jobs")
	cmd.Flags().Int("max-runs", 0, "max executions before auto-disable (required for every/cron)")
	cmd.Flags().String("expires-at", "", "auto-disable after this time RFC3339 (required for every/cron)")
	cmd.Flags().String("worker-type", "", "AI Agent engine (claude_code|opencode_server|codex_cli|acp)")
	return cmd
}

// applyFlags applies changed CLI flags to the job and returns true if any were changed.
func applyFlags(cmd *cobra.Command, job *cron.CronJob) (bool, error) {
	changed := false

	if cmd.Flags().Changed("schedule") {
		raw, _ := cmd.Flags().GetString("schedule")
		if sched, err := croncli.ParseSchedule(raw); err == nil {
			job.Schedule = sched
			changed = true
		}
	}
	if cmd.Flags().Changed("message") {
		msg, _ := cmd.Flags().GetString("message")
		job.Payload.Message = cron.SanitizePrompt(msg)
		changed = true
	}
	if cmd.Flags().Changed("description") {
		job.Description, _ = cmd.Flags().GetString("description")
		changed = true
	}
	if cmd.Flags().Changed("work-dir") {
		job.WorkDir, _ = cmd.Flags().GetString("work-dir")
		changed = true
	}
	if cmd.Flags().Changed("bot-id") {
		job.BotID, _ = cmd.Flags().GetString("bot-id")
		changed = true
	}
	if cmd.Flags().Changed("bot-name") {
		job.BotName, _ = cmd.Flags().GetString("bot-name")
		if err := agentconfig.ValidateBotName(job.BotName); err != nil {
			return false, fmt.Errorf("invalid bot_name: %w", err)
		}
		changed = true
	}
	if cmd.Flags().Changed("owner-id") {
		job.OwnerID, _ = cmd.Flags().GetString("owner-id")
		changed = true
	}
	if cmd.Flags().Changed("timeout") {
		job.TimeoutSec, _ = cmd.Flags().GetInt("timeout")
		changed = true
	}
	if cmd.Flags().Changed("allowed-tools") {
		raw, _ := cmd.Flags().GetString("allowed-tools")
		job.Payload.AllowedTools = strings.Split(raw, ",")
		changed = true
	}
	if cmd.Flags().Changed("enabled") {
		job.Enabled, _ = cmd.Flags().GetBool("enabled")
		changed = true
	}
	if cmd.Flags().Changed("delete-after-run") {
		job.DeleteAfterRun, _ = cmd.Flags().GetBool("delete-after-run")
		changed = true
	}
	if cmd.Flags().Changed("silent") {
		job.Silent, _ = cmd.Flags().GetBool("silent")
		changed = true
	}
	if cmd.Flags().Changed("max-retries") {
		job.MaxRetries, _ = cmd.Flags().GetInt("max-retries")
		changed = true
	}
	if cmd.Flags().Changed("max-runs") {
		job.MaxRuns, _ = cmd.Flags().GetInt("max-runs")
		changed = true
	}
	if cmd.Flags().Changed("expires-at") {
		job.ExpiresAt, _ = cmd.Flags().GetString("expires-at")
		changed = true
	}
	if cmd.Flags().Changed("worker-type") {
		job.Payload.WorkerType, _ = cmd.Flags().GetString("worker-type")
		changed = true
	}

	return changed, nil
}

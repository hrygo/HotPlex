package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hotplex/hotplex-worker/internal/cli/onboard"
)

func newOnboardCmd() *cobra.Command {
	var nonInteractive, force bool
	var configPath string
	var enableSlack, enableFeishu bool
	var slackAllowFrom, feishuAllowFrom []string
	var slackDMPolicy, slackGroupPolicy string
	var feishuDMPolicy, feishuGroupPolicy string

	cmd := &cobra.Command{
		Use:   "onboard",
		Short: "Interactive configuration wizard",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				configPath = "~/.hotplex/config.yaml"
			}
			if strings.HasPrefix(configPath, "~/") {
				home, _ := os.UserHomeDir()
				if home != "" {
					configPath = filepath.Join(home, configPath[2:])
				}
			}

			result, err := onboard.Run(context.Background(), onboard.WizardOptions{
				ConfigPath:        configPath,
				NonInteractive:    nonInteractive,
				Force:             force,
				EnableSlack:       enableSlack,
				EnableFeishu:      enableFeishu,
				SlackAllowFrom:    slackAllowFrom,
				SlackDMPolicy:     slackDMPolicy,
				SlackGroupPolicy:  slackGroupPolicy,
				FeishuAllowFrom:   feishuAllowFrom,
				FeishuDMPolicy:    feishuDMPolicy,
				FeishuGroupPolicy: feishuGroupPolicy,
			})
			if err != nil {
				return err
			}

			fmt.Fprintln(os.Stderr)
			fmt.Fprintf(os.Stderr, "HotPlex Onboard %s\n\n", versionString())

			for _, step := range result.Steps {
				symbol := "?"
				switch step.Status {
				case "pass":
					symbol = "✓"
				case "skip":
					symbol = "○"
				case "fail":
					symbol = "✗"
				}
				fmt.Fprintf(os.Stderr, "  %s %-20s %s\n", symbol, step.Name, step.Detail)
			}

			fmt.Fprintln(os.Stderr)

			var hasFail bool
			for _, step := range result.Steps {
				if step.Status == "fail" {
					hasFail = true
					break
				}
			}
			if hasFail {
				fmt.Fprintln(os.Stderr, "  Some steps failed. Review errors above.")
				os.Exit(1)
			}

			fmt.Fprintln(os.Stderr, "  Configuration complete. Run 'hotplex gateway' to start.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "use defaults, no prompts")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing configuration")
	cmd.Flags().StringVarP(&configPath, "config", "c", "~/.hotplex/config.yaml", "config file path")

	cmd.Flags().BoolVar(&enableSlack, "enable-slack", false, "enable Slack in non-interactive mode (credentials in .env)")
	cmd.Flags().BoolVar(&enableFeishu, "enable-feishu", false, "enable Feishu in non-interactive mode (credentials in .env)")
	cmd.Flags().StringSliceVar(&slackAllowFrom, "slack-allow-from", nil, "Slack allowed user IDs")
	cmd.Flags().StringVar(&slackDMPolicy, "slack-dm-policy", "allowlist", "Slack DM policy: open, allowlist, disabled")
	cmd.Flags().StringVar(&slackGroupPolicy, "slack-group-policy", "allowlist", "Slack group policy: open, allowlist, disabled")
	cmd.Flags().StringSliceVar(&feishuAllowFrom, "feishu-allow-from", nil, "Feishu allowed user IDs")
	cmd.Flags().StringVar(&feishuDMPolicy, "feishu-dm-policy", "allowlist", "Feishu DM policy: open, allowlist, disabled")
	cmd.Flags().StringVar(&feishuGroupPolicy, "feishu-group-policy", "allowlist", "Feishu group policy: open, allowlist, disabled")

	return cmd
}

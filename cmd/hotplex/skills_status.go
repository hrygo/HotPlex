package main

import (
	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/skills/reconcile"
)

func newSkillsStatusCmd(deps skillsCommandDeps) *cobra.Command {
	var (
		configPath  string
		profile     string
		workerFlags []string
		jsonOutput  bool
	)
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Inspect built-in skill inventory and projections",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSkillsOperation(cmd, deps, configPath, profile, workerFlags, false, jsonOutput,
				func(runner reconcile.Runner, options reconcile.Options) (reconcile.Report, error) {
					return runner.Status(cmd.Context(), options)
				})
		},
	}
	configFlag(cmd, &configPath)
	cmd.Flags().StringVar(&profile, "profile", "runtime", "built-in skill profile (runtime or operator)")
	cmd.Flags().StringArrayVar(&workerFlags, "worker", nil, "worker type (repeatable; default resolves enabled messaging workers)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output the typed report as JSON")
	return cmd
}

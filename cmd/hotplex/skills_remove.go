package main

import (
	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/skills/reconcile"
)

func newSkillsRemoveCmd(deps skillsCommandDeps) *cobra.Command {
	var (
		configPath  string
		profile     string
		workerFlags []string
		dryRun      bool
		jsonOutput  bool
	)
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove managed built-in skill projections",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSkillsOperation(cmd, deps, configPath, profile, workerFlags, dryRun, jsonOutput,
				func(runner reconcile.Runner, options reconcile.Options) (reconcile.Report, error) {
					return runner.Remove(cmd.Context(), options)
				})
		},
	}
	configFlag(cmd, &configPath)
	cmd.Flags().StringVar(&profile, "profile", "runtime", "built-in skill profile (runtime or operator)")
	cmd.Flags().StringArrayVar(&workerFlags, "worker", nil, "worker type (repeatable; default resolves enabled messaging workers)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report changes without writing")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output the typed report as JSON")
	return cmd
}

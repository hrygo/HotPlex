package main

import "github.com/spf13/cobra"

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "hotplex",
		Short: "HotPlex Worker Gateway",
		Long: "HotPlex Worker Gateway — unified access layer for AI Coding Agent sessions.\n\n" +
			"WebSocket gateway abstracting Claude Code and OpenCode Server protocol differences.\n" +
			"Connects users across Web, Slack, and Feishu through one optimized binary.\n\n" +
			"Quick start:\n" +
			"  hotplex dev                  # Start in development mode\n" +
			"  hotplex gateway start        # Start production gateway\n" +
			"  hotplex onboard              # Interactive setup wizard\n" +
			"  hotplex doctor               # Run diagnostic checks",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.AddCommand(
		newGatewayCmd(),
		newDoctorCmd(),
		newSecurityCmd(),
		newOnboardCmd(),
		newVersionCmd(),
		newDevCmd(),
		newConfigCmd(),
		newStatusCmd(),
		newServiceCmd(),
		newInstallCmd(),
		newUpdateCmd(),
		newSlackCmd(),
		newCronCmd(),
		newSkillsCmd(),
		newAdminCmd(),
		newAuditCmd(),
		newRuntimeCmd(),
	)
	return rootCmd
}

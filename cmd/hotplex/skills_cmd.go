package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/skills/builtin"
	"github.com/hrygo/hotplex/internal/skills/reconcile"
)

type skillsCommandDeps struct {
	LoadConfig     func(string) (*config.Config, error)
	UserHomeDir    func() (string, error)
	HotplexHomeDir func() string
	NewRunner      func(userHome, hotplexHome string) (reconcile.Runner, error)
	Output         io.Writer
	ErrOutput      io.Writer
}

func newSkillsCmd() *cobra.Command {
	return newSkillsCmdWithDeps(skillsCommandDeps{
		LoadConfig:     config.Load,
		UserHomeDir:    os.UserHomeDir,
		HotplexHomeDir: config.HotplexHome,
		NewRunner:      newSkillsRunner,
		Output:         os.Stdout,
		ErrOutput:      os.Stderr,
	})
}

func newSkillsCmdWithDeps(deps skillsCommandDeps) *cobra.Command {
	defaults := skillsCommandDeps{
		LoadConfig:     config.Load,
		UserHomeDir:    os.UserHomeDir,
		HotplexHomeDir: config.HotplexHome,
		NewRunner:      newSkillsRunner,
		Output:         io.Discard,
		ErrOutput:      io.Discard,
	}
	if deps.LoadConfig == nil {
		deps.LoadConfig = defaults.LoadConfig
	}
	if deps.UserHomeDir == nil {
		deps.UserHomeDir = defaults.UserHomeDir
	}
	if deps.HotplexHomeDir == nil {
		deps.HotplexHomeDir = defaults.HotplexHomeDir
	}
	if deps.NewRunner == nil {
		deps.NewRunner = defaults.NewRunner
	}
	if deps.Output == nil {
		deps.Output = defaults.Output
	}
	if deps.ErrOutput == nil {
		deps.ErrOutput = defaults.ErrOutput
	}

	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage built-in agent skills",
	}
	cmd.SetOut(deps.Output)
	cmd.SetErr(deps.ErrOutput)
	cmd.AddCommand(
		newSkillsStatusCmd(deps),
		newSkillsSyncCmd(deps),
		newSkillsRemoveCmd(deps),
	)
	return cmd
}

func newSkillsRunner(userHome, hotplexHome string) (reconcile.Runner, error) {
	registry, err := builtin.NewRegistry()
	if err != nil {
		return nil, err
	}
	paths := reconcile.Paths{
		UserHome:     userHome,
		HotplexHome:  hotplexHome,
		InventoryDir: filepath.Join(hotplexHome, "skills", "builtin"),
		StateDir:     filepath.Join(hotplexHome, "state", "skills"),
		NativeRoots: map[reconcile.WorkerType]string{
			reconcile.WorkerClaude:   filepath.Join(userHome, ".claude", "skills"),
			reconcile.WorkerCodex:    filepath.Join(userHome, ".agents", "skills"),
			reconcile.WorkerOpenCode: filepath.Join(userHome, ".agents", "skills"),
		},
	}
	return reconcile.New(registry, paths, reconcile.NewOSFileSystem())
}

func loadSkillsOptions(deps skillsCommandDeps, configPath, profile string, workerFlags []string, dryRun bool) (reconcile.Options, error) {
	selectedProfile, err := parseSkillsProfile(profile)
	if err != nil {
		return reconcile.Options{}, err
	}
	workerTypes := make([]reconcile.WorkerType, 0, len(workerFlags))
	for _, value := range workerFlags {
		workerType, parseErr := reconcile.ParseWorkerType(value)
		if parseErr != nil {
			return reconcile.Options{}, parseErr
		}
		workerTypes = append(workerTypes, workerType)
	}
	if len(workerTypes) == 0 {
		cfg, loadErr := deps.LoadConfig(configPath)
		if loadErr != nil {
			return reconcile.Options{}, loadErr
		}
		if cfg != nil {
			for _, value := range cfg.EnabledWorkerTypes() {
				workerType, parseErr := reconcile.ParseWorkerType(value)
				if parseErr != nil {
					return reconcile.Options{}, parseErr
				}
				workerTypes = append(workerTypes, workerType)
			}
		}
	}
	if len(workerTypes) == 0 {
		return reconcile.Options{}, reconcile.ErrNoWorkerTargets
	}
	return reconcile.Options{Profile: selectedProfile, WorkerTypes: workerTypes, DryRun: dryRun}, nil
}

func parseSkillsProfile(value string) (builtin.Profile, error) {
	switch builtin.Profile(value) {
	case builtin.ProfileRuntime, builtin.ProfileOperator:
		return builtin.Profile(value), nil
	default:
		return "", fmt.Errorf("%w: %s", reconcile.ErrUnknownProfile, value)
	}
}

func runSkillsOperation(
	cmd *cobra.Command,
	deps skillsCommandDeps,
	configPath, profile string,
	workerFlags []string,
	dryRun, jsonOutput bool,
	operation func(reconcile.Runner, reconcile.Options) (reconcile.Report, error),
) error {
	options, err := loadSkillsOptions(deps, configPath, profile, workerFlags, dryRun)
	if err != nil {
		return err
	}
	userHome, err := deps.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve user home: %w", err)
	}
	hotplexHome := deps.HotplexHomeDir()
	if strings.TrimSpace(hotplexHome) == "" {
		return fmt.Errorf("resolve HotPlex home: empty path")
	}
	runner, err := deps.NewRunner(userHome, hotplexHome)
	if err != nil {
		return err
	}
	if runner == nil {
		return fmt.Errorf("skills: nil runner")
	}
	report, operationErr := operation(runner, options)
	if outputErr := renderSkillsReport(cmd.OutOrStdout(), report, jsonOutput); outputErr != nil {
		return outputErr
	}
	if operationErr != nil {
		return operationErr
	}
	return report.Err()
}

func renderSkillsReport(output io.Writer, report reconcile.Report, jsonOutput bool) error {
	if jsonOutput {
		return json.NewEncoder(output).Encode(report)
	}
	for _, item := range report.Items {
		if _, err := fmt.Fprintf(output, "action=%s outcome=%s reason=%s", item.Action, item.Outcome, item.ReasonCode); err != nil {
			return err
		}
		if item.Target != "" {
			if _, err := fmt.Fprintf(output, " target=%s", item.Target); err != nil {
				return err
			}
		}
		if len(item.WorkerAliases) > 0 {
			aliases := make([]string, 0, len(item.WorkerAliases))
			for _, alias := range item.WorkerAliases {
				aliases = append(aliases, string(alias))
			}
			if _, err := fmt.Fprintf(output, " aliases=%s", strings.Join(aliases, ",")); err != nil {
				return err
			}
		}
		if item.BackupPath != "" {
			if _, err := fmt.Fprintf(output, " backup=%s", item.BackupPath); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(output); err != nil {
			return err
		}
	}
	return nil
}

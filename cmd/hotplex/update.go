package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/cli/output"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/service"
	"github.com/hrygo/hotplex/internal/skills/builtin"
	"github.com/hrygo/hotplex/internal/skills/reconcile"
	"github.com/hrygo/hotplex/internal/updater"
)

type updateSkillsDeps struct {
	LoadConfig     func(string) (*config.Config, error)
	UserHomeDir    func() (string, error)
	HotplexHomeDir func() string
	NewRunner      func(userHome, hotplexHome string) (reconcile.Runner, error)
}

func defaultUpdateSkillsDeps() updateSkillsDeps {
	return updateSkillsDeps{
		LoadConfig:     config.Load,
		UserHomeDir:    os.UserHomeDir,
		HotplexHomeDir: config.HotplexHome,
		NewRunner:      newSkillsRunner,
	}
}

func newUpdateCmd() *cobra.Command {
	var checkOnly, yes, restart, syncSkills bool
	var skillsProfile string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update hotplex to the latest version",
		Long: `Check for updates and install the latest version of hotplex.

Downloads the archive from GitHub Releases, verifies the sha256 checksum,
extracts the binary, and atomically replaces the running binary.

Supports all platforms: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64,
windows/amd64, windows/arm64.`,
		Example: `  hotplex update              # Interactive update
  hotplex update --check      # Only check, don't download
  hotplex update -y           # Skip confirmation prompt
  hotplex update --restart    # Restart service after update
  hotplex update --sync-skills --skills-profile runtime # Explicitly sync built-in skills`,
		RunE: func(cmd *cobra.Command, args []string) error {
			profile, profileErr := parseSkillsProfile(skillsProfile)
			if profileErr != nil {
				return profileErr
			}
			if !syncSkills && cmd.Flags().Changed("skills-profile") && profile != builtin.ProfileRuntime {
				return fmt.Errorf("--skills-profile %s requires --sync-skills", profile)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Minute)
			defer cancel()

			u := updater.New(versionString())

			fmt.Fprintf(os.Stderr, "  Current: %s\n", versionString())

			if updater.IsDocker() {
				fmt.Fprintf(os.Stderr, "  %s Running inside Docker — updates will be lost on container restart\n",
					output.StatusSymbol("warn"))
			}

			result, err := u.Check(ctx)
			if err != nil {
				return err
			}

			if !result.UpdateAvailable {
				fmt.Fprintf(os.Stderr, "  Already up-to-date (%s)\n", result.LatestVersion)
				return nil
			}

			fmt.Fprintf(os.Stderr, "  Update available: %s → %s\n",
				result.CurrentVersion, result.LatestVersion)

			if checkOnly {
				fmt.Fprintf(os.Stderr, "  Use %s to install\n", output.Bold("hotplex update"))
				return nil
			}

			if !yes {
				fmt.Fprintf(os.Stderr, "  Do you want to update to %s? [y/N] ", result.LatestVersion)
				reader := bufio.NewReader(os.Stdin)
				input, _ := reader.ReadString('\n')
				if strings.ToLower(strings.TrimSpace(input)) != "y" && strings.ToLower(strings.TrimSpace(input)) != "yes" {
					fmt.Fprintf(os.Stderr, "  Update cancelled\n")
					return nil
				}
			}

			// Check write permission before downloading.
			if _, err := updater.IsWritable(); err != nil {
				return err
			}

			// Download archive (or legacy raw binary).
			fmt.Fprintf(os.Stderr, "  Downloading %s ...\n", result.AssetName)
			archivePath, err := u.Download(ctx, result.DownloadURL)
			if err != nil {
				return err
			}
			defer func() { _ = os.Remove(archivePath) }()

			// Verify checksum.
			fmt.Fprintf(os.Stderr, "  Verifying checksum...\n")
			if err := u.VerifyChecksum(ctx, result.ChecksumURL, result.AssetName, archivePath); err != nil {
				return fmt.Errorf("checksum verification failed: %w", err)
			}

			// Extract binary from archive (skip for legacy raw binary releases).
			var tmpPath string
			if result.IsLegacyBinary {
				tmpPath = archivePath
			} else {
				fmt.Fprintf(os.Stderr, "  Extracting...\n")
				tmpPath, err = u.Extract(archivePath)
				if err != nil {
					return err
				}
				defer func() { _ = os.Remove(tmpPath) }()
			}
			// Detect running gateway before replacing.
			gatewayInst, gatewayErr := findRunningGateway()

			// Replace binary.
			fmt.Fprintf(os.Stderr, "  Installing...\n")
			if err := u.Replace(tmpPath); err != nil {
				return err
			}

			if syncSkills {
				report, syncErr := maybeSyncSkillsAfterUpdate(ctx, true, cmd.Flags().Changed("skills-profile"), config.DefaultConfigPath(), profile, defaultUpdateSkillsDeps())
				if outputErr := renderSkillsReport(cmd.OutOrStdout(), report, false); outputErr != nil {
					return outputErr
				}
				if syncErr != nil {
					return syncErr
				}
				if reportErr := report.Err(); reportErr != nil {
					return reportErr
				}
				fmt.Fprintf(os.Stderr, "  %s Built-in skills synchronized\n", output.StatusSymbol("pass"))
			}

			fmt.Fprintf(os.Stderr, "  %s Updated to %s\n",
				output.StatusSymbol("pass"), result.LatestVersion)

			// Handle service restart.
			if gatewayErr != nil {
				return nil
			}

			shouldRestart := restart || yes
			if !shouldRestart {
				fmt.Fprintf(os.Stderr, "  Gateway is running (PID %d). Restart now? [y/N] ", gatewayInst.PID)
				reader := bufio.NewReader(os.Stdin)
				input, _ := reader.ReadString('\n')
				shouldRestart = strings.EqualFold(strings.TrimSpace(input), "y")
			}

			if !shouldRestart {
				fmt.Fprintf(os.Stderr, "  Gateway will use the new binary on next restart\n")
				return nil
			}

			return restartAfterUpdate(gatewayInst)
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "only check for updates, do not download")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&restart, "restart", false, "restart service after successful update")
	cmd.Flags().BoolVar(&syncSkills, "sync-skills", false, "explicitly synchronize built-in skill projections after update")
	cmd.Flags().StringVar(&skillsProfile, "skills-profile", "runtime", "built-in skill profile (runtime or operator); requires --sync-skills for operator")
	return cmd
}

func maybeSyncSkillsAfterUpdate(ctx context.Context, syncRequested, profileChanged bool, configPath string, profile builtin.Profile, deps updateSkillsDeps) (reconcile.Report, error) {
	if profile != builtin.ProfileRuntime && profile != builtin.ProfileOperator {
		return reconcile.Report{Profile: profile}, reconcile.ErrUnknownProfile
	}
	if !syncRequested {
		if profileChanged && profile != builtin.ProfileRuntime {
			return reconcile.Report{Profile: profile}, fmt.Errorf("--skills-profile %s requires --sync-skills", profile)
		}
		return reconcile.Report{}, nil
	}
	if deps.LoadConfig == nil {
		deps.LoadConfig = config.Load
	}
	if deps.UserHomeDir == nil {
		deps.UserHomeDir = os.UserHomeDir
	}
	if deps.HotplexHomeDir == nil {
		deps.HotplexHomeDir = config.HotplexHome
	}
	if deps.NewRunner == nil {
		deps.NewRunner = newSkillsRunner
	}
	report := reconcile.Report{Profile: profile}
	cfg, err := deps.LoadConfig(configPath)
	if err != nil {
		return report, fmt.Errorf("load config for skills sync: %w", err)
	}
	if cfg == nil {
		return report, fmt.Errorf("load config for skills sync: empty config")
	}
	workers, err := parseUpdateSkillWorkerTypes(cfg.EnabledWorkerTypes())
	if err != nil {
		return report, err
	}
	if len(workers) == 0 {
		return report, reconcile.ErrNoWorkerTargets
	}
	userHome, err := deps.UserHomeDir()
	if err != nil {
		return report, fmt.Errorf("resolve user home: %w", err)
	}
	runner, err := deps.NewRunner(userHome, deps.HotplexHomeDir())
	if err != nil {
		return report, err
	}
	if runner == nil {
		return report, fmt.Errorf("skills: nil runner")
	}
	report, err = runner.Sync(ctx, reconcile.Options{Profile: profile, WorkerTypes: workers})
	if err != nil {
		return report, err
	}
	return report, report.Err()
}

func parseUpdateSkillWorkerTypes(values []string) ([]reconcile.WorkerType, error) {
	workers := make([]reconcile.WorkerType, 0, len(values))
	for _, value := range values {
		worker, err := reconcile.ParseWorkerType(value)
		if err != nil {
			return nil, err
		}
		workers = append(workers, worker)
	}
	return workers, nil
}

// restartAfterUpdate stops and restarts the gateway after a binary update.
func restartAfterUpdate(inst *gatewayInstance) error {
	if err := stopGateway(inst); err != nil {
		return fmt.Errorf("stop gateway: %w", err)
	}
	fmt.Fprintf(os.Stderr, "  Stopped gateway (PID %d)\n", inst.PID)

	// Wait for process to exit (PID source only — service manager handles its own lifecycle).
	if inst.Source == sourcePID {
		waitForProcessExit(inst.PID, 5*time.Second)
	} else {
		time.Sleep(2 * time.Second)
	}

	// Restart using the appropriate mechanism, preserving original config.
	switch inst.Source {
	case sourceService:
		mgr := service.NewManager()
		if err := mgr.Start("hotplex", inst.Level); err != nil {
			return fmt.Errorf("start service: %w", err)
		}
		fmt.Fprintf(os.Stderr, "  %s Service restarted\n", output.StatusSymbol("pass"))
	case sourcePID:
		if err := startDaemon(inst.ConfigPath, inst.DevMode); err != nil {
			return fmt.Errorf("start daemon: %w", err)
		}
		fmt.Fprintf(os.Stderr, "  %s Gateway restarted\n", output.StatusSymbol("pass"))
	}
	return nil
}

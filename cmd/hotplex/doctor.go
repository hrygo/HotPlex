package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/cli"
	"github.com/hrygo/hotplex/internal/cli/checkers"
	"github.com/hrygo/hotplex/internal/cli/output"
)

func newDoctorCmd() *cobra.Command {
	var fix, verbose, jsonOutput bool
	var category string

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run diagnostic checks",
		Long: `Run diagnostic checks to verify your HotPlex environment is properly configured.
Checks are organized by category: environment, config, dependencies, security, runtime, messaging.
Use --fix to automatically resolve issues where possible.`,
		Example: `  hotplex doctor                     # Run all checks
  hotplex doctor -v                  # Verbose output with details
  hotplex doctor --fix               # Auto-fix issues
  hotplex doctor -C security         # Only security checks
  hotplex doctor --json              # JSON output for scripting`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, _ := cmd.Flags().GetString("config")
			if configPath == "" {
				configPath = "~/.hotplex/config.yaml"
			}
			configPath = expandPath(configPath)
			checkers.SetConfigPath(configPath)

			var checkersToRun []cli.Checker
			if category != "" {
				checkersToRun = cli.DefaultRegistry.ByCategory(category)
			} else {
				checkersToRun = cli.DefaultRegistry.All()
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			var diags []cli.Diagnostic
			for _, c := range checkersToRun {
				d := c.Check(ctx)
				diags = append(diags, d)
			}

			if fix {
				fixed, fixFailed := 0, 0
				for i, d := range diags {
					if d.Status != cli.StatusPass && d.FixFunc != nil {
						if err := d.FixFunc(); err != nil {
							diags[i].Message = fmt.Sprintf("%s (fix failed: %s)", d.Message, err)
							fixFailed++
						} else {
							recheck := checkersToRun[i].Check(ctx)
							diags[i] = recheck
							fixed++
						}
					}
				}
				if fixFailed > 0 {
					outputResults(os.Stderr, diags, verbose, jsonOutput)
					fmt.Fprintf(os.Stderr, "\n%d fix(es) applied, %d failed\n", fixed, fixFailed)
					os.Exit(3)
				}
				if fixed > 0 {
					fmt.Fprintf(os.Stderr, "%d fix(es) applied successfully\n", fixed)
				}
			}

			outputResults(os.Stderr, diags, verbose, jsonOutput)

			if fail := countFailures(diags); fail > 0 {
				os.Exit(1)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&fix, "fix", false, "automatically fix issues")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show detailed information")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output in JSON format")
	cmd.Flags().StringVarP(&category, "category", "C", "", "only check specified category (environment, config, dependencies, security, runtime, messaging)")
	cmd.Flags().StringP("config", "c", "~/.hotplex/config.yaml", "config file path")
	return cmd
}

func countFailures(diags []cli.Diagnostic) int {
	var fail int
	for _, d := range diags {
		if d.Status == cli.StatusFail {
			fail++
		}
	}
	return fail
}

func outputResults(out *os.File, diags []cli.Diagnostic, verbose, jsonOutput bool) {
	if jsonOutput {
		report := output.NewJSONReport(versionString(), diags)
		if err := output.WriteJSON(os.Stdout, report); err != nil {
			fmt.Fprintf(os.Stderr, "error writing JSON: %s\n", err)
		}
		return
	}

	_, _ = fmt.Fprintf(out, "HotPlex Doctor %s\n\n", versionString())

	var pass, warn, fail, fixable int
	for _, d := range diags {
		switch d.Status {
		case cli.StatusPass:
			pass++
		case cli.StatusWarn:
			warn++
		case cli.StatusFail:
			fail++
		}
		if d.FixFunc != nil && d.Status != cli.StatusPass {
			fixable++
		}
	}

	for _, d := range diags {
		output.PrintDiagnostic(out, d, verbose)
	}

	_, _ = fmt.Fprintln(out)
	output.PrintSummary(out, pass, warn, fail, fixable)
}

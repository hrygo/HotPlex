package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/cli/install"
	"github.com/hrygo/hotplex/internal/cli/output"
)

func newInstallCmd() *cobra.Command {
	var targetPath string
	var force bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install hotplex binary to PATH",
		Long: `Install the hotplex binary to a directory in your PATH.

If hotplex is already in PATH with the same content, the command is a no-op.
If the binary differs, it will be updated in-place.

When installing to a new location, the directory is automatically added to PATH
via shell RC file (~/.zshrc, ~/.bashrc) or Windows User environment variable.`,
		Example: `  hotplex install              # Install to default location (~/.local/bin)
  hotplex install --path /usr/local/bin  # Install to specific directory
  hotplex install --force                # Reinstall even if already installed`,
		RunE: func(cmd *cobra.Command, args []string) error {
			src, err := install.ResolveSourceBinary()
			if err != nil {
				return err
			}

			exeName := install.ExeName()

			// Case 1: hotplex already in PATH — update in-place.
			if pathBin, lpErr := exec.LookPath("hotplex"); lpErr == nil {
				if abs, err := filepath.Abs(pathBin); err == nil {
					pathBin = abs
				}
				if resolved, err := filepath.EvalSymlinks(pathBin); err == nil {
					pathBin = resolved
				}

				if install.SameContent(src, pathBin) && !force {
					fmt.Fprintf(os.Stderr, "  %s Already installed at %s\n",
						output.StatusSymbol("pass"), pathBin)
					return nil
				}

				fmt.Fprintf(os.Stderr, "  Updating %s\n", pathBin)
				if err := install.CopyBinary(src, pathBin); err != nil {
					return fmt.Errorf("update failed: %w", err)
				}
				fmt.Fprintf(os.Stderr, "  %s Updated %s\n",
					output.StatusSymbol("pass"), pathBin)
				return nil
			}

			// Case 2: not in PATH — install to target directory.
			if targetPath == "" {
				targetPath, err = install.DefaultInstallDir()
				if err != nil {
					return err
				}
			}
			dst := filepath.Join(targetPath, exeName)

			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return fmt.Errorf("create directory: %w", err)
			}

			fmt.Fprintf(os.Stderr, "  Installing to %s\n", dst)
			if err := install.CopyBinary(src, dst); err != nil {
				return fmt.Errorf("install failed: %w", err)
			}

			if !install.IsInPATH(targetPath) {
				if pErr := install.AddToUserPath(targetPath); pErr != nil {
					fmt.Fprintf(os.Stderr, "  %s Installed to %s\n",
						output.StatusSymbol("pass"), dst)
					fmt.Fprintf(os.Stderr, "  %s Could not add %s to PATH: %v\n",
						output.StatusSymbol("warn"), targetPath, pErr)
					fmt.Fprintf(os.Stderr, "  Add it manually: export PATH=\"%s:$PATH\"\n", targetPath)
					return nil
				}
				fmt.Fprintf(os.Stderr, "  %s Installed to %s\n",
					output.StatusSymbol("pass"), dst)
				fmt.Fprintf(os.Stderr, "  Added %s to PATH (restart your shell)\n", targetPath)
				return nil
			}

			fmt.Fprintf(os.Stderr, "  %s Installed to %s\n",
				output.StatusSymbol("pass"), dst)
			return nil
		},
	}
	cmd.Flags().StringVar(&targetPath, "path", "", "target directory (default: ~/.local/bin)")
	cmd.Flags().BoolVar(&force, "force", false, "reinstall even if already installed")
	return cmd
}

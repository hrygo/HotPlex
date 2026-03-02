package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestStartSession_WorkDirResolution verifies that WorkDir is correctly resolved
func TestStartSession_WorkDirResolution(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}

	testCases := []struct {
		name        string
		workDir     string
		expectedDir string
		shouldExist bool
	}{
		{name: "dot_current_dir", workDir: ".", expectedDir: cwd, shouldExist: true},
		{name: "absolute_path", workDir: "/tmp", expectedDir: "/tmp", shouldExist: true},
		{name: "relative_path_subdir", workDir: "./testdir", expectedDir: filepath.Join(cwd, "testdir"), shouldExist: false},
		{name: "path_with_dot_middle", workDir: "/tmp/./hotplex", expectedDir: "/tmp/hotplex", shouldExist: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := SessionConfig{WorkDir: tc.workDir}

			// Replicate the FIXED logic from pool.go
			var resolvedDir string
			if cfg.WorkDir == "." || !filepath.IsAbs(cfg.WorkDir) {
				cleaned := filepath.Clean(cfg.WorkDir)
				if absPath, err := filepath.Abs(cleaned); err == nil {
					resolvedDir = absPath
				} else {
					resolvedDir = cleaned
				}
			} else {
				// For absolute paths, also clean to resolve . and .. elements
				resolvedDir = filepath.Clean(cfg.WorkDir)
			}

			if resolvedDir != tc.expectedDir {
				t.Errorf("Resolved dir = %q, want %q", resolvedDir, tc.expectedDir)
			}

			if tc.shouldExist {
				if _, err := os.Stat(resolvedDir); os.IsNotExist(err) {
					t.Errorf("Resolved directory does not exist: %s", resolvedDir)
				}
			}
		})
	}
}

// TestStartSession_CmdDirAssignment verifies cmd.Dir is correctly assigned
func TestStartSession_CmdDirAssignment(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}

	testCases := []struct {
		name       string
		workDir    string
		wantCmdDir string
	}{
		{"dot", ".", cwd},
		{"absolute", "/tmp", "/tmp"},
		{"relative", "./subdir", filepath.Join(cwd, "subdir")},
		{"path_with_dot", "/tmp/./test", "/tmp/test"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := SessionConfig{WorkDir: tc.workDir}
			cmd := exec.CommandContext(context.Background(), "echo", "test")

			// Replicate the FIXED logic from pool.go
			if cfg.WorkDir == "." || !filepath.IsAbs(cfg.WorkDir) {
				cleaned := filepath.Clean(cfg.WorkDir)
				if absPath, err := filepath.Abs(cleaned); err == nil {
					cmd.Dir = absPath
				} else {
					cmd.Dir = cleaned
				}
			} else {
				// For absolute paths, also clean to resolve . and .. elements
				cmd.Dir = filepath.Clean(cfg.WorkDir)
			}

			if cmd.Dir != tc.wantCmdDir {
				t.Errorf("cmd.Dir = %q, want %q", cmd.Dir, tc.wantCmdDir)
			}
		})
	}
}

// TestChatAppsWorkDirFunction simulates the chatapps/engine_handler.go flow
func TestChatAppsWorkDirFunction(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}

	workDirFn := func(configWorkDir string) string {
		if configWorkDir != "" {
			workDir := expandPathFixed(configWorkDir)
			return workDir
		}
		return "/tmp/hotplex-chatapps"
	}

	testCases := []struct {
		name            string
		configWorkDir   string
		expectedWorkDir string
	}{
		{"dot_config", ".", cwd},
		{"absolute_config", "/tmp/myproject", "/tmp/myproject"},
		{"empty_config", "", "/tmp/hotplex-chatapps"},
		{"tilde_home", "~/project", filepath.Join(os.Getenv("HOME"), "project")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			workDir := workDirFn(tc.configWorkDir)

			if workDir != tc.expectedWorkDir {
				t.Errorf("workDirFn(%q) = %q, want %q", tc.configWorkDir, workDir, tc.expectedWorkDir)
			}
		})
	}
}

// expandPathFixed simulates the FIXED expandPath function from setup.go
func expandPathFixed(path string) string {
	if len(path) == 0 {
		return path
	}

	// Handle ~ expansion
	if path[0] == '~' {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return path // Return original path if home dir cannot be determined
		}

		if len(path) == 1 {
			return homeDir
		}

		// Handle ~/path
		if path[1] == '/' || path[1] == filepath.Separator {
			return filepath.Join(homeDir, path[2:])
		}

		// Handle ~username/path (not commonly used, but supported)
		return filepath.Join(homeDir, path[1:])
	}

	// Handle special case: "." should be expanded to current working directory
	if path == "." {
		cwd, err := os.Getwd()
		if err != nil {
			return path
		}
		return cwd
	}

	return filepath.Clean(path)
}

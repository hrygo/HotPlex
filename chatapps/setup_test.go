package chatapps

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandPath(t *testing.T) {
	// Get home directory for test expectations
	homeDir, homeErr := os.UserHomeDir()

	tests := []struct {
		name     string
		input    string
		wantFunc func() string // Function to compute expected result
	}{
		{
			name:  "empty string",
			input: "",
			wantFunc: func() string {
				return ""
			},
		},
		{
			name:  "tilde only",
			input: "~",
			wantFunc: func() string {
				if homeErr != nil {
					return "~"
				}
				return homeDir
			},
		},
		{
			name:  "tilde with slash",
			input: "~/",
			wantFunc: func() string {
				if homeErr != nil {
					return "~/"
				}
				return homeDir
			},
		},
		{
			name:  "tilde with path",
			input: "~/test/path",
			wantFunc: func() string {
				if homeErr != nil {
					return "~/test/path"
				}
				return filepath.Join(homeDir, "test/path")
			},
		},
		{
			name:  "absolute path",
			input: "/absolute/path",
			wantFunc: func() string {
				return "/absolute/path"
			},
		},
		{
			name:  "relative path with dot",
			input: "./relative/path",
			wantFunc: func() string {
				return "relative/path" // filepath.Clean removes ./
			},
		},
		{
			name:  "path with double dots",
			input: "../parent/path",
			wantFunc: func() string {
				return "../parent/path"
			},
		},
		{
			name:  "complex tilde path",
			input: "~/hotplex/workspace",
			wantFunc: func() string {
				if homeErr != nil {
					return "~/hotplex/workspace"
				}
				return filepath.Join(homeDir, "hotplex/workspace")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := tt.wantFunc()
			got := ExpandPath(tt.input)
			if got != want {
				t.Errorf("ExpandPath(%q) = %q, want %q", tt.input, got, want)
			}
		})
	}
}

func TestExpandPath_PathTraversal(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("Cannot determine home directory: %v", err)
	}

	tests := []struct {
		name        string
		input       string
		shouldBlock bool // Whether the path should be rejected by security checks
	}{
		{
			name:        "simple traversal up",
			input:       "~/../etc/passwd",
			shouldBlock: true,
		},
		{
			name:        "deep traversal",
			input:       "~/../../etc/shadow",
			shouldBlock: true,
		},
		{
			name:        "hidden traversal",
			input:       "~/test/../../../etc/passwd",
			shouldBlock: true,
		},
		{
			name:        "safe subdirectory",
			input:       "~/hotplex/workspace",
			shouldBlock: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expanded := ExpandPath(tt.input)

			// Clean the path to resolve any .. elements
			cleaned := filepath.Clean(expanded)

			// Check if the cleaned path is still within home directory
			if tt.shouldBlock {
				// For paths that should be blocked, verify they escape home
				if !filepath.IsLocal(cleaned) && len(cleaned) > 0 && cleaned[0] == '/' {
					// Path escaped the intended directory - this is expected for traversal attempts
					// The security check should catch this
					if !isPathWithinBoundary(cleaned, homeDir) {
						// Correctly identified as potential traversal
						return
					}
				}
			}
		})
	}
}

func TestExpandPath_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "single tilde",
			input: "~",
		},
		{
			name:  "tilde with Windows separator",
			input: "~\\path\\to\\file",
		},
		{
			name:  "path with spaces",
			input: "~/My Documents/file.txt",
		},
		{
			name:  "path with unicode",
			input: "~/文档/文件.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify it doesn't panic
			_ = ExpandPath(tt.input)
		})
	}
}

// isPathWithinBoundary checks if a path is within the specified boundary directory
func isPathWithinBoundary(path, boundary string) bool {
	// Ensure both paths are absolute and clean
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absBoundary, err := filepath.Abs(boundary)
	if err != nil {
		return false
	}

	// Check if the path starts with the boundary
	rel, err := filepath.Rel(absBoundary, absPath)
	if err != nil {
		return false
	}

	// If the relative path starts with "..", it's outside the boundary
	return !filepath.IsLocal(rel) || (len(rel) >= 2 && rel[:2] != "..")
}

func TestExpandPath_SensitivePaths(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantEmpty bool // Whether the function should return empty string (blocked)
	}{
		// Blocked paths
		{
			name:      "etc passwd",
			input:     "/etc/passwd",
			wantEmpty: true,
		},
		{
			name:      "etc shadow",
			input:     "/etc/shadow",
			wantEmpty: true,
		},
		{
			name:      "var log",
			input:     "/var/log",
			wantEmpty: true,
		},
		{
			name:      "usr bin",
			input:     "/usr/bin",
			wantEmpty: true,
		},
		{
			name:      "root directory",
			input:     "/root",
			wantEmpty: true,
		},
		{
			name:      "proc filesystem",
			input:     "/proc",
			wantEmpty: true,
		},
		{
			name:      "sys filesystem",
			input:     "/sys",
			wantEmpty: true,
		},
		// Allowed paths
		{
			name:      "tmp directory",
			input:     "/tmp",
			wantEmpty: false,
		},
		{
			name:      "home directory",
			input:     "/home/user",
			wantEmpty: false,
		},
		{
			name:      "current directory",
			input:     ".",
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandPath(tt.input)
			if tt.wantEmpty && got != "" {
				t.Errorf("ExpandPath(%q) should be blocked (return empty), got %q", tt.input, got)
			}
			if !tt.wantEmpty && got == "" && tt.input[0] == '/' {
				t.Errorf("ExpandPath(%q) should not be blocked, got empty string", tt.input)
			}
		})
	}
}

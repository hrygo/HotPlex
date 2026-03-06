package sys

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// ExpandPath expands the home directory tilde (~) in a path.
// If the path starts with "~/", it is replaced with the current user's home directory.
// If the path is exactly "~", it is replaced with the current user's home directory.
func ExpandPath(path string) string {
	if path == "~" {
		return getHomeDir()
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(getHomeDir(), path[2:])
	}
	return path
}

func getHomeDir() string {
	// Try os.UserHomeDir first (standard Go 1.12+)
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	// Fallback to os/user
	if u, err := user.Current(); err == nil {
		return u.HomeDir
	}
	// Manual fallback for Unix
	return os.Getenv("HOME")
}

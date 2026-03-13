package sys

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// ExpandPath expands the home directory tilde (~) and environment variables in a path.
func ExpandPath(path string) string {
	if path == "" {
		return path
	}

	// 1. Handle ~ expansion
	if path == "~" {
		return getHomeDir()
	}
	if strings.HasPrefix(path, "~/") {
		path = filepath.Join(getHomeDir(), path[2:])
	} else if strings.HasPrefix(path, "~") && !strings.Contains(path[1:], "/") && !strings.Contains(path[1:], string(filepath.Separator)) {
		// Handle ~username/path (simplified to current home for robustness)
		path = filepath.Join(getHomeDir(), path[1:])
	}

	// 2. Handle environment variable expansion ($VAR or ${VAR})
	return os.Expand(path, func(vars string) string {
		val := os.Getenv(vars)
		if vars == "HOME" && val == "" {
			return getHomeDir()
		}
		return val
	})
}

// ConfigDir returns the platform-specific directory for configuration files (XDG_CONFIG_HOME).
func ConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "hotplex")
	}
	return filepath.Join(getHomeDir(), ".config", "hotplex")
}

// DataDir returns the platform-specific directory for data files (XDG_DATA_HOME).
func DataDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "hotplex")
	}
	return filepath.Join(getHomeDir(), ".local", "share", "hotplex")
}

// LogDir returns the platform-specific directory for log files.
func LogDir() string {
	return filepath.Join(DataDir(), "logs")
}

func getHomeDir() string {
	// Try os.UserHomeDir first (standard Go 1.12+)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	// Manual fallback check for HOME env
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	// Fallback to os/user
	if u, err := user.Current(); err == nil {
		return u.HomeDir
	}
	return "/" // Absolute fallback
}

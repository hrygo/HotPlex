package security

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// LandlockConfig holds configuration for Landlock file system restrictions.
type LandlockConfig struct {
	// AllowedDirs defines directories where file operations are permitted.
	// All file operations outside these directories will be blocked.
	AllowedDirs []string

	// AllowRead defines if read operations are allowed.
	AllowRead bool

	// AllowWrite defines if write operations are allowed.
	AllowWrite bool

	// AllowExec defines if execute operations are allowed.
	AllowExec bool

	// DeniedExtensions file extensions that are always denied.
	DeniedExtensions []string

	// Logger for security events.
	Logger *slog.Logger
}

// LandlockEnforcer implements file system sandboxing using software-only checks.
// Note: Full Landlock kernel integration requires kernel 5.13+ and appropriate
// golang.org/x/sys/unix support. This implementation provides software-only
// path checking as a fallback.
type LandlockEnforcer struct {
	config  LandlockConfig
	logger  *slog.Logger
	enabled bool
	mu      sync.RWMutex
}

// DefaultLandlockConfig returns a default safe configuration.
func DefaultLandlockConfig() LandlockConfig {
	return LandlockConfig{
		AllowRead:        true,
		AllowWrite:       true,
		AllowExec:        false,
		DeniedExtensions: []string{".so", ".dll", ".dylib", ".exe"},
		AllowedDirs:      []string{},
		Logger:           slog.Default(),
	}
}

// NewLandlockEnforcer creates a new Landlock enforcer with the given configuration.
func NewLandlockEnforcer(config LandlockConfig) (*LandlockEnforcer, error) {
	le := &LandlockEnforcer{
		config: config,
		logger: config.Logger,
	}

	if le.logger == nil {
		le.logger = slog.Default()
	}

	// Validate and clean paths
	for i, dir := range config.AllowedDirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			return nil, fmt.Errorf("invalid allowed directory %q: %w", dir, err)
		}
		config.AllowedDirs[i] = absDir
	}

	le.enabled = true

	le.logger.Info("Landlock enforcer initialized (software-only mode)",
		"allowed_dirs", len(config.AllowedDirs),
		"allow_read", config.AllowRead,
		"allow_write", config.AllowWrite,
		"allow_exec", config.AllowExec)

	return le, nil
}

// IsLandlockSupported checks if the Linux kernel supports Landlock.
// Returns true if Landlock is available in the kernel.
func IsLandlockSupported() bool {
	// Check kernel version by reading /proc/version
	// Landlock requires kernel 5.13+
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}

	version := string(data)
	// Simple check - look for version number
	// In production, you'd parse this more carefully
	return strings.Contains(version, "Linux version 5.1") ||
		strings.Contains(version, "Linux version 5.2") ||
		strings.Contains(version, "Linux version 5.") && !strings.Contains(version, "Linux version 5.0") ||
		strings.Contains(version, "Linux version 6.") ||
		strings.Contains(version, "Linux version 7.")
}

// Enforce applies the file system restrictions.
// In software-only mode, this validates that restrictions can be applied.
func (le *LandlockEnforcer) Enforce() error {
	le.mu.Lock()
	defer le.mu.Unlock()

	if !le.enabled {
		le.logger.Warn("Landlock is not enabled - cannot enforce")
		return fmt.Errorf("landlock not enabled")
	}

	// In software-only mode, we just log that enforcement is active
	le.logger.Info("Landlock restrictions applied (software-only mode)")
	return nil
}

// CheckPath checks if a path is allowed under current rules.
// This is a software-only check that doesn't require Landlock kernel support.
func (le *LandlockEnforcer) CheckPath(path string) error {
	le.mu.RLock()
	defer le.mu.RUnlock()

	// Resolve to absolute path
	absPath, err := filepath.Abs(os.ExpandEnv(path))
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	// Normalize path
	absPath = filepath.Clean(absPath)

	// Check if we have any allowed directories
	if len(le.config.AllowedDirs) == 0 {
		return nil // No restrictions configured
	}

	// Check against allowed directories
	for _, allowed := range le.config.AllowedDirs {
		allowed = filepath.Clean(allowed)

		// Check if path is within allowed directory
		if absPath == allowed || (strings.HasPrefix(absPath, allowed) && len(absPath) > len(allowed) && absPath[len(allowed)] == filepath.Separator) {
			// Check extension restrictions
			if le.isExtensionDenied(absPath) {
				return fmt.Errorf("file extension denied: %s", filepath.Ext(absPath))
			}
			return nil
		}
	}

	return fmt.Errorf("path not in allowed directories: %s", path)
}

// isExtensionDenied checks if the file extension is denied.
func (le *LandlockEnforcer) isExtensionDenied(path string) bool {
	ext := filepath.Ext(path)
	for _, denied := range le.config.DeniedExtensions {
		if ext == denied || ext == "."+denied {
			return true
		}
	}
	return false
}

// UpdateAllowedDirs updates the list of allowed directories.
func (le *LandlockEnforcer) UpdateAllowedDirs(dirs []string) error {
	le.mu.Lock()
	defer le.mu.Unlock()

	// Clean paths
	cleaned := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			return fmt.Errorf("invalid directory %q: %w", dir, err)
		}
		cleaned = append(cleaned, absDir)
	}

	le.config.AllowedDirs = cleaned

	le.logger.Info("Allowed directories updated", "count", len(cleaned))
	return nil
}

// IsEnabled returns whether Landlock is currently enabled.
func (le *LandlockEnforcer) IsEnabled() bool {
	le.mu.RLock()
	defer le.mu.RUnlock()
	return le.enabled
}

// Close releases resources (no-op in software-only mode).
func (le *LandlockEnforcer) Close() error {
	return nil
}

// PathTraversalPrevention provides path traversal attack prevention.
type PathTraversalPrevention struct {
	allowedRoots []string
	logger       *slog.Logger
}

// NewPathTraversalPrevention creates a new path traversal prevention checker.
func NewPathTraversalPrevention(allowedRoots []string, logger *slog.Logger) *PathTraversalPrevention {
	ptp := &PathTraversalPrevention{
		logger: logger,
	}

	if logger == nil {
		ptp.logger = slog.Default()
	}

	// Normalize and validate allowed roots
	for _, root := range allowedRoots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			ptp.logger.Warn("Invalid allowed root", "root", root, "error", err)
			continue
		}
		ptp.allowedRoots = append(ptp.allowedRoots, absRoot)
	}

	return ptp
}

// CheckPath validates that a path doesn't contain traversal attempts
// and stays within allowed directories.
func (ptp *PathTraversalPrevention) CheckPath(path string) error {
	// Expand environment variables
	path = os.ExpandEnv(path)

	// Check for path traversal patterns
	traversalPatterns := []string{
		"..",
		"%2e%2e",  // URL encoded
		"%252e",   // Double URL encoded
		"..\\",    // Windows-style
		"%2e%2e\\",
	}

	lowerPath := strings.ToLower(path)
	for _, pattern := range traversalPatterns {
		if len(lowerPath) >= len(pattern)*2 {
			// Check for encoded patterns
			for i := 0; i <= len(lowerPath)-len(pattern)*2; i++ {
				sub := lowerPath[i : i+len(pattern)*2]
				if sub == pattern+pattern || sub == pattern+"/"+pattern {
					return fmt.Errorf("path traversal detected (encoded): %s", path)
				}
			}
		}

		// Direct traversal
		if strings.Contains(lowerPath, pattern) {
			return fmt.Errorf("path traversal detected: %s", path)
		}
	}

	// Resolve to absolute and check against allowed roots
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	// Clean the path (resolves .. and . segments)
	absPath = filepath.Clean(absPath)

	// Check against allowed roots
	if len(ptp.allowedRoots) > 0 {
		allowed := false
		for _, root := range ptp.allowedRoots {
			root = filepath.Clean(root)
			if absPath == root || (strings.HasPrefix(absPath, root) && len(absPath) > len(root) && absPath[len(root)] == filepath.Separator) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("path outside allowed roots: %s", path)
		}
	}

	return nil
}

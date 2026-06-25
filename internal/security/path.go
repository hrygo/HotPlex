package security

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidateBaseDir checks that the base directory is in the allowed list.
func ValidateBaseDir(baseDir string) error {
	securityConfigMutex.RLock()
	ok := allowedBaseDirs[baseDir]
	securityConfigMutex.RUnlock()
	if !ok {
		return fmt.Errorf("security: base directory %q not in whitelist", baseDir)
	}
	return nil
}

// GetAllowedBaseDirs returns a defensive copy for testing.
func GetAllowedBaseDirs() map[string]bool {
	securityConfigMutex.RLock()
	defer securityConfigMutex.RUnlock()

	result := make(map[string]bool, len(allowedBaseDirs))
	for k, v := range allowedBaseDirs {
		result[k] = v
	}
	return result
}

// GetForbiddenWorkDirs returns a defensive copy for testing.
func GetForbiddenWorkDirs() []string {
	securityConfigMutex.RLock()
	defer securityConfigMutex.RUnlock()

	result := make([]string, len(forbiddenWorkDirs))
	copy(result, forbiddenWorkDirs)
	return result
}

// ValidateWorkDir validates that a work directory is safe for worker execution.
//
// Rules:
//  1. Must be an absolute path.
//  2. Must be clean (no ".." components).
//  3. Must not be or reside under a forbidden system directory.
//  4. Symlinks are resolved and the real path is also checked against the blacklist.
//  5. Must not contain "|" — this is the delimiter used by session.DeriveSessionKey
//     to concatenate hash fields; a "|" in the path could cause a theoretical
//     session-key collision (review P3 fix).
func ValidateWorkDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("security: work dir must not be empty")
	}

	if strings.ContainsRune(dir, '|') {
		return fmt.Errorf("security: work dir must not contain '|'")
	}

	cleaned := filepath.Clean(dir)

	if !filepath.IsAbs(cleaned) {
		return fmt.Errorf("security: work dir must be absolute: %s", dir)
	}

	if err := checkForbidden(cleaned); err != nil {
		return err
	}

	// Resolve symlinks and check the real path too.
	realPath, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		// Directory doesn't exist yet — that's OK, we already validated the logical path.
		return nil
	}
	return checkForbidden(realPath)
}

// ErrWorkDirOutsideSandbox 由 ValidateWorkspaceWorkDir 在 work_dir 越出 owner 的 workspace
// 沙箱前缀（$HOME/.hotplex/workspaces/<ownerUserID>）时返回。
var ErrWorkDirOutsideSandbox = errors.New("security: work dir outside owner workspace sandbox")

// ValidateWorkspaceWorkDir 校验 dir 恰好等于或位于 owner 的 workspace 沙箱前缀下：
//
//	$HOME/.hotplex/workspaces/<ownerUserID>
//
// 这是 workspace 专用的额外约束，不替代通用 ValidateWorkDir（黑名单/symlink 仍需独立调用）。
// dir 必须已是绝对路径（调用前先经 config.ExpandAndAbs）。
func ValidateWorkspaceWorkDir(dir, ownerUserID string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("security: cannot resolve $HOME: %w", err)
	}
	return validateWorkspaceWorkDir(dir, home, ownerUserID)
}

// validateWorkspaceWorkDir 是 ValidateWorkspaceWorkDir 的可测试内核，注入 home 目录，
// 使前缀/越界/owner 隔离逻辑可在不依赖进程级 $HOME 的情况下并行测试。
func validateWorkspaceWorkDir(dir, home, ownerUserID string) error {
	if dir == "" || ownerUserID == "" {
		return ErrWorkDirOutsideSandbox
	}
	base := filepath.Join(home, ".hotplex", "workspaces", ownerUserID)
	rel, err := filepath.Rel(base, dir)
	if err != nil {
		return ErrWorkDirOutsideSandbox
	}
	// rel == "." → dir == base（沙箱根本身，合法）；
	// 任何以 ".." 开头的相对路径意味着 dir 在 base 之外 —— owner 隔离由此保证。
	if rel == "." {
		return nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ErrWorkDirOutsideSandbox
	}
	return nil
}

// checkForbidden returns an error if path is exactly or under a forbidden directory.
// Whitelist (allowedBaseDirs) takes priority over blacklist (forbiddenWorkDirs).
func checkForbidden(path string) error {
	securityConfigMutex.RLock()

	// Check whitelist first (highest priority)
	for allowedDir := range allowedBaseDirs {
		if path == allowedDir || pathHasPrefix(path, allowedDir+string(filepath.Separator)) {
			securityConfigMutex.RUnlock()
			return nil
		}
	}

	securityConfigMutex.RUnlock()

	// Intelligent user directory analysis (Unix-only, no-op on Windows)
	// This allows /home/<current_user>/*, /Users/<current_user>/*, /usr/local/<current_user>/*
	// even if /home or /usr is in the blacklist.
	if isUserAccessibleDirectory(path) {
		return nil
	}

	// Check blacklist under a single lock acquisition
	securityConfigMutex.RLock()
	for _, forbidden := range forbiddenWorkDirs {
		if pathEqual(path, forbidden) {
			securityConfigMutex.RUnlock()
			return fmt.Errorf("security: work dir %q is a forbidden system directory", path)
		}
		if pathHasPrefix(path, forbidden+string(filepath.Separator)) {
			securityConfigMutex.RUnlock()
			return fmt.Errorf("security: work dir %q is under forbidden directory %q", path, forbidden)
		}
	}
	securityConfigMutex.RUnlock()

	// Reject root itself — no process should use the root as its working directory.
	if isRootPath(path) {
		return fmt.Errorf("security: work dir %q is a forbidden system directory", path)
	}

	return nil
}

// SafePathJoin safely joins a base directory with a user-provided path,
// preventing path traversal attacks.
//
// Security guarantees:
//  1. Rejects absolute paths from user input.
//  2. Resolves all symlinks via filepath.EvalSymlinks.
//  3. Verifies the resolved path is still within baseDir.
func SafePathJoin(baseDir, userPath string) (string, error) {
	// Reject absolute paths — they bypass baseDir entirely.
	if filepath.IsAbs(userPath) {
		return "", fmt.Errorf("security: absolute paths not allowed: %s", userPath)
	}

	// Clean the user path.
	clean := filepath.Clean(userPath)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("security: traversal attempt detected: %s", userPath)
	}

	// Join with baseDir.
	joined := filepath.Join(baseDir, clean)

	// Resolve symlinks in the joined path.
	realPath, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", fmt.Errorf("security: path error: %w", err)
	}

	// Resolve symlinks in baseDir.
	realBase, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return "", fmt.Errorf("security: base dir error: %w", err)
	}

	// Verify resolved path is inside baseDir.
	realBase = strings.TrimSuffix(realBase, string(filepath.Separator))
	if !pathHasPrefix(realPath, realBase+string(filepath.Separator)) {
		return "", fmt.Errorf("security: path escapes base directory: %s", userPath)
	}

	return realPath, nil
}

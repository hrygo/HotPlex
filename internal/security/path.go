package security

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hrygo/hotplex/internal/config"
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

// ErrWorkDirOutsideSandbox 由 ValidateWorkspaceWorkDir 在 work_dir 越出 owner 的
// workspace 沙箱根（WorkspaceSandboxRoot(username)）时返回。
var ErrWorkDirOutsideSandbox = errors.New("security: work dir outside owner workspace sandbox")

// WorkspaceSandboxRoot 返回 username 的 workspace 沙箱根目录：
//
//	HotplexHome()/workspaces/<sandboxDirSegment(username)>
//
// 与 HotplexHome() 同源（跟随 HOTPLEX_HOME，未设置时回退 ~/.hotplex）；
// 内部执行目录段映射（sandboxDirSegment）与 filepath.Abs（HOTPLEX_HOME 可能为
// 相对值），是所有 workspace 路径校验/错误消息/API 暴露的唯一事实源。
func WorkspaceSandboxRoot(username string) string {
	base := filepath.Join(config.HotplexHome(), "workspaces", sandboxDirSegment(username))
	abs, err := filepath.Abs(base)
	if err != nil { // 不可达（HotplexHome 恒可拼接）；防御性保留原值
		return base
	}
	return abs
}

// sandboxDirSegment 将 username 映射为沙箱目录段（四身份空间隔离，filesystem-safe）：
//   - 无 ":" → 原样（密码用户经 ValidateUsername 已保证 [a-zA-Z0-9_.-]；
//     系统身份 anonymous/api_user 同属此路径，字面量被 P1 封锁给密码用户）
//   - "apikey:" 前缀 → 机器用户："apikey-" + lossySafeSegment(rest)
//   - 其他含 ":" → OAuth 用户："oauth-" + lossySafeSegment(whole)
//
// 段注入性（G1b）：lossySafeSegment 在 sanitize 有损或含大写时追加稳定哈希，
// 杜绝同空间内不同身份映射到同一目录段（如 "user/1" 与 "user-1"）。
func sandboxDirSegment(username string) string {
	if i := strings.IndexByte(username, ':'); i >= 0 {
		prefix := username[:i]
		switch prefix {
		case "apikey":
			return "apikey-" + lossySafeSegment(username[i+1:])
		default: // OAuth provider:subject
			return "oauth-" + lossySafeSegment(username)
		}
	}
	return lossySafeSegment(username)
}

// sanitizePathSegment 将任意字符串转为 filesystem-safe 的单段目录名：
// 非 [a-zA-Z0-9_.-] 字符替换为 "-"，连续 "-" 折叠，去首尾 "-"；
// 结果为 "." / ".."（路径逃逸段）时返回空串。
func sanitizePathSegment(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '_' || r == '.' || r == '-'
		if !ok {
			r = '-'
		}
		if r == '-' && prevDash {
			continue
		}
		prevDash = r == '-'
		b.WriteRune(r)
	}
	out := strings.Trim(b.String(), "-")
	if out == "." || out == ".." {
		return ""
	}
	return out
}

// lossySafeSegment 返回 filesystem-safe 且注入（无碰撞）的目录段：
// sanitize 恒等且全小写且非空 → 原样返回（可读性保留）；否则追加完整 SHA-256
// 十六进制摘要（64 字符，碰撞等价于 SHA-256 碰撞，计算上不可行；评审 F1 第二轮
// 否决 4-byte 截断——32 位前缀生日碰撞可被暴力构造）。空段/空输入退化为纯摘要段。
// 覆盖：sanitize 有损（"a/b" vs "a-b"）、大小写敏感文件系统（"Alice" vs "alice"）、
// 逃逸段（".."）、空输入。
func lossySafeSegment(s string) string {
	seg := sanitizePathSegment(s)
	if seg == s && seg == strings.ToLower(s) && s != "" {
		return seg
	}
	sum := sha256.Sum256([]byte(s))
	base := strings.ToLower(seg)
	if base == "" {
		return hex.EncodeToString(sum[:])
	}
	return base + "-" + hex.EncodeToString(sum[:])
}

// ValidateWorkspaceWorkDir 校验 dir 恰好等于或位于 sandboxRoot 下。
// 这是 workspace 专用的额外约束，不替代通用 ValidateWorkDir（黑名单/symlink 仍需独立调用）。
// dir 必须已是绝对路径（调用前先经 config.ExpandAndAbs）。
// sandboxRoot 由调用方经 WorkspaceSandboxRoot(...) 组装。
func ValidateWorkspaceWorkDir(dir, sandboxRoot string) error {
	if dir == "" || sandboxRoot == "" {
		return ErrWorkDirOutsideSandbox
	}
	rel, err := filepath.Rel(sandboxRoot, dir)
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

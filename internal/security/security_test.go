//go:build darwin || linux

package security

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
)

// ─── Command whitelist ────────────────────────────────────────────────────────

func TestBuildSafeCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		binary  string
		args    []string
		wantErr bool
	}{
		{"claude allowed", "claude", []string{"--print", "--session-id", "sess_abc"}, false},
		{"opencode allowed", "opencode", []string{"run"}, false},
		{"opencode with args", "opencode", []string{"--model", "claude-sonnet-4-6"}, false},
		{"rm rejected", "rm", nil, true},
		{"bash rejected", "bash", nil, true},
		{"sh rejected", "sh", nil, true},
		{"python rejected", "python", nil, true},
		{"empty binary", "", nil, true},
		{"docker rejected", "docker", nil, true},
		{"curl rejected", "curl", nil, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd, err := BuildSafeCommand(tt.binary, tt.args...)
			if tt.wantErr {
				require.Error(t, err)
				require.Nil(t, cmd)
			} else {
				require.NoError(t, err)
				require.NotNil(t, cmd)
				// Binary name should match (exec.Command resolves to absolute path from PATH)
				require.Equal(t, tt.binary, filepath.Base(cmd.Path))
			}
		})
	}
}

func TestValidateCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cmd     string
		wantErr bool
	}{
		{"claude valid", "claude", false},
		{"opencode valid", "opencode", false},
		{"empty rejected", "", true},
		{"rm rejected", "rm", true},
		{"curl rejected", "curl", true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateCommand(tt.cmd)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ─── Dangerous characters ─────────────────────────────────────────────────────

func TestContainsDangerousChars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"plain text", "hello world", true},
		{"valid session id", "sess_abc123", false},
		{"semicolon", "a;b", true},
		{"pipe", "a|b", true},
		{"ampersand", "a&b", true},
		{"backtick command sub", "`id`", true},
		{"dollar parens", "$(id)", true},
		{"backslash", "a\\b", true},
		{"newline", "a\nb", true},
		{"carriage return", "a\rb", true},
		{"parentheses", "(echo)", true},
		{"braces", "{echo}", true},
		{"brackets", "[echo]", true},
		{"angle brackets", "<file", true},
		{"greater than", ">", true},
		{"exclamation", "!", true},
		{"hash comment", "#comment", true},
		{"tilde", "~", true},
		{"star", "*", true},
		{"question mark", "?", true},
		{"space", "hello world", true},
		{"tab", "hello\tworld", true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := containsDangerousChars(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

// ─── ValidateWorkDir ──────────────────────────────────────────────────────────

func TestValidateWorkDir(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	currentUser := getCurrentUser()

	tests := []struct {
		name    string
		dir     string
		wantErr bool
	}{
		{"empty rejected", "", true},
		{"relative rejected", "relative/path", true},
		{"root forbidden", "/", true},
		{"usr forbidden", "/usr", true},
		{"usr sub forbidden", "/usr/local/bin", true},
		{"etc forbidden", "/etc", true},
		{"etc sub forbidden", "/etc/nginx/conf.d", true},
		{"bin forbidden", "/bin", true},
		{"sbin forbidden", "/sbin", true},
		{"boot forbidden", "/boot", true},
		{"dev forbidden", "/dev", true},
		{"proc forbidden", "/proc", true},
		{"sys forbidden", "/sys", true},
		{"lib forbidden", "/lib", true},
		{"lib64 forbidden", "/lib64", true},
		{"root home forbidden", "/root", true},
		{"home forbidden", "/home", true},
		{"run forbidden", "/run", true},
		{"srv forbidden", "/srv", true},
		{"System forbidden (macOS)", "/System", true},
		{"tmp dir allowed", tmpDir, false},
		{"non-existent clean path allowed", filepath.Join(tmpDir, "nonexistent", "project"), false},
		{"current user home allowed", "/home/" + currentUser + "/workspace", false},
		{"current user home sub allowed", "/home/" + currentUser + "/projects/test", false},
		{"current user usr/local allowed", "/usr/local/" + currentUser + "/bin", false},
		{"other user home rejected", "/home/otheruser/workspace", true},
		{"other user usr/local rejected", "/usr/local/otheruser/bin", true},
		{"usr/local without username rejected", "/usr/local/bin", true},
		{"pipe separator rejected", tmpDir + "|workspace", true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateWorkDir(tt.dir)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ─── ValidateWorkspaceWorkDir ─────────────────────────────────────────────────

func TestValidateWorkspaceWorkDir(t *testing.T) {
	t.Parallel()

	root := filepath.Join("/home/testuser", ".hotplex", "workspaces", "alice")
	otherRoot := filepath.Join("/home/testuser", ".hotplex", "workspaces", "bob")

	tests := []struct {
		name    string
		dir     string
		root    string
		wantErr bool
	}{
		{"empty dir rejected", "", root, true},
		{"empty root rejected", root, "", true},
		{"sandbox root allowed", root, root, false},
		{"direct subdir allowed", filepath.Join(root, "proj"), root, false},
		{"nested subdir allowed", filepath.Join(root, "a", "b", "c"), root, false},
		{"owner isolation: sibling root rejected", filepath.Join(otherRoot, "x"), root, true},
		{"outside workspaces tree rejected", filepath.Join("/home/testuser", "Documents", "proj"), root, true},
		{"unrelated tmp rejected", "/tmp/elsewhere", root, true},
		{"system dir rejected", "/etc/nginx", root, true},
		{"parent traversal to sibling root rejected", filepath.Join(root, "..", "bob"), root, true},
		{"deep escape to system rejected", filepath.Join(root, "..", "..", "..", "..", "etc"), root, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateWorkspaceWorkDir(tt.dir, tt.root)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrWorkDirOutsideSandbox)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestValidateWorkspaceWorkDir_HomeResolution 证明沙箱根跟随 HotplexHome() 而非真实
// 用户主目录：dir 落在 $HOTPLEX_HOME/workspaces/<segment> 下通过，落在
// $HOME/.hotplex/workspaces/<segment> 下被拒（HOTPLEX_HOME 设置时的行为分裂回归）。
// 串行：修改进程级 HOTPLEX_HOME/HOME。
func TestValidateWorkspaceWorkDir_HomeResolution(t *testing.T) {
	home := t.TempDir()
	hot := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HOTPLEX_HOME", hot)

	root := WorkspaceSandboxRoot("alice")
	require.Equal(t, filepath.Join(hot, "workspaces", "alice"), root, "sandbox root must follow HOTPLEX_HOME")

	inside := filepath.Join(root, "proj")
	require.NoError(t, ValidateWorkspaceWorkDir(inside, root))

	// dir 位于真实 $HOME/.hotplex/workspaces/<segment> → 拒绝（不再锚定真实 home）。
	legacy := filepath.Join(home, ".hotplex", "workspaces", "alice", "proj")
	require.ErrorIs(t, ValidateWorkspaceWorkDir(legacy, root), ErrWorkDirOutsideSandbox)

	outside := filepath.Join(t.TempDir(), "elsewhere")
	require.ErrorIs(t, ValidateWorkspaceWorkDir(outside, root), ErrWorkDirOutsideSandbox)
}

// ─── WorkspaceSandboxRoot / sandboxDirSegment ─────────────────────────────────

// TestSandboxDirSegment 覆盖四身份空间隔离编码（spec §5.1.2）：
// 密码/系统原样、机器 apikey- 前缀、OAuth oauth- 前缀；sanitize 有损或含大写时
// 追加完整 SHA-256 摘要保证段注入性（G1b，评审 F1 修复）。
func TestSandboxDirSegment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		username string
		want     string
	}{
		// 密码用户（ValidateUsername 已保证 [a-zA-Z0-9_.-]，恒等 + 全小写 → 原样）
		{"password user", "alice", "alice"},
		{"password user with separators", "alice-smith.dev", "alice-smith.dev"},
		// 系统身份（uid 字面量，无 users 行）
		{"system anonymous", "anonymous", "anonymous"},
		{"system api_user", "api_user", "api_user"},
		// 机器用户（apikey: 前缀）
		{"machine user", "apikey:abc", "apikey-abc"},
		{"machine user with dash", "apikey:a-b", "apikey-a-b"},
		{"machine user slash folded + full digest", "apikey:a/b", "apikey-a-b-c14cddc033f64b9dea80ea675cf280a015e672516090a5626781153dc68fea11"},
		{"machine user dotdot digest only", "apikey:..", "apikey-5ec1f7e700f37c3d0b2981d04855fc34b94aaa15457b05ca571817442d228f81"},
		{"machine user unicode digest only", "apikey:用户", "apikey-0d0e1a86b3aa787709b00329fcd32b5baf036c87067c8d6c27a466675cb6b355"},
		{"machine user uppercase folded + digest", "apikey:A", "apikey-a-559aead08264d5795d3909718cdd05abd49572e84fe55590eef31a88a08fdffd"},
		// OAuth 用户（provider:subject）
		{"oauth user", "github:user", "oauth-github-user-e14ec4764cff30ca3657d76edc1e77f2f41da3475298407fc58685620054ea66"},
		{"oauth subject slash folded + digest", "github:user/1", "oauth-github-user-1-b2c25428504c4527fa319ee48cf2611f9392c9a52f746ab4e5f325f5f85bb3ee"},
		{"oauth subject with colon defensively", "foo:apikey:bar", "oauth-foo-apikey-bar-67aea289500f4b0fe6927e9f96615c7b7fa7283a22ec2747b02ac7e68a8a07f6"},
		{"oauth provider named apikey defensively", "apikey:sub", "apikey-sub"},
		// 大小写敏感文件系统（G1b）
		{"password user uppercase folded + digest", "Alice", "alice-3bc51062973c458d5a6f2d8d64a023246354ad7e064b1e4e009ec8a0699a3043"},
		// 空输入（防御：空段退化为纯摘要段，不产生空目录段）
		{"empty input digest only", "", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, sandboxDirSegment(tt.username), "username=%q", tt.username)
		})
	}
}

// TestSandboxDirSegment_Injectivity 证明 sanitize 有损与大小写差异不会造成同空间
// 段碰撞（评审 F1，G1b）：sanitize 等价的输入对必须映射到不同目录段。
// 同时覆盖跨分支碰撞（评审 R6）：digest 分支输出形态本身是合法恒等输入
// （"apikey:Abc" → "apikey-abc-<h>" 与恒等输入 "apikey:abc-<h>"；空 base 变体
// "!!!" → "<h>" 与恒等输入 "<h>"），恒等分支必须排除 digest 形态输入。
func TestSandboxDirSegment_Injectivity(t *testing.T) {
	t.Parallel()
	sha256Hex := func(s string) string {
		sum := sha256.Sum256([]byte(s))
		return hex.EncodeToString(sum[:])
	}
	pairs := [][2]string{
		{"apikey:a/b", "apikey:a-b"},
		{"apikey:..", "apikey:."},
		{"apikey:用户", "apikey:abc"},
		{"github:user/1", "github:user-1"},
		{"Alice", "alice"},
		{"apikey:A", "apikey:a"},
		// 跨分支碰撞：digest 输出可被恒等输入"回放"（R6）
		{"apikey:Abc", "apikey:abc-" + sha256Hex("Abc")},
		// 空 base 变体：纯 64-hex digest 输出可被恒等输入"回放"（R6）
		{"!!!", sha256Hex("!!!")},
	}
	for _, p := range pairs {
		p := p
		t.Run(p[0]+"_vs_"+p[1], func(t *testing.T) {
			t.Parallel()
			require.NotEqual(t, sandboxDirSegment(p[0]), sandboxDirSegment(p[1]),
				"%q and %q must not collide", p[0], p[1])
		})
	}
}

// TestWorkspaceSandboxRoot 覆盖 HotplexHome() 同源 + filepath.Abs 兜底
// （spec §5.1.1）。串行：t.Setenv 修改进程级 HOTPLEX_HOME/HOME。
func TestWorkspaceSandboxRoot(t *testing.T) {
	// ① 绝对 HOTPLEX_HOME → 直接拼接
	t.Setenv("HOTPLEX_HOME", "/x")
	require.Equal(t, filepath.Join("/x", "workspaces", "alice"), WorkspaceSandboxRoot("alice"))

	// ④ 机器用户段
	require.Equal(t, filepath.Join("/x", "workspaces", "apikey-svc1"), WorkspaceSandboxRoot("apikey:svc1"))

	// ⑤ OAuth 用户段（含冒号有损 → 完整 SHA-256 摘要后缀，G1b）
	require.Equal(t, filepath.Join("/x", "workspaces", "oauth-github-user-e14ec4764cff30ca3657d76edc1e77f2f41da3475298407fc58685620054ea66"), WorkspaceSandboxRoot("github:user"))

	// ③ 相对 HOTPLEX_HOME → Abs 兜底（root 恒为绝对路径）
	absRel, err := filepath.Abs("rel")
	require.NoError(t, err)
	t.Setenv("HOTPLEX_HOME", "rel")
	require.Equal(t, filepath.Join(absRel, "workspaces", "alice"), WorkspaceSandboxRoot("alice"))

	// ② 未设置 HOTPLEX_HOME → $HOME/.hotplex/workspaces/<segment>
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HOTPLEX_HOME", "")
	require.Equal(t, filepath.Join(home, ".hotplex", "workspaces", "alice"), WorkspaceSandboxRoot("alice"))
}

// ─── Intelligent directory access ─────────────────────────────────────────────

func TestMatchesUserHomePattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path     string
		match    bool
		username string
	}{
		{"/home/alice/project", true, "alice"},
		{"/home/bob/workspace/test", true, "bob"},
		{"/Users/charlie/dev", true, "charlie"},
		{"/Users/charlie", true, "charlie"},
		{"/home", false, ""},
		{"/home/", false, ""},
		{"/usr/local/alice/bin", false, ""},
		{"/etc/passwd", false, ""},
		{"/tmp/test", false, ""},
		{"/home//project", false, ""}, // empty username
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			match, username := matchesUserHomePattern(tt.path)
			require.Equal(t, tt.match, match)
			require.Equal(t, tt.username, username)
		})
	}
}

func TestMatchesUsrLocalPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path     string
		match    bool
		username string
	}{
		{"/usr/local/alice/bin", true, "alice"},
		{"/usr/local/bob/lib", true, "bob"},
		{"/usr/local/alice", true, "alice"},
		{"/usr/local", false, ""},
		{"/usr/local/", false, ""},
		{"/home/alice/bin", false, ""},
		{"/usr/bin", false, ""},
		{"/usr/local//bin", false, ""}, // empty username
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			match, username := matchesUsrLocalPattern(tt.path)
			require.Equal(t, tt.match, match)
			require.Equal(t, tt.username, username)
		})
	}
}

func TestIsOwnedByCurrentUser(t *testing.T) {
	t.Parallel()

	// Test with existing directory owned by current user
	tmpDir := t.TempDir()
	owned, err := isOwnedByCurrentUser(tmpDir)
	require.NoError(t, err)
	require.True(t, owned, "temp dir should be owned by current user")

	// Test with non-existent path
	owned, err = isOwnedByCurrentUser("/nonexistent/path/that/does/not/exist")
	require.NoError(t, err)
	require.False(t, owned, "non-existent path should return false")

	// Test with system directory (likely not owned by current user)
	owned, err = isOwnedByCurrentUser("/etc")
	require.NoError(t, err)
	// Result depends on whether we're running as root, so we just check no error
	require.NotNil(t, owned)
}

func TestGetCurrentUser(t *testing.T) {
	t.Parallel()

	// Test that getCurrentUser returns a non-empty string
	// (either from $USER or os/user.Current)
	username := getCurrentUser()
	require.NotEmpty(t, username, "getCurrentUser should return a username")

	// On Unix systems, username should be alphanumeric
	require.Regexp(t, `^[a-zA-Z0-9_-]+$`, username,
		"username should contain only alphanumeric characters, hyphens, and underscores")

	// Test fallback to os/user.Current by unsetting $USER
	// Note: This test cannot be parallel because it modifies environment variables
	if os.Getenv("USER") != "" {
		// Save original value
		originalUser := os.Getenv("USER")

		// Unset $USER and test fallback
		unsetErr := os.Unsetenv("USER")
		require.NoError(t, unsetErr, "should be able to unset USER env var")

		// Now getCurrentUser should use os/user.Current fallback
		fallbackUsername := getCurrentUser()
		require.NotEmpty(t, fallbackUsername, "getCurrentUser should fallback to os/user.Current")
		require.Regexp(t, `^[a-zA-Z0-9_-]+$`, fallbackUsername,
			"fallback username should be alphanumeric")

		// Restore original value (cleanup)
		restoreErr := os.Setenv("USER", originalUser)
		require.NoError(t, restoreErr, "should be able to restore USER env var")
	}
}

func TestIsUserAccessibleDirectory_Integration(t *testing.T) {
	t.Parallel()

	currentUser := getCurrentUser()
	require.NotEmpty(t, currentUser)

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "current user home pattern",
			path:     "/home/" + currentUser + "/workspace",
			expected: true,
		},
		{
			name:     "current user usr/local pattern",
			path:     "/usr/local/" + currentUser + "/bin",
			expected: true,
		},
		{
			name:     "other user home pattern",
			path:     "/home/otheruser/workspace",
			expected: false,
		},
		{
			name:     "other user usr/local pattern",
			path:     "/usr/local/otheruser/bin",
			expected: false,
		},
		{
			name:     "system directory",
			path:     "/usr/bin",
			expected: false,
		},
		{
			name:     "usr/local without username",
			path:     "/usr/local/bin",
			expected: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isUserAccessibleDirectory(tt.path)
			require.Equal(t, tt.expected, result)
		})
	}
}

// ─── Path safety ─────────────────────────────────────────────────────────────

func TestSafePathJoin(t *testing.T) {
	t.Parallel()

	// Use an existing system directory for real path resolution tests.
	// Note: filepath.EvalSymlinks on non-existent paths returns an error,
	// so we use /tmp which always exists.
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		base    string
		user    string
		wantErr bool
	}{
		{
			name:    "simple relative path",
			base:    tmpDir,
			user:    "project1/file.txt",
			wantErr: false,
		},
		{
			name:    "dot path",
			base:    tmpDir,
			user:    "./file.txt",
			wantErr: false,
		},
		{
			name:    "double dot traversal rejected",
			base:    tmpDir,
			user:    "../etc/passwd",
			wantErr: true,
		},
		{
			name:    "absolute path rejected",
			base:    tmpDir,
			user:    "/etc/passwd",
			wantErr: true,
		},
		{
			name:    "absolute with traversal rejected",
			base:    tmpDir,
			user:    "/var/hotplex/../../../etc/passwd",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// SafePathJoin calls filepath.EvalSymlinks which requires the path to exist.
			// Create the file so EvalSymlinks succeeds for non-error cases.
			if !tt.wantErr {
				path := filepath.Join(tt.base, tt.user)
				dir := filepath.Dir(path)
				if dir != tt.base {
					_ = os.MkdirAll(dir, 0755)
				}
				_ = os.WriteFile(path, []byte("test"), 0644)
			}
			_, err := SafePathJoin(tt.base, tt.user)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateBaseDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		base    string
		wantErr bool
	}{
		{"var hotplex projects allowed", "/var/hotplex/projects", false},
		{"tmp hotplex allowed", config.TempBaseDir(), false},
		{"home rejected", "/home/user", true},
		{"root rejected", "/", true},
		{"empty rejected", "", true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateBaseDir(tt.base)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ─── Tool validation ──────────────────────────────────────────────────────────

func TestValidateTools(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tools   []string
		wantErr bool
	}{
		{"empty list allowed", []string{}, false},
		{"single valid tool", []string{"Read"}, false},
		{"multiple valid tools", []string{"Read", "Edit", "Bash"}, false},
		{"all allowed tools", []string{"Read", "Edit", "Write", "Bash", "Grep", "Glob", "Agent", "WebFetch", "NotebookEdit", "TodoWrite"}, false},
		{"unknown tool rejected", []string{"Exec"}, true},
		{"mixed valid and invalid", []string{"Read", "Exec", "Bash"}, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateTools(tt.tools)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestIsToolAllowed(t *testing.T) {
	t.Parallel()

	require.True(t, IsToolAllowed("Read"))
	require.True(t, IsToolAllowed("Bash"))
	require.False(t, IsToolAllowed("Exec"))
	require.False(t, IsToolAllowed(""))
}

func TestBuildAllowedToolsArgs(t *testing.T) {
	t.Parallel()

	args := BuildAllowedToolsArgs([]string{"Read", "Bash"})
	require.Equal(t, []string{"--allowed-tools", "Read", "--allowed-tools", "Bash"}, args)

	argsEmpty := BuildAllowedToolsArgs([]string{})
	require.Empty(t, argsEmpty)
}

// ─── Model validation ─────────────────────────────────────────────────────────

func TestValidateModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		model   string
		wantErr bool
	}{
		{"claude-sonnet-4-6 allowed", "claude-sonnet-4-6", false},
		{"claude-opus-4-6 allowed", "claude-opus-4-6", false},
		{"claude-3-5-sonnet allowed", "claude-3-5-sonnet-20241022", false},
		{"case insensitive", "CLAUDE-SONNET-4-6", false},
		{"empty rejected", "", true},
		{"unknown model rejected", "gpt-4o", true},
		{"random string rejected", "abc123", true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateModel(tt.model)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestIsModelAllowed(t *testing.T) {
	t.Parallel()

	require.True(t, IsModelAllowed("claude-sonnet-4-6"))
	require.True(t, IsModelAllowed("CLAUDE-SONNET-4-6"))
	require.False(t, IsModelAllowed("gpt-4o"))
	require.False(t, IsModelAllowed(""))
}

// ─── Bash command policy ──────────────────────────────────────────────────────

func TestCheckBashCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cmd     string
		wantNil bool
		wantP0  bool
		wantP1  bool
	}{
		{"empty nil", "", true, false, false},
		{"harmless ls", "ls -la /tmp", true, false, false},
		{"harmless echo", "echo hello", true, false, false},
		{"rm -rf / P0", "rm -rf /", false, true, false},
		{"rm -rf / with spaces P0", "  rm  -rf  /  ", false, true, false},
		{"dd of=/dev/sda P0", "dd if=/dev/zero of=/dev/sda", false, true, false},
		{"mkfs P0", "mkfs.ext4 /dev/sda1", false, true, false},
		{"fdisk P0", "fdisk -l", false, true, false},
		{"fork bomb P0", ":(){:|:}", false, true, false},
		{"SSH key file P1", "scp -i ~/.ssh/id_rsa file.txt host:/tmp", false, false, true},
		{"reading ssh keys P1", "cat ~/.ssh/authorized_keys", false, false, true},
		{"gh auth token P1", "gh auth token", false, false, true},
		{"curl AWS metadata P1", "curl http://169.254.169.254/latest/meta-data/", false, false, true},
		{"wget AWS metadata P1", "wget -qO- http://169.254.169.254/", false, false, true},
		{"curl GCP metadata P1", "curl metadata.google.internal", false, false, true},
		{"crontab -e P1", "crontab -e", false, false, true},
		{"authorized_keys persistence P1", "echo 'ssh-rsa AAAA...' >> ~/.ssh/authorized_keys", false, false, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			violation := CheckBashCommand(tt.cmd)
			if tt.wantNil {
				require.Nil(t, violation)
			} else {
				require.NotNil(t, violation)
				if tt.wantP0 {
					require.True(t, violation.IsAutoDeny())
					require.Equal(t, "P0", violation.Severity)
				}
				if tt.wantP1 {
					require.False(t, violation.IsAutoDeny())
					require.Equal(t, "P1", violation.Severity)
				}
			}
		})
	}
}

func TestBashPolicyViolation_IsAutoDeny(t *testing.T) {
	t.Parallel()

	p0 := &BashPolicyViolation{Severity: "P0", Reason: "test", Command: "rm -rf /"}
	require.True(t, p0.IsAutoDeny())

	p1 := &BashPolicyViolation{Severity: "P1", Reason: "test", Command: "curl 169.254.169.254"}
	require.False(t, p1.IsAutoDeny())

	nilViolation := (*BashPolicyViolation)(nil)
	require.False(t, nilViolation.IsAutoDeny())
}

// ─── SSRF protection ─────────────────────────────────────────────────────────

func TestValidateURL(t *testing.T) {
	// Mock DNS resolution to avoid dependency on external DNS servers.
	prevLookupHost := LookupHost
	LookupHost = func(host string) ([]string, error) {
		if host == "example.com" {
			return []string{"93.184.216.34"}, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	t.Cleanup(func() { LookupHost = prevLookupHost })

	t.Parallel()

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		// Allowed URLs
		{"https example.com", "https://example.com", false},
		{"http example.com", "http://example.com", false},
		{"https with path", "https://example.com/api/v1/users", false},
		{"https with query", "https://example.com/search?q=golang", false},
		{"https with port", "https://example.com:8443/api", false},

		// Protocol rejections
		{"ftp rejected", "ftp://example.com", true},
		{"file rejected", "file:///etc/passwd", true},
		{"javascript rejected", "javascript:alert(1)", true},
		{"data url rejected", "data:text/html,<script>alert(1)</script>", true},

		// Empty host
		{"empty host", "http://", true},
		{"missing scheme", "example.com", true},

		// Blocked IP: loopback
		{"loopback 127.0.0.1 blocked", "http://127.0.0.1/", true},
		{"loopback 127.1 blocked", "http://127.1/", true},

		// Blocked IP: private network 10.x.x.x
		{"private 10.x.x.x blocked", "http://10.0.0.1/", true},
		{"private 10.255.255.254 blocked", "http://10.255.255.254/", true},

		// Blocked IP: 172.16.x.x
		{"private 172.16.0.0 blocked", "http://172.16.0.1/", true},
		{"private 172.31.255.255 blocked", "http://172.31.255.255/", true},

		// Blocked IP: 192.168.x.x
		{"private 192.168.0.1 blocked", "http://192.168.0.1/", true},
		{"private 192.168.255.255 blocked", "http://192.168.255.255/", true},

		// Blocked IP: cloud metadata
		{"AWS metadata blocked", "http://169.254.169.254/", true},
		{"Alibaba metadata blocked", "http://100.100.100.200/", true},

		// Public IPs should pass (if not in a blocked range)
		{"public ip allowed", "https://1.1.1.1/", false},
		{"cloudflare DNS allowed", "https://1.1.1.1/cdn-cgi/trace", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateURL(tt.url)
			if tt.wantErr {
				require.Error(t, err)
				ssrfErr := new(SSRFProtectionError)
				require.ErrorAs(t, err, &ssrfErr)
				require.NotEmpty(t, ssrfErr.Reason)
				require.NotEmpty(t, ssrfErr.URL)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSSRFProtectionError_Error(t *testing.T) {
	t.Parallel()

	err := &SSRFProtectionError{
		URL:     "http://127.0.0.1/",
		Reason:  "bare IP in URL is blocked",
		Blocked: "127.0.0.1",
	}
	msg := err.Error()
	require.Contains(t, msg, "SSRF blocked")
	require.Contains(t, msg, "127.0.0.1")
	require.Contains(t, msg, "bare IP")
}

func TestIsIPBlocked(t *testing.T) {
	t.Parallel()

	blocked := []net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("10.0.0.1"),
		net.ParseIP("172.16.0.1"),
		net.ParseIP("192.168.1.1"),
		net.ParseIP("169.254.169.254"),
		net.ParseIP("::1"),
	}
	for _, ip := range blocked {
		require.True(t, isIPBlocked(ip), "expected %v to be blocked", ip)
	}

	allowed := []net.IP{
		net.ParseIP("8.8.8.8"),
		net.ParseIP("1.1.1.1"),
		net.ParseIP("140.82.112.4"), // github.com
	}
	for _, ip := range allowed {
		require.False(t, isIPBlocked(ip), "expected %v to be allowed", ip)
	}
}

func TestStripNestedAgent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  []string
		want []string
	}{
		{
			name: "no CLAUDECODE",
			env:  []string{"HOME=/home/user", "PATH=/usr/bin"},
			want: []string{"HOME=/home/user", "PATH=/usr/bin"},
		},
		{
			name: "CLAUDECODE stripped",
			env:  []string{"HOME=/home/user", "CLAUDECODE=inner-agent", "PATH=/usr/bin"},
			want: []string{"HOME=/home/user", "PATH=/usr/bin"},
		},
		{
			name: "only CLAUDECODE",
			env:  []string{"CLAUDECODE=inner"},
			want: []string{},
		},
		{
			name: "multiple CLAUDECODE variants stripped",
			env:  []string{"CLAUDECODE=val1", "CLAUDECODE=val2", "PATH=/bin"},
			want: []string{"PATH=/bin"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := StripNestedAgent(tt.env)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSanitizeArg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "hello world", "hello world"},
		{"printable only", "abc123!@#", "abc123!@#"},
		{"spaces kept", "hello world", "hello world"}, // space (r=32) is printable ASCII
		{"newlines removed", "line1\nline2", "line1line2"},
		{"tabs removed", "col1\tcol2", "col1col2"},
		{"null byte removed", "hel\x00lo", "hello"},
		{"ANSI escape partially kept", "test\x1b[31m", "test[31m"}, // [, ], digits kept; \x1b (27) removed
		{"unicode kept", "hello\u4e16", "hello\u4e16"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SanitizeArg(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

// ─── Coverage: getters and register functions ─────────────────────────────────

func TestGetAllowedBaseDirs(t *testing.T) {
	t.Parallel()
	dirs := GetAllowedBaseDirs()
	require.NotNil(t, dirs)
	// Should return a copy, not the original
	dirs["__test__"] = true
	dirs2 := GetAllowedBaseDirs()
	require.False(t, dirs2["__test__"], "GetAllowedBaseDirs must return a defensive copy")
}

func TestGetForbiddenWorkDirs(t *testing.T) {
	t.Parallel()
	dirs := GetForbiddenWorkDirs()
	require.NotNil(t, dirs)
	require.NotEmpty(t, dirs, "forbidden dirs should have system directories")
}

func TestIsProtected(t *testing.T) {
	t.Parallel()
	require.True(t, IsProtected("HOME"))
	require.True(t, IsProtected("PATH"))
	require.True(t, IsProtected("home"))
	require.True(t, IsProtected("CLAUDECODE"))
	require.False(t, IsProtected("MY_VAR"))
	require.False(t, IsProtected(""))
}

func TestRegisterTool(t *testing.T) {
	t.Parallel()
	RegisterTool("__test_tool__")
	require.True(t, IsToolAllowed("__test_tool__"))
}

func TestRegisterModel(t *testing.T) {
	t.Parallel()
	RegisterModel("__test_model__")
	require.True(t, IsModelAllowed("__test_model__"))
}

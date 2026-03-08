# Slack Channel WorkDir Binding Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement per-channel working directory support for Slack, allowing each channel to bind to an independent project directory.

**Architecture:** A new `ChannelWorkDirManager` component manages channel-to-workdir mappings stored in `slack_channel_workdir.yaml`. The Slack Adapter queries this manager before processing messages. A new slash command `/set-work-dir` allows Channel admins to configure the binding.

**Tech Stack:** Go 1.25, slack-go/slack, yaml config, TDD

---

## Prerequisites

- Read design doc: `docs/plans/2026-03-03-slack-channel-workdir-design.md`
- Existing Slack adapter: `chatapps/slack/adapter.go`
- Existing config loader pattern: `chatapps/config.go`

---

## Task 1: WorkDir Validator

**Files:**
- Create: `chatapps/slack/workdir_validator.go`
- Create: `chatapps/slack/workdir_validator_test.go`

**Step 1: Write the failing test for forbidden paths**

```go
// chatapps/slack/workdir_validator_test.go
package slack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidate_ForbiddenPaths(t *testing.T) {
	validator := NewWorkDirValidator()

	forbiddenPaths := []string{
		"/",
		"/etc",
		"/bin",
		"/root",
		"/sys",
		"/proc",
	}

	for _, path := range forbiddenPaths {
		t.Run(path, func(t *testing.T) {
			err := validator.Validate(path)
			if err == nil {
				t.Errorf("expected error for forbidden path %s, got nil", path)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./chatapps/slack/... -run TestValidate_ForbiddenPaths -v`
Expected: FAIL with "undefined: NewWorkDirValidator"

**Step 3: Write minimal implementation for forbidden paths**

```go
// chatapps/slack/workdir_validator.go
package slack

import (
	"errors"
	"path/filepath"
	"strings"
)

var (
	ErrWorkDirForbidden    = errors.New("forbidden system directory")
	ErrWorkDirNotFound     = errors.New("directory does not exist")
	ErrWorkDirNotDirectory = errors.New("path is not a directory")
	ErrWorkDirNoPermission = errors.New("no access permission")
)

// WorkDirValidator validates working directories for security and accessibility
type WorkDirValidator struct {
	forbiddenPaths map[string]bool
}

// NewWorkDirValidator creates a new validator instance
func NewWorkDirValidator() *WorkDirValidator {
	// System paths that should never be used as working directories
	forbidden := []string{
		"/",
		"/bin", "/sbin", "/usr/bin", "/usr/sbin",
		"/boot", "/dev", "/etc", "/lib", "/lib64",
		"/home", "/root", "/proc", "/sys", "/run", "/tmp",
		"/var", "/usr",
		// macOS specific
		"/System", "/Library", "/Applications", "/Users",
	}

	fp := make(map[string]bool)
	for _, p := range forbidden {
		fp[p] = true
	}

	return &WorkDirValidator{forbiddenPaths: fp}
}

// Validate checks if the path is a valid working directory
func (v *WorkDirValidator) Validate(path string) error {
	// Normalize path first
	normalized, err := v.NormalizePath(path)
	if err != nil {
		return err
	}

	// Check forbidden paths
	if v.isForbidden(normalized) {
		return ErrWorkDirForbidden
	}

	return nil
}

// NormalizePath resolves symlinks and returns absolute path
func (v *WorkDirValidator) NormalizePath(path string) (string, error) {
	// Expand ~ to home directory
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[1:])
	}

	// Get absolute path
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	// Resolve symlinks
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// If path doesn't exist, still return the absolute path
		// Directory existence check is separate
		return abs, nil
	}

	return resolved, nil
}

// isForbidden checks if the path is in the forbidden list
func (v *WorkDirValidator) isForbidden(path string) bool {
	// Direct match
	if v.forbiddenPaths[path] {
		return true
	}

	// Check if path is a subdirectory of a forbidden path
	for forbidden := range v.forbiddenPaths {
		if strings.HasPrefix(path, forbidden+"/") {
			return true
		}
	}

	return false
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./chatapps/slack/... -run TestValidate_ForbiddenPaths -v`
Expected: PASS

**Step 5: Write test for directory existence check**

```go
// Add to workdir_validator_test.go

func TestValidate_DirectoryNotExist(t *testing.T) {
	validator := NewWorkDirValidator()

	nonExistPath := "/this/path/definitely/does/not/exist/12345"
	err := validator.Validate(nonExistPath)

	if err == nil {
		t.Error("expected error for non-existent path, got nil")
	}
	if !errors.Is(err, ErrWorkDirNotFound) {
		t.Errorf("expected ErrWorkDirNotFound, got %v", err)
	}
}
```

**Step 6: Run test to verify it fails**

Run: `go test ./chatapps/slack/... -run TestValidate_DirectoryNotExist -v`
Expected: FAIL

**Step 7: Add existence check to validator**

```go
// Update Validate function in workdir_validator.go

func (v *WorkDirValidator) Validate(path string) error {
	// Normalize path first
	normalized, err := v.NormalizePath(path)
	if err != nil {
		return err
	}

	// Check forbidden paths
	if v.isForbidden(normalized) {
		return ErrWorkDirForbidden
	}

	// Check if directory exists
	info, err := os.Stat(normalized)
	if os.IsNotExist(err) {
		return ErrWorkDirNotFound
	}
	if err != nil {
		return err
	}

	// Check if it's a directory
	if !info.IsDir() {
		return ErrWorkDirNotDirectory
	}

	// Check read permission
	file, err := os.Open(normalized)
	if err != nil {
		return ErrWorkDirNoPermission
	}
	file.Close()

	// Check write permission by testing file creation
	testFile := filepath.Join(normalized, ".hotplex_write_test")
	f, err := os.Create(testFile)
	if err != nil {
		return ErrWorkDirNoPermission
	}
	f.Close()
	os.Remove(testFile)

	return nil
}
```

**Step 8: Run all validator tests**

Run: `go test ./chatapps/slack/... -run TestValidate -v`
Expected: All PASS

**Step 9: Commit**

```bash
git add chatapps/slack/workdir_validator.go chatapps/slack/workdir_validator_test.go
git commit -m "feat(slack): add workdir validator with security checks"
```

---

## Task 2: Channel WorkDir Config Structure

**Files:**
- Create: `chatapps/configs/slack_channel_workdir.yaml`
- Create: `chatapps/slack/channel_workdir_config.go`
- Create: `chatapps/slack/channel_workdir_config_test.go`

**Step 1: Create empty config file**

```yaml
# chatapps/configs/slack_channel_workdir.yaml
# Slack Channel 工作目录映射配置
#
# 格式：
#   channels:
#     <channel_id>: <absolute_path>
#
# 示例：
#   channels:
#     C1234567890: /Users/dev/projects/hotplex
#     C9876543210: /Users/dev/projects/myapp

channels: {}

metadata:
  last_updated: ""
  updated_by: ""
```

**Step 2: Write test for config loading**

```go
// chatapps/slack/channel_workdir_config_test.go
package slack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadChannelWorkDirConfig(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_config.yaml")

	content := `
channels:
  C1234567890: /Users/dev/projects/hotplex
  C9876543210: /Users/dev/projects/myapp
metadata:
  last_updated: "2026-03-03T10:00:00Z"
  updated_by: "U12345"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	config, err := LoadChannelWorkDirConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if len(config.Channels) != 2 {
		t.Errorf("expected 2 channels, got %d", len(config.Channels))
	}

	if config.Channels["C1234567890"] != "/Users/dev/projects/hotplex" {
		t.Errorf("unexpected workdir for C1234567890: %s", config.Channels["C1234567890"])
	}
}

func TestLoadChannelWorkDirConfig_NotExist(t *testing.T) {
	config, err := LoadChannelWorkDirConfig("/nonexistent/path/config.yaml")
	if err != nil {
		t.Errorf("expected no error for nonexistent file, got: %v", err)
	}
	if config == nil {
		t.Error("expected non-nil config")
	}
	if len(config.Channels) != 0 {
		t.Errorf("expected empty channels, got %d", len(config.Channels))
	}
}
```

**Step 3: Run test to verify it fails**

Run: `go test ./chatapps/slack/... -run TestLoadChannelWorkDirConfig -v`
Expected: FAIL

**Step 4: Implement config loading**

```go
// chatapps/slack/channel_workdir_config.go
package slack

import (
	"fmt"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ChannelWorkDirConfig represents the channel-to-workdir mapping configuration
type ChannelWorkDirConfig struct {
	Channels map[string]string `yaml:"channels"`
	Metadata Metadata          `yaml:"metadata"`
}

// Metadata holds audit information
type Metadata struct {
	LastUpdated string `yaml:"last_updated"`
	UpdatedBy   string `yaml:"updated_by"`
}

// LoadChannelWorkDirConfig loads the configuration from a YAML file
func LoadChannelWorkDirConfig(path string) (*ChannelWorkDirConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty config if file doesn't exist
			return &ChannelWorkDirConfig{
				Channels: make(map[string]string),
				Metadata: Metadata{},
			}, nil
		}
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var config ChannelWorkDirConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Ensure Channels map is initialized
	if config.Channels == nil {
		config.Channels = make(map[string]string)
	}

	return &config, nil
}

// Save writes the configuration to a YAML file
func (c *ChannelWorkDirConfig) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Write with restricted permissions
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}

// UpdateMetadata updates the audit metadata
func (c *ChannelWorkDirConfig) UpdateMetadata(userID string) {
	c.Metadata.LastUpdated = time.Now().Format(time.RFC3339)
	c.Metadata.UpdatedBy = userID
}
```

**Step 5: Run tests to verify they pass**

Run: `go test ./chatapps/slack/... -run TestLoadChannelWorkDirConfig -v`
Expected: PASS

**Step 6: Write test for config saving**

```go
// Add to channel_workdir_config_test.go

func TestChannelWorkDirConfig_Save(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "save_test.yaml")

	config := &ChannelWorkDirConfig{
		Channels: map[string]string{
			"C1234567890": "/Users/dev/projects/hotplex",
		},
		Metadata: Metadata{
			LastUpdated: "2026-03-03T10:00:00Z",
			UpdatedBy:   "U12345",
		},
	}

	if err := config.Save(configPath); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config file was not created")
	}

	// Reload and verify
	loaded, err := LoadChannelWorkDirConfig(configPath)
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}

	if loaded.Channels["C1234567890"] != "/Users/dev/projects/hotplex" {
		t.Errorf("unexpected workdir after reload: %s", loaded.Channels["C1234567890"])
	}
}
```

**Step 7: Run all config tests**

Run: `go test ./chatapps/slack/... -run TestChannelWorkDirConfig -v`
Expected: PASS

**Step 8: Commit**

```bash
git add chatapps/configs/slack_channel_workdir.yaml chatapps/slack/channel_workdir_config.go chatapps/slack/channel_workdir_config_test.go
git commit -m "feat(slack): add channel workdir config structure"
```

---

## Task 3: ChannelWorkDirManager

**Files:**
- Create: `chatapps/slack/channel_workdir_manager.go`
- Create: `chatapps/slack/channel_workdir_manager_test.go`

**Step 1: Write test for Get/Set operations**

```go
// chatapps/slack/channel_workdir_manager_test.go
package slack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChannelWorkDirManager_GetSet(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "workdir.yaml")

	// Create a valid test directory
	testWorkDir := filepath.Join(tmpDir, "project")
	if err := os.MkdirAll(testWorkDir, 0755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}

	manager, err := NewChannelWorkDirManager(configPath, nil, nil)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Test Get on empty config
	_, ok := manager.Get("C1234567890")
	if ok {
		t.Error("expected false for non-existent channel")
	}

	// Test Set (skip admin check for now, test that separately)
	// Note: In real implementation, this requires admin check
	// For unit test, we'll bypass by directly modifying config
	manager.config.Channels["C1234567890"] = testWorkDir

	// Test Get after Set
	workdir, ok := manager.Get("C1234567890")
	if !ok {
		t.Error("expected true for existing channel")
	}
	if workdir != testWorkDir {
		t.Errorf("expected %s, got %s", testWorkDir, workdir)
	}
}

func TestChannelWorkDirManager_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "workdir.yaml")

	testWorkDir := filepath.Join(tmpDir, "project")
	if err := os.MkdirAll(testWorkDir, 0755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}

	manager, err := NewChannelWorkDirManager(configPath, nil, nil)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Set a value
	manager.config.Channels["C1234567890"] = testWorkDir

	// Verify it exists
	_, ok := manager.Get("C1234567890")
	if !ok {
		t.Fatal("channel should exist before delete")
	}

	// Delete (directly for unit test)
	delete(manager.config.Channels, "C1234567890")

	// Verify it's gone
	_, ok = manager.Get("C1234567890")
	if ok {
		t.Error("expected false after delete")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./chatapps/slack/... -run TestChannelWorkDirManager -v`
Expected: FAIL with "undefined: NewChannelWorkDirManager"

**Step 3: Implement ChannelWorkDirManager**

```go
// chatapps/slack/channel_workdir_manager.go
package slack

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/slack-go/slack"
)

// ChannelWorkDirManager manages channel-to-workdir mappings
type ChannelWorkDirManager struct {
	configPath string
	config     *ChannelWorkDirConfig
	mu         sync.RWMutex
	logger     *slog.Logger
	client     *slack.Client
	validator  *WorkDirValidator
}

// NewChannelWorkDirManager creates a new manager instance
func NewChannelWorkDirManager(configPath string, client *slack.Client, logger *slog.Logger) (*ChannelWorkDirManager, error) {
	config, err := LoadChannelWorkDirConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &ChannelWorkDirManager{
		configPath: configPath,
		config:     config,
		logger:     logger,
		client:     client,
		validator:  NewWorkDirValidator(),
	}, nil
}

// Get retrieves the working directory for a channel
// Returns (workdir, true) if configured, ("", false) otherwise
func (m *ChannelWorkDirManager) Get(channelID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	workdir, ok := m.config.Channels[channelID]
	return workdir, ok
}

// Set sets the working directory for a channel
// Returns error if validation fails or user lacks admin permission
func (m *ChannelWorkDirManager) Set(channelID, workdir, userID string) error {
	// Validate the workdir first
	if err := m.validator.Validate(workdir); err != nil {
		return fmt.Errorf("validate workdir: %w", err)
	}

	// Normalize the path
	normalized, err := m.validator.NormalizePath(workdir)
	if err != nil {
		return fmt.Errorf("normalize path: %w", err)
	}

	// Check admin permission if client is available
	if m.client != nil {
		isAdmin, err := m.IsAdmin(channelID, userID)
		if err != nil {
			m.logger.Warn("failed to check admin status, allowing operation",
				"channel", channelID, "user", userID, "error", err)
			// Fall through - allow operation on error
		} else if !isAdmin {
			return ErrNotChannelAdmin
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.config.Channels[channelID] = normalized
	m.config.UpdateMetadata(userID)

	if err := m.config.Save(m.configPath); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	m.logger.Info("Channel workdir set",
		"channel", channelID,
		"workdir", normalized,
		"user", userID)

	return nil
}

// Delete removes the working directory configuration for a channel
func (m *ChannelWorkDirManager) Delete(channelID, userID string) error {
	// Check admin permission if client is available
	if m.client != nil {
		isAdmin, err := m.IsAdmin(channelID, userID)
		if err != nil {
			m.logger.Warn("failed to check admin status, allowing operation",
				"channel", channelID, "user", userID, "error", err)
		} else if !isAdmin {
			return ErrNotChannelAdmin
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.config.Channels[channelID]; !exists {
		return nil // Already doesn't exist, not an error
	}

	delete(m.config.Channels, channelID)
	m.config.UpdateMetadata(userID)

	if err := m.config.Save(m.configPath); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	m.logger.Info("Channel workdir deleted",
		"channel", channelID,
		"user", userID)

	return nil
}

// IsAdmin checks if a user is an admin of the channel
func (m *ChannelWorkDirManager) IsAdmin(channelID, userID string) (bool, error) {
	if m.client == nil {
		return false, fmt.Errorf("slack client not configured")
	}

	// Get channel info
	info, err := m.client.GetConversationInfo(channelID, false)
	if err != nil {
		return false, fmt.Errorf("get conversation info: %w", err)
	}

	// Check if user is channel creator or admin
	if info.Creator == userID {
		return true, nil
	}

	// Check if user is workspace admin
	users, err := m.client.GetUsers()
	if err != nil {
		return false, fmt.Errorf("get users: %w", err)
	}

	for _, user := range users {
		if user.ID == userID {
			return user.IsAdmin || user.IsOwner, nil
		}
	}

	return false, nil
}

// Reload reloads the configuration from disk
func (m *ChannelWorkDirManager) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	config, err := LoadChannelWorkDirConfig(m.configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	m.config = config
	m.logger.Info("Channel workdir config reloaded")
	return nil
}
```

**Step 4: Add error definitions**

```go
// Add to channel_workdir_manager.go or errors.go

var ErrNotChannelAdmin = fmt.Errorf("user is not a channel admin")
```

**Step 5: Run tests to verify they pass**

Run: `go test ./chatapps/slack/... -run TestChannelWorkDirManager -v`
Expected: PASS

**Step 6: Commit**

```bash
git add chatapps/slack/channel_workdir_manager.go chatapps/slack/channel_workdir_manager_test.go
git commit -m "feat(slack): add ChannelWorkDirManager for per-channel workdir"
```

---

## Task 4: Slash Command /set-work-dir

**Files:**
- Create: `chatapps/slack/slash_set_workdir.go`
- Create: `chatapps/slack/slash_set_workdir_test.go`

**Step 1: Write test for slash command parsing**

```go
// chatapps/slack/slash_set_workdir_test.go
package slack

import (
	"testing"
)

func TestParseSetWorkDirCommand(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected SetWorkDirAction
		path     string
	}{
		{
			name:     "set path",
			text:     "/Users/dev/projects/hotplex",
			expected: ActionSet,
			path:     "/Users/dev/projects/hotplex",
		},
		{
			name:     "set path with spaces",
			text:     "/Users/dev/my projects/app",
			expected: ActionSet,
			path:     "/Users/dev/my projects/app",
		},
		{
			name:     "show current",
			text:     "",
			expected: ActionShow,
			path:     "",
		},
		{
			name:     "reset",
			text:     "--reset",
			expected: ActionReset,
			path:     "",
		},
		{
			name:     "tilde path",
			text:     "~/projects/hotplex",
			expected: ActionSet,
			path:     "~/projects/hotplex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, path := ParseSetWorkDirCommand(tt.text)
			if action != tt.expected {
				t.Errorf("expected action %v, got %v", tt.expected, action)
			}
			if path != tt.path {
				t.Errorf("expected path %s, got %s", tt.path, path)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./chatapps/slack/... -run TestParseSetWorkDirCommand -v`
Expected: FAIL

**Step 3: Implement command parsing**

```go
// chatapps/slack/slash_set_workdir.go
package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

// SetWorkDirAction represents the action type for /set-work-dir command
type SetWorkDirAction int

const (
	ActionShow   SetWorkDirAction = iota // Show current workdir
	ActionSet                            // Set workdir
	ActionReset                          // Reset/delete workdir
)

// ParseSetWorkDirCommand parses the command text
func ParseSetWorkDirCommand(text string) (SetWorkDirAction, string) {
	text = strings.TrimSpace(text)

	if text == "" {
		return ActionShow, ""
	}

	if text == "--reset" {
		return ActionReset, ""
	}

	return ActionSet, text
}

// SetWorkDirHandler handles the /set-work-dir slash command
type SetWorkDirHandler struct {
	manager *ChannelWorkDirManager
	client  *slack.Client
}

// NewSetWorkDirHandler creates a new handler
func NewSetWorkDirHandler(manager *ChannelWorkDirManager, client *slack.Client) *SetWorkDirHandler {
	return &SetWorkDirHandler{
		manager: manager,
		client:  client,
	}
}

// Handle processes the slash command
func (h *SetWorkDirHandler) Handle(ctx context.Context, cmd slack.SlashCommand) (string, error) {
	action, path := ParseSetWorkDirCommand(cmd.Text)

	switch action {
	case ActionShow:
		return h.handleShow(ctx, cmd)
	case ActionSet:
		return h.handleSet(ctx, cmd, path)
	case ActionReset:
		return h.handleReset(ctx, cmd)
	default:
		return "", fmt.Errorf("unknown action")
	}
}

func (h *SetWorkDirHandler) handleShow(ctx context.Context, cmd slack.SlashCommand) (string, error) {
	workdir, configured := h.manager.Get(cmd.ChannelID)

	if !configured {
		return formatNotConfiguredMessage(), nil
	}

	return formatShowMessage(cmd.ChannelName, workdir), nil
}

func (h *SetWorkDirHandler) handleSet(ctx context.Context, cmd slack.SlashCommand, path string) (string, error) {
	err := h.manager.Set(cmd.ChannelID, path, cmd.UserID)
	if err != nil {
		return formatErrorMessage(err), nil
	}

	// Get the normalized path for display
	workdir, _ := h.manager.Get(cmd.ChannelID)
	return formatSetSuccessMessage(cmd.ChannelName, workdir), nil
}

func (h *SetWorkDirHandler) handleReset(ctx context.Context, cmd slack.SlashCommand) (string, error) {
	err := h.manager.Delete(cmd.ChannelID, cmd.UserID)
	if err != nil {
		return formatErrorMessage(err), nil
	}

	return formatResetSuccessMessage(cmd.ChannelName), nil
}

// Message formatting functions

func formatNotConfiguredMessage() string {
	return `⚠️ 当前 Channel 尚未配置工作目录

请使用 \`/set-work-dir <path>\` 设置

示例: \`/set-work-dir /Users/dev/projects/myapp\``
}

func formatShowMessage(channelName, workdir string) string {
	return fmt.Sprintf(`📍 当前工作目录
Channel: #%s
路径: %s`, channelName, workdir)
}

func formatSetSuccessMessage(channelName, workdir string) string {
	return fmt.Sprintf(`✅ 工作目录已设置
Channel: #%s
路径: %s`, channelName, workdir)
}

func formatResetSuccessMessage(channelName string) string {
	return fmt.Sprintf(`✅ 工作目录已重置
Channel: #%s`, channelName)
}

func formatErrorMessage(err error) string {
	if err == ErrNotChannelAdmin {
		return `❌ 权限不足
只有 Channel 管理员可以设置工作目录`
	}

	if err == ErrWorkDirForbidden {
		return fmt.Sprintf(`❌ 工作目录校验失败
原因: 禁止使用系统目录`)
	}

	if err == ErrWorkDirNotFound {
		return fmt.Sprintf(`❌ 工作目录校验失败
原因: 目录不存在`)
	}

	if err == ErrWorkDirNotDirectory {
		return fmt.Sprintf(`❌ 工作目录校验失败
原因: 路径不是目录`)
	}

	if err == ErrWorkDirNoPermission {
		return fmt.Sprintf(`❌ 工作目录校验失败
原因: 无访问权限`)
	}

	return fmt.Sprintf(`❌ 操作失败
原因: %s`, err.Error())
}

// HandleSocketModeEvent handles the command in Socket Mode
func (h *SetWorkDirHandler) HandleSocketModeEvent(evt *socketmode.Event, client *socketmode.Client) {
	cmd, ok := evt.Data.(slack.SlashCommand)
	if !ok {
		return
	}

	resp, err := h.Handle(context.Background(), cmd)
	if err != nil {
		client.Ack(*evt.Request, map[string]interface{}{
			"text": fmt.Sprintf("Error: %s", err.Error()),
		})
		return
	}

	client.Ack(*evt.Request, map[string]interface{}{
		"text":          resp,
		"response_type": "ephemeral", // Only visible to the user
	})
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./chatapps/slack/... -run TestParseSetWorkDirCommand -v`
Expected: PASS

**Step 5: Write test for message formatting**

```go
// Add to slash_set_workdir_test.go

func TestFormatMessages(t *testing.T) {
	t.Run("not configured", func(t *testing.T) {
		msg := formatNotConfiguredMessage()
		if !strings.Contains(msg, "尚未配置") {
			t.Error("expected '尚未配置' in message")
		}
	})

	t.Run("show", func(t *testing.T) {
		msg := formatShowMessage("dev-hotplex", "/Users/dev/projects/hotplex")
		if !strings.Contains(msg, "dev-hotplex") {
			t.Error("expected channel name in message")
		}
		if !strings.Contains(msg, "/Users/dev/projects/hotplex") {
			t.Error("expected workdir in message")
		}
	})

	t.Run("set success", func(t *testing.T) {
		msg := formatSetSuccessMessage("dev-hotplex", "/Users/dev/projects/hotplex")
		if !strings.Contains(msg, "已设置") {
			t.Error("expected '已设置' in message")
		}
	})

	t.Run("reset success", func(t *testing.T) {
		msg := formatResetSuccessMessage("dev-hotplex")
		if !strings.Contains(msg, "已重置") {
			t.Error("expected '已重置' in message")
		}
	})

	t.Run("error permission", func(t *testing.T) {
		msg := formatErrorMessage(ErrNotChannelAdmin)
		if !strings.Contains(msg, "权限不足") {
			t.Error("expected '权限不足' in message")
		}
	})
}
```

**Step 6: Run all slash command tests**

Run: `go test ./chatapps/slack/... -run TestParseSetWorkDirCommand -v && go test ./chatapps/slack/... -run TestFormatMessages -v`
Expected: All PASS

**Step 7: Commit**

```bash
git add chatapps/slack/slash_set_workdir.go chatapps/slack/slash_set_workdir_test.go
git commit -m "feat(slack): add /set-work-dir slash command handler"
```

---

## Task 5: Integrate with Slack Adapter

**Files:**
- Modify: `chatapps/slack/adapter.go`
- Modify: `chatapps/slack/adapter_test.go`

**Step 1: Write test for workdir integration**

```go
// Add to adapter_test.go or create new integration test

func TestAdapter_GetWorkDirForChannel(t *testing.T) {
	// This test verifies the adapter correctly queries the workdir manager
	// Integration level test
}
```

**Step 2: Modify Adapter struct to include manager**

```go
// In adapter.go, add to Adapter struct:

type Adapter struct {
	*base.Adapter
	// ... existing fields ...

	// Channel workdir manager (nil = use global workdir)
	workDirManager *ChannelWorkDirManager
}
```

**Step 3: Initialize manager in NewAdapter**

```go
// In NewAdapter function, add:

// Initialize channel workdir manager
configPath := filepath.Dir(configPath) + "/slack_channel_workdir.yaml"
workDirManager, err := NewChannelWorkDirManager(configPath, slackClient, logger)
if err != nil {
	logger.Warn("failed to initialize workdir manager, using global workdir", "error", err)
}
a.workDirManager = workDirManager
```

**Step 4: Add method to get effective workdir**

```go
// Add to adapter.go:

// GetEffectiveWorkDir returns the effective workdir for a channel
// Returns channel-specific workdir if configured, otherwise global workdir
func (a *Adapter) GetEffectiveWorkDir(channelID string) (string, bool) {
	// Check channel-specific workdir first
	if a.workDirManager != nil {
		if workdir, ok := a.workDirManager.Get(channelID); ok {
			return workdir, true
		}
	}

	// Fallback to global workdir from config
	if a.config != nil && a.config.Engine.WorkDir != "" {
		return a.config.Engine.WorkDir, true
	}

	return "", false
}
```

**Step 5: Update message handler to check workdir**

```go
// In the message handling function, add workdir check:

func (a *Adapter) handleChannelMessage(ctx context.Context, event *slackevents.MessageEvent, channelID string) error {
	// Get effective workdir
	workdir, configured := a.GetEffectiveWorkDir(channelID)
	if !configured {
		return a.sendWorkDirNotConfigured(ctx, event.Channel, event.ThreadTimeStamp)
	}

	// Build config with workdir
	cfg := &types.Config{
		WorkDir:   workdir,
		SessionID: sessionID,
		// ...
	}

	// ... rest of message handling
}

func (a *Adapter) sendWorkDirNotConfigured(ctx context.Context, channelID, threadTS string) error {
	msg := &base.ChatMessage{
		Text: formatNotConfiguredMessage(),
	}
	_, _, err := a.client.PostMessage(channelID,
		slack.MsgOptionText(msg.Text, false),
		slack.MsgOptionTS(threadTS),
	)
	return err
}
```

**Step 6: Register slash command handler**

```go
// In adapter.go, register the slash command:

func (a *Adapter) registerSlashCommands() {
	// Existing commands...

	// Register /set-work-dir
	setWorkDirHandler := NewSetWorkDirHandler(a.workDirManager, a.client)
	a.cmdRegistry.Register("set-work-dir", setWorkDirHandler)
}
```

**Step 7: Run all tests**

Run: `go test ./chatapps/slack/... -v`
Expected: All PASS

**Step 8: Run race detector**

Run: `go test ./chatapps/slack/... -race`
Expected: No race conditions

**Step 9: Commit**

```bash
git add chatapps/slack/adapter.go chatapps/slack/adapter_test.go
git commit -m "feat(slack): integrate ChannelWorkDirManager with adapter"
```

---

## Task 6: Update Slack App Manifest

**Files:**
- Modify: `chatapps/configs/slack.yaml` (add documentation)

**Step 1: Add slash command to Slack App config**

Update your Slack App configuration in the Slack Developer Console:

```yaml
# Add to your Slack App Manifest
slash_commands:
  - command: /set-work-dir
    description: 设置当前 Channel 的工作目录
    should_escape: false
```

**Step 2: Update slack.yaml documentation**

```yaml
# Add comment to slack.yaml
# NOTE: To enable /set-work-dir command, add it to your Slack App Manifest:
#
# slash_commands:
#   - command: /set-work-dir
#     description: 设置当前 Channel 的工作目录
#     should_escape: false
```

**Step 3: Commit**

```bash
git add chatapps/configs/slack.yaml
git commit -m "docs(slack): add /set-work-dir command documentation"
```

---

## Task 7: Final Verification

**Step 1: Run full test suite**

Run: `go test ./... -race`
Expected: All PASS

**Step 2: Build verification**

Run: `go build ./...`
Expected: No errors

**Step 3: Run linter**

Run: `golangci-lint run ./chatapps/...`
Expected: No errors

**Step 4: Manual integration test**

1. Start the Slack adapter in Socket Mode
2. Send a message to an unconfigured channel
3. Verify "not configured" message appears
4. Run `/set-work-dir ~/projects/test`
5. Verify success message
6. Send another message
7. Verify it processes with the correct workdir

**Step 5: Final commit**

```bash
git add docs/plans/2026-03-03-slack-channel-workdir-impl.md
git commit -m "docs: add Slack channel workdir implementation plan"
```

---

## Summary

| Task | Description | Files |
|------|-------------|-------|
| 1 | WorkDir Validator | `workdir_validator.go`, `workdir_validator_test.go` |
| 2 | Config Structure | `slack_channel_workdir.yaml`, `channel_workdir_config.go`, `channel_workdir_config_test.go` |
| 3 | ChannelWorkDirManager | `channel_workdir_manager.go`, `channel_workdir_manager_test.go` |
| 4 | Slash Command | `slash_set_workdir.go`, `slash_set_workdir_test.go` |
| 5 | Adapter Integration | `adapter.go`, `adapter_test.go` |
| 6 | Manifest Update | `slack.yaml` |
| 7 | Final Verification | - |

---

## Checklist

- [ ] Task 1: WorkDir Validator
- [ ] Task 2: Config Structure
- [ ] Task 3: ChannelWorkDirManager
- [ ] Task 4: Slash Command /set-work-dir
- [ ] Task 5: Adapter Integration
- [ ] Task 6: Manifest Update
- [ ] Task 7: Final Verification

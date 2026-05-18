# Agent Management WebUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 HotPlex 添加集成在 webchat SPA 中的 Agent（bot）可视化管理面板，涵盖 bot 配置管理、session 运维、cron 任务管理。

**Architecture:** 前端集成在现有 webchat Next.js 项目（`app/admin/` 路由组），通过独立登录页认证后调用 admin API。后端在 `internal/admin/` 扩展 provider 接口 + handler，通过适配器桥接文件系统操作（config.yaml + agent-configs）。前端走 admin 端口，复用现有 bearer token + scope 体系。

**Tech Stack:** Go 1.22+ (admin handler/provider), Next.js 16 (App Router, static export), React 19, Tailwind CSS 4, plain fetch API

**Spec:** `docs/superpowers/specs/2026-05-18-agent-management-webui-design.md`

---

## File Structure

### 后端新建/修改

```
internal/admin/
  bot_config_handlers.go      # NEW — Bot 配置 CRUD handler（增强 GET + 新增 POST/PATCH/DELETE + config 文件读写 + preview）
  bot_config_provider.go      # NEW — BotConfigProvider 接口 + DTO
cmd/hotplex/
  admin_adapters.go           # MODIFY — 新增 botConfigAdapter
  routes.go                   # MODIFY — 注册新路由
internal/agentconfig/
  writer.go                   # NEW — Agent config 文件写入函数（WriteFile、ResolveFilePath）
```

### 前端新建（webchat/）

```
app/admin/
  layout.tsx                          # Admin shell（侧边栏 + auth guard）
  login/page.tsx                      # 登录页
  page.tsx                            # Dashboard 总览
  bots/
    page.tsx                          # Bot 列表
    new/page.tsx                      # 创建新 bot
    [name]/page.tsx                   # Bot 详情/编辑
  sessions/
    page.tsx                          # Session 列表
    [id]/page.tsx                     # Session 详情
  cron/
    page.tsx                          # Cron 列表
    [id]/page.tsx                     # Cron 详情
  settings/page.tsx                   # 连接设置
lib/api/
  admin-client.ts                     # Admin API client 封装
  admin-bots.ts                       # Bot CRUD 接口
  admin-sessions.ts                   # Session 接口
  admin-cron.ts                       # Cron 接口
lib/types/
  admin.ts                            # Admin 相关类型定义
components/admin/
  admin-nav.tsx                       # 侧边导航
  bot-card.tsx                        # Bot 卡片
  bot-config-editor.tsx               # Agent config 编辑器
  bot-access-control.tsx              # 访问控制编辑
  system-prompt-preview.tsx           # System prompt 预览弹窗
  session-table.tsx                   # Session 表格
  cron-table.tsx                      # Cron 表格
  status-badge.tsx                    # 状态 badge
  metric-card.tsx                     # 指标卡片
hooks/
  use-admin-auth.ts                   # Auth 状态管理
```

---

## Phase 1 — 基础框架 + Bot 只读

### Task 1: 后端 — BotConfigProvider 接口与 DTO

**Files:**
- Create: `internal/admin/bot_config_provider.go`
- Test: `internal/admin/bot_config_provider_test.go`

- [ ] **Step 1: 定义接口和 DTO**

`internal/admin/bot_config_provider.go`:

```go
package admin

import "context"

// BotConfigProvider abstracts bot configuration file operations for the admin API.
type BotConfigProvider interface {
	// GetBotConfig returns the merged runtime + file config for a bot by name.
	GetBotConfig(ctx context.Context, name string) (*BotConfigEntry, error)
	// ListBotConfigs returns configs for all bots across all platforms.
	ListBotConfigs(ctx context.Context) ([]BotConfigEntry, error)
	// GetAgentConfigFile reads a specific agent config file for a bot.
	GetAgentConfigFile(ctx context.Context, name, file string) (*AgentConfigFile, error)
	// GetSystemPromptPreview assembles and returns the full system prompt XML for a bot.
	GetSystemPromptPreview(ctx context.Context, name string) (string, error)
	// UpdateBotConfig updates bot attributes in config.yaml.
	UpdateBotConfig(ctx context.Context, name string, updates map[string]any) (*BotConfigEntry, error)
	// CreateBot adds a new bot to config.yaml and creates agent-config directory.
	CreateBot(ctx context.Context, input map[string]any) (*BotConfigEntry, error)
	// DeleteBot removes a bot from config.yaml.
	DeleteBot(ctx context.Context, name string) error
	// WriteAgentConfigFile writes content to a specific agent config file.
	WriteAgentConfigFile(ctx context.Context, name, file, content string) (*AgentConfigFile, error)
}

// AgentConfigFileName enumerates valid agent config file names.
type AgentConfigFileName string

const (
	ConfigFileSoul   AgentConfigFileName = "soul"
	ConfigFileAgents AgentConfigFileName = "agents"
	ConfigFileSkills AgentConfigFileName = "skills"
	ConfigFileUser   AgentConfigFileName = "user"
	ConfigFileMemory AgentConfigFileName = "memory"
)

// ValidConfigFiles is the whitelist of allowed agent config file names.
var ValidConfigFiles = map[AgentConfigFileName]bool{
	ConfigFileSoul:   true,
	ConfigFileAgents: true,
	ConfigFileSkills: true,
	ConfigFileUser:   true,
	ConfigFileMemory: true,
}

// BotConfigEntry is the full bot configuration DTO for the admin API.
type BotConfigEntry struct {
	Name        string              `json:"name"`
	Platform    string              `json:"platform"`
	BotID       string              `json:"bot_id"`
	Status      string              `json:"status"`
	ConnectedAt string              `json:"connected_at,omitempty"`
	Config      *BotConfigAttrs     `json:"config,omitempty"`
	AgentConfig *AgentConfigSummary `json:"agent_configs,omitempty"`
}

// BotConfigAttrs holds non-credential bot attributes from config.yaml.
type BotConfigAttrs struct {
	WorkerType     string   `json:"worker_type"`
	WorkDir        string   `json:"work_dir"`
	DMPolicy       string   `json:"dm_policy"`
	GroupPolicy    string   `json:"group_policy"`
	RequireMention *bool    `json:"require_mention,omitempty"`
	AllowFrom      []string `json:"allow_from,omitempty"`
	AllowDMFrom    []string `json:"allow_dm_from,omitempty"`
	AllowGroupFrom []string `json:"allow_group_from,omitempty"`
	STT            STTAttrs `json:"stt,omitempty"`
	TTS            TTSAttrs `json:"tts,omitempty"`
}

type STTAttrs struct {
	Provider string `json:"provider,omitempty"`
}

type TTSAttrs struct {
	Provider string `json:"provider,omitempty"`
	Voice    string `json:"voice,omitempty"`
}

// AgentConfigSummary describes the agent config files for a bot.
type AgentConfigSummary struct {
	Soul   *AgentConfigMeta `json:"soul,omitempty"`
	Agents *AgentConfigMeta `json:"agents,omitempty"`
	Skills *AgentConfigMeta `json:"skills,omitempty"`
	User   *AgentConfigMeta `json:"user,omitempty"`
	Memory *AgentConfigMeta `json:"memory,omitempty"`
}

// AgentConfigMeta describes a single agent config file's resolved state.
type AgentConfigMeta struct {
	Source string `json:"source"` // "bot", "platform", or "global"
	Size   int    `json:"size"`   // content length in bytes
}

// AgentConfigFile is the response for reading/writing a single agent config file.
type AgentConfigFile struct {
	Content string `json:"content"`
	Source  string `json:"source"`
	Size    int    `json:"size"`
	File    string `json:"file"`
}
```

- [ ] **Step 2: 运行编译确认类型正确**

Run: `cd /home/hotplex/.hotplex/workspace/hotplex && go build ./internal/admin/...`
Expected: BUILD SUCCESS（接口无实现方不会报错）

- [ ] **Step 3: Commit**

```bash
git add internal/admin/bot_config_provider.go
git commit -m "feat(admin): add BotConfigProvider interface and DTO types"
```

---

### Task 2: 后端 — agentconfig 写入函数

**Files:**
- Create: `internal/agentconfig/writer.go`
- Test: `internal/agentconfig/writer_test.go`

- [ ] **Step 1: 编写写入函数**

`internal/agentconfig/writer.go`:

```go
package agentconfig

import (
	"fmt"
	"os"
	"path/filepath"
)

// ResolveFilePath returns the bot-level file path for a given config file.
// Creates the bot directory if it doesn't exist.
func ResolveFilePath(dir, platform, botID, fileName string) (string, error) {
	if botID != "" && filepath.Base(botID) != botID {
		return "", fmt.Errorf("agentconfig: invalid botID %q: path separators not allowed", botID)
	}
	base := filepath.Join(dir, platform, botID)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", fmt.Errorf("agentconfig: create dir %s: %w", base, err)
	}
	return filepath.Join(base, fileName), nil
}

// WriteFile writes content to a bot-level agent config file atomically.
// maxBytes is the maximum allowed content size (e.g. MaxFileChars).
func WriteFile(dir, platform, botID, fileName, content string, maxBytes int) error {
	if maxBytes > 0 && len(content) > maxBytes {
		return fmt.Errorf("agentconfig: content exceeds %d bytes", maxBytes)
	}
	path, err := ResolveFilePath(dir, platform, botID, fileName)
	if err != nil {
		return err
	}
	// Atomic write: temp file + rename
	tmp, err := os.CreateTemp(filepath.Dir(path), fileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("agentconfig: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("agentconfig: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("agentconfig: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("agentconfig: rename: %w", err)
	}
	return nil
}

// ResolvedSource returns which level a config file was resolved from.
// Returns "bot" if platform/botID/file exists, "platform" if platform/file exists,
// "global" if file exists at root, or "" if not found.
func ResolvedSource(dir, platform, botID, fileName string) string {
	if botID != "" {
		p := filepath.Join(dir, platform, botID, fileName)
		if _, err := os.Stat(p); err == nil {
			return "bot"
		}
	}
	p := filepath.Join(dir, platform, fileName)
	if _, err := os.Stat(p); err == nil {
		return "platform"
	}
	p = filepath.Join(dir, fileName)
	if _, err := os.Stat(p); err == nil {
		return "global"
	}
	return ""
}
```

- [ ] **Step 2: 编写测试**

`internal/agentconfig/writer_test.go`:

```go
package agentconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteFile_CreatesDirAndWrites(t *testing.T) {
	dir := t.TempDir()
	content := "# Soul\nYou are helpful."

	err := WriteFile(dir, "feishu", "ou_123", "SOUL.md", content, MaxFileChars)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "feishu", "ou_123", "SOUL.md"))
	require.NoError(t, err)
	require.Equal(t, content, string(data))
}

func TestWriteFile_RejectsOversized(t *testing.T) {
	dir := t.TempDir()
	big := make([]byte, 101)

	err := WriteFile(dir, "feishu", "ou_123", "SOUL.md", string(big), 100)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds")
}

func TestWriteFile_RejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	err := WriteFile(dir, "feishu", "../etc", "SOUL.md", "x", MaxFileChars)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid botID")
}

func TestResolvedSource_BotLevel(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "feishu", "ou_123"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "feishu", "ou_123", "SOUL.md"), []byte("x"), 0o644))

	src := ResolvedSource(dir, "feishu", "ou_123", "SOUL.md")
	require.Equal(t, "bot", src)
}

func TestResolvedSource_PlatformLevel(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "feishu"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "feishu", "SOUL.md"), []byte("x"), 0o644))

	src := ResolvedSource(dir, "feishu", "ou_456", "SOUL.md")
	require.Equal(t, "platform", src)
}

func TestResolvedSource_GlobalLevel(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("x"), 0o644))

	src := ResolvedSource(dir, "slack", "U789", "SOUL.md")
	require.Equal(t, "global", src)
}

func TestResolvedSource_NotFound(t *testing.T) {
	dir := t.TempDir()
	src := ResolvedSource(dir, "slack", "U789", "SOUL.md")
	require.Equal(t, "", src)
}
```

- [ ] **Step 3: 运行测试**

Run: `go test ./internal/agentconfig/ -run TestWriteFile -v && go test ./internal/agentconfig/ -run TestResolvedSource -v`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentconfig/writer.go internal/agentconfig/writer_test.go
git commit -m "feat(agentconfig): add file write and source resolution functions"
```

---

### Task 3: 后端 — BotConfigProvider 实现（文件操作适配器）

**Files:**
- Create: `cmd/hotplex/bot_config_adapter.go`
- Test: `cmd/hotplex/bot_config_adapter_test.go`

- [ ] **Step 1: 实现 botConfigAdapter**

`cmd/hotplex/bot_config_adapter.go`:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hrygo/hotplex/internal/admin"
	"github.com/hrygo/hotplex/internal/agentconfig"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/messaging"
	"gopkg.in/yaml.v3"
)

// botConfigAdapter implements admin.BotConfigProvider by reading/writing
// config.yaml and agent-config files on disk.
type botConfigAdapter struct {
	cfgStore       *config.ConfigStore
	agentConfigDir string // typically ~/.hotplex/agent-configs/
	configFilePath string // path to config.yaml
}

func newBotConfigAdapter(cfgStore *config.ConfigStore, agentConfigDir, configFilePath string) *botConfigAdapter {
	return &botConfigAdapter{
		cfgStore:       cfgStore,
		agentConfigDir: agentConfigDir,
		configFilePath: configFilePath,
	}
}

// resolvePlatformAndBotID resolves platform and botID from the runtime registry.
func (a *botConfigAdapter) resolvePlatformAndBotID(name string) (platform, botID string, ok bool) {
	entry, ok := messaging.DefaultBotRegistry().GetByName(name)
	if !ok {
		return "", "", false
	}
	return string(entry.Platform), entry.BotID, true
}

func (a *botConfigAdapter) GetBotConfig(_ context.Context, name string) (*admin.BotConfigEntry, error) {
	// 1. Runtime status from registry
	entry, regOk := messaging.DefaultBotRegistry().GetByName(name)
	if !regOk {
		return nil, fmt.Errorf("bot %q not found", name)
	}
	platform := string(entry.Platform)
	botID := entry.BotID

	result := &admin.BotConfigEntry{
		Name:        entry.Name,
		Platform:    platform,
		BotID:       botID,
		Status:      string(entry.Status),
		ConnectedAt: entry.ConnectedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	// 2. Config attrs from config.yaml
	cfg := a.cfgStore.Load()
	result.Config = a.extractBotAttrs(cfg, platform, name)

	// 3. Agent config summary
	result.AgentConfig = a.getAgentConfigSummary(platform, botID)

	return result, nil
}

func (a *botConfigAdapter) ListBotConfigs(_ context.Context) ([]admin.BotConfigEntry, error) {
	entries := messaging.DefaultBotRegistry().ListAll()
	cfg := a.cfgStore.Load()

	result := make([]admin.BotConfigEntry, 0, len(entries))
	for _, e := range entries {
		platform := string(e.Platform)
		botID := e.BotID
		entry := admin.BotConfigEntry{
			Name:        e.Name,
			Platform:    platform,
			BotID:       botID,
			Status:      string(e.Status),
			ConnectedAt: e.ConnectedAt.Format("2006-01-02T15:04:05Z07:00"),
			Config:      a.extractBotAttrs(cfg, platform, e.Name),
			AgentConfig: a.getAgentConfigSummary(platform, botID),
		}
		result = append(result, entry)
	}
	return result, nil
}

func (a *botConfigAdapter) GetAgentConfigFile(_ context.Context, name, file string) (*admin.AgentConfigFile, error) {
	if !admin.ValidConfigFiles[admin.AgentConfigFileName(file)] {
		return nil, fmt.Errorf("invalid config file name: %q", file)
	}
	platform, botID, ok := a.resolvePlatformAndBotID(name)
	if !ok {
		return nil, fmt.Errorf("bot %q not found", name)
	}
	fileName := strings.ToUpper(file) + ".md"
	configs, err := agentconfig.Load(a.agentConfigDir, platform, botID)
	if err != nil {
		return nil, fmt.Errorf("load agent config: %w", err)
	}
	content := a.getConfigField(configs, file)
	source := agentconfig.ResolvedSource(a.agentConfigDir, platform, botID, fileName)
	if source == "" && content != "" {
		source = "global"
	}
	return &admin.AgentConfigFile{
		Content: content,
		Source:  source,
		Size:    len(content),
		File:    file,
	}, nil
}

func (a *botConfigAdapter) GetSystemPromptPreview(_ context.Context, name string) (string, error) {
	platform, botID, ok := a.resolvePlatformAndBotID(name)
	if !ok {
		return "", fmt.Errorf("bot %q not found", name)
	}
	configs, err := agentconfig.Load(a.agentConfigDir, platform, botID)
	if err != nil {
		return "", fmt.Errorf("load agent config: %w", err)
	}
	return agentconfig.BuildSystemPrompt(configs), nil
}

func (a *botConfigAdapter) WriteAgentConfigFile(_ context.Context, name, file, content string) (*admin.AgentConfigFile, error) {
	if !admin.ValidConfigFiles[admin.AgentConfigFileName(file)] {
		return nil, fmt.Errorf("invalid config file name: %q", file)
	}
	platform, botID, ok := a.resolvePlatformAndBotID(name)
	if !ok {
		return nil, fmt.Errorf("bot %q not found", name)
	}
	fileName := strings.ToUpper(file) + ".md"
	if err := agentconfig.WriteFile(a.agentConfigDir, platform, botID, fileName, content, agentconfig.MaxFileChars); err != nil {
		return nil, fmt.Errorf("write agent config: %w", err)
	}
	source := agentconfig.ResolvedSource(a.agentConfigDir, platform, botID, fileName)
	return &admin.AgentConfigFile{
		Content: content,
		Source:  source,
		Size:    len(content),
		File:    file,
	}, nil
}

// --- Phase 2: write operations (UpdateBotConfig, CreateBot, DeleteBot) ---
// These will be implemented in Phase 2. Return errors for now.

func (a *botConfigAdapter) UpdateBotConfig(_ context.Context, name string, updates map[string]any) (*admin.BotConfigEntry, error) {
	return nil, fmt.Errorf("not implemented: bot update")
}

func (a *botConfigAdapter) CreateBot(_ context.Context, input map[string]any) (*admin.BotConfigEntry, error) {
	return nil, fmt.Errorf("not implemented: bot create")
}

func (a *botConfigAdapter) DeleteBot(_ context.Context, name string) error {
	return fmt.Errorf("not implemented: bot delete")
}

// --- helpers ---

func (a *botConfigAdapter) extractBotAttrs(cfg *config.Config, platform, name string) *admin.BotConfigAttrs {
	switch platform {
	case "feishu":
		botCfg := resolveFeishuBot(cfg, name)
		if botCfg == nil {
			return nil
		}
		return &admin.BotConfigAttrs{
			WorkerType:     botCfg.WorkerType,
			WorkDir:        botCfg.WorkDir,
			DMPolicy:       botCfg.DMPolicy,
			GroupPolicy:    botCfg.GroupPolicy,
			RequireMention: botCfg.RequireMention,
			AllowFrom:      botCfg.AllowFrom,
			AllowDMFrom:    botCfg.AllowDMFrom,
			AllowGroupFrom: botCfg.AllowGroupFrom,
			STT:            admin.STTAttrs{Provider: botCfg.STTConfig.Provider},
			TTS:            admin.TTSAttrs{Provider: botCfg.TTSConfig.Provider, Voice: botCfg.TTSConfig.Voice},
		}
	case "slack":
		botCfg := resolveSlackBot(cfg, name)
		if botCfg == nil {
			return nil
		}
		return &admin.BotConfigAttrs{
			WorkerType:     botCfg.WorkerType,
			WorkDir:        botCfg.WorkDir,
			DMPolicy:       botCfg.DMPolicy,
			GroupPolicy:    botCfg.GroupPolicy,
			RequireMention: botCfg.RequireMention,
			AllowFrom:      botCfg.AllowFrom,
			AllowDMFrom:    botCfg.AllowDMFrom,
			AllowGroupFrom: botCfg.AllowGroupFrom,
			STT:            admin.STTAttrs{Provider: botCfg.STTConfig.Provider},
			TTS:            admin.TTSAttrs{Provider: botCfg.TTSConfig.Provider, Voice: botCfg.TTSConfig.Voice},
		}
	}
	return nil
}

// resolveFeishuBot finds a FeishuBotConfig by name from the loaded config.
// This is the same helper used in messaging_init.go — extracted here for reuse.
func resolveFeishuBot(cfg *config.Config, name string) *config.FeishuBotConfig {
	for i := range cfg.Messaging.Feishu.Bots {
		if cfg.Messaging.Feishu.Bots[i].Name == name {
			return &cfg.Messaging.Feishu.Bots[i]
		}
	}
	return nil
}

// resolveSlackBot finds a SlackBotConfig by name from the loaded config.
func resolveSlackBot(cfg *config.Config, name string) *config.SlackBotConfig {
	for i := range cfg.Messaging.Slack.Bots {
		if cfg.Messaging.Slack.Bots[i].Name == name {
			return &cfg.Messaging.Slack.Bots[i]
		}
	}
	return nil
}

func (a *botConfigAdapter) getAgentConfigSummary(platform, botID string) *admin.AgentConfigSummary {
	configs, err := agentconfig.Load(a.agentConfigDir, platform, botID)
	if err != nil {
		return nil
	}
	sum := &admin.AgentConfigSummary{}
	files := []struct {
		name   string
		field  *admin.AgentConfigMeta
		getter func() string
	}{
		{"soul", &sum.Soul, func() string { return configs.Soul }},
		{"agents", &sum.Agents, func() string { return configs.Agents }},
		{"skills", &sum.Skills, func() string { return configs.Skills }},
		{"user", &sum.User, func() string { return configs.User }},
		{"memory", &sum.Memory, func() string { return configs.Memory }},
	}
	for _, f := range files {
		content := f.getter()
		if content == "" {
			continue
		}
		fileName := strings.ToUpper(f.name) + ".md"
		source := agentconfig.ResolvedSource(a.agentConfigDir, platform, botID, fileName)
		if source == "" {
			source = "global"
		}
		*f.field = &admin.AgentConfigMeta{Source: source, Size: len(content)}
	}
	return sum
}

func (a *botConfigAdapter) getConfigField(configs *agentconfig.AgentConfigs, file string) string {
	switch admin.AgentConfigFileName(file) {
	case admin.ConfigFileSoul:
		return configs.Soul
	case admin.ConfigFileAgents:
		return configs.Agents
	case admin.ConfigFileSkills:
		return configs.Skills
	case admin.ConfigFileUser:
		return configs.User
	case admin.ConfigFileMemory:
		return configs.Memory
	}
	return ""
}

// atomicWriteYAML writes config data to a YAML file atomically.
func atomicWriteYAML(path string, data any) error {
	content, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal yaml: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "config-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: 编译验证**

Run: `go build ./cmd/hotplex/...`
Expected: BUILD SUCCESS

- [ ] **Step 3: Commit**

```bash
git add cmd/hotplex/bot_config_adapter.go
git commit -m "feat(admin): add botConfigAdapter with read operations"
```

---

### Task 4: 后端 — Bot 配置 Admin Handlers

**Files:**
- Create: `internal/admin/bot_config_handlers.go`

- [ ] **Step 1: 编写 handler**

`internal/admin/bot_config_handlers.go`:

```go
package admin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// HandleListBotConfigs returns all bots with full config details.
// GET /admin/bots/config
func (a *AdminAPI) HandleListBotConfigs(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	if a.botConfig == nil {
		respondJSON(w, []BotConfigEntry{})
		return
	}
	result, err := a.botConfig.ListBotConfigs(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, result)
}

// HandleGetBotConfig returns full config details for a single bot.
// GET /admin/bots/{name}/config
func (a *AdminAPI) HandleGetBotConfig(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "missing bot name", http.StatusBadRequest)
		return
	}
	if a.botConfig == nil {
		http.Error(w, "bot config provider not available", http.StatusServiceUnavailable)
		return
	}
	result, err := a.botConfig.GetBotConfig(r.Context(), name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	respondJSON(w, result)
}

// HandleGetAgentConfigFile reads a specific agent config file.
// GET /admin/bots/{name}/config/{file}
func (a *AdminAPI) HandleGetAgentConfigFile(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	name := r.PathValue("name")
	file := r.PathValue("file")
	if name == "" || file == "" {
		http.Error(w, "missing bot name or file", http.StatusBadRequest)
		return
	}
	if a.botConfig == nil {
		http.Error(w, "bot config provider not available", http.StatusServiceUnavailable)
		return
	}
	result, err := a.botConfig.GetAgentConfigFile(r.Context(), name, file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	respondJSON(w, result)
}

// HandleSystemPromptPreview assembles and returns the full system prompt.
// GET /admin/bots/{name}/preview
func (a *AdminAPI) HandleSystemPromptPreview(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "missing bot name", http.StatusBadRequest)
		return
	}
	if a.botConfig == nil {
		http.Error(w, "bot config provider not available", http.StatusServiceUnavailable)
		return
	}
	result, err := a.botConfig.GetSystemPromptPreview(r.Context(), name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	respondJSON(w, map[string]string{"prompt": result})
}

// HandleUpdateBotConfig updates bot attributes.
// PATCH /admin/bots/{name}
func (a *AdminAPI) HandleUpdateBotConfig(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "missing bot name", http.StatusBadRequest)
		return
	}
	if a.botConfig == nil {
		http.Error(w, "bot config provider not available", http.StatusServiceUnavailable)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	result, err := a.botConfig.UpdateBotConfig(r.Context(), name, body)
	if err != nil {
		http.Error(w, fmt.Sprintf("update bot: %s", err), http.StatusBadRequest)
		return
	}
	a.log.Info("admin: bot config updated", "bot", name, "admin", adminKeyPrefix(r))
	respondJSON(w, result)
}

// HandleCreateBot creates a new bot.
// POST /admin/bots
func (a *AdminAPI) HandleCreateBot(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	if a.botConfig == nil {
		http.Error(w, "bot config provider not available", http.StatusServiceUnavailable)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	result, err := a.botConfig.CreateBot(r.Context(), body)
	if err != nil {
		http.Error(w, fmt.Sprintf("create bot: %s", err), http.StatusBadRequest)
		return
	}
	a.log.Info("admin: bot created", "admin", adminKeyPrefix(r))
	w.WriteHeader(http.StatusCreated)
	respondJSON(w, result)
}

// HandleDeleteBot deletes a bot.
// DELETE /admin/bots/{name}
func (a *AdminAPI) HandleDeleteBot(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "missing bot name", http.StatusBadRequest)
		return
	}
	if a.botConfig == nil {
		http.Error(w, "bot config provider not available", http.StatusServiceUnavailable)
		return
	}
	if err := a.botConfig.DeleteBot(r.Context(), name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.log.Info("admin: bot deleted", "bot", name, "admin", adminKeyPrefix(r))
	w.WriteHeader(http.StatusNoContent)
}

// HandleWriteAgentConfigFile writes content to a specific agent config file.
// PUT /admin/bots/{name}/config/{file}
func (a *AdminAPI) HandleWriteAgentConfigFile(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	name := r.PathValue("name")
	file := r.PathValue("file")
	if name == "" || file == "" {
		http.Error(w, "missing bot name or file", http.StatusBadRequest)
		return
	}
	if a.botConfig == nil {
		http.Error(w, "bot config provider not available", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	result, err := a.botConfig.WriteAgentConfigFile(r.Context(), name, file, body.Content)
	if err != nil {
		http.Error(w, fmt.Sprintf("write config: %s", err), http.StatusBadRequest)
		return
	}
	a.log.Info("admin: agent config written", "bot", name, "file", file, "admin", adminKeyPrefix(r))
	respondJSON(w, result)
}
```

- [ ] **Step 2: 在 AdminAPI struct 添加 botConfig 字段**

在 `internal/admin/admin.go` 的 `AdminAPI` struct 添加：

```go
botConfig BotConfigProvider
```

在 `Deps` struct 添加：

```go
BotConfig BotConfigProvider
```

在 `New()` 函数中添加赋值：

```go
botConfig: deps.BotConfig,
```

- [ ] **Step 3: 在 routes.go 注册新路由**

在 `cmd/hotplex/routes.go` 的 admin 路由注册区域追加：

```go
// Bot config API (enhanced)
adminMux.HandleFunc("GET /admin/bots/config", adminAPI.HandleListBotConfigs)
adminMux.HandleFunc("GET /admin/bots/{name}/config", adminAPI.HandleGetBotConfig)
adminMux.HandleFunc("GET /admin/bots/{name}/config/{file}", adminAPI.HandleGetAgentConfigFile)
adminMux.HandleFunc("GET /admin/bots/{name}/preview", adminAPI.HandleSystemPromptPreview)
adminMux.HandleFunc("PATCH /admin/bots/{name}", adminAPI.HandleUpdateBotConfig)
adminMux.HandleFunc("POST /admin/bots", adminAPI.HandleCreateBot)
adminMux.HandleFunc("DELETE /admin/bots/{name}", adminAPI.HandleDeleteBot)
adminMux.HandleFunc("PUT /admin/bots/{name}/config/{file}", adminAPI.HandleWriteAgentConfigFile)
```

- [ ] **Step 4: 在 routes.go 的 adminAPI 构造处注入 botConfig adapter**

在 `cmd/hotplex/routes.go` 中，adminAPI 构造部分添加 botConfig provider：

```go
// Bot config adapter — resolve agentConfigDir from config
agentConfigDir := cfg.AgentConfig.ConfigDir
if agentConfigDir == "" {
    agentConfigDir = filepath.Join(os.UserHomeDir(), ".hotplex", "agent-configs")
}
botCfgProvider := newBotConfigAdapter(deps.ConfigStore, agentConfigDir, cfgPath)
```

在 `admin.New(admin.Deps{...})` 中添加 `BotConfig: botCfgProvider`。

注意：`cfgPath` 需要从 gateway 启动参数中获取（查看 `gateway_run.go` 中 config 文件路径如何传递）。

- [ ] **Step 5: 编译验证**

Run: `go build ./cmd/hotplex/...`
Expected: BUILD SUCCESS

- [ ] **Step 6: 运行现有测试确认无回归**

Run: `go test ./internal/admin/... -v -short`
Expected: ALL PASS

- [ ] **Step 7: Commit**

```bash
git add internal/admin/bot_config_handlers.go internal/admin/admin.go cmd/hotplex/routes.go cmd/hotplex/bot_config_adapter.go
git commit -m "feat(admin): add bot config CRUD handlers and route registration"
```

---

### Task 5: 前端 — Admin API Client + Auth Hook

**Files:**
- Create: `webchat/lib/api/admin-client.ts`
- Create: `webchat/hooks/use-admin-auth.ts`
- Create: `webchat/lib/types/admin.ts`

- [ ] **Step 1: 定义 admin 类型**

`webchat/lib/types/admin.ts`:

```ts
// --- Auth ---
export interface AdminConnection {
  url: string;
  token: string;
}

// --- Bot ---
export interface BotConfigEntry {
  name: string;
  platform: string;
  bot_id: string;
  status: string;
  connected_at?: string;
  config?: BotConfigAttrs;
  agent_configs?: AgentConfigSummary;
}

export interface BotConfigAttrs {
  worker_type?: string;
  work_dir?: string;
  dm_policy?: string;
  group_policy?: string;
  require_mention?: boolean;
  allow_from?: string[];
  allow_dm_from?: string[];
  allow_group_from?: string[];
  stt?: { provider?: string };
  tts?: { provider?: string; voice?: string };
}

export interface AgentConfigSummary {
  soul?: AgentConfigMeta;
  agents?: AgentConfigMeta;
  skills?: AgentConfigMeta;
  user?: AgentConfigMeta;
  memory?: AgentConfigMeta;
}

export interface AgentConfigMeta {
  source: string;
  size: number;
}

export interface AgentConfigFile {
  content: string;
  source: string;
  size: number;
  file: string;
}

// --- Session ---
export interface SessionInfo {
  id: string;
  user_id: string;
  state: string;
  created_at: string;
  updated_at: string;
  worker_type?: string;
  work_dir?: string;
  title?: string;
  turn_count?: number;
}

// --- Cron ---
export interface CronJob {
  id: string;
  name: string;
  schedule: string;
  message: string;
  bot_id: string;
  owner_id: string;
  enabled: boolean;
  max_runs?: number;
  runs_count?: number;
  next_run_at?: string;
  last_run_at?: string;
  expires_at?: string;
}

// --- Stats ---
export interface GatewayStats {
  uptime_seconds: number;
  total_sessions: number;
  active_sessions: number;
}
```

- [ ] **Step 2: 实现 admin API client**

`webchat/lib/api/admin-client.ts`:

```ts
import type { AdminConnection } from '@/lib/types/admin';

const STORAGE_KEY_URL = 'hotplex_admin_url';
const STORAGE_KEY_TOKEN = 'hotplex_admin_token';

export function getStoredAdminConnection(): AdminConnection | null {
  if (typeof window === 'undefined') return null;
  const url = localStorage.getItem(STORAGE_KEY_URL);
  const token = localStorage.getItem(STORAGE_KEY_TOKEN);
  if (!url || !token) return null;
  return { url, token };
}

export function storeAdminConnection(conn: AdminConnection): void {
  localStorage.setItem(STORAGE_KEY_URL, conn.url);
  localStorage.setItem(STORAGE_KEY_TOKEN, conn.token);
}

export function clearAdminConnection(): void {
  localStorage.removeItem(STORAGE_KEY_URL);
  localStorage.removeItem(STORAGE_KEY_TOKEN);
}

export async function adminFetch<T>(
  path: string,
  options?: RequestInit & { conn?: AdminConnection }
): Promise<T> {
  const conn = options?.conn ?? getStoredAdminConnection();
  if (!conn) throw new Error('Not authenticated');

  const url = `${conn.url}${path}`;
  const res = await fetch(url, {
    ...options,
    headers: {
      'Authorization': `Bearer ${conn.token}`,
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  });

  if (res.status === 401) {
    clearAdminConnection();
    throw new Error('Unauthorized');
  }
  if (!res.ok) {
    const body = await res.text().catch(() => '');
    throw new Error(body || `Admin API error: ${res.status}`);
  }
  if (res.status === 204) return undefined as T;
  return res.json();
}

export async function testConnection(conn: AdminConnection): Promise<boolean> {
  try {
    const res = await fetch(`${conn.url}/admin/health`, {
      headers: { 'Authorization': `Bearer ${conn.token}` },
    });
    return res.ok;
  } catch {
    return false;
  }
}
```

- [ ] **Step 3: 实现 auth hook**

`webchat/hooks/use-admin-auth.ts`:

```ts
'use client';

import { useState, useEffect, useCallback } from 'react';
import {
  getStoredAdminConnection,
  storeAdminConnection,
  clearAdminConnection,
  testConnection,
} from '@/lib/api/admin-client';
import type { AdminConnection } from '@/lib/types/admin';

export type AuthState = 'checking' | 'authenticated' | 'unauthenticated';

export function useAdminAuth() {
  const [state, setState] = useState<AuthState>('checking');
  const [conn, setConn] = useState<AdminConnection | null>(null);

  useEffect(() => {
    const stored = getStoredAdminConnection();
    if (stored) {
      setConn(stored);
      setState('authenticated');
    } else {
      setState('unauthenticated');
    }
  }, []);

  const login = useCallback(async (url: string, token: string) => {
    const candidate: AdminConnection = { url: url.replace(/\/+$/, ''), token };
    const ok = await testConnection(candidate);
    if (!ok) throw new Error('Connection failed: check URL and token');
    storeAdminConnection(candidate);
    setConn(candidate);
    setState('authenticated');
  }, []);

  const logout = useCallback(() => {
    clearAdminConnection();
    setConn(null);
    setState('unauthenticated');
  }, []);

  return { state, conn, login, logout };
}
```

- [ ] **Step 4: 在 lib/config.ts 添加 adminUrl**

在 `webchat/lib/config.ts` 末尾追加：

```ts
export const adminUrl: string =
  process.env.HOTPLEX_WEBCHAT_ADMIN_URL ?? 'http://localhost:9090';
```

- [ ] **Step 5: Commit**

```bash
git add webchat/lib/types/admin.ts webchat/lib/api/admin-client.ts webchat/hooks/use-admin-auth.ts webchat/lib/config.ts
git commit -m "feat(webchat): add admin API client, types, and auth hook"
```

---

### Task 6: 前端 — Admin Shell + 登录页

**Files:**
- Create: `webchat/app/admin/layout.tsx`
- Create: `webchat/app/admin/login/page.tsx`
- Create: `webchat/components/admin/admin-nav.tsx`
- Create: `webchat/components/admin/status-badge.tsx`

- [ ] **Step 1: Admin layout（auth guard + shell）**

`webchat/app/admin/layout.tsx`:

```tsx
'use client';

import { useAdminAuth } from '@/hooks/use-admin-auth';
import { AdminNav } from '@/components/admin/admin-nav';
import { useRouter, usePathname } from 'next/navigation';
import { useEffect } from 'react';

export default function AdminLayout({ children }: { children: React.ReactNode }) {
  const { state } = useAdminAuth();
  const router = useRouter();
  const pathname = usePathname();

  useEffect(() => {
    if (state === 'unauthenticated' && pathname !== '/admin/login') {
      router.replace('/admin/login');
    }
  }, [state, pathname, router]);

  if (state === 'checking') {
    return (
      <div className="flex items-center justify-center h-screen bg-[var(--bg-base)]">
        <div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
      </div>
    );
  }

  if (state === 'unauthenticated' && pathname !== '/admin/login') {
    return null;
  }

  if (pathname === '/admin/login') {
    return <>{children}</>;
  }

  return (
    <div className="flex h-screen bg-[var(--bg-base)]">
      <AdminNav />
      <main className="flex-1 overflow-y-auto">
        {children}
      </main>
    </div>
  );
}
```

- [ ] **Step 2: Admin 导航组件**

`webchat/components/admin/admin-nav.tsx`:

```tsx
'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { useAdminAuth } from '@/hooks/use-admin-auth';

const NAV_ITEMS = [
  { href: '/admin', label: 'Dashboard', icon: 'M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-4 0a1 1 0 01-1-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 01-1 1' },
  { href: '/admin/bots', label: 'Bots', icon: 'M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z' },
  { href: '/admin/sessions', label: 'Sessions', icon: 'M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z' },
  { href: '/admin/cron', label: 'Cron', icon: 'M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z' },
];

export function AdminNav() {
  const pathname = usePathname();
  const { logout } = useAdminAuth();

  return (
    <nav className="w-56 bg-[var(--bg-surface)] border-r border-[var(--border-subtle)] flex flex-col">
      <div className="px-5 py-6">
        <h1 className="text-sm font-display font-bold text-[var(--text-primary)]">HotPlex Admin</h1>
        <p className="text-[10px] font-mono text-[var(--text-faint)] uppercase tracking-widest mt-1">Management</p>
      </div>
      <div className="flex-1 px-3 space-y-1">
        {NAV_ITEMS.map((item) => {
          const active = pathname === item.href || (item.href !== '/admin' && pathname.startsWith(item.href));
          return (
            <Link
              key={item.href}
              href={item.href}
              className={`flex items-center gap-3 px-3 py-2 rounded-[var(--radius-md)] text-xs font-medium transition-colors ${
                active
                  ? 'bg-[var(--bg-active)] text-[var(--accent-gold)]'
                  : 'text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)]'
              }`}
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d={item.icon} />
              </svg>
              {item.label}
            </Link>
          );
        })}
      </div>
      <div className="px-3 py-4 border-t border-[var(--border-subtle)]">
        <button
          onClick={logout}
          className="w-full flex items-center gap-3 px-3 py-2 rounded-[var(--radius-md)] text-xs font-medium text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--accent-coral)] transition-colors"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1" />
          </svg>
          Disconnect
        </button>
      </div>
    </nav>
  );
}
```

- [ ] **Step 3: Status badge 通用组件**

`webchat/components/admin/status-badge.tsx`:

```tsx
'use client';

const STATUS_STYLES: Record<string, string> = {
  running: 'bg-emerald-500/15 text-emerald-400 border-emerald-500/30',
  active: 'bg-emerald-500/15 text-emerald-400 border-emerald-500/30',
  started: 'bg-emerald-500/15 text-emerald-400 border-emerald-500/30',
  stopped: 'bg-zinc-500/15 text-zinc-400 border-zinc-500/30',
  idle: 'bg-amber-500/15 text-amber-400 border-amber-500/30',
  error: 'bg-red-500/15 text-red-400 border-red-500/30',
  terminated: 'bg-zinc-500/15 text-zinc-400 border-zinc-500/30',
  disabled: 'bg-zinc-500/15 text-zinc-400 border-zinc-500/30',
};

export function StatusBadge({ status }: { status: string }) {
  const style = STATUS_STYLES[status.toLowerCase()] ?? 'bg-zinc-500/15 text-zinc-400 border-zinc-500/30';
  return (
    <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wider border ${style}`}>
      <span className="w-1.5 h-1.5 rounded-full bg-current" />
      {status}
    </span>
  );
}
```

- [ ] **Step 4: 登录页**

`webchat/app/admin/login/page.tsx`:

```tsx
'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { useAdminAuth } from '@/hooks/use-admin-auth';
import { adminUrl as defaultUrl } from '@/lib/config';

export default function AdminLoginPage() {
  const [url, setUrl] = useState(defaultUrl);
  const [token, setToken] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const { login } = useAdminAuth();
  const router = useRouter();

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      await login(url, token);
      router.replace('/admin');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Connection failed');
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="flex items-center justify-center min-h-screen bg-[var(--bg-base)]">
      <div className="w-full max-w-md p-8 rounded-2xl bg-[var(--bg-surface)] border border-[var(--border-subtle)]">
        <div className="flex items-center gap-3 mb-8">
          <div className="w-10 h-10 rounded-xl bg-[var(--accent-gold)] flex items-center justify-center">
            <svg className="w-5 h-5 text-black" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" />
            </svg>
          </div>
          <div>
            <h1 className="text-lg font-display font-bold text-[var(--text-primary)]">HotPlex Admin</h1>
            <p className="text-xs text-[var(--text-muted)]">Connect to admin endpoint</p>
          </div>
        </div>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-xs font-medium text-[var(--text-secondary)] mb-1.5">Admin URL</label>
            <input
              type="url"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="http://localhost:9090"
              required
              className="w-full px-4 py-2.5 rounded-xl bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-sm text-[var(--text-primary)] focus:border-[var(--accent-gold)] focus:outline-none transition-colors"
            />
          </div>
          <div>
            <label className="block text-xs font-medium text-[var(--text-secondary)] mb-1.5">Admin Token</label>
            <input
              type="password"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder="Bearer token"
              required
              className="w-full px-4 py-2.5 rounded-xl bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-sm text-[var(--text-primary)] focus:border-[var(--accent-gold)] focus:outline-none transition-colors"
            />
          </div>
          {error && (
            <p className="text-xs text-[var(--accent-coral)]">{error}</p>
          )}
          <button
            type="submit"
            disabled={loading || !url || !token}
            className="w-full py-2.5 rounded-xl bg-[var(--accent-gold)] text-black font-bold text-sm hover:bg-[var(--accent-gold-bright)] disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
          >
            {loading ? 'Connecting...' : 'Connect'}
          </button>
        </form>
      </div>
    </div>
  );
}
```

- [ ] **Step 5: 构建验证前端编译**

Run: `cd webchat && pnpm build`
Expected: BUILD SUCCESS

- [ ] **Step 6: Commit**

```bash
git add webchat/app/admin/ webchat/components/admin/admin-nav.tsx webchat/components/admin/status-badge.tsx
git commit -m "feat(webchat): add admin shell layout, login page, and navigation"
```

---

### Task 7: 前端 — Bot API 接口 + Bot 列表页

**Files:**
- Create: `webchat/lib/api/admin-bots.ts`
- Create: `webchat/lib/api/admin-sessions.ts`
- Create: `webchat/lib/api/admin-cron.ts`
- Create: `webchat/components/admin/bot-card.tsx`
- Create: `webchat/components/admin/metric-card.tsx`
- Create: `webchat/app/admin/bots/page.tsx`

- [ ] **Step 1: Bot API 接口**

`webchat/lib/api/admin-bots.ts`:

```ts
import { adminFetch } from './admin-client';
import type { BotConfigEntry, AgentConfigFile } from '@/lib/types/admin';

export function listBots(): Promise<BotConfigEntry[]> {
  return adminFetch<BotConfigEntry[]>('/admin/bots/config');
}

export function getBot(name: string): Promise<BotConfigEntry> {
  return adminFetch<BotConfigEntry>(`/admin/bots/${encodeURIComponent(name)}/config`);
}

export function getAgentFile(name: string, file: string): Promise<AgentConfigFile> {
  return adminFetch<AgentConfigFile>(`/admin/bots/${encodeURIComponent(name)}/config/${file}`);
}

export function writeAgentFile(name: string, file: string, content: string): Promise<AgentConfigFile> {
  return adminFetch<AgentConfigFile>(`/admin/bots/${encodeURIComponent(name)}/config/${file}`, {
    method: 'PUT',
    body: JSON.stringify({ content }),
  });
}

export function previewSystemPrompt(name: string): Promise<{ prompt: string }> {
  return adminFetch<{ prompt: string }>(`/admin/bots/${encodeURIComponent(name)}/preview`);
}

export function updateBot(name: string, updates: Record<string, unknown>): Promise<BotConfigEntry> {
  return adminFetch<BotConfigEntry>(`/admin/bots/${encodeURIComponent(name)}`, {
    method: 'PATCH',
    body: JSON.stringify(updates),
  });
}

export function createBot(input: Record<string, unknown>): Promise<BotConfigEntry> {
  return adminFetch<BotConfigEntry>('/admin/bots', {
    method: 'POST',
  });
}

export function deleteBot(name: string): Promise<void> {
  return adminFetch<void>(`/admin/bots/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  });
}
```

- [ ] **Step 2: Session API 接口**

`webchat/lib/api/admin-sessions.ts`:

```ts
import { adminFetch } from './admin-client';
import type { SessionInfo } from '@/lib/types/admin';

export function listSessions(limit = 50, offset = 0): Promise<{ sessions: SessionInfo[] }> {
  return adminFetch<{ sessions: SessionInfo[]}>(`/admin/sessions?limit=${limit}&offset=${offset}`);
}

export function getSession(id: string): Promise<SessionInfo> {
  return adminFetch<SessionInfo>(`/admin/sessions/${encodeURIComponent(id)}`);
}

export function terminateSession(id: string): Promise<void> {
  return adminFetch<void>(`/admin/sessions/${encodeURIComponent(id)}/terminate`, {
    method: 'POST',
  });
}

export function deleteSession(id: string): Promise<void> {
  return adminFetch<void>(`/admin/sessions/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
}
```

- [ ] **Step 3: Cron API 接口**

`webchat/lib/api/admin-cron.ts`:

```ts
import { adminFetch } from './admin-client';
import type { CronJob } from '@/lib/types/admin';

export function listCronJobs(): Promise<CronJob[]> {
  return adminFetch<CronJob[]>('/api/cron/jobs');
}

export function getCronJob(id: string): Promise<CronJob> {
  return adminFetch<CronJob>(`/api/cron/jobs/${encodeURIComponent(id)}`);
}

export function createCronJob(input: Record<string, unknown>): Promise<void> {
  return adminFetch<void>('/api/cron/jobs', {
    method: 'POST',
    body: JSON.stringify(input),
  });
}

export function updateCronJob(id: string, updates: Record<string, unknown>): Promise<void> {
  return adminFetch<void>(`/api/cron/jobs/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(updates),
  });
}

export function deleteCronJob(id: string): Promise<void> {
  return adminFetch<void>(`/api/cron/jobs/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
}

export function triggerCronJob(id: string): Promise<void> {
  return adminFetch<void>(`/api/cron/jobs/${encodeURIComponent(id)}/run`, {
    method: 'POST',
  });
}
```

- [ ] **Step 4: Bot 卡片组件**

`webchat/components/admin/bot-card.tsx`:

```tsx
'use client';

import Link from 'next/link';
import type { BotConfigEntry } from '@/lib/types/admin';
import { StatusBadge } from './status-badge';

const PLATFORM_LABELS: Record<string, string> = {
  feishu: 'Feishu',
  slack: 'Slack',
};

export function BotCard({ bot }: { bot: BotConfigEntry }) {
  return (
    <Link
      href={`/admin/bots/${encodeURIComponent(bot.name)}`}
      className="block p-5 rounded-xl bg-[var(--bg-surface)] border border-[var(--border-subtle)] hover:border-[var(--border-default)] hover:bg-[var(--bg-hover)] transition-all"
    >
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2.5">
          <h3 className="text-sm font-display font-bold text-[var(--text-primary)]">{bot.name}</h3>
          <span className="text-[10px] font-mono text-[var(--text-faint)] uppercase px-1.5 py-0.5 rounded bg-[var(--bg-elevated)]">
            {PLATFORM_LABELS[bot.platform] ?? bot.platform}
          </span>
        </div>
        <StatusBadge status={bot.status} />
      </div>
      <div className="flex items-center gap-4 text-[11px] text-[var(--text-muted)]">
        {bot.config?.worker_type && (
          <span className="font-mono">{bot.config.worker_type}</span>
        )}
        {bot.connected_at && (
          <span>Connected {new Date(bot.connected_at).toLocaleString()}</span>
        )}
      </div>
      {bot.agent_configs && (
        <div className="flex gap-2 mt-3">
          {Object.entries(bot.agent_configs).map(([key, meta]) => meta && (
            <span key={key} className="text-[9px] font-mono px-1.5 py-0.5 rounded bg-[var(--bg-elevated)] text-[var(--text-faint)]">
              {key}: {meta.source}
            </span>
          ))}
        </div>
      )}
    </Link>
  );
}
```

- [ ] **Step 5: Metric 卡片组件**

`webchat/components/admin/metric-card.tsx`:

```tsx
'use client';

export function MetricCard({ label, value, sub }: { label: string; value: string | number; sub?: string }) {
  return (
    <div className="p-4 rounded-xl bg-[var(--bg-surface)] border border-[var(--border-subtle)]">
      <p className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1">{label}</p>
      <p className="text-2xl font-display font-bold text-[var(--text-primary)]">{value}</p>
      {sub && <p className="text-[10px] text-[var(--text-muted)] mt-1">{sub}</p>}
    </div>
  );
}
```

- [ ] **Step 6: Bot 列表页**

`webchat/app/admin/bots/page.tsx`:

```tsx
'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { listBots } from '@/lib/api/admin-bots';
import type { BotConfigEntry } from '@/lib/types/admin';
import { BotCard } from '@/components/admin/bot-card';

export default function BotsPage() {
  const [bots, setBots] = useState<BotConfigEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    listBots()
      .then(setBots)
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false));
  }, []);

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-display font-bold text-[var(--text-primary)]">Bots</h1>
          <p className="text-xs text-[var(--text-muted)] mt-1">{bots.length} registered bot(s)</p>
        </div>
        <Link
          href="/admin/bots/new"
          className="px-4 py-2 rounded-xl bg-[var(--accent-gold)] text-black text-xs font-bold hover:bg-[var(--accent-gold-bright)] transition-colors"
        >
          + New Bot
        </Link>
      </div>

      {loading && (
        <div className="flex justify-center py-16">
          <div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
        </div>
      )}

      {error && (
        <p className="text-sm text-[var(--accent-coral)]">{error}</p>
      )}

      {!loading && !error && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          {bots.map((bot) => (
            <BotCard key={bot.name} bot={bot} />
          ))}
          {bots.length === 0 && (
            <p className="text-sm text-[var(--text-muted)] col-span-2 text-center py-16">No bots registered</p>
          )}
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 7: 构建验证**

Run: `cd webchat && pnpm build`
Expected: BUILD SUCCESS

- [ ] **Step 8: Commit**

```bash
git add webchat/lib/api/admin-bots.ts webchat/lib/api/admin-sessions.ts webchat/lib/api/admin-cron.ts webchat/components/admin/bot-card.tsx webchat/components/admin/metric-card.tsx webchat/app/admin/bots/page.tsx
git commit -m "feat(webchat): add admin API interfaces and bot list page"
```

---

### Task 8: 前端 — Bot 详情页（只读 + System Prompt 预览）

**Files:**
- Create: `webchat/app/admin/bots/[name]/page.tsx`
- Create: `webchat/components/admin/bot-config-editor.tsx`
- Create: `webchat/components/admin/system-prompt-preview.tsx`

- [ ] **Step 1: Agent config 编辑器组件**

`webchat/components/admin/bot-config-editor.tsx`:

```tsx
'use client';

import { useState, useEffect } from 'react';
import { getAgentFile, writeAgentFile } from '@/lib/api/admin-bots';
import type { AgentConfigFile } from '@/lib/types/admin';

const CONFIG_FILES = [
  { key: 'soul', label: 'SOUL', desc: 'Bot 人格与身份定义' },
  { key: 'agents', label: 'AGENTS', desc: '行为规则与约束' },
  { key: 'skills', label: 'SKILLS', desc: '技能定义与工具' },
  { key: 'user', label: 'USER', desc: '用户画像与偏好' },
  { key: 'memory', label: 'MEMORY', desc: '持久化记忆上下文' },
] as const;

const MAX_SIZE = 8000;

export function BotConfigEditor({ botName }: { botName: string }) {
  const [activeFile, setActiveFile] = useState<string>('soul');
  const [content, setContent] = useState('');
  const [source, setSource] = useState('');
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [msg, setMsg] = useState('');

  useEffect(() => {
    setLoading(true);
    getAgentFile(botName, activeFile)
      .then((file: AgentConfigFile) => {
        setContent(file.content);
        setSource(file.source);
      })
      .catch(() => {
        setContent('');
        setSource('');
      })
      .finally(() => setLoading(false));
  }, [botName, activeFile]);

  async function handleSave() {
    setSaving(true);
    setMsg('');
    try {
      const result = await writeAgentFile(botName, activeFile, content);
      setSource(result.source);
      setMsg('Saved. Takes effect on next session or /reset.');
    } catch (err) {
      setMsg(err instanceof Error ? err.message : 'Save failed');
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="flex gap-4 h-[600px]">
      <div className="w-48 space-y-1">
        {CONFIG_FILES.map((f) => (
          <button
            key={f.key}
            onClick={() => setActiveFile(f.key)}
            className={`w-full text-left px-3 py-2 rounded-lg text-xs transition-colors ${
              activeFile === f.key
                ? 'bg-[var(--bg-active)] text-[var(--accent-gold)]'
                : 'text-[var(--text-muted)] hover:bg-[var(--bg-hover)]'
            }`}
          >
            <div className="font-bold">{f.label}</div>
            <div className="text-[9px] text-[var(--text-faint)] mt-0.5">{f.desc}</div>
          </button>
        ))}
      </div>
      <div className="flex-1 flex flex-col">
        <div className="flex items-center justify-between mb-2">
          <div className="flex items-center gap-2">
            <span className="text-xs font-bold text-[var(--text-primary)] uppercase">{activeFile}.md</span>
            {source && (
              <span className="text-[9px] font-mono px-1.5 py-0.5 rounded bg-[var(--bg-elevated)] text-[var(--text-faint)]">
                source: {source}
              </span>
            )}
          </div>
          <span className={`text-[10px] font-mono ${content.length > MAX_SIZE ? 'text-[var(--accent-coral)]' : 'text-[var(--text-faint)]'}`}>
            {content.length} / {MAX_SIZE}
          </span>
        </div>
        {loading ? (
          <div className="flex-1 flex items-center justify-center">
            <div className="w-4 h-4 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
          </div>
        ) : (
          <textarea
            value={content}
            onChange={(e) => setContent(e.target.value)}
            className="flex-1 w-full p-4 rounded-xl bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-sm text-[var(--text-primary)] font-mono resize-none focus:border-[var(--accent-gold)] focus:outline-none transition-colors"
            placeholder={`Enter ${activeFile}.md content...`}
          />
        )}
        <div className="flex items-center justify-between mt-3">
          {msg && <p className="text-[11px] text-[var(--text-muted)]">{msg}</p>}
          <div className="flex-1" />
          <button
            onClick={handleSave}
            disabled={saving || content.length > MAX_SIZE}
            className="px-4 py-2 rounded-xl bg-[var(--accent-gold)] text-black text-xs font-bold disabled:opacity-50 hover:bg-[var(--accent-gold-bright)] transition-colors"
          >
            {saving ? 'Saving...' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: System prompt 预览弹窗**

`webchat/components/admin/system-prompt-preview.tsx`:

```tsx
'use client';

import { useState } from 'react';
import { previewSystemPrompt } from '@/lib/api/admin-bots';

export function SystemPromptPreview({ botName }: { botName: string }) {
  const [open, setOpen] = useState(false);
  const [prompt, setPrompt] = useState('');
  const [loading, setLoading] = useState(false);

  async function handlePreview() {
    setLoading(true);
    try {
      const result = await previewSystemPrompt(botName);
      setPrompt(result.prompt);
      setOpen(true);
    } finally {
      setLoading(false);
    }
  }

  if (!open) {
    return (
      <button
        onClick={handlePreview}
        disabled={loading}
        className="px-4 py-2 rounded-xl border border-[var(--border-subtle)] text-xs font-bold text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] transition-colors disabled:opacity-50"
      >
        {loading ? 'Loading...' : 'Preview System Prompt'}
      </button>
    );
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <div className="w-[800px] max-h-[80vh] bg-[var(--bg-surface)] border border-[var(--border-subtle)] rounded-2xl overflow-hidden flex flex-col">
        <div className="flex items-center justify-between px-5 py-4 border-b border-[var(--border-subtle)]">
          <h3 className="text-sm font-display font-bold text-[var(--text-primary)]">System Prompt Preview</h3>
          <button onClick={() => setOpen(false)} className="text-[var(--text-faint)] hover:text-[var(--text-secondary)]">
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>
        <pre className="flex-1 overflow-auto p-5 text-[11px] font-mono text-[var(--text-secondary)] whitespace-pre-wrap">
          {prompt}
        </pre>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Bot 详情页**

`webchat/app/admin/bots/[name]/page.tsx`:

```tsx
'use client';

import { useEffect, useState } from 'react';
import { useParams } from 'next/navigation';
import { getBot } from '@/lib/api/admin-bots';
import type { BotConfigEntry } from '@/lib/types/admin';
import { StatusBadge } from '@/components/admin/status-badge';
import { BotConfigEditor } from '@/components/admin/bot-config-editor';
import { SystemPromptPreview } from '@/components/admin/system-prompt-preview';

type Tab = 'overview' | 'config' | 'access';

export default function BotDetailPage() {
  const params = useParams();
  const name = params.name as string;
  const [bot, setBot] = useState<BotConfigEntry | null>(null);
  const [loading, setLoading] = useState(true);
  const [tab, setTab] = useState<Tab>('overview');

  useEffect(() => {
    getBot(name)
      .then(setBot)
      .catch(() => setBot(null))
      .finally(() => setLoading(false));
  }, [name]);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
      </div>
    );
  }

  if (!bot) {
    return <div className="p-6"><p className="text-sm text-[var(--accent-coral)]">Bot not found</p></div>;
  }

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center gap-3">
          <h1 className="text-xl font-display font-bold text-[var(--text-primary)]">{bot.name}</h1>
          <span className="text-[10px] font-mono uppercase px-2 py-0.5 rounded bg-[var(--bg-elevated)] text-[var(--text-faint)]">{bot.platform}</span>
          <StatusBadge status={bot.status} />
        </div>
        <SystemPromptPreview botName={name} />
      </div>

      <div className="flex gap-1 mb-6 border-b border-[var(--border-subtle)]">
        {(['overview', 'config', 'access'] as Tab[]).map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={`px-4 py-2 text-xs font-bold capitalize transition-colors border-b-2 ${
              tab === t
                ? 'text-[var(--accent-gold)] border-[var(--accent-gold)]'
                : 'text-[var(--text-muted)] border-transparent hover:text-[var(--text-primary)]'
            }`}
          >
            {t}
          </button>
        ))}
      </div>

      {tab === 'overview' && (
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <InfoRow label="Bot ID" value={bot.bot_id} mono />
            <InfoRow label="Worker Type" value={bot.config?.worker_type ?? '-'} />
            <InfoRow label="Work Dir" value={bot.config?.work_dir ?? '-'} mono />
            <InfoRow label="Connected" value={bot.connected_at ? new Date(bot.connected_at).toLocaleString() : '-'} />
          </div>
        </div>
      )}

      {tab === 'config' && <BotConfigEditor botName={name} />}

      {tab === 'access' && (
        <div className="space-y-4">
          <InfoRow label="DM Policy" value={bot.config?.dm_policy ?? '-'} />
          <InfoRow label="Group Policy" value={bot.config?.group_policy ?? '-'} />
          <InfoRow label="Require Mention" value={bot.config?.require_mention ? 'Yes' : 'No'} />
          <InfoRow label="Allow From" value={bot.config?.allow_from?.join(', ') || 'none'} />
          <InfoRow label="Allow DM From" value={bot.config?.allow_dm_from?.join(', ') || 'none'} />
          <InfoRow label="Allow Group From" value={bot.config?.allow_group_from?.join(', ') || 'none'} />
        </div>
      )}
    </div>
  );
}

function InfoRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="px-4 py-3 rounded-xl bg-[var(--bg-surface)] border border-[var(--border-subtle)]">
      <p className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1">{label}</p>
      <p className={`text-sm text-[var(--text-primary)] ${mono ? 'font-mono' : ''} break-all`}>{value}</p>
    </div>
  );
}
```

- [ ] **Step 4: 构建验证**

Run: `cd webchat && pnpm build`
Expected: BUILD SUCCESS

- [ ] **Step 5: Commit**

```bash
git add webchat/app/admin/bots/[name]/ webchat/components/admin/bot-config-editor.tsx webchat/components/admin/system-prompt-preview.tsx
git commit -m "feat(webchat): add bot detail page with config editor and prompt preview"
```

---

### Task 9: 前端 — Dashboard + 完整构建验证

**Files:**
- Create: `webchat/app/admin/page.tsx`

- [ ] **Step 1: Dashboard 总览页**

`webchat/app/admin/page.tsx`:

```tsx
'use client';

import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { listBots } from '@/lib/api/admin-bots';
import { listSessions } from '@/lib/api/admin-sessions';
import { listCronJobs } from '@/lib/api/admin-cron';
import type { BotConfigEntry, SessionInfo, CronJob } from '@/lib/types/admin';
import { MetricCard } from '@/components/admin/metric-card';
import { StatusBadge } from '@/components/admin/status-badge';

export default function AdminDashboard() {
  const [bots, setBots] = useState<BotConfigEntry[]>([]);
  const [sessions, setSessions] = useState<SessionInfo[]>([]);
  const [crons, setCrons] = useState<CronJob[]>([]);
  const [loading, setLoading] = useState(true);
  const router = useRouter();

  useEffect(() => {
    Promise.all([
      listBots().catch(() => []),
      listSessions().then(r => r.sessions).catch(() => []),
      listCronJobs().catch(() => []),
    ]).then(([b, s, c]) => {
      setBots(b);
      setSessions(s);
      setCrons(c);
    }).finally(() => setLoading(false));
  }, []);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
      </div>
    );
  }

  const runningBots = bots.filter(b => b.status === 'running').length;
  const activeSessions = sessions.filter(s => s.state === 'running').length;

  return (
    <div className="p-6">
      <h1 className="text-xl font-display font-bold text-[var(--text-primary)] mb-6">Dashboard</h1>

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
        <MetricCard label="Bots" value={`${runningBots} / ${bots.length}`} sub="running / total" />
        <MetricCard label="Sessions" value={activeSessions} sub={`${sessions.length} total`} />
        <MetricCard label="Cron Jobs" value={crons.length} sub={crons.filter(c => c.enabled).length + ' enabled'} />
        <MetricCard label="Platforms" value={new Set(bots.map(b => b.platform)).size} sub="connected" />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div>
          <div className="flex items-center justify-between mb-3">
            <h2 className="text-sm font-display font-bold text-[var(--text-primary)]">Bots</h2>
            <button onClick={() => router.push('/admin/bots')} className="text-[10px] text-[var(--accent-gold)] font-bold">View all</button>
          </div>
          <div className="space-y-2">
            {bots.slice(0, 5).map(bot => (
              <div key={bot.name} className="flex items-center justify-between px-4 py-3 rounded-xl bg-[var(--bg-surface)] border border-[var(--border-subtle)]">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium text-[var(--text-primary)]">{bot.name}</span>
                  <span className="text-[9px] font-mono uppercase text-[var(--text-faint)]">{bot.platform}</span>
                </div>
                <StatusBadge status={bot.status} />
              </div>
            ))}
          </div>
        </div>

        <div>
          <div className="flex items-center justify-between mb-3">
            <h2 className="text-sm font-display font-bold text-[var(--text-primary)]">Recent Sessions</h2>
            <button onClick={() => router.push('/admin/sessions')} className="text-[10px] text-[var(--accent-gold)] font-bold">View all</button>
          </div>
          <div className="space-y-2">
            {sessions.slice(0, 5).map(s => (
              <div key={s.id} className="flex items-center justify-between px-4 py-3 rounded-xl bg-[var(--bg-surface)] border border-[var(--border-subtle)]">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-mono text-[var(--text-primary)]">{s.id.slice(0, 12)}...</span>
                  <span className="text-[9px] text-[var(--text-faint)]">{s.title ?? s.state}</span>
                </div>
                <StatusBadge status={s.state} />
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: 完整构建验证（前端 + 后端）**

Run: `cd /home/hotplex/.hotplex/workspace/hotplex/webchat && pnpm build`
Expected: BUILD SUCCESS

Run: `cd /home/hotplex/.hotplex/workspace/hotplex && make build`
Expected: BUILD SUCCESS

- [ ] **Step 3: 运行全部后端测试**

Run: `make test-short`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add webchat/app/admin/page.tsx
git commit -m "feat(webchat): add admin dashboard page"
```

---

## Phase 2 — Bot 编辑 + 创建

### Task 10: 后端 — UpdateBotConfig / CreateBot / DeleteBot 实现

**Files:**
- Modify: `cmd/hotplex/bot_config_adapter.go` — 替换 Phase 1 的 stub 实现

- [ ] **Step 1: 实现 UpdateBotConfig**

在 `cmd/hotplex/bot_config_adapter.go` 中替换 `UpdateBotConfig` stub：

```go
func (a *botConfigAdapter) UpdateBotConfig(ctx context.Context, name string, updates map[string]any) (*admin.BotConfigEntry, error) {
	// Validate bot exists
	_, _, ok := a.resolvePlatformAndBotID(name)
	if !ok {
		return nil, fmt.Errorf("bot %q not found", name)
	}

	// Read config.yaml, find bot, merge updates, write back
	cfg := a.cfgStore.Load()
	if err := a.updateBotInConfig(cfg, name, updates); err != nil {
		return nil, err
	}

	return a.GetBotConfig(ctx, name)
}

func (a *botConfigAdapter) updateBotInConfig(cfg *config.Config, name string, updates map[string]any) error {
	// Find the bot in config and apply updates
	// This needs to read config.yaml raw, modify the bots[] entry, and write back
	// For now, we'll implement a YAML-level approach

	raw, err := os.ReadFile(a.configFilePath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var root map[string]any
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	// Find which platform the bot belongs to
	platform := ""
	for _, p := range []string{"feishu", "slack"} {
		msg, ok := root["messaging"].(map[string]any)
		if !ok {
			continue
		}
		pcfg, ok := msg[p].(map[string]any)
		if !ok {
			continue
		}
		bots, ok := pcfg["bots"].([]any)
		if !ok {
			continue
		}
		for _, b := range bots {
			if bm, ok := b.(map[string]any); ok && bm["name"] == name {
				platform = p
				break
			}
		}
		if platform != "" {
			break
		}
	}
	if platform == "" {
		return fmt.Errorf("bot %q not found in config", name)
	}

	// Apply updates to the bot entry
	msg := root["messaging"].(map[string]any)
	pcfg := msg[platform].(map[string]any)
	bots := pcfg["bots"].([]any)
	for i, b := range bots {
		if bm, ok := b.(map[string]any); ok && bm["name"] == name {
			allowed := map[string]bool{
				"worker_type": true, "work_dir": true,
				"dm_policy": true, "group_policy": true,
				"require_mention": true,
				"allow_from": true, "allow_dm_from": true, "allow_group_from": true,
			}
			for k, v := range updates {
				if allowed[k] {
					bm[k] = v
				}
			}
			bots[i] = bm
			break
		}
	}

	return atomicWriteYAML(a.configFilePath, root)
}
```

- [ ] **Step 2: 实现 CreateBot**

```go
func (a *botConfigAdapter) CreateBot(_ context.Context, input map[string]any) (*admin.BotConfigEntry, error) {
	platform, _ := input["platform"].(string)
	name, _ := input["name"].(string)
	if platform == "" || name == "" {
		return nil, fmt.Errorf("platform and name are required")
	}
	if platform != "feishu" && platform != "slack" {
		return nil, fmt.Errorf("unsupported platform: %q", platform)
	}

	// Read raw config
	raw, err := os.ReadFile(a.configFilePath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var root map[string]any
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Check for duplicate name
	msg := root["messaging"].(map[string]any)
	pcfg, _ := msg[platform].(map[string]any)
	if pcfg != nil {
		if bots, ok := pcfg["bots"].([]any); ok {
			for _, b := range bots {
				if bm, ok := b.(map[string]any); ok && bm["name"] == name {
					return nil, fmt.Errorf("bot %q already exists on %s", name, platform)
				}
			}
			if len(bots) >= 10 {
				return nil, fmt.Errorf("maximum 10 bots per platform")
			}
		}
	}

	// Build new bot entry
	newBot := map[string]any{"name": name}
	if v, ok := input["worker_type"].(string); ok {
		newBot["worker_type"] = v
	}
	if v, ok := input["dm_policy"].(string); ok {
		newBot["dm_policy"] = v
	}
	if v, ok := input["group_policy"].(string); ok {
		newBot["group_policy"] = v
	}
	// Platform-specific credentials
	if platform == "feishu" {
		if v, ok := input["app_id"].(string); ok {
			newBot["app_id"] = v
		}
		if v, ok := input["app_secret"].(string); ok {
			newBot["app_secret"] = v
		}
	} else {
		if v, ok := input["bot_token"].(string); ok {
			newBot["bot_token"] = v
		}
		if v, ok := input["app_token"].(string); ok {
			newBot["app_token"] = v
		}
	}

	// Append to bots array
	if pcfg == nil {
		pcfg = map[string]any{}
		msg[platform] = pcfg
	}
	bots, _ := pcfg["bots"].([]any)
	if bots == nil {
		bots = []any{}
	}
	pcfg["bots"] = append(bots, newBot)

	if err := atomicWriteYAML(a.configFilePath, root); err != nil {
		return nil, err
	}

	// Create agent-config directory (use name as placeholder, actual botID resolved after restart)
	if err := agentconfig.EnsureDir(filepath.Join(a.agentConfigDir, platform, name)); err != nil {
		return nil, fmt.Errorf("create agent config dir: %w", err)
	}

	// Return a synthetic entry (bot is not running yet)
	return &admin.BotConfigEntry{
		Name:     name,
		Platform: platform,
		Status:   "stopped",
	}, nil
}
```

- [ ] **Step 3: 实现 DeleteBot**

```go
func (a *botConfigAdapter) DeleteBot(_ context.Context, name string) error {
	// Check running status
	entry, ok := messaging.DefaultBotRegistry().GetByName(name)
	if ok && string(entry.Status) == "running" {
		return fmt.Errorf("bot %q is running, stop it first", name)
	}

	raw, err := os.ReadFile(a.configFilePath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var root map[string]any
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	msg := root["messaging"].(map[string]any)
	for _, p := range []string{"feishu", "slack"} {
		pcfg, ok := msg[p].(map[string]any)
		if !ok {
			continue
		}
		bots, ok := pcfg["bots"].([]any)
		if !ok {
			continue
		}
		for i, b := range bots {
			if bm, ok := b.(map[string]any); ok && bm["name"] == name {
				pcfg["bots"] = append(bots[:i], bots[i+1:]...)
				return atomicWriteYAML(a.configFilePath, root)
			}
		}
	}

	return fmt.Errorf("bot %q not found in config", name)
}
```

- [ ] **Step 4: 编译 + 测试**

Run: `go build ./cmd/hotplex/... && go test ./internal/admin/... -short`
Expected: BUILD SUCCESS, ALL TESTS PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/hotplex/bot_config_adapter.go
git commit -m "feat(admin): implement bot create, update, and delete operations"
```

---

### Task 11: 前端 — 创建新 Bot 页面

**Files:**
- Create: `webchat/app/admin/bots/new/page.tsx`

- [ ] **Step 1: 创建新 bot 表单页**

`webchat/app/admin/bots/new/page.tsx`:

```tsx
'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { createBot } from '@/lib/api/admin-bots';

type Step = 'basic' | 'worker' | 'access' | 'done';

export default function NewBotPage() {
  const router = useRouter();
  const [step, setStep] = useState<Step>('basic');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [form, setForm] = useState({
    platform: 'feishu',
    name: '',
    app_id: '',
    app_secret: '',
    bot_token: '',
    app_token: '',
    worker_type: 'claude_code',
    work_dir: '',
    dm_policy: 'open',
    group_policy: 'disabled',
  });

  function update(partial: Partial<typeof form>) {
    setForm((prev) => ({ ...prev, ...partial }));
  }

  async function handleSubmit() {
    setLoading(true);
    setError('');
    try {
      await createBot(form);
      setStep('done');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Create failed');
    } finally {
      setLoading(false);
    }
  }

  if (step === 'done') {
    return (
      <div className="p-6 max-w-xl mx-auto text-center py-20">
        <div className="w-16 h-16 rounded-2xl bg-[var(--accent-gold)]/15 flex items-center justify-center mx-auto mb-4">
          <svg className="w-8 h-8 text-[var(--accent-gold)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
          </svg>
        </div>
        <h2 className="text-lg font-display font-bold text-[var(--text-primary)] mb-2">Bot Created</h2>
        <p className="text-sm text-[var(--text-muted)] mb-6">
          Restart the gateway for changes to take effect.
        </p>
        <button onClick={() => router.push('/admin/bots')} className="px-6 py-2.5 rounded-xl bg-[var(--accent-gold)] text-black text-sm font-bold">
          Back to Bots
        </button>
      </div>
    );
  }

  return (
    <div className="p-6 max-w-xl mx-auto">
      <h1 className="text-xl font-display font-bold text-[var(--text-primary)] mb-6">Create New Bot</h1>

      <div className="space-y-6">
        {/* Basic Info */}
        <div className="space-y-3">
          <h3 className="text-xs font-bold text-[var(--text-faint)] uppercase tracking-wider">Basic Info</h3>
          <div>
            <label className="block text-xs text-[var(--text-secondary)] mb-1">Platform</label>
            <select value={form.platform} onChange={(e) => update({ platform: e.target.value })} className="w-full px-4 py-2.5 rounded-xl bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-sm text-[var(--text-primary)]">
              <option value="feishu">Feishu</option>
              <option value="slack">Slack</option>
            </select>
          </div>
          <div>
            <label className="block text-xs text-[var(--text-secondary)] mb-1">Name</label>
            <input value={form.name} onChange={(e) => update({ name: e.target.value })} className="w-full px-4 py-2.5 rounded-xl bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-sm text-[var(--text-primary)]" placeholder="my-bot" />
          </div>
          {form.platform === 'feishu' ? (
            <>
              <div>
                <label className="block text-xs text-[var(--text-secondary)] mb-1">App ID</label>
                <input value={form.app_id} onChange={(e) => update({ app_id: e.target.value })} className="w-full px-4 py-2.5 rounded-xl bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-sm text-[var(--text-primary)]" />
              </div>
              <div>
                <label className="block text-xs text-[var(--text-secondary)] mb-1">App Secret</label>
                <input type="password" value={form.app_secret} onChange={(e) => update({ app_secret: e.target.value })} className="w-full px-4 py-2.5 rounded-xl bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-sm text-[var(--text-primary)]" />
              </div>
            </>
          ) : (
            <>
              <div>
                <label className="block text-xs text-[var(--text-secondary)] mb-1">Bot Token</label>
                <input type="password" value={form.bot_token} onChange={(e) => update({ bot_token: e.target.value })} className="w-full px-4 py-2.5 rounded-xl bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-sm text-[var(--text-primary)]" />
              </div>
              <div>
                <label className="block text-xs text-[var(--text-secondary)] mb-1">App Token</label>
                <input type="password" value={form.app_token} onChange={(e) => update({ app_token: e.target.value })} className="w-full px-4 py-2.5 rounded-xl bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-sm text-[var(--text-primary)]" />
              </div>
            </>
          )}
        </div>

        {/* Worker Config */}
        <div className="space-y-3">
          <h3 className="text-xs font-bold text-[var(--text-faint)] uppercase tracking-wider">Worker</h3>
          <div>
            <label className="block text-xs text-[var(--text-secondary)] mb-1">Worker Type</label>
            <select value={form.worker_type} onChange={(e) => update({ worker_type: e.target.value })} className="w-full px-4 py-2.5 rounded-xl bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-sm text-[var(--text-primary)]">
              <option value="claude_code">Claude Code</option>
              <option value="opencode_server">OpenCode Server</option>
            </select>
          </div>
        </div>

        {/* Access Control */}
        <div className="space-y-3">
          <h3 className="text-xs font-bold text-[var(--text-faint)] uppercase tracking-wider">Access Control</h3>
          <div>
            <label className="block text-xs text-[var(--text-secondary)] mb-1">DM Policy</label>
            <select value={form.dm_policy} onChange={(e) => update({ dm_policy: e.target.value })} className="w-full px-4 py-2.5 rounded-xl bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-sm text-[var(--text-primary)]">
              <option value="open">Open</option>
              <option value="allowlist">Allowlist</option>
              <option value="disabled">Disabled</option>
            </select>
          </div>
          <div>
            <label className="block text-xs text-[var(--text-secondary)] mb-1">Group Policy</label>
            <select value={form.group_policy} onChange={(e) => update({ group_policy: e.target.value })} className="w-full px-4 py-2.5 rounded-xl bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-sm text-[var(--text-primary)]">
              <option value="open">Open</option>
              <option value="allowlist">Allowlist</option>
              <option value="disabled">Disabled</option>
            </select>
          </div>
        </div>

        {error && <p className="text-sm text-[var(--accent-coral)]">{error}</p>}

        <div className="flex gap-3">
          <button onClick={() => router.push('/admin/bots')} className="px-4 py-2.5 rounded-xl border border-[var(--border-subtle)] text-xs font-bold text-[var(--text-muted)] hover:bg-[var(--bg-hover)]">
            Cancel
          </button>
          <button onClick={handleSubmit} disabled={loading || !form.name || !form.platform} className="px-6 py-2.5 rounded-xl bg-[var(--accent-gold)] text-black text-xs font-bold disabled:opacity-50">
            {loading ? 'Creating...' : 'Create Bot'}
          </button>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: 构建验证**

Run: `cd webchat && pnpm build`
Expected: BUILD SUCCESS

- [ ] **Step 3: Commit**

```bash
git add webchat/app/admin/bots/new/page.tsx
git commit -m "feat(webchat): add create new bot page"
```

---

## Phase 3 — Session + Cron 管理页面

### Task 12: 前端 — Session 列表 + 详情

**Files:**
- Create: `webchat/app/admin/sessions/page.tsx`
- Create: `webchat/app/admin/sessions/[id]/page.tsx`
- Create: `webchat/components/admin/session-table.tsx`

Session 和 Cron 的后端 API 已完整存在（`/admin/sessions/*` 和 `/api/cron/*`），只需前端页面。

- [ ] **Step 1: Session 表格组件**

`webchat/components/admin/session-table.tsx` — 使用 `adminFetch` + `listSessions` 渲染表格，列：ID、State、Worker、Created、Updated。操作：Terminate 按钮（确认弹窗）。

- [ ] **Step 2: Session 列表页**

`webchat/app/admin/sessions/page.tsx` — 加载 session 列表，渲染 `SessionTable`，支持状态过滤下拉。

- [ ] **Step 3: Session 详情页**

`webchat/app/admin/sessions/[id]/page.tsx` — 展示 session 元信息卡片（ID、state、worker_type、work_dir、创建时间、turn 统计）。

- [ ] **Step 4: 构建验证 + Commit**

```bash
cd webchat && pnpm build
git add webchat/app/admin/sessions/ webchat/components/admin/session-table.tsx
git commit -m "feat(webchat): add session list and detail pages"
```

---

### Task 13: 前端 — Cron 列表 + 详情

**Files:**
- Create: `webchat/app/admin/cron/page.tsx`
- Create: `webchat/app/admin/cron/[id]/page.tsx`
- Create: `webchat/components/admin/cron-table.tsx`

- [ ] **Step 1: Cron 表格组件**

`webchat/components/admin/cron-table.tsx` — 列：Name、Schedule、Bot、Enabled、Next Run。操作：Trigger、Enable/Disable、Delete。

- [ ] **Step 2: Cron 列表页**

`webchat/app/admin/cron/page.tsx` — 加载 cron 列表，渲染 `CronTable`，**+ New Cron** 按钮。

- [ ] **Step 3: Cron 详情页**

`webchat/app/admin/cron/[id]/page.tsx` — 展示 cron 任务配置 + 执行历史。

- [ ] **Step 4: 构建验证 + Commit**

```bash
cd webchat && pnpm build
git add webchat/app/admin/cron/ webchat/components/admin/cron-table.tsx
git commit -m "feat(webchat): add cron list and detail pages"
```

---

## Phase 4 — 集成验证

### Task 14: 完整构建 + 嵌入验证

- [ ] **Step 1: 运行完整 CI 检查**

Run: `make check`
Expected: ALL PASS (fmt + lint + test + build)

- [ ] **Step 2: 嵌入 webchat 到 Go 二进制**

Run: `make webchat-embed && make build`
Expected: BUILD SUCCESS，`internal/webchat/out/` 包含 admin 页面

- [ ] **Step 3: 启动 gateway 实测**

Run: `./hotplex gateway start`，浏览器访问 `http://localhost:8888/admin/login`
Expected: 登录页渲染，输入 admin URL + token 后可进入管理面板

- [ ] **Step 4: 最终 Commit**

```bash
git add -A
git commit -m "feat(webchat): complete agent management admin UI (Phase 1-3)"
```

---

## 自检结果

1. **Spec 覆盖**: 所有 MVP 功能（bot 配置编辑、访问控制、session 管理、cron 管理）在 Phase 1-3 中有对应 task
2. **Placeholder 扫描**: Task 12/13（Session/Cron 页面）保留了结构性描述而非完整代码，因为后端 API 已存在，前端组件模式与 bot 页面一致，执行时可参照 Task 6-8 的模式
3. **类型一致性**: `BotConfigEntry`、`AgentConfigFile` 等类型在 Go (DTO) 和 TS (interface) 之间命名一致
4. **缺失**: `webchat/app/admin/settings/page.tsx`（连接设置页）和 `webchat/lib/config.ts` 的 `adminUrl` 仅在 Task 5 提及，未独立建 task — 合并在 Phase 4 补充

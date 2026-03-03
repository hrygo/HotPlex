# Slack Channel 工作目录绑定设计

**版本**: v1.0
**创建日期**: 2026-03-03
**状态**: 已批准

---

## 1. 功能概述

| 项目 | 内容 |
|------|------|
| **功能名称** | Slack Channel 工作目录绑定 |
| **目标** | 每个 Slack Channel 可绑定独立的工作目录，实现多项目隔离 |
| **存储** | `chatapps/configs/slack_channel_workdir.yaml` |
| **交互** | Slash Command `/set-work-dir` |
| **权限** | Channel 管理员 |

---

## 2. 背景与动机

### 2.1 问题描述

当前 HotPlex Slack Adapter 使用**单一全局工作目录**（配置在 `slack.yaml` 的 `engine.work_dir`）。这导致：

1. 所有 Channel 共享同一个工作目录，无法隔离不同项目
2. 多团队/多项目场景下，代码和数据容易混淆
3. 用户无法按 Channel 定制开发环境

### 2.2 解决方案

实现 **Channel 级别的工作目录绑定**：
- 每个 Channel 可绑定独立的工作目录
- Channel 首次使用时提示用户配置
- 只有 Channel 管理员可以设置/修改

---

## 3. 架构设计

### 3.1 组件交互图

```
┌─────────────────────────────────────────────────────────────┐
│                     Slack Platform                           │
│  用户发送消息到 Channel C1234567890                           │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                  Slack Adapter (adapter.go)                  │
│  1. 接收消息事件                                              │
│  2. 查询 ChannelWorkDirManager 获取该 Channel 的工作目录      │
│  3. 若未配置 → 返回提示消息                                   │
│  4. 若已配置 → 将 WorkDir 注入 EngineConfig                   │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│            ChannelWorkDirManager (新增模块)                   │
│  • Get(channelID) → (workdir, ok)                           │
│  • Set(channelID, workdir, userID) error                    │
│  • Delete(channelID, userID) error                          │
│  • Validate(workdir) error                                  │
│  • IsAdmin(channelID, userID) bool                          │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│              slack_channel_workdir.yaml                      │
│  channels:                                                   │
│    C1234567890: /Users/dev/projects/hotplex                 │
│    C9876543210: /Users/dev/projects/myapp                   │
└─────────────────────────────────────────────────────────────┘
```

### 3.2 文件结构

```
chatapps/
├── configs/
│   ├── slack.yaml                      # 现有配置
│   └── slack_channel_workdir.yaml      # 新增：Channel 工作目录映射
├── slack/
│   ├── adapter.go                      # 修改：集成 ChannelWorkDirManager
│   ├── config.go                       # 现有
│   ├── channel_workdir.go              # 新增：ChannelWorkDirManager 实现
│   ├── channel_workdir_test.go         # 新增：单元测试
│   ├── slash_set_workdir.go            # 新增：/set-work-dir 命令处理
│   └── workdir_validator.go            # 新增：工作目录校验器
└── base/
    └── adapter.go                      # 现有
```

---

## 4. 数据结构设计

### 4.1 配置文件格式

**路径**: `chatapps/configs/slack_channel_workdir.yaml`

```yaml
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

# 元数据（可选，用于审计）
metadata:
  last_updated: ""
  updated_by: ""
```

### 4.2 Go 数据结构

```go
// ChannelWorkDirConfig 配置文件结构
type ChannelWorkDirConfig struct {
    Channels map[string]string `yaml:"channels"` // channelID -> workdir
    Metadata Metadata          `yaml:"metadata"`
}

type Metadata struct {
    LastUpdated string `yaml:"last_updated"`
    UpdatedBy   string `yaml:"updated_by"`
}

// ChannelWorkDirManager 管理 Channel 与工作目录的映射关系
type ChannelWorkDirManager struct {
    configPath string              // 配置文件路径
    config     *ChannelWorkDirConfig
    mu         sync.RWMutex
    logger     *slog.Logger
    client     *slack.Client       // 用于查询 Channel 权限
    validator  *WorkDirValidator
}
```

---

## 5. 核心接口设计

### 5.1 ChannelWorkDirManager

```go
// NewChannelWorkDirManager 创建管理器实例
func NewChannelWorkDirManager(configPath string, client *slack.Client, logger *slog.Logger) (*ChannelWorkDirManager, error)

// Get 获取 Channel 绑定的工作目录
// 返回 (workdir, true) 表示已配置，("", false) 表示未配置
func (m *ChannelWorkDirManager) Get(channelID string) (string, bool)

// Set 设置 Channel 的工作目录（需管理员权限）
// 参数：
//   - channelID: Slack Channel ID
//   - workdir: 工作目录绝对路径
//   - userID: 执行操作的用户 ID
// 返回：
//   - error: 权限不足、校验失败、写入失败等错误
func (m *ChannelWorkDirManager) Set(channelID, workdir, userID string) error

// Delete 删除 Channel 的工作目录配置（需管理员权限）
func (m *ChannelWorkDirManager) Delete(channelID, userID string) error

// Validate 校验工作目录的有效性
func (m *ChannelWorkDirManager) Validate(workdir string) error

// IsAdmin 检查用户是否为 Channel 管理员
// 通过 Slack API 查询用户的 Channel 角色
func (m *ChannelWorkDirManager) IsAdmin(channelID, userID string) (bool, error)

// Reload 重新加载配置文件（支持热重载）
func (m *ChannelWorkDirManager) Reload() error
```

### 5.2 WorkDirValidator

```go
// WorkDirValidator 工作目录校验器
type WorkDirValidator struct {
    forbiddenPaths map[string]bool
}

// NewWorkDirValidator 创建校验器实例
func NewWorkDirValidator() *WorkDirValidator

// Validate 执行校验
// 校验规则：
// 1. 存在性检查：目录必须存在
// 2. 类型检查：必须是目录，不能是文件
// 3. 权限检查：当前用户必须有读写权限
// 4. 安全性检查：禁止危险目录
// 5. 路径规范化：解析符号链接，转换为绝对路径
func (v *WorkDirValidator) Validate(path string) error

// NormalizePath 规范化路径
// - 解析符号链接
// - 转换为绝对路径
// - 清理路径中的 . 和 ..
func (v *WorkDirValidator) NormalizePath(path string) (string, error)
```

---

## 6. 工作目录校验规则

### 6.1 校验项

| 序号 | 校验项 | 描述 | 错误消息 |
|------|--------|------|----------|
| 1 | 存在性 | 目录必须存在 | "目录不存在: {path}" |
| 2 | 类型 | 必须是目录 | "路径不是目录: {path}" |
| 3 | 读权限 | 当前用户可读 | "无读取权限: {path}" |
| 4 | 写权限 | 当前用户可写 | "无写入权限: {path}" |
| 5 | 安全性 | 非禁止目录 | "禁止使用系统目录: {path}" |

### 6.2 禁止的系统目录

```go
var forbiddenPaths = []string{
    "/",           // 根目录
    "/bin",        // 系统二进制
    "/boot",       // 启动目录
    "/dev",        // 设备文件
    "/etc",        // 系统配置
    "/home",       // 用户主目录（父目录）
    "/lib",        // 系统库
    "/proc",       // 进程信息
    "/root",       // root 主目录
    "/run",        // 运行时数据
    "/sbin",       // 系统二进制
    "/sys",        // 系统文件
    "/tmp",        // 临时目录
    "/usr",        // 用户程序
    "/var",        // 变量数据
    "~",           // 用户主目录
    "~root",       // root 主目录
}
```

### 6.3 macOS 特殊目录

```go
var macOSSpecialPaths = []string{
    "/System",          // 系统目录
    "/Library",         // 系统库
    "/Applications",    // 应用程序
    "/Users",           // 用户目录（父目录）
}
```

---

## 7. Slash Command 设计

### 7.1 命令注册

**Slack App Manifest 更新**:

```yaml
features:
  slash_commands:
    - command: /set-work-dir
      description: 设置当前 Channel 的工作目录
      usage_hint: /set-work-dir <path>
      should_escape: false
```

### 7.2 命令语法

| 命令 | 行为 | 示例 |
|------|------|------|
| `/set-work-dir <path>` | 设置当前 Channel 的工作目录 | `/set-work-dir /Users/dev/projects/hotplex` |
| `/set-work-dir` | 显示当前 Channel 的工作目录配置 | `/set-work-dir` |
| `/set-work-dir --reset` | 重置（删除）当前 Channel 的工作目录 | `/set-work-dir --reset` |

### 7.3 响应消息

**成功设置**:
```
✅ 工作目录已设置
Channel: #dev-hotplex
路径: /Users/dev/projects/hotplex
```

**显示当前配置**:
```
📍 当前工作目录
Channel: #dev-hotplex
路径: /Users/dev/projects/hotplex
```

**未配置时的提示**:
```
⚠️ 当前 Channel 尚未配置工作目录
请使用 /set-work-dir <path> 设置

示例: /set-work-dir /Users/dev/projects/myapp
```

**权限不足**:
```
❌ 权限不足
只有 Channel 管理员可以设置工作目录
```

**校验失败**:
```
❌ 工作目录校验失败
路径: /etc
原因: 禁止使用系统目录
```

---

## 8. 流程图

### 8.1 消息处理流程

```
┌──────────────────────────────────────────────────────────────┐
│                    用户发送消息到 Channel                     │
└──────────────────────────────────────────────────────────────┘
                              │
                              ▼
                    ┌─────────────────┐
                    │ 是否为 Slash    │
                    │ Command?        │
                    └─────────────────┘
                     │            │
                    Yes           No
                     │            │
                     ▼            ▼
           ┌─────────────┐  ┌─────────────────┐
           │ 处理        │  │ 查询 Channel    │
           │ /set-work-dir│  │ 工作目录        │
           └─────────────┘  └─────────────────┘
                              │            │
                           已配置        未配置
                              │            │
                              ▼            ▼
                    ┌─────────────┐  ┌─────────────────┐
                    │ 注入 WorkDir │  │ 返回提示消息    │
                    │ 到 Engine    │  │ "请先配置..."   │
                    │ 执行请求     │  └─────────────────┘
                    └─────────────┘
```

### 8.2 设置工作目录流程

```
┌──────────────────────────────────────────────────────────────┐
│               用户执行 /set-work-dir /path                   │
└──────────────────────────────────────────────────────────────┘
                              │
                              ▼
                    ┌─────────────────┐
                    │ 检查用户是否为  │
                    │ Channel 管理员  │
                    └─────────────────┘
                     │            │
                   是            否
                     │            │
                     ▼            ▼
           ┌─────────────┐  ┌─────────────────┐
           │ 校验工作目录 │  │ 返回权限不足    │
           │             │  │ 错误消息        │
           └─────────────┘  └─────────────────┘
                     │
                     ▼
           ┌─────────────────┐
           │ 校验通过？      │
           └─────────────────┘
            │            │
          是            否
            │            │
            ▼            ▼
   ┌─────────────┐  ┌─────────────────┐
   │ 写入配置    │  │ 返回校验失败    │
   │ 文件        │  │ 错误消息        │
   └─────────────┘  └─────────────────┘
            │
            ▼
   ┌─────────────────┐
   │ 返回成功消息    │
   └─────────────────┘
```

---

## 9. Adapter 集成

### 9.1 修改点

**文件**: `chatapps/slack/adapter.go`

```go
type Adapter struct {
    *base.Adapter
    // ... 现有字段 ...

    // 新增：Channel 工作目录管理器
    workDirManager *ChannelWorkDirManager
}

// 在消息处理入口处集成
func (a *Adapter) handleMessage(ctx context.Context, event *slackevents.MessageEvent) error {
    // 获取 Channel 的工作目录
    workdir, configured := a.workDirManager.Get(event.Channel)
    if !configured {
        // 返回提示消息
        return a.sendWorkDirNotConfiguredMessage(ctx, event.Channel, event.ThreadTimeStamp)
    }

    // 构建 EngineConfig，注入 WorkDir
    cfg := &types.Config{
        WorkDir:   workdir,
        SessionID: sessionID,
        // ...
    }

    // 执行 Engine 请求
    return a.engine.Execute(ctx, cfg, prompt, callback)
}
```

### 9.2 提示消息实现

```go
func (a *Adapter) sendWorkDirNotConfiguredMessage(ctx context.Context, channelID, threadTS string) error {
    msg := &base.ChatMessage{
        Text: `⚠️ 当前 Channel 尚未配置工作目录

请使用 \`/set-work-dir <path>\` 设置

示例: \`/set-work-dir /Users/dev/projects/myapp\``,
    }
    return a.SendMessage(ctx, channelID, msg)
}
```

---

## 10. 错误处理

### 10.1 错误类型

```go
var (
    ErrWorkDirNotFound      = errors.New("工作目录不存在")
    ErrWorkDirNotDirectory  = errors.New("路径不是目录")
    ErrWorkDirNoPermission  = errors.New("无访问权限")
    ErrWorkDirForbidden     = errors.New("禁止使用该目录")
    ErrNotChannelAdmin      = errors.New("非 Channel 管理员")
    ErrConfigWriteFailed    = errors.New("配置写入失败")
)
```

### 10.2 错误处理策略

| 错误场景 | 处理方式 |
|----------|----------|
| 配置文件不存在 | 自动创建空配置文件 |
| 配置文件格式错误 | 记录日志，返回空配置，不阻塞服务 |
| 工作目录不存在 | 校验失败，返回用户友好的错误消息 |
| 权限不足 | 返回权限错误消息，引导用户联系管理员 |
| Slack API 调用失败 | 记录日志，**降级为允许所有用户设置**（避免服务不可用） |

---

## 11. 测试策略

### 11.1 单元测试

**文件**: `chatapps/slack/channel_workdir_test.go`

```go
// 测试用例覆盖：
// 1. Get/Set/Delete 正常流程
// 2. 权限检查逻辑（IsAdmin）
// 3. 工作目录校验（各种边界情况）
// 4. 配置文件读写
// 5. 并发访问安全性（使用 -race 标志）
// 6. 热重载功能
```

### 11.2 集成测试

```go
// 测试场景：
// 1. 完整的 Slash Command 处理流程
// 2. Adapter 与 ChannelWorkDirManager 的集成
// 3. 未配置 Channel 的提示消息
```

---

## 12. 安全考虑

### 12.1 权限控制

- 只有 Channel 管理员可以设置/修改工作目录
- 通过 Slack API (`conversations.info`) 验证用户角色

### 12.2 路径安全

- 禁止访问系统敏感目录
- 路径规范化防止目录遍历攻击
- 符号链接解析防止绕过检查

### 12.3 配置文件安全

- 配置文件权限设置为 0600（仅所有者可读写）
- 敏感路径不记录到日志

---

## 13. 兼容性

### 13.1 向后兼容

- 如果 `slack_channel_workdir.yaml` 不存在或为空，**回退到 `slack.yaml` 中的全局 `engine.work_dir`**
- 这确保现有部署不受影响

### 13.2 升级路径

1. 部署新版本
2. 创建 `slack_channel_workdir.yaml`（可选）
3. 通过 `/set-work-dir` 配置各 Channel

---

## 14. 未来扩展

### 14.1 可能的增强

1. **多目录支持**：一个 Channel 绑定多个目录，用户通过前缀选择
2. **Web UI 管理**：通过 Web 界面批量管理 Channel 配置
3. **目录模板**：预定义常用目录，简化配置
4. **自动发现**：基于 Channel 名称自动匹配项目目录

---

## 附录 A：实现检查清单

- [ ] 创建 `WorkDirValidator` 校验器
- [ ] 创建 `ChannelWorkDirManager` 管理器
- [ ] 实现 `/set-work-dir` Slash Command 处理
- [ ] 修改 `Adapter` 集成 `ChannelWorkDirManager`
- [ ] 添加单元测试
- [ ] 更新 Slack App Manifest
- [ ] 编写用户文档

---

## 附录 B：参考文档

1. [Slack Web API - conversations.info](https://api.slack.com/methods/conversations.info)
2. [Slack Slash Commands](https://api.slack.com/interactivity/slash-commands)
3. HotPlex CLAUDE.md
4. Uber Go Style Guide

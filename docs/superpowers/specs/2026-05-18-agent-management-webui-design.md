# Agent (Bot) Management WebUI Design

**日期**: 2026-05-18
**状态**: Draft
**版本**: v1.15.0

---

## 1. 概述

为 HotPlex 多 bot 架构提供可视化管理界面，集成到现有 webchat Next.js SPA 中，通过 admin API 端口进行后端通信。涵盖 bot 配置管理、session 运维监控、cron 任务管理三大功能域。

### 决策记录

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 定位 | Bot 管理 + 运维监控 | 核心管理需求，不含完整 metrics/日志分析 |
| 部署位置 | 集成到 webchat SPA | 共享 Next.js 基建，零额外构建步骤 |
| 持久化方式 | 直接文件操作 | 简单直接，重启生效 |
| 认证方式 | 独立登录页 | 灵活，token + admin URL 用户自行配置 |
| 架构方案 | 扩展 webchat + 扩展 admin API | 关注分离，复用现有 admin auth 体系 |

---

## 2. 整体架构

### 2.1 前端路由

webchat Next.js 项目新增 `app/admin/` 路由组，使用独立 layout：

```
app/
  page.tsx                          # 现有 chat 入口（不变）
  admin/
    layout.tsx                      # Admin shell（侧边栏 + auth guard）
    login/page.tsx                  # 登录页
    page.tsx                        # Dashboard 总览
    bots/
      page.tsx                      # Bot 列表
      new/page.tsx                  # 创建新 bot
      [name]/page.tsx               # Bot 详情/编辑
    sessions/
      page.tsx                      # Session 列表
      [id]/page.tsx                 # Session 详情
    cron/
      page.tsx                      # Cron 任务列表
      [id]/page.tsx                 # Cron 任务详情
    settings/page.tsx               # 连接设置（修改 token/URL）
```

### 2.2 认证流程

```
1. 用户访问 /admin/* → layout.tsx 检查 localStorage: adminToken + adminUrl
2. 无 token → 重定向 /admin/login
3. 登录页：用户输入 admin URL（如 http://host:9090）+ admin token
4. 点击"连接" → GET {adminUrl}/admin/health 验证连通性 + token 有效性
5. 成功 → 存 localStorage → 跳转 /admin
6. 后续所有请求带 Authorization: Bearer {token}，baseURL 为 {adminUrl}
7. 401 响应 → 自动清除 token + 跳转登录页
```

### 2.3 API 通信

```
webchat SPA (:8888)
  ├── Chat 功能 → gateway :8888 WebSocket + /api/sessions/*   [现有，不变]
  └── Admin 功能 → admin :9090 /admin/* + /api/cron/*         [新增]
```

前端新增 `lib/api/admin-client.ts` 封装所有 admin API 调用，自动注入 auth header 和错误处理。

---

## 3. 后端 API 设计

### 3.1 Bot 管理（新增）

| 方法 | 路由 | Scope | 功能 |
|------|------|-------|------|
| GET | `/admin/bots` | `admin:read` | 列出所有 bot（运行时 + 配置 + agent-config 元数据） |
| GET | `/admin/bots/{name}` | `admin:read` | 单个 bot 详情（含完整配置属性） |
| POST | `/admin/bots` | `admin:write` | 创建新 bot |
| PATCH | `/admin/bots/{name}` | `admin:write` | 更新 bot 属性 |
| DELETE | `/admin/bots/{name}` | `admin:write` | 删除 bot |

### 3.2 Agent 配置文件（新增）

| 方法 | 路由 | Scope | 功能 |
|------|------|-------|------|
| GET | `/admin/bots/{name}/config/{file}` | `admin:read` | 读取配置文件（soul/agents/skills/user/memory） |
| PUT | `/admin/bots/{name}/config/{file}` | `admin:write` | 写入配置文件 |
| GET | `/admin/bots/{name}/preview` | `admin:read` | 预览组装后的 system prompt |

### 3.3 已有端点（无需新增）

- **Session**: GET list, GET detail, DELETE, POST terminate, GET stats
- **Cron**: GET list, GET detail, POST create, PATCH update, DELETE, POST trigger, GET history
- **健康/监控**: GET health, GET stats, GET workers, GET logs

### 3.4 响应格式

**Bot 详情响应**：

```json
{
  "name": "hermes",
  "platform": "feishu",
  "bot_id": "ou_da88...",
  "status": "running",
  "connected_at": "2026-05-18T16:10:00+08:00",
  "config": {
    "app_id": "***masked***",
    "worker_type": "claude_code",
    "work_dir": "/home/user/.hotplex/workspace/hotplex",
    "dm_policy": "open",
    "group_policy": "allowlist",
    "require_mention": true,
    "allow_from": [],
    "allow_dm_from": [],
    "allow_group_from": ["ou_xxx"],
    "stt": { "provider": "feishu" },
    "tts": { "provider": "edge", "voice": "zh-CN-YunxiNeural" }
  },
  "agent_configs": {
    "soul": { "source": "bot", "size": 2048 },
    "agents": { "source": "platform", "size": 1024 },
    "skills": { "source": "global", "size": 4096 },
    "user": { "source": "bot", "size": 512 },
    "memory": { "source": "bot", "size": 256 }
  }
}
```

**创建 Bot 请求**：

```json
{
  "platform": "feishu",
  "name": "new-bot",
  "app_id": "cli_xxx",
  "app_secret": "yyy",
  "worker_type": "claude_code",
  "dm_policy": "open",
  "group_policy": "disabled"
}
```

### 3.5 实现位置

| 文件 | 职责 |
|------|------|
| `internal/admin/bot_config_handlers.go` | Bot 配置 CRUD handler |
| `internal/admin/bot_config_provider.go` | 接口定义 + 文件操作适配器 |
| `cmd/hotplex/routes.go` | admin mux 注册新路由 |
| `cmd/hotplex/admin_adapters.go` | provider 接口适配器 |

### 3.6 安全措施

- 凭据字段返回时 mask（`app_secret`、`bot_token` 只显示 `***masked***`）
- 写入前校验 config.yaml 格式 + agent-config 文件大小（8KB 上限）
- 文件名白名单（soul/agents/skills/user/memory），防路径遍历
- 写入操作原子化（临时文件 → rename）

---

## 4. 前端模块设计

### 4.1 布局

Admin 使用独立 layout，和 chat 功能并行：

```
/admin/login     → 全屏登录页（居中卡片：admin URL + token 输入）
/admin/*         → 管理后台 shell（左侧导航 + 右侧内容区）
```

**Admin Shell**：

```
┌─────────────────────────────────────────────┐
│  Header: HotPlex Admin    [连接状态] [设置]  │
├──────┬──────────────────────────────────────┤
│ 导航 │          内容区                       │
│ Bot  │                                      │
│ Sess │                                      │
│ Cron │                                      │
│ Logs │                                      │
└──────┴──────────────────────────────────────┘
```

### 4.2 页面设计

#### Dashboard（`/admin`）

总览卡片：bot 状态汇总、活跃 session 数、cron 任务数 + 下次执行时间、gateway 运行时长。

#### Bot 列表（`/admin/bots`）

卡片列表：每 bot 一张卡片（名称 + platform 图标 + 状态 badge + worker_type + 连接时间）。右上角 **+ New Bot** 按钮。

#### Bot 详情（`/admin/bots/[name]`）

Tab 页结构：
- **Overview**: 运行状态 + 配置属性表格（policy、worker_type、allowlist）
- **Agent Config**: 5 个文件编辑器（SOUL/AGENTS/SKILLS/USER/MEMORY）
  - 左侧文件列表 + 来源标识（bot/platform/global）
  - 右侧 markdown 编辑器（textarea，字符计数 + 8KB 限制）
  - 底部"预览 System Prompt"按钮 → 弹窗展示完整 XML
- **Access Control**: DM/Group policy 下拉 + allowlist 编辑
- **Settings**: worker_type、work_dir、STT/TTS 配置

#### 创建新 Bot（`/admin/bots/new`）

分步表单：基本信息（platform/name/credentials）→ Worker 配置 → 访问控制 → Agent 配置（可选）。创建成功提示"需要重启 gateway 生效"。

#### Session 列表（`/admin/sessions`）

表格：Session ID、Bot、User、状态、创建时间、最后活跃。操作：查看详情、Terminate。按 bot/状态过滤。

#### Session 详情（`/admin/sessions/[id]`）

元信息卡片 + 事件历史时间线 + Turn 统计。

#### Cron 列表（`/admin/cron`）

表格：名称、Schedule、Bot、状态、上次/下次执行。操作：编辑、触发、启用/禁用、删除。**+ New Cron** 按钮。

#### Cron 详情（`/admin/cron/[id]`）

任务配置编辑 + 执行历史列表。

### 4.3 新增前端文件

```
webchat/
  app/admin/
    layout.tsx                          # Admin shell
    login/page.tsx                      # 登录页
    page.tsx                            # Dashboard
    bots/page.tsx                       # Bot 列表
    bots/new/page.tsx                   # 创建新 bot
    bots/[name]/page.tsx                # Bot 详情
    sessions/page.tsx                   # Session 列表
    sessions/[id]/page.tsx              # Session 详情
    cron/page.tsx                       # Cron 列表
    cron/[id]/page.tsx                  # Cron 详情
    settings/page.tsx                   # 连接设置
  lib/api/
    admin-client.ts                     # Admin API client
    admin-bots.ts                       # Bot CRUD 接口
    admin-sessions.ts                   # Session 接口
    admin-cron.ts                       # Cron 接口
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

### 4.4 技术选型

- **状态管理**: React hooks + useState（和现有 webchat 一致）
- **样式**: Tailwind（复用 webchat 主题）
- **表单**: 原生 HTML form + controlled components
- **Markdown 编辑**: textarea + 预览按钮
- **路由**: Next.js App Router（和现有 webchat 一致）

---

## 5. 数据流

### 5.1 Bot 列表读取

```
GET /admin/bots
  1. BotRegistry.ListAll() → 运行时状态 [{name, platform, bot_id, status, connected_at}]
  2. config.yaml → config.Load() → 提取每个 bot 的配置属性（非凭据字段）
  3. agentconfig.Load() → 每个文件的 source 层级 + size（不返回内容）
  4. 合并响应：运行时状态 + 文件配置 + agent-config 元数据
```

### 5.2 Agent Config 文件读取

```
GET /admin/bots/{name}/config/{file}
  1. BotRegistry → 解析 platform + botID
  2. agentconfig.Load(dir, platform, botID) → 返回文件内容 + source 层级
```

### 5.3 System Prompt 预览

```
GET /admin/bots/{name}/preview
  1. BotRegistry → platform + botID
  2. agentconfig.Load(dir, platform, botID) → AgentConfigs
  3. agentconfig.BuildSystemPrompt(configs) → 完整 XML
```

### 5.4 Bot 属性更新

```
PATCH /admin/bots/{name}
  1. 校验字段白名单 + 值合法性
  2. 读取 config.yaml → 定位目标 bot 条目
  3. 合并更新字段（只改请求中的字段）
  4. 原子写入 config.yaml（临时文件 → rename）
  5. 返回更新后配置 + 提示"重启 gateway 后生效"
```

### 5.5 创建新 Bot

```
POST /admin/bots
  1. 校验必填字段 + 名称去重 + 数量上限 (MaxBotsPerPlatform=10)
  2. 读取 config.yaml → 追加到 platform.bots[]
  3. 创建 agent-config 目录 ~/.hotplex/agent-configs/{platform}/{name}/
  4. 原子写入 config.yaml
  5. 返回新 bot 配置 + 提示"需要重启 gateway 生效"
```

### 5.6 Agent Config 文件写入

```
PUT /admin/bots/{name}/config/{file}
  1. 校验文件名白名单 + 内容大小 ≤ 8KB + 路径安全
  2. 解析目标路径 ~/.hotplex/agent-configs/{platform}/{botID}/{FILE}.md
  3. 目录不存在 → os.MkdirAll 创建
  4. 原子写入（临时文件 → rename）
  5. 返回更新后元数据 + 提示"新 session 或 /reset 后生效"
```

### 5.7 删除 Bot

```
DELETE /admin/bots/{name}
  1. 检查运行时状态（running → 拒绝 409）
  2. config.yaml → 移除 platform.bots[] 中的目标条目
  3. 可选：删除 agent-config 目录（需用户确认）
  4. 原子写入 config.yaml
```

### 5.8 错误处理

| 场景 | HTTP 状态 | 处理 |
|------|-----------|------|
| config.yaml 格式损坏 | 500 | 错误详情，建议手动修复 |
| 文件大小超限 | 422 | 提示最大限制 |
| bot 名称重复 | 409 | Conflict |
| bot 数量超限 | 422 | 提示上限 (10) |
| 运行中 bot 删除 | 409 | 提示先停止 |
| 写入失败 | 500 | 原因（权限/磁盘） |
| token 无效 | 401 | 前端自动跳转登录页 |
| admin 端口不可达 | - | 前端登录页提示连接失败 |

### 5.9 并发安全

- config.yaml 写入通过文件锁（`flock`）防止并发冲突
- agent-config 文件各 bot 独立目录，天然无冲突
- 读取始终读磁盘最新状态，不缓存

---

## 6. 构建与部署

### 6.1 构建集成

admin 页面作为 webchat Next.js 项目的一部分，同构建同嵌入，零额外步骤：

```
webchat/pnpm build → webchat/out/ → cp → internal/webchat/out/ → go:embed → 二进制
```

### 6.2 新增环境变量

```env
HOTPLEX_WEBCHAT_ADMIN_URL=http://localhost:9090
```

`lib/config.ts` 新增 `adminUrl` 读取，和现有 `wsUrl`、`apiKey` 模式一致。

---

## 7. 实施分期

### Phase 1 — 基础框架 + Bot 只读

- Admin shell（layout、导航、登录页、auth hook）
- `admin-client.ts` API 通信层
- Bot 列表页 + Bot 详情页（只读：Overview + Agent Config 查看）
- System Prompt 预览
- 后端：GET bot 详情增强（配置属性 + agent-config 元数据）

### Phase 2 — Bot 编辑 + 创建

- Agent Config 编辑器（markdown textarea + 保存）
- Bot 属性编辑（PATCH）
- 访问控制编辑（policy + allowlist）
- 创建新 bot（分步表单）
- 删除 bot
- 后端：POST/PATCH/PUT/DELETE 端点

### Phase 3 — Session + Cron 管理

- Session 列表 + 详情 + Terminate
- Cron 列表 + 详情 + CRUD + 手动触发
- Dashboard 总览页

### Phase 4 — 增强体验

- Logs 查看页面
- Config.yaml 原始编辑模式
- 多 bot 配置对比视图

---

## 8. 测试策略

| 层级 | 方式 | 覆盖 |
|------|------|------|
| 后端 API | `net/http/httptest` + table-driven | CRUD 正常/异常/边界 |
| 文件操作 | `t.TempDir()` | 原子写入、并发写入、大小校验 |
| 前端 | Next.js dev 手动验证 | 页面渲染、表单交互、错误提示 |
| E2E | 构建 + 启动 gateway | 登录→查看→编辑→保存 |

---

## 9. 风险与缓解

| 风险 | 缓解 |
|------|------|
| config.yaml 手动编辑破坏格式 | 写入前 validate + yaml.Marshal 保格式 |
| 并发写入冲突 | 文件锁 + 乐观提示"配置已被修改，请刷新" |
| agent-config 文件编码问题 | 强制 UTF-8，写入前检测 |
| admin 端口不可达 | 登录页实时验证连通性 |
| bot 创建后 botID 未确定 | 创建时用 name 占位目录，运行后由 adapter 解析实际 botID |

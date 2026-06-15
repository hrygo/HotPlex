# WebChat 一等公民化与多租户路线图

**日期**: 2026-06-15
**状态**: Planning（spec ① 已设计，②-⑥ 待逐个 brainstorm）
**分支**: main · **基线版本**: v1.29.0 (fb857af1)
**关联设计**: [`2026-06-15-webchat-multitenancy-foundation-design.md`](./2026-06-15-webchat-multitenancy-foundation-design.md)（spec ①）

---

## 1. 愿景

将 WebChat 从"共享 `webchat_user` 身份的匿名前端"升级为**一等公民**，提供完整的用户级多租户能力：

- 用户级 workspace 隔离（per-user per-bot / per-workspace / per-worker）
- 用户级 worker 选择、workspace 选择、agent-configs 配置
- 用户基于这些资源创建会话，完善的隔离与配额

**多租户形态**：单组织·多终端用户（一个 HotPlex 实例服务一组受信用户，每人独立身份与 workspace，互不可见）。

---

## 2. 子系统分解与依赖

整个愿景拆成 **6 个独立 spec**，有明确依赖顺序。spec ①（地基）已设计完成，②-⑥ 各自走独立的 design → plan → implementation 周期。

```
① 身份 + workspace + 隔离（地基，已设计）   ← 一切的前提
        │
        ├──────────────┐
        ▼              ▼
② per-user/per-ws   ③ 用户级 worker 选择
   配置继承             （配置 fallback 链 + API）
        │              │
        ├──────────────┤
        ▼              ▼
④ OAuth/SSO provider 落地（IdentityProvider 第二实现）
        │
        ▼
⑤ 多租户配额增强（内存维度、计费等）
        │
        ▼
⑥ webchat 前端一等公民化（登录/切换/选择/配置 UI）  ← 集大成
```

**依赖逻辑**：
- ①是地基，所有 per-user 能力都依赖真实的 user_id。
- ②③④ 可在 ① 之后并行推进（互不依赖）。
- ⑤依赖 ①②（配额细化到配置维度需要 ②的配置模型）。
- ⑥是集大成，依赖 ①②③④全部就绪才能提供完整前端体验。

---

## 3. 阶段规划

### 阶段 A：地基（进行中）

| spec | 标题 | 状态 | 文档 |
|---|---|---|---|
| ① | 身份 + workspace + 隔离 | ✅ 设计完成，待实现 | [foundation-design](./2026-06-15-webchat-multitenancy-foundation-design.md) |

阶段 A 交付后：WebChat 后端具备真实用户身份、workspace 实体、会话隔离、多租户配额框架。验收通过 HTTP API 测试（webchat 前端生产登录 UI 归阶段 C）。

### 阶段 B：能力补全（① 之后并行）

| spec | 标题 | 依赖 | 核心改动 |
|---|---|---|---|
| ② | per-user / per-workspace agent-configs 继承 | ① | 改造 `loader.go` fallback 链，新增 per-user / per-workspace 层 |
| ③ | 用户级 worker 选择 | ① | 改造 worker_type fallback 链 + API，填充 `workspaces.worker_preference` |
| ④ | OAuth/SSO provider 落地 | ① | `IdentityProvider` 第二实现（飞书/Slack/OIDC） |

阶段 B 交付后：用户可在 workspace 级定制 agent-configs 与 worker，且可用 OAuth 登录（企业 SSO 体验）。

### 阶段 C：增强与收尾

| spec | 标题 | 依赖 | 核心改动 |
|---|---|---|---|
| ⑤ | 多租户配额增强 | ①② | PoolManager 内存维度细化到 workspace、可选计费/用量统计 |
| ⑥ | webchat 前端一等公民化 | ①②③④ | 登录页、workspace 切换、worker 选择、配置编辑 UI |

阶段 C 交付后：愿景达成——WebChat 完整多租户一等公民体验。

---

## 4. 各 spec 详细规划

### spec ② — per-user / per-workspace agent-configs 继承

**目标**：让同一 bot 下不同用户/不同 workspace 拥有不同的 agent-configs（SOUL/AGENTS/SKILLS/USER/MEMORY 等），而非现状的全局/bot 级共享。

**现状**：三级 fallback 全局 → 平台 → bot（`internal/agentconfig/loader.go:177-216`），完全无 per-user 层。同一 bot 所有用户共享同一套配置。

**关键改动点**：
- `internal/agentconfig/loader.go` `Load` 函数签名：新增 user/workspace 维度参数。
- `resolveFile` 路径解析：新增 per-user / per-workspace 目录层（如 `dir/{platform}/{botName}/{userID}/` 或经 workspace_id）。
- `internal/gateway/bridge_worker.go:328` `injectAgentConfig`：传递 user/workspace 参数。
- 填充 `workspaces.agent_config_overrides`（spec ① 建表留空字段）。
- 遵守 META-COGNITION 约束：B 通道无条件覆盖 C 通道；`META-COGNITION.md` 仍 go:embed 强制注入首位，不可排除。

**验收**：两个用户/两个 workspace 各自加载不同 agent-configs，互不影响；单元测试覆盖新 fallback 层优先级。

**风险**：fallback 层数增加后的路径解析复杂度与性能（需评估缓存）。

### spec ③ — 用户级 worker 选择

**目标**：用户（调用方）能在 workspace 级选择用哪个 worker（claude_code / opencode_server / codex_cli / acp），而非完全由配置决定。

**现状**：用户完全不能选 worker（`cmd/hotplex/messaging_init.go:152-157` 的 fallback 链），webchat API init 的 `worker_type` 参数仅限 API 调用方。

**关键改动点**：
- 填充 `workspaces.worker_preference`（spec ① 建表留空字段）。
- 改造 worker_type fallback 链：`workspace.worker_preference`（新，最高）→ bot 级 → 平台级 → messaging → 编译默认。
- `CreateSession`（spec ① 已改造）消费 `workspace.worker_preference`。
- 前端选择 UI 归 spec ⑥。

**验收**：workspace 设置 worker 偏好后，新会话使用该 worker；未设置走原 fallback；白名单约束（仅 4 类合法 worker）。

**风险**：worker 切换的会话连续性（同一 workspace 切 worker = 不同 session key = 新会话）。

### spec ④ — OAuth/SSO provider 落地

**目标**：员工用已有的工作账号（飞书/Slack/OIDC）一键登录 WebChat，企业 SSO 体验，避免"又一套账号密码"。

**现状**：spec ① 抽象了 `IdentityProvider` 接口，仅 `LocalAccountProvider`（内建账号）一个实现。OAuth 是预留扩展点。

**关键改动点**：
- 新增 `OAuthProvider` 实现 `IdentityProvider`（`internal/security/`）。
- OAuth callback 端点（`/api/auth/oauth/{provider}/callback`）。
- 复用 HotPlex 已配置的飞书/Slack OAuth 凭证（bot 配置），或独立 OIDC 配置。
- 登录页（前端归 spec ⑥）提供 OAuth 入口。

**验收**：飞书/Slack/OIDC 至少一个 provider 端到端登录成功，映射到 users.id。

**风险**：强依赖 bot OAuth 配置（若未配置飞书/Slack 则需独立 OIDC）；多 provider 同一用户的账号合并策略。

### spec ⑤ — 多租户配额增强

**目标**：细化配额到内存维度（per-workspace），可选提供用量统计/计费基础。

**现状**：spec ① 的 PoolManager 三层（全局 + per-user + per-workspace 并发），内存维度不细分到 workspace。

**关键改动点**：
- `internal/session/pool.go`：per-workspace 内存配额层。
- 可选：`metrics/` 新增 per-user/per-workspace 用量 Prometheus 指标。
- 配额配置热重载。

**验收**：workspace 内存超限拒绝新会话；指标可观测。

**风险**：内存估算准确性（现状固定 512MB/worker，`pool.go:55`）。

### spec ⑥ — webchat 前端一等公民化

**目标**：WebChat 前端从"匿名 SPA"升级为完整多租户一等公民 UI。

**现状**：webchat 前端无登录 UI（`webchat/app/page.tsx`），所有访问者共享身份。Admin 面板有独立登录但与聊天功能无关。

**关键改动点**：
- `webchat/app/`：登录页（内建账号 + spec ④ OAuth 入口）。
- workspace 切换 UI（侧边栏/下拉）。
- worker 选择 UI（消费 spec ③ 的 `worker_preference`）。
- agent-configs 编辑 UI（消费 spec ② 的 `agent_config_overrides`）。
- 移除 spec ① 标记的过渡状态（webchat 生产登录断开）。

**验收**：端到端跑通——用户登录 → 看到自己的 workspace → 选择 worker/配置 → 创建隔离会话。

**风险**：前端工作量最大；与后端 API 契约对齐。

---

## 5. 范围排除（明确不做）

- **多组织 SaaS**：不引入"组织/租户"实体（那是真多租户 SaaS 形态）。协作场景暂以"不同用户指向同一 work_dir"解决。
- **workspace 多成员协作**：YAGNI。workspace 私有。
- **公开自助注册**：仅 admin 邀请制。

---

## 6. 路线图级决策（启动各 spec 前拍板）

以下决策不阻塞 spec ①，但应在启动对应 spec 前 brainstorm 时明确：

1. **spec ② 配置覆盖粒度**：per-user 还是 per-workspace，或两层并存？目录结构如何组织？
2. **spec ④ OAuth provider 优先级**：先飞书、先 Slack，还是先通用 OIDC？账号合并策略？
3. **spec ⑤ 是否需要计费**：纯配额，还是含用量统计/计费基础？
4. **spec ⑥ 前端技术栈演进**：继续 Next.js + assistant-ui，还是重构？
5. **全局：是否需要审计日志**：多租户下用户/workspace 操作是否审计？

---

## 7. 推进节奏

- 每个 spec 独立 brainstorm → 设计文档（`docs/specs/`）→ writing-plans → 实现。
- spec ① 实现完成并合入后，启动 spec ②③④（可并行）。
- spec ⑥ 在 ②③④就绪后启动。
- 路线图文档随各 spec 推进更新状态。

**下一步**：审阅 spec ① 设计文档 → 通过后用 writing-plans 生成 spec ① 实现计划。

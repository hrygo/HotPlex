# WebChat 一等公民化与多租户路线图

**日期**: 2026-06-16
**状态**: spec ① 实现完成 Phase 0-7 + review 修复 R4-R7（[PR #746](https://github.com/hrygo/hotplex/pull/746)，P1 阻塞项已修待 re-review）；②-⑥ 待逐个 brainstorm
**分支**: main · **基线版本**: v1.29.0 (fb857af1)
**关联设计**: [`WebChat-Multitenancy-Foundation-Design-Spec.md`](./WebChat-Multitenancy-Foundation-Design-Spec.md)（spec ①）

---

## 1. 愿景

将 WebChat 从"共享 `webchat_user` 身份的匿名前端"升级为**一等公民**，提供完整的用户级多租户能力：

- 用户级 workspace 隔离（per-user per-workspace per-worker；WebChat 会话**不选 Bot**，见 spec ① §2.4）
- 用户级 worker 选择、workspace 选择、agent-configs 配置
- 用户基于这些资源创建会话，完善的隔离与配额

**多租户形态**：单组织·多终端用户（一个 HotPlex 实例服务一组受信用户，每人独立身份与 workspace，互不可见）。

**双轨定位（spec ① §2.1 已确立）**：多租户能力**仅限 WebChat 轨**（团队共享一个实例，用户级隔离）；**Message Channel 轨（Slack/Feishu，个人独享实例）保持现状**，永不引入 User/Workspace/per-user 配置。本路线图所有 spec（①-⑥）都只作用于 WebChat 轨。

---

## 2. 子系统分解与依赖

整个愿景拆成 **6 个独立 spec**，有明确依赖顺序。spec ①（地基）已实现完成（Phase 0-7，PR #746），②-⑥ 各自走独立的 design → plan → implementation 周期。

```
① 身份 + workspace + 隔离（地基，✅ 已实现 PR#746）← 一切的前提
        │
        ├──────────────┐
        ▼              ▼
② per-ws 配置继承     ③ workspace 级 worker 选择
   （团队默认→ws 自定义） （ws.worker_preference + fallback 链）
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
- ①是地基，所有 workspace 级能力都依赖真实的 user_id + workspace_id（§2 以 workspace 为中心的模型）。
- ②③④ 可在 ① 之后并行推进（互不依赖）。
- ⑤依赖 ①②（配额细化到配置维度需要 ②的配置模型）。
- ⑥是集大成，依赖 ①②③④全部就绪才能提供完整前端体验。

---

## 3. 阶段规划

### 阶段 A：地基（spec ① 实现完成，待合入）

| spec | 标题 | 状态 | 文档 |
|---|---|---|---|
| ① | 身份 + workspace + 隔离 | ✅ 实现完成 Phase 0-7 + review 修复 R4-R7（[PR #746](https://github.com/hrygo/hotplex/pull/746)，待 re-review） | [foundation-design](./WebChat-Multitenancy-Foundation-Design-Spec.md) |

阶段 A 已交付（PR #746）：WebChat 后端具备真实用户身份、workspace 实体、会话隔离（ListSessions SQL 级 workspace 过滤 + authorizeSession 二次校验）、多租户配额（PoolManager 全局 + per-user + per-workspace 三层并发）。`make check` 通过。剩余增量（迁移验证测试 / 旧 webchat 会话清理 / e2e 集成测试）作为 spec ① 后续提交，不阻塞阶段 B 启动。

**Review 修复进展（R4-R7，7 轮迭代）**：
- **R4-R6**（`b7f07092` / `62dfd3bd` / `e7f65f7b`）修复 hotplex-ai reviewer 的并发/语义问题：P1 **AttachWorker 读 WorkspaceID data race**（`manager.go:711-726` 将 info 读取纳入 `ms.mu` 作用域，与 `Get` 锁序一致）、P2 配额计数漂移（re-validate 复用 pre-check 的 workspaceID 参与 pool 操作）、P2 SwitchWorkDir 确定性 session key（改用 `DeriveSessionKey`）、P3 clientKey `|` 校验（防 session key 别名化）。
- **R7**（`e61a644c`）修复静态 code review 的安全/质量问题：AdminListUsers **密码哈希泄漏**（`User.PasswordHash` 加 `json:"-"`）、AcceptInvite **用户名枚举**（先 CAS 消费邀请码再建用户，消除持单码无限枚举）、`/api/sessions/*` 错误响应统一 JSON envelope、列表分页（`ListInvitations`/`AdminListUsers` 支持 limit/offset）+ `users.created_at` 索引（迁移 019）、scanUser 时间戳赋值（消除死扫描）、Logout cookie 清理（`CookieAuth.Clear`）、admin 判定去重（`resolveCookieAdmin`）。

PR #746 最新 review（基线 `68b1660`）早于 R6，其 **P1 阻塞项已在 R6 修复**，待 reviewer re-review 确认。剩余 P3（DeleteWorkspace COUNT+DELETE TOCTOU / MarkInvitationUsed 错误语义区分 / migration 018 缓存失效窗口）作为后续迭代，不阻塞合入。

### 阶段 B：能力补全（① 之后并行）

| spec | 标题 | 依赖 | 核心改动 |
|---|---|---|---|
| ② | per-workspace agent-configs 自定义 | ① | 改造 `loader.go`，WebChat 轨走两层（团队默认 → workspace 自定义） |
| ③ | workspace 级 worker 选择 | ① | worker_type fallback 链（WebChat 轨：团队默认 → workspace）+ API，填充 `workspaces.worker_preference` |
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

### spec ② — per-workspace agent-configs 自定义（两层继承）

**目标**：让 WebChat 轨的 agent-configs（SOUL/AGENTS/SKILLS/USER/MEMORY 等）走 spec ① §2.4 确立的**两层继承**（团队默认 → workspace 自定义），而非现状的全局/Bot 级共享。同一团队内不同 workspace 拥有各自定制。

**现状**：Message Channel 轨的三级 fallback 全局 → 平台 → Bot（`internal/agentconfig/loader.go`）——**这条链保持不动**。WebChat 轨需新增独立解析路径。

**关键改动点**：
- `internal/agentconfig/loader.go`：WebChat 轨按 `workspace_id` 解析，两层优先级：workspace 自定义 > 团队默认。**不引入 per-user 层**（spec ① §2.4 决策）。
- `internal/gateway/bridge_worker.go` `injectAgentConfig`：WebChat 会话传 `workspace_id`，Message Channel 会话维持原路径。
- 填充 `workspaces.agent_config_overrides`（spec ① 建表留空字段）。
- 遵守 META-COGNITION 约束：B 通道无条件覆盖 C 通道；`META-COGNITION.md` 仍 go:embed 强制注入首位，不可排除。
- **双轨隔离**：agent-configs 解析必须按 session 是否带 `workspace_id` 分流，绝不污染 Message Channel 的三级链。

**验收**：两个 workspace 各自加载不同 agent-configs，互不影响；Message Channel 轨配置行为零变化；单元测试覆盖两条独立解析路径。

**风险**：fallback 层数增加后的路径解析复杂度与性能（需评估缓存）。

### spec ③ — 用户级 worker 选择

**目标**：调用方（前端/用户）能在 workspace 级选择用哪个 worker（claude_code / opencode_server / codex_cli / acp），WebChat 轨走 spec ① §2.4 的两层（团队默认 → workspace 选择）。

**现状**：Message Channel 轨的 worker_type 由 Bot → 平台 → messaging → 默认 决定（`config_defaults.go`），webchat API init 的 `worker_type` 仅限 API 调用方。WebChat 轨的 `workspaces.worker_preference` 字段已由 spec ① 预留。

**关键改动点**：
- 填充 `workspaces.worker_preference`（spec ① 建表留空字段）。
- WebChat 轨 worker_type fallback：`workspace.worker_preference`（最高）→ 团队默认。**不含 Bot 级**（WebChat 会话不选 Bot，spec ① §2.4）。
- `CreateSession`（spec ① 已改造）消费 `workspace.worker_preference`。
- Message Channel 轨 fallback 链保持不变（双轨隔离）。
- 凭证仍由 worker 二进制自管（spec ① §2.5），本 spec 只选 worker type。
- 前端选择 UI 归 spec ⑥。

**验收**：workspace 设置 worker 偏好后，新会话使用该 worker；未设置走团队默认；白名单约束（仅 4 类合法 worker）；Message Channel 轨行为零变化。

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
- 移除 spec ① §8.4 标记的过渡状态（webchat 生产登录断开）。

**验收**：端到端跑通——用户登录 → 看到自己的 workspace → 选择 worker/配置 → 创建隔离会话。

**风险**：前端工作量最大；与后端 API 契约对齐。

---

## 5. 范围排除（明确不做）

- **Message Channel 多租户化**：永不。Slack/Feishu 轨保持个人独享现状，不引入 User/Workspace/per-user 配置（spec ① §2.1 双轨完全隔离）。
- **多组织 SaaS**：不引入"组织/租户"实体（那是真多租户 SaaS 形态）。跨用户协作暂以"不同 workspace 指向同一 work_dir"解决。
- **workspace 多成员协作**：YAGNI。workspace 私有（仅 owner 可见）。
- **Worker 凭证管理**：永不。凭证由 worker 二进制自管，HotPlex 不存储/注入/代理（spec ① §2.5）。
- **WebChat 选 Bot**：永不。WebChat 会话不关联 Bot（spec ① §2.4），Bot 概念仅 Message Channel 轨用。
- **公开自助注册**：仅 admin 邀请制。

---

## 6. 路线图级决策（启动各 spec 前拍板）

以下决策不阻塞 spec ①，但应在启动对应 spec 前 brainstorm 时明确：

1. **spec ② 配置继承层数**：~~per-user 还是 per-workspace~~ ✅ spec ① §2.4 已拍板——**两层（团队默认 → workspace 自定义），无 per-user 层**。spec ② 只需确定 workspace 自定义的存储形式（DB 字段 `agent_config_overrides` vs 目录文件）。
2. **spec ④ OAuth provider 优先级**：先飞书、先 Slack，还是先通用 OIDC？账号合并策略？
3. **spec ⑤ 是否需要计费**：纯配额，还是含用量统计/计费基础？
4. **spec ⑥ 前端技术栈演进**：继续 Next.js + assistant-ui，还是重构？
5. **全局：是否需要审计日志**：多租户下用户/workspace 操作是否审计？

---

## 7. 推进节奏

- 每个 spec 独立 brainstorm → 设计文档（`docs/specs/`）→ writing-plans → 实现。
- spec ① 已实现完成（Phase 0-7，PR #746），合入后启动 spec ②③④（可并行）。
- spec ⑥ 在 ②③④就绪后启动。
- 路线图文档随各 spec 推进更新状态。

**下一步**：PR #746 re-review 合入（P1 AttachWorker race 已在 R6 修复，R7 完成安全/质量加固）→ 启动 spec ②③④ brainstorm（per-workspace agent-configs / workspace 级 worker 选择 / OAuth SSO，三者互不依赖可并行）。spec ① 剩余增量（review P3：DeleteWorkspace TOCTOU / MarkInvitationUsed 语义 / migration 018 缓存；迁移验证 / e2e）可穿插提交。

# HotPlex 首次初始化体验 P0 优化实施计划

> 范围：基于当前 `main` 分支实测状态，执行首次初始化闭环中最高收益、低风险的一组修复。已有 `docs/specs/Onboard-UX-Improvement-Spec.md` 作为背景参考；本计划只落地本轮确认的 P0 子集。

## 目标

让普通用户在 `hotplex onboard` / `hotplex doctor` 中看到与实际运行配置一致的结果，并在重跑初始化时不丢失用户自定义环境变量；Gateway 启动后提供可机器验证的健康状态。

## 实施步骤

### 1. 统一有效配置边界

- 调查 `internal/cli/checkers` 的现有测试与注册方式。
- 将 Feishu/Slack 凭据诊断从过时的裸环境变量读取改为读取当前 effective config；保留对正式环境变量加载链路的支持。
- 增加配置来源诊断，显示 config 与同目录 `.env` 的安全路径信息，不输出密钥值。
- 用 table-driven 测试覆盖：启用且凭据完整、启用但凭据缺失、平台关闭、multi-bot 配置。

### 2. 保留 `.env` 用户内容

- 抽取或补充环境文件合并逻辑：更新 HotPlex 管理的键，同时保留未知键、注释和空行。
- 覆盖重复运行 onboard、已存在自定义键、已存在同名托管键等场景。
- 保持当前生成文件的可读格式和敏感值不进入诊断输出。

### 3. 增加 Gateway 健康诊断

- 在 runtime 诊断中增加 Gateway `/health` 检查，地址从 effective config 获取。
- Gateway 未启动时给出可操作 warning，不把“尚未启动”误报成配置错误；HTTP 200 才报告通过，其余状态报告失败或警告并包含下一步命令。
- 使用可注入 HTTP client/endpoint 的测试结构，避免测试依赖真实服务。

### 4. 文档与验收

- 更新 Feishu 初始化/doctor 文档，明确诊断读取的是 effective config，并说明 Gateway health 的含义。
- 运行相关 Go 单测、`go test` / 构建与仓库规定的最小质量检查。
- 复查 diff，确认不读取或输出完整凭据、不覆盖现有用户配置、不改动 Skills 默认策略。

## 验收标准

- 当前实例使用正式 `HOTPLEX_MESSAGING_FEISHU_*` 配置时，`hotplex doctor` 不再报告 “Feishu not configured”。
- onboard 重跑后，原 `.env` 中未由 HotPlex 管理的键和注释仍存在。
- Gateway 运行时 doctor 能明确报告 health 200；未运行时给出可操作提示。
- 所有新增行为有自动化测试，专项测试和构建通过。

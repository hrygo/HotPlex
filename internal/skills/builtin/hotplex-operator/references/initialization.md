# 首次初始化

新主机、配置缺失或用户明确请求重新配置时读取本 reference。它描述从可信二进制到已验证 Gateway 的完整交接；每个写操作仍需明确的 operator 授权。

## 目标结果

只有在请求范围内满足以下条件，初始化才算完成：

- 通过预期 `PATH` 找到可信的 `hotplex` 二进制；
- `config.yaml` 和 `.env` 已生成或有意保留；
- 选定 Worker 和请求的消息平台已配置；
- 配置后已运行 `hotplex doctor`，失败项已解决或警告已记录；
- 请求的服务级别已安装、启动并检查，或已明确选择前台/开发启动；
- 已报告版本、Gateway 状态、Worker 证据和剩余风险，且没有暴露秘密。

## 流程

### 1. 确定目标

确认这是全新安装、重新配置、开发运行还是服务部署。变更前检查已安装命令面：

```bash
hotplex version --help
hotplex install --help
hotplex onboard --help
hotplex doctor --help
hotplex service --help
```

记录目标 config 路径、服务级别（`user` 或 `system`）、Worker、平台，以及请求是否包含服务安装、Skill 同步或仅本地配置。不要从当前机器状态推断这些选择。

### 2. 准备二进制

如果当前命令已可信且解析到目标二进制，记录 `hotplex version` 后继续。如果缺失或指向其他二进制，使用选定的官方 artifact 或仓库记录的源码构建路径；只有 operator 授权目标写入时才检查并运行 `hotplex install`。不要发明远程脚本安装器，也不要用未经审核的文件替换已有二进制。

### 3. 执行 onboard

首次主机默认使用交互式设置，除非用户明确要求自动化：

```bash
hotplex onboard
```

自动化时使用 `--non-interactive`，并显式指定请求的平台参数和策略。以下参数分别代表独立且可审查的副作用：

- `--force` 可能覆盖现有配置；
- `--enable-slack` / `--enable-feishu` 会启用平台配置块，并要求有效 `.env` 来源中存在对应凭据；
- `--install-service --service-level user|system` 会安装服务；
- `--sync-skills` 会写入 Worker 可见的内置 Skill projection。

向导还会选择 Worker、创建或保留 AgentConfig 文件、检查 STT/TTS 前置条件，并以受保护权限写入配置。报告缺少凭据或可选依赖警告；不要打印凭据值，也不要自动启用未请求的平台。

### 4. 解读 doctor

`onboard` 后运行完整只读报告：

```bash
hotplex doctor --json
```

按当前报告中的 category 归类每个 `fail` 和 `warn`（`environment`、`config`、`dependencies`、`security`、`runtime`、`messaging`、`stt`、`tts`、`agent_config`、`skills` 或 `worker`）。只解决用户授权范围内的项目，然后重新运行报告。`--fix` 属于变更，需要单独明确授权；配置检查通过不能证明服务或平台连接健康。

### 5. 按请求安装并启动服务

服务安装不等于 Gateway 已运行。如果 `onboard` 没有使用 `--install-service`，先安装；如果已使用，则跳过重复安装并继续检查/启动。所有命令使用同一明确级别：

```bash
hotplex service install --level user
hotplex service start --level user
hotplex service status --level user --json
hotplex service logs --level user --lines 100
```

只有具备所需提升权限时才使用 `--level system`。`onboard --install-service` 后仍检查状态；平台没有自动启动时再启动服务。如果用户选择前台或开发运行，读取当前 `hotplex gateway --help` 并单独验收，不要把服务安装作为隐式回退。

### 6. 验证并交接

至少验证已安装版本、doctor 结果、服务状态（如适用）、最新启动日志、选定 Worker 证据和请求的平台状态。即使服务管理器显示 `active`，日志中有配置、Worker 或消息失败也不能算健康。报告实际执行的检查、观察到的状态、未解决警告和下一步授权动作。

## 失败边界

- 二进制缺失、OS 不支持、Worker 缺失、配置无效或必需凭据缺失都会阻断完成；不要声称主机已就绪。
- 服务启动失败时应检查状态和日志，不要反复重启。
- 用户未请求语音或平台时，STT/TTS 和平台设置可以是可选项；用户请求了这些能力时，对应警告必须纳入结果。
- 保留现有配置和备份。不要删除状态、修改全局 AgentConfig 影响单个 Bot，或在没有新授权时回滚二进制。

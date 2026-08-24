---
title: Config Management Guide
weight: 28
description: Configuration layers, inheritance, hot reload, history rollback, and multi-environment strategies for HotPlex.
---

# Config Management Guide

> 面向企业运维团队的 HotPlex 配置管理指南。涵盖 5 层优先级、配置继承、热重载、版本回滚和多环境策略。

---

## 1. 5 层配置优先级

配置加载遵循 **后者覆盖前者** 原则，共 5 层：

| 优先级 | 来源 | 示例 | 说明 |
|--------|------|------|------|
| 1（最低） | Code Defaults | `Default()` 函数 | 所有字段均有零配置默认值 |
| 2 | 父配置继承 | `inherits: base.yaml` | 递归加载，支持多级 |
| 3 | 配置文件 | `config.yaml` | YAML/JSON/TOML |
| 4 | 环境变量 | `HOTPLEX_POOL_MAX_SIZE=200` | `HOTPLEX_` 前缀 |
| 5（最高） | CLI Flags | `--gateway-addr :9090` | 命令行参数 |

**关键特性**：二进制无需任何配置文件即可运行（Convention over Configuration）。

---

## 2. 配置继承

`inherits` 字段支持配置文件链式继承，实现基础配置共享：

```yaml
# configs/config-base.yaml — 团队共享基线
gateway:
  addr: "0.0.0.0:8888"
pool:
  max_size: 100
  max_idle_per_user: 5

# configs/config-prod.yaml — 生产环境覆盖
inherits: config-base.yaml
pool:
  max_size: 500        # 覆盖基线
security:
  tls_enabled: true    # 生产特有
```

### 环路检测

继承链自动检测循环引用，避免无限递归：

```
config-a.yaml → config-b.yaml → config-a.yaml
# 返回: config: inheritance cycle detected: [a.yaml, b.yaml] → a.yaml
```

### 解析顺序

子文件值覆盖父文件：先加载父配置 → 再用子文件 Viper 实例覆盖 → 最终得到合并结果。

---

## 3. 热重载机制

### 动态字段（立即生效）

以下字段修改后自动生效，无需重启：

| 字段 | 说明 |
|------|------|
| `log.level` | 日志级别 |
| `session.gc_scan_interval` | Session GC 扫描间隔 |
| `pool.max_size` | 全局 Session 上限 |
| `pool.max_idle_per_user` | 每用户 Session 上限 |
| `worker.max_lifetime` / `idle_timeout` / `execution_timeout` | Worker 超时 |
| `worker.auto_retry` | 自动重试策略 |
| `security.api_keys` | API Key 列表 |
| `security.allowed_origins` | CORS 来源 |
| `admin.tokens` / `requests_per_sec` / `burst` | Admin API 控制 |

### 静态字段（需重启）

修改后仅记录日志，下次重启生效：

| 字段 | 原因 |
|------|------|
| `gateway.addr` | 端口绑定 |
| `db.path` | 数据库连接 |
| `tls_enabled` / `tls_cert_file` / `tls_key_file` | TLS 配置 |
| `log.format` | 日志格式 |

### 实现原理

- **fsnotify** 监听配置文件所在目录
- **500ms debounce** 防抖，合并连续写入
- `ConfigStore` 原子交换 + 观察者通知
- 回调并发限制（semaphore=4），防止 goroutine 爆发

---

## 4. 配置历史与回滚

### 版本快照

Watcher 维护最多 **64 个版本**的完整配置快照：

- 每次 reload 生成新快照
- 回滚从内存快照恢复，不重读磁盘
- 支持多步回滚（`version=1` 回退一步）

### 回滚操作

```bash
# 回滚到上一个版本
curl -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://localhost:9999/admin/config/rollback?version=1
```

### 审计日志

每次配置变更记录完整 diff（字段名、旧值、新值、是否热生效），上限 256 条。敏感字段自动脱敏。

---

## 5. 环境变量展开

配置值支持 `${VAR:-default}` 模板语法：

```yaml
worker:
  environment:
    - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY:-}    # 无默认值 → 未设置时排除
    - CUSTOM_VAR=${CUSTOM_VAR:-fallback_value}     # 有默认值 → 未设置时使用默认
```

**Worker 环境条目规则**：
- 引用已设置的变量 → 展开后注入
- 引用未设置变量 + 有 `:-default` → 使用默认值注入
- 引用未设置变量 + 无默认 → 该条目自动排除（不注入空值）

---

## 6. .env 文件加载顺序

Gateway 启动时读取与本次 `config.yaml` 同目录的单个 `.env` 文件；已经存在的 Shell 环境变量
优先于文件中的同名值。`make dev` 由 `scripts/dev.sh` 额外加载仓库根目录 `.env`，而
`.env.local` 不会被 Gateway 自动读取，除非启动脚本或 Shell 显式加载它。

建议实践：
- `.env` 存放当前运行实例需要的凭据和覆盖项，并确保权限为仅用户可读
- 若使用 `.env.local`，必须由启动脚本显式加载，并确认不会与 config 路径混用
- 生产环境使用系统密钥管理（Vault / K8s Secrets）

---

## 7. 工作区根目录（HOTPLEX_HOME）

`HOTPLEX_HOME` 是**唯一一个**控制 HotPlex 全部状态目录的环境变量（v1.41.0+）。设置后，默认配置文件、数据、日志、PID、Agent 人格、skills/phrases、Worker 默认工作目录与 WebChat 工作区沙箱**整体迁移**，避免"配置在 A、数据在 B"的分离。

| 目录 | 默认 | `HOTPLEX_HOME=/data/hotplex` 时 |
|------|------|--------------------------------|
| 默认配置 | `~/.hotplex/config.yaml` | `/data/hotplex/config.yaml` |
| 数据 / 日志 / PID | `~/.hotplex/{data,logs,.pids}/` | `/data/hotplex/{data,logs,.pids}/` |
| Agent 人格 | `~/.hotplex/agent-configs/` | `/data/hotplex/agent-configs/` |
| skills / phrases | `~/.hotplex/{skills,phrases}/` | `/data/hotplex/{skills,phrases}/` |
| Worker 默认工作目录 | `~/.hotplex/workspace` | `/data/hotplex/workspace` |
| WebChat 工作区沙箱 | `~/.hotplex/workspaces/` | `/data/hotplex/workspaces/` |

**优先级关系**：`--config <path>` > `$HOTPLEX_HOME/config.yaml` > `~/.hotplex/config.yaml`。显式传入的 `--config` 始终优先，`HOTPLEX_HOME` 只决定"未指定 `--config` 时的默认配置位置"与其余状态目录。

**生产迁移 SOP**：

```bash
# 1. 停机
hotplex service stop

# 2. 整体迁移（不要遗漏任何子目录）
mv ~/.hotplex /data/hotplex

# 3. 注入环境变量（systemd 用户级服务示例）
systemctl --user edit hotplex
#   [Service]
#   Environment=HOTPLEX_HOME=/data/hotplex

# 4. 启动并验证
hotplex service start
hotplex doctor          # 所有检查 PASS，配置与数据目录一致
curl localhost:8888/health
```

**注意事项**：

- `HOTPLEX_HOME` 不自动搬运旧数据——必须先手动迁移再启动，否则新目录为空、旧数据被忽略（重新 onboard 或从空状态开始）。
- 未设置时行为与旧版完全一致（回退 `~/.hotplex`）。
- 属于"静态"设置：运行中修改不生效，需重启 gateway。
- Docker/K8s 场景可将 `HOTPLEX_HOME` 挂载到持久卷路径（如 `-e HOTPLEX_HOME=/data` + volume 挂载 `/data`）。

---

## 8. 多环境策略

### 目录结构

```
configs/
├── config.yaml              # 默认配置（开发环境）
├── config-staging.yaml      # Staging 覆盖
├── config-prod.yaml         # 生产覆盖
└── env.example              # 环境变量模板
```

### 通过 inherits 共享基线

```yaml
# config-staging.yaml
inherits: config.yaml
pool:
  max_size: 50

# config-prod.yaml
inherits: config.yaml
security:
  tls_enabled: true
pool:
  max_size: 500
  max_memory_per_user: 10737418240  # 10GB
```

### 部署时指定配置

```bash
# systemd
ExecStart=/usr/local/bin/hotplex gateway start --config /etc/hotplex/config-prod.yaml

# Docker
docker run -e HOTPLEX_POOL_MAX_SIZE=500 hotplex/gateway:latest

# 环境变量覆盖
export HOTPLEX_GATEWAY_ADDR=0.0.0.0:8888
export HOTPLEX_SECURITY_TLS_ENABLED=true
```

---

## 9. 配置变更 SOP

1. 在非生产环境验证配置变更
2. 通过热重载应用动态字段变更
3. 检查审计日志确认变更已生效
4. 静态字段变更安排维护窗口重启
5. 变更后验证健康检查和关键业务流程
6. 出现问题时立即执行 `rollback?version=1`

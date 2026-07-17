---
name: hotplex-update
description: HotPlex 二进制更新。完整工作流：构建 → 安装 → 服务重启 → 验证 → 错误处理和回滚机制。支持用户级和系统级服务，跨平台兼容（Linux/macOS/Windows）。
---

# HotPlex 更新与服务重启工作流

## 重启指令选择

- **纯重启（不替换二进制）**：必须使用 `hotplex service restart` 原子指令
- **二进制替换场景**：需要手动 `stop → sleep 2 → cp → start`，因为服务管理器需要时间释放文件句柄

## 前置条件

- 已安装并配置 `hotplex` CLI、`make` 和 `go` 1.26+
- 已安装服务（`hotplex service install`，支持 user/system 级别）
- 对安装目录（默认 `~/.local/bin/`）的写入权限

## 工作流步骤

### 步骤 1：构建新二进制

```bash
make build
```

构建失败：检查编译错误、确认 Go 版本（1.26+）、检查缺少的依赖。

### 步骤 2：验证二进制时间戳

```bash
ls -lh ./bin/hotplex-$(go env GOOS)-$(go env GOARCH) ~/.local/bin/hotplex
```

确认新构建的时间戳比当前运行的版本更新。

### 步骤 3：停止服务并等待

```bash
hotplex service stop
sleep 2    # 重要：服务管理器需要 1-2 秒释放文件句柄
```

服务未运行是正常的（命令幂等）。停止失败：检查 `hotplex service status` 和 `hotplex service logs -n 20`。

### 步骤 4：替换二进制

```bash
cp -f ./bin/hotplex-$(go env GOOS)-$(go env GOARCH) ~/.local/bin/hotplex
```

"Text file busy" → 服务未完全停止，返回步骤 3 等待更长时间。"Permission denied" → 检查 `~/.local/bin/` 写入权限。

### 步骤 5：启动服务

```bash
hotplex service start
```

启动失败：检查 `hotplex service logs`（常见问题：端口 8888/9999 被占用、配置语法错误、运行时错误、缺少依赖）。严重问题考虑回滚。

### 步骤 6：验证服务状态

```bash
hotplex service status
```

确认状态为 `active`，PID 与更新前不同（证明确实重启了）。

### 步骤 7：验证服务健康

```bash
sleep 2 && hotplex service logs | tail -20
```

成功标志：Gateway banner 正确显示、无 panic/error、至少一个适配器连接成功、Gateway 在端口 8888 监听。

### 步骤 8：功能验证（可选）

对安全策略更新：测试 `/cd` 命令。对错误消息改进：测试触发场景。对新功能：按更新内容设计验证用例。

---

## 回滚程序

更新失败时快速恢复：

```bash
# 1. 停止服务
hotplex service stop

# 2. 恢复先前二进制（有备份时）
cp /tmp/hotplex.backup.<timestamp> ~/.local/bin/hotplex

# 或从 Git 历史重新构建
git log --oneline -5
git checkout <previous-commit-hash>
make build
cp -f ./bin/hotplex-$(go env GOOS)-$(go env GOARCH) ~/.local/bin/hotplex

# 3. 重启并验证
hotplex service start
hotplex service status
hotplex service logs | tail -20
```

回滚后：如切换了 git commit，记得切回正确分支；记录问题原因以便修复。

---

## 快速参考

```bash
# 带备份的完整流程（推荐生产环境）
cp ~/.local/bin/hotplex /tmp/hotplex.backup.$(date +%s)
make build && \
hotplex service stop && \
sleep 2 && \
cp -f ./bin/hotplex-$(go env GOOS)-$(go env GOARCH) ~/.local/bin/hotplex && \
hotplex service start && \
sleep 2 && hotplex service logs | tail -20
```

---

## 注意事项

- **停机时间**：通常 3-5 秒，重启期间所有活动会话终止
- **替换前备份**：`cp ~/.local/bin/hotplex /tmp/hotplex.backup.$(date +%s)` — 几秒钟，但节省大量回滚时间
- **跨平台**：所有 `hotplex service` 命令跨平台通用（Linux → systemd，macOS → launchd，Windows → SCM）。不要直接调用 `systemctl`/`launchctl`/`sc.exe`

## 故障排除

Text file busy、服务启动失败、旧版本残留、新功能不工作、服务频繁重启——诊断和解决方法见 `references/troubleshooting.md`。

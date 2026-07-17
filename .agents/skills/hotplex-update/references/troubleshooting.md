# 更新故障排除

## 目录

- [Text file busy](#text-file-busy)
- [更新后服务启动失败](#更新后服务启动失败)
- [更新后旧版本仍在运行](#更新后旧版本仍在运行)
- [服务启动成功但新功能不工作](#服务启动成功但新功能不工作)
- [更新后服务频繁重启](#更新后服务频繁重启)

---

## Text file busy

**症状**：`cp: cannot create regular file '~/.local/bin/hotplex': Text file busy`

**原因**：服务未完全停止，进程仍锁定文件。

**解决**：
```bash
hotplex service status
hotplex service stop
sleep 3
ps aux | grep hotplex
pkill -9 hotplex    # 如仍有残留进程
cp -f ./bin/hotplex-$(go env GOOS)-$(go env GOARCH) ~/.local/bin/hotplex
```

## 更新后服务启动失败

**症状**：状态显示 `failed`。

**可能原因**：新二进制运行时错误、配置格式错误、端口占用、缺少依赖。

**解决**：检查 `hotplex service logs -n 50`，严重问题立即回滚（见 SKILL.md 回滚程序）。

## 更新后旧版本仍在运行

**症状**：状态 active 但行为/版本号未变。

**解决**：
```bash
ls -lh ~/.local/bin/hotplex    # 检查时间戳
# 时间戳旧 → 重新替换（stop → sleep 2 → cp -f → start）
# 时间戳新 → hotplex service restart 强制重启
hotplex service status          # 确认 PID 已改变
```

## 服务启动成功但新功能不工作

**症状**：状态 active、日志无错误，但功能异常。

**解决**：
```bash
hotplex service restart
rm -rf ~/.hotplex/cache/*
hotplex service logs | grep -i "version\|config\|security"
# 仍不行 → 重新 make build 并部署
```

## 更新后服务频繁重启

**症状**：状态在 active/inactive 之间切换，PID 不断变化。

**可能原因**：代码 panic、资源耗尽、健康检查失败。

**解决**：立即 `hotplex service stop` 防止循环，查看 `hotplex service logs -n 200` 找崩溃原因，回滚到稳定版本。

# 飞书适配器生产部署检查清单

**文档版本**: 1.0  
**创建日期**: 2026-03-03  
**适用版本**: HotPlex v0.3.0+  
**关联 Issue**: #134 #143

---

## 📋 部署前检查

### 1. 环境变量验证

- [ ] **FEISHU_APP_ID** 已配置且正确
  ```bash
  echo $FEISHU_APP_ID
  # 预期输出：cli_xxxxxxxxxxxxx
  ```

- [ ] **FEISHU_APP_SECRET** 已配置且正确
  ```bash
  echo $FEISHU_APP_SECRET | head -c 8
  # 预期输出：前 8 个字符（不要完整输出！）
  ```

- [ ] **FEISHU_VERIFICATION_TOKEN** 已配置
  ```bash
  test -n "$FEISHU_VERIFICATION_TOKEN" && echo "OK" || echo "MISSING"
  ```

- [ ] **FEISHU_ENCRYPT_KEY** 已配置
  ```bash
  test -n "$FEISHU_ENCRYPT_KEY" && echo "OK" || echo "MISSING"
  ```

- [ ] **FEISHU_SERVER_ADDR** 已配置（可选，默认 :8082）
  ```bash
  echo ${FEISHU_SERVER_ADDR:-:8082}
  ```

- [ ] 敏感信息已加密存储（不要明文提交到代码库）
  ```bash
  # 使用 secrets 管理工具
  grep -r "FEISHU_APP_SECRET" . --exclude-dir=.git || echo "OK: 未发现明文"
  ```

---

### 2. 飞书开发者后台配置检查

- [ ] 应用已创建且状态为「已启用」
  - 访问：https://open.feishu.cn/app
  - 检查应用状态：✅ 已启用

- [ ] 权限已正确配置
  - [ ] `im:message` - 发送消息
  - [ ] `im:message.receive_v1` - 接收消息
  - [ ] `im:bot` - 机器人配置

- [ ] 事件订阅已启用
  - [ ] 订阅地址可公网访问
  - [ ] 订阅事件：`im.message.receive_v1`
  - [ ] Verification Token 已复制
  - [ ] Encrypt Key 已复制

- [ ] 机器人命令已配置
  - [ ] `/reset` 命令已添加
  - [ ] `/dc` 命令已添加
  - [ ] 命令权限设置为「所有成员」

- [ ] 应用版本已发布
  - [ ] 创建新版本
  - [ ] 提交审核（如需）
  - [ ] 启用应用

---

### 3. 网络/防火墙检查

- [ ] Webhook 端点可公网访问
  ```bash
  # 检查端点可达性
  curl -X POST https://your-domain.com/feishu/events \
    -H "Content-Type: application/json" \
    -d '{"challenge": "test"}'
  
  # 预期响应：{"challenge":"test"}
  ```

- [ ] 防火墙规则已配置
  ```bash
  # 检查端口监听
  netstat -tlnp | grep 8082
  
  # 检查防火墙规则
  sudo ufw status | grep 8082
  ```

- [ ] HTTPS 证书有效
  ```bash
  # 检查 SSL 证书
  openssl s_client -connect your-domain.com:443 -servername your-domain.com | openssl x509 -noout -dates
  
  # 预期输出：证书未过期
  ```

- [ ] 域名 DNS 解析正确
  ```bash
  dig your-domain.com
  # 预期：A 记录指向服务器 IP
  ```

---

### 4. 服务配置检查

- [ ] HotPlex 配置文件正确
  ```yaml
  # config.yaml
  chatapps:
    feishu:
      enabled: true
      app_id: ${FEISHU_APP_ID}
      app_secret: ${FEISHU_APP_SECRET}
      verification_token: ${FEISHU_VERIFICATION_TOKEN}
      encrypt_key: ${FEISHU_ENCRYPT_KEY}
      server_addr: :8082
  ```

- [ ] 日志配置正确
  ```bash
  # 检查日志目录
  ls -la /var/log/hotplex/
  
  # 检查日志轮转
  cat /etc/logrotate.d/hotplex
  ```

- [ ] 监控端点已启用
  ```bash
  # 检查健康检查端点
  curl http://localhost:8080/health
  
  # 预期响应：{"status":"healthy"}
  ```

---

## 🚀 部署流程

### 步骤 1: 构建

```bash
cd /Users/huangzhonghui/.openclaw/workspace/hotplex

# 编译
go build -o hotplex ./cmd/hotplex

# 验证
./hotplex version
```

- [ ] 编译成功
- [ ] 版本信息正确

---

### 步骤 2: 测试

```bash
# 运行单元测试
go test ./chatapps/feishu/... -v

# 运行集成测试（可选）
go test ./chatapps/feishu/... -tags=integration -v

# 运行压力测试（可选，需要真实环境）
go test ./chatapps/feishu/... -tags=pressure -v
```

- [ ] 单元测试通过（24 个测试）
- [ ] 测试覆盖率 > 50%
- [ ] 无编译警告

---

### 步骤 3: 部署

```bash
# 停止旧服务
sudo systemctl stop hotplex

# 备份旧版本
sudo cp /usr/local/bin/hotplex /usr/local/bin/hotplex.bak

# 部署新版本
sudo cp hotplex /usr/local/bin/
sudo chmod +x /usr/local/bin/hotplex

# 启动新服务
sudo systemctl start hotplex

# 检查状态
sudo systemctl status hotplex
```

- [ ] 旧版本已备份
- [ ] 新版本已部署
- [ ] 服务启动成功

---

### 步骤 4: 验证

```bash
# 检查服务状态
sudo systemctl status hotplex

# 检查日志
tail -f /var/log/hotplex/hotplex.log | grep feishu

# 测试消息发送
# 在飞书中向机器人发送消息，检查响应

# 检查健康端点
curl http://localhost:8080/health
```

- [ ] 服务运行正常
- [ ] 日志无错误
- [ ] 消息收发正常
- [ ] 健康检查通过

---

## 📊 监控配置

### 1. 日志监控

- [ ] 错误日志告警已配置
  ```bash
  # 示例：logrotate 配置
  /var/log/hotplex/*.log {
      daily
      rotate 7
      compress
      missingok
      notifempty
  }
  ```

- [ ] 关键错误模式已定义
  - `Authentication failure` - 认证失败
  - `Invalid signature` - 签名验证失败
  - `Token expired` - Token 过期
  - `API error` - API 调用失败

---

### 2. 指标监控

- [ ] Prometheus 指标已暴露
  ```bash
  # 检查指标端点
  curl http://localhost:8080/metrics
  ```

- [ ] 关键指标已定义
  - `hotplex_feishu_messages_total` - 消息总数
  - `hotplex_feishu_errors_total` - 错误总数
  - `hotplex_feishu_latency_seconds` - 延迟分布

---

### 3. 告警规则

- [ ] 告警规则已配置
  ```yaml
  # prometheus_alerts.yaml
  groups:
  - name: feishu_adapter
    rules:
    - alert: FeishuHighErrorRate
      expr: rate(hotplex_feishu_errors_total[5m]) > 0.1
      for: 5m
      annotations:
        summary: "飞书适配器错误率过高"
    
    - alert: FeishuTokenExpiring
      expr: hotplex_feishu_token_expiry_seconds < 3600
      for: 1m
      annotations:
        summary: "飞书 Token 即将过期"
  ```

---

## 🔄 回滚方案

### 场景 1: 新版本故障

```bash
# 停止新服务
sudo systemctl stop hotplex

# 恢复旧版本
sudo cp /usr/local/bin/hotplex.bak /usr/local/bin/hotplex

# 启动旧服务
sudo systemctl start hotplex

# 验证
sudo systemctl status hotplex
```

- [ ] 回滚脚本已测试
- [ ] 备份版本可用

---

### 场景 2: 配置错误

```bash
# 恢复配置文件
sudo cp /etc/hotplex/config.yaml.bak /etc/hotplex/config.yaml

# 重启服务
sudo systemctl restart hotplex
```

- [ ] 配置备份已创建
- [ ] 配置恢复流程已验证

---

## 🚨 应急预案

### 问题 1: Token 过期

**症状**: 日志出现 `app access token invalid` 错误

**处理步骤**:

1. 检查 Token 刷新逻辑
   ```bash
   tail -f /var/log/hotplex/hotplex.log | grep "token refreshed"
   ```

2. 手动刷新 Token（如需）
   ```bash
   # 重启服务触发刷新
   sudo systemctl restart hotplex
   ```

3. 检查 AppID/AppSecret 是否正确
   ```bash
   # 对比飞书开发者后台
   echo $FEISHU_APP_ID
   ```

---

### 问题 2: 服务宕机

**症状**: 服务无响应，健康检查失败

**处理步骤**:

1. 检查服务状态
   ```bash
   sudo systemctl status hotplex
   ```

2. 查看错误日志
   ```bash
   tail -100 /var/log/hotplex/hotplex.log
   ```

3. 重启服务
   ```bash
   sudo systemctl restart hotplex
   ```

4. 如仍失败，回滚到旧版本（见回滚方案）

---

### 问题 3: 消息堆积

**症状**: 用户反馈消息响应延迟

**处理步骤**:

1. 检查队列状态
   ```bash
   curl http://localhost:8080/metrics | grep queue
   ```

2. 检查并发连接数
   ```bash
   netstat -an | grep :8082 | wc -l
   ```

3. 扩容（如需）
   ```bash
   # 增加实例数或提升资源配置
   ```

---

## ✅ 验收标准

部署完成后，验证以下标准：

- [ ] **功能验收**
  - [ ] 用户发送消息，机器人正常响应
  - [ ] `/reset` 命令正常工作
  - [ ] `/dc` 命令正常工作
  - [ ] 卡片交互正常响应

- [ ] **性能验收**
  - [ ] P95 响应时间 < 2 秒
  - [ ] P99 响应时间 < 5 秒
  - [ ] 无消息丢失

- [ ] **稳定性验收**
  - [ ] 服务持续运行 24 小时无故障
  - [ ] 错误率 < 1%
  - [ ] 自动恢复机制正常

- [ ] **监控验收**
  - [ ] 日志正常输出
  - [ ] 指标正常采集
  - [ ] 告警正常触发（测试）

---

## 📝 部署记录

| 日期 | 版本 | 操作人 | 结果 | 备注 |
|------|------|--------|------|------|
| 2026-03-03 | v0.3.0 | | ⏳ 待部署 | Phase 3 首次部署 |

---

## 🔗 参考文档

- [飞书开放平台](https://open.feishu.cn/)
- [HotPlex 架构设计](architecture.md)
- [生产部署指南](production-guide.md)
- [飞书适配器文档](../chatapps/feishu/README.md)

---

*维护者*: HotPlex Team  
*最后更新*: 2026-03-03

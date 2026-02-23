# DingTalk Hook 示例

本示例展示如何在 HotPlex 中集成钉钉机器人进行事件通知。

## 配置步骤

### 1. 获取钉钉 Webhook

1. 打开钉钉群 → 设置 → 智能群助手
2. 添加机器人 → 自定义机器人
3. 设置机器人名称
4. 开启**签名校验**（推荐）
5. 复制 Webhook 地址和 Secret

### 2. 配置环境变量

复制 `.env.example` 为 `.env` 并配置：

```bash
# 钉钉机器人 Webhook
HOTPLEX_DINGTALK_WEBHOOK_URL=https://oapi.dingtalk.com/robot/send?access_token=YOUR_TOKEN

# 签名密钥（如果开启了签名校验）
HOTPLEX_DINGTALK_SECRET=SECxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

# 过滤事件类型
HOTPLEX_DINGTALK_FILTER_EVENTS=danger.blocked,session.error
```

### 3. 运行示例

```bash
# 设置环境变量
export HOTPLEX_DINGTALK_WEBHOOK_URL="https://oapi.dingtalk.com/robot/send?access_token=YOUR_TOKEN"
export HOTPLEX_DINGTALK_SECRET="SECxxxxxxxx"

# 运行
go run _examples/dingtalk_hook/main.go
```

## 事件类型

| 事件 | 说明 | 钉钉显示 |
|------|------|---------|
| `session.start` | 会话开始 | 🚀 |
| `session.end` | 会话结束 | ✅ |
| `session.error` | 会话错误 | ⚠️ |
| `danger.blocked` | 危险命令被拦截 | 🚨 |

## 消息预览

```
🚨 【danger.blocked】
会话: test-session-001
命名空间: production
错误: 检测到危险命令: rm -rf /
时间: 2026-02-23 12:53:56
```

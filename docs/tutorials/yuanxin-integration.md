---
title: 原心 (Yuanxin) 集成教程
weight: 3
description: 一步步将 HotPlex Gateway 接入原心平台，通过 Pulsar 消息队列实现 AI 对话
---

# 原心集成教程

本教程指导你完成 HotPlex Gateway 与原心平台的集成，通过 Pulsar 消息队列实现双向 AI 对话。

## 前置条件

- HotPlex 已安装（`hotplex version` 可执行）
- Pulsar 集群可用（已有 tenant 和 namespace）
- 已在原心平台注册应用并获取 App ID
- 已配置 Worker（Claude Code 或 OpenCode Server 可用）

---

## 1. 配置 Pulsar

原心平台通过 Apache Pulsar 消息队列与 HotPlex Gateway 通信。你需要准备以下 Pulsar 资源：

### 1.1 Pulsar 连接信息

| 参数 | 说明 | 示例 |
|------|------|------|
| Pulsar URL | Broker 服务地址 | `pulsar://pulsar.example.com:6650` |
| Tenant | Pulsar 租户名称 | `public` |
| Namespace | 命名空间 | `default` |

### 1.2 Topic 规划

HotPlex Gateway 会自动在指定 namespace 下创建和使用以下 Topic：

- 接收消息 Topic：Gateway 从此 Topic 消费原心平台发送的用户消息
- 发送消息 Topic：Gateway 将 AI 响应发布到此 Topic，由原心平台消费

确保 Pulsar 集群的 Tenant/Namespace 已创建，且 Gateway 使用的凭证具有生产和消费权限。

**验证**：使用 Pulsar 客户端工具连接集群，确认 Tenant 和 Namespace 存在且可访问。

---

## 2. 创建原心应用

### 2.1 注册应用

1. 登录原心平台管理后台
2. 进入「应用管理」→「创建应用」
3. 填写应用名称（如 `HotPlex Bot`）和描述
4. 记录生成的 **App ID**

### 2.2 配置消息路由

在原心平台中配置消息路由规则，将用户消息转发到 Pulsar Topic，并指定 HotPlex 响应消息的回传 Topic。

**验证**：应用创建成功后，在应用列表中可以看到对应的 App ID。

---

## 3. 配置 HotPlex

编辑 `config.yaml`（或对应的配置文件），添加原心平台的 messaging 配置：

```yaml
messaging:
  yuanxin:
    enabled: true
    worker_type: "claude_code"
    work_dir: "./.hotplex/workspace"
    tenant: "public"
    namespace: "default"
    app_id: "${YUANXIN_APP_ID}"
    pulsar_url: "${YUANXIN_PULSAR_URL}"
```

### 配置项说明

| 配置项 | 类型 | 说明 |
|--------|------|------|
| `enabled` | bool | 是否启用原心适配器 |
| `worker_type` | string | 使用的 Worker 类型，如 `claude_code`、`opencode_server` |
| `work_dir` | string | Worker 工作目录 |
| `tenant` | string | Pulsar 租户名称 |
| `namespace` | string | Pulsar 命名空间 |
| `app_id` | string | 原心平台应用 ID，支持环境变量引用 |
| `pulsar_url` | string | Pulsar Broker 地址，支持环境变量引用 |
| `producer_topic` | string | 响应消息发送的 Pulsar Topic（默认 `global-open-claw-response-topic`） |

---

## 4. 环境变量

在 `.env` 文件（首次使用：`cp configs/env.example .env`）中配置原心相关的环境变量：

```bash
# 原心平台应用 ID — 从原心管理后台获取
YUANXIN_APP_ID=your_app_id_here

# Pulsar Broker 地址
YUANXIN_PULSAR_URL=pulsar://pulsar.example.com:6650
```

### 变量说明

| 变量 | 用途 |
|------|------|
| `YUANXIN_APP_ID` | 原心平台分配的应用唯一标识，用于消息路由和鉴权 |
| `YUANXIN_PULSAR_URL` | Pulsar Broker 连接地址，格式为 `pulsar://host:port` |

**验证**：确认 `.env` 中两个变量均已取消注释且值非空。

---

## 5. 启动与验证

### 5.1 启动 Gateway

```bash
hotplex gateway start -d
```

`-d` 表示后台运行（daemon 模式）。

### 5.2 检查连接状态

```bash
hotplex status
```

输出中应包含原心适配器的连接状态，显示 Pulsar 连接已建立。

### 5.3 查看日志确认

```bash
hotplex service logs -f
```

期望看到类似以下日志：

```
yuanxin adapter started  app_id=xxx  pulsar_url=pulsar://...
pulsar consumer connected  tenant=public  namespace=default
pulsar producer connected  tenant=public  namespace=default
```

**验证**：日志中无错误信息，Pulsar consumer 和 producer 均已连接。

---

## 6. 消息格式

原心平台通过 Pulsar 发送的消息采用 JSON 格式：

```json
{
  "metadata": {
    "botId": "695E4978394F4523",
    "replyUserCodes": "002287",
    "sysId": 1,
    "messageId": "002287-1",
    "secret": "123",
    "platform": "yx"
  },
  "msg": "你好，请帮我写一个排序算法"
}
```

### 字段说明

| 字段 | 类型 | 说明 |
|------|------|------|
| `metadata` | object | 消息元数据，包含用户标识、会话信息等扩展字段 |
| `metadata.botId` | string | 机器人应用 ID |
| `metadata.replyUserCodes` | string | 用户标识，用于会话连续性 |
| `metadata.sysId` | int | 系统 ID |
| `metadata.messageId` | string | 消息/会话标识，用于路由响应 |
| `metadata.secret` | string | 消息扩展字段，原心平台用于消息扩展，传给 Worker 处理。此字段不由 Gateway 校验，安全依赖 Pulsar 和原心平台认证 |
| `metadata.platform` | string | 来源平台标识 |
| `msg` | string | 用户发送的文本消息内容 |

HotPlex Gateway 的响应同样以 JSON 格式发回 Pulsar，原心平台消费后呈现给用户。

---

## 故障排查

| 症状 | 检查项 |
|------|--------|
| 适配器未启动 | 确认 `config.yaml` 中 `yuanxin.enabled` 为 `true` |
| Pulsar 连接失败 | 检查 `YUANXIN_PULSAR_URL` 地址是否正确，网络是否可达 |
| 消息无回复 | `hotplex service logs -f` 查看 Worker 是否启动成功 |
| 消息消费延迟 | 检查 Pulsar 集群负载和 Topic 积压情况 |
| 鉴权失败 | 确认 `YUANXIN_APP_ID` 正确且应用已在原心平台发布 |

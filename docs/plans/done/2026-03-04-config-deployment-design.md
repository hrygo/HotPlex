# HotPlex 配置文件管理方案 - 部署态设计

**Date**: 2026-03-04
**Status**: Approved
**Related**: chatapps/configs, .env

---

## 1. 背景与目标

### 当前问题

1. **配置分散** - `.env` 在 `$HOME`，YAML 在 `./chatapps/configs/`，关系不清晰
2. **部署路径不明确** - Docker/K8s 挂载路径需要用户自行判断
3. **缺乏优先级** - 环境变量 vs YAML 文件，无明确覆盖规则

### 目标

- 统一配置存储位置（开发态 & 部署态）
- 清晰的分层加载优先级
- 支持 Docker/K8s/二进制多场景部署

---

## 2. 配置分层设计

### 2.1 目录结构

```
~/.hotplex/                      # 用户配置根目录
├── .env                          # 敏感配置（token、secret）
└── configs/                      # 平台行为配置
    ├── slack.yaml
    ├── telegram.yaml
    ├── dingtalk.yaml
    ├── discord.yaml
    └── whatsapp.yaml
```

### 2.2 优先级（从高到低）

| 优先级 | 位置 | 用途 | 典型内容 |
|--------|------|------|----------|
| 1 | `~/.hotplex/configs/*.yaml` | 用户自定义配置 | 覆盖默认行为 |
| 2 | `./chatapps/configs/*.yaml` | 代码库默认配置 | 模板配置 |
| 3 | 内置默认值 | 兜底 | 硬编码默认值 |

### 2.3 环境变量

| 变量 | 默认值 | 描述 |
|------|--------|------|
| `HOTPLEX_CONFIG_DIR` | `~/.hotplex` | 配置根目录 |
| `HOTPLEX_ENV_FILE` | `{HOTPLEX_CONFIG_DIR}/.env` | 环境变量文件 |

**注意**：`HOTPLEX_CHATAPPS_CONFIG_DIR` 仍然有效，但优先级低于 `~/.hotplex/configs/`

---

## 3. 敏感信息分离

### 3.1 `.env`（敏感）

- Bot tokens (`HOTPLEX_SLACK_BOT_TOKEN`, `HOTPLEX_TELEGRAM_BOT_TOKEN` 等)
- Signing secrets (`HOTPLEX_SLACK_SIGNING_SECRET`)
- API keys (`HOTPLEX_API_KEY`, `HOTPLEX_BRAIN_API_KEY`)

### 3.2 YAML（行为+非敏感）

- 平台标识 (`platform: slack`)
- AI 配置 (`provider.type`, `model`)
- 功能开关 (`features.chunking.enabled`)
- 权限策略 (`security.permission.dm_policy`)
- 工作目录 (`engine.work_dir`)

---

## 4. 部署场景

### 4.1 Docker

```bash
# 方式 1: 挂载用户目录
docker run -v ~/.hotplex:/root/.hotplex hotplex:latest

# 方式 2: 只挂载配置（推荐）
docker run -v ./configs:/root/.hotplex/configs \
           -v ./secrets.env:/root/.hotplex/.env \
           hotplex:latest
```

### 4.2 Docker Compose

```yaml
services:
  hotplex:
    image: hotplex:latest
    volumes:
      - hotplex-config:/root/.hotplex
    environment:
      - HOTPLEX_CONFIG_DIR=/root/.hotplex

volumes:
  hotplex-config:
```

### 4.3 Kubernetes

```yaml
# ConfigMap - 行为配置
apiVersion: v1
kind: ConfigMap
metadata:
  name: hotplex-config
data:
  slack.yaml: |
    platform: slack
    features:
      chunking:
        enabled: true

---
# Secret - 敏感配置
apiVersion: v1
kind: Secret
metadata:
  name: hotplex-secrets
stringData:
  .env: |
    HOTPLEX_SLACK_BOT_TOKEN=xoxb-...
    HOTPLEX_API_KEY=...

---
# Deployment
spec:
  containers:
  - name: hotplex
    volumeMounts:
    - name: config
      mountPath: /root/.hotplex/configs
    - name: secrets
      mountPath: /root/.hotplex/.env
      subPath: .env
```

### 4.4 二进制部署

```bash
# 创建配置目录
mkdir -p ~/.hotplex/configs

# 复制模板
cp chatapps/configs/slack.yaml ~/.hotplex/configs/

# 编辑配置
vim ~/.hotplex/.env

# 运行
./hotplexd
```

---

## 5. 实现要点

### 5.1 配置加载逻辑

```go
// 搜索路径（按优先级）
var configPaths = []string{
    os.Getenv("HOTPLEX_CONFIG_DIR") + "/configs",  // 用户配置
    "chatapps/configs",                              // 默认配置
}

// 加载顺序：找到第一个存在的文件即停止
for _, path := range configPaths {
    if _, err := os.Stat(path); err == nil {
        configDir = path
        break
    }
}
```

### 5.2 向后兼容

- `HOTPLEX_CHATAPPS_CONFIG_DIR` 仍然有效，但作为 fallback
- 现有部署无需修改，除非需要用户自定义配置

---

## 6. 迁移步骤

1. **Phase 1**: 修改配置加载逻辑，添加路径搜索
2. **Phase 2**: 更新文档（docker-deployment_zh.md）
3. **Phase 3**: 提供迁移脚本/指南

---

## 7. 验收标准

- [ ] Docker 挂载 `~/.hotplex/configs` 可生效
- [ ] `HOTPLEX_CONFIG_DIR` 环境变量可覆盖默认路径
- [ ] 现有部署不破坏（向后兼容）
- [ ] 文档更新完成

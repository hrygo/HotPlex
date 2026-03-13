# Docker 容器中 Git/GitHub CLI 认证问题排查

> 日期：2026-03-06
> 问题：机器人2 报错"环境缺少 GitHub 凭证"，但机器人1正常工作

## 问题现象

- **机器人 1 (hotplex-01)**：能成功创建 PR
- **机器人 2 (hotplex-02)**：报错缺少 GitHub 凭证

## 排查过程

### 1. 检查环境变量

两个容器的 `.env` 文件都配置了相同的 `GITHUB_TOKEN`：

```bash
GITHUB_TOKEN=ghp_xxx...
```

### 2. 检查 gh CLI 认证状态

```bash
docker exec hotplex-01 gh auth status
docker exec hotplex-02 gh auth status
```

**结果**：两个容器都显示认证成功！

```
github.com
  ✓ Logged in to github.com account aaronwong1989 (GITHUB_TOKEN)
```

### 3. 检查 SSH 目录

```bash
docker exec hotplex-01 ls -la ~/.ssh
docker exec hotplex-02 ls -la ~/.ssh
```

**结果**：两个容器都没有 SSH 目录（预期行为，未挂载）。

### 4. 检查 Git Remote 配置（关键！）

```bash
docker exec hotplex-01 bash -c "cd /home/hotplex/projects/hotplex && git remote -v"
docker exec hotplex-02 bash -c "cd /home/hotplex/projects/hotplex && git remote -v"
```

**结果**：

| 容器 | origin URL |
|------|-----------|
| hotplex-01 | `https://x-access-token:ghp_xxx@github.com/...` ✅ |
| hotplex-02 | `https://github.com/...` ❌ |

## 根本原因

**Git 推送不使用 `GITHUB_TOKEN` 环境变量**，而是依赖：
1. SSH 密钥
2. Git credential helper
3. **URL 中嵌入的 token**

机器人1的项目目录中 git remote URL 已嵌入 access token，而机器人2没有。

## 解决方案

```bash
docker exec hotplex-02 bash -c \
  "cd /home/hotplex/projects/hotplex && \
   git remote set-url origin https://x-access-token:\${GITHUB_TOKEN}@github.com/aaronwong1989/hotplex.git"
```

或使用硬编码 token（注意安全）：

```bash
git remote set-url origin https://x-access-token:ghp_xxx@github.com/owner/repo.git
```

## 预防措施

### 方案1：在 docker-compose.yml 中初始化

```yaml
services:
  hotplex-02:
    entrypoint:
      - /bin/sh
      - -c
      - |
        cd /home/hotplex/projects/hotplex
        git remote set-url origin https://x-access-token:$${GITHUB_TOKEN}@github.com/owner/repo.git
        exec /app/hotplexd
```

### 方案2：使用 Git Credential Helper

在 `.gitconfig` 中配置：

```ini
[credential]
    helper = store
[credential "https://github.com"]
    username = x-access-token
    password =
```

### 方案3：挂载 SSH 密钥（推荐生产环境）

```yaml
volumes:
  - ${HOME}/.ssh:/home/hotplex/.ssh:ro
```

## 经验总结

| 检查项 | 说明 |
|--------|------|
| `gh auth status` | 只检查 gh CLI 认证，不代表 git push 能用 |
| `GITHUB_TOKEN` | gh CLI 会读取，但 **git 命令不会** |
| `git remote -v` | 检查 URL 是否包含认证信息 |
| SSH vs HTTPS | HTTPS 需要 token 嵌入 URL，SSH 需要密钥挂载 |

## 相关文件

- `docker-compose.yml` - 容器编排配置
- `.env-01` / `.env-02` - 环境变量
- `~/.gitconfig-hotplex*` - Git 全局配置挂载

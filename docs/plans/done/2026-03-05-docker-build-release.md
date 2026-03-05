# Docker 构建与发布功能实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**目标**：实现 Docker All-in-One 构建与发布功能（hotplexd + Claude Code）

**架构**：单 Dockerfile + Build Args，使用 Buildx 进行多平台构建

**技术栈**：Docker, Buildx, GHCR, GoReleaser

## 技术方案详解

### All-in-One 构建

包含 hotplexd + Claude Code，镜像约 ~200MB，开箱即用。

### 目录映射

| 宿主机路径 | 容器内路径 | 模式 | 说明 |
|-----------|-----------|------|------|
| `$HOME/.claude` | `/home/hotplex/.claude` | ro | Claude Code 配置（settings.json, plugins, hooks, MCPs） |
| `$HOME/.claude/projects` | `/home/hotplex/.claude/projects` | rw | 会话历史（需持久化） |
| `$HOME/.hotplex` | `/.hotplex` | rw | HotPlex 配置 |
| `$HOME/projects` | `/home/hotplex/projects` | rw | 工作目录（AI 操作的文件） |

### 权限设计

- 容器内用户：`hotplex` (UID=1000)
- 通过 `HOST_UID` build-arg 支持自定义
- Docker 中默认跳过权限确认：`--dangerously-skip-permissions`
- HotPlex 需在调用 Claude Code 时自动添加此参数

### 网络访问

- **Linux**: 默认 bridge + 端口映射
- **macOS**: 推荐使用 Colima，容器内网络与宿主机一致

---

## Task 1: 创建 All-in-One Dockerfile（支持宿主机配置映射）

**Files:**
- Create: `Dockerfile`

### 目录映射与权限

**Volume 映射**：
| 宿主机 | 容器内 | 模式 | 说明 |
|--------|--------|------|------|
| `$HOME/.claude/settings.json` | `/home/hotplex/.claude/settings.json` | ro | Claude Code 配置 |
| `$HOME/.claude/projects` | `/home/hotplex/.claude/projects` | rw | 会话历史 |
| `$HOME/.hotplex` | `/.hotplex` | rw | HotPlex 配置 |
| `$HOME/projects` | `/home/hotplex/projects` | rw | 工作目录 |

**权限配置**：
- 容器用户：`hotplex` (UID=1000)
- 通过 `HOST_UID` build-arg 适配宿主机用户
- HotPlex 调用 Claude Code 时添加 `--dangerously-skip-permissions`

### 完整 All-in-One Dockerfile

```dockerfile
# ============================================
# Stage 1: Build binary
# ============================================
FROM golang:1.25-alpine AS builder

WORKDIR /build
RUN apk add --no-cache git make curl
COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=0 go build \
    -ldflags="-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildTime=${BUILD_TIME}" \
    -o hotplexd ./cmd/hotplexd

# ============================================
# Stage 2: Install Claude Code
# ============================================
FROM alpine:3.19 AS claude-installer
RUN apk add --no-cache curl jq

ARG CLAUDE_VERSION="latest"
ENV CLAUDE_VERSION=${CLAUDE_VERSION}

# 安装 Claude Code 到临时位置
RUN mkdir -p /tmp/claude-install && \
    curl -fsSL https://raw.githubusercontent.com/anthropics/claude-code/main/install.sh | \
    sh -s -- -v ${CLAUDE_VERSION} -d /tmp/claude-install && \
    ls -la /tmp/claude-install/usr/local/bin/

# ============================================
# Stage 3: Final runtime image
# ============================================
FROM alpine:3.19
WORKDIR /app
RUN apk add --no-cache ca-certificates

# Copy binary
COPY --from=builder /build/hotplexd /usr/local/bin/

# Copy Claude Code
COPY --from=claude-installer /tmp/claude-install/usr/local/bin/claude /usr/local/bin/
COPY --from=claude-installer /tmp/claude-install/usr/local/bin/claude-code /usr/local/bin/

# Verify CLI
RUN /usr/local/bin/claude --version || /usr/local/bin/claude-code --version

# Create user matching host UID (for config file access)
ARG HOST_UID=1000
RUN adduser -D -u ${HOST_UID} hotplex
USER hotplex

EXPOSE 8080
ENTRYPOINT ["hotplexd"]
```

**Step 2: 提交**

```bash
git add Dockerfile
git commit -m "feat(docker): add All-in-One Dockerfile with Claude Code"
```

---

## Task 2: 添加 Makefile Docker targets

**Files:**
- Modify: `Makefile`

**Step 1: 添加 Docker targets**

在 Makefile 末尾添加：

```makefile
# =============================================================================
# 🐳 DOCKER
# =============================================================================

DOCKER_IMAGE    ?= hotplex
DOCKER_TAG      ?= latest
DOCKER_REGISTRY ?= ghcr.io/hrygo
HOST_UID        ?= $(shell id -u)

docker-build:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg HOST_UID=$(HOST_UID) \
		-t $(DOCKER_IMAGE):$(DOCKER_TAG) .

docker-build-tag:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg HOST_UID=$(HOST_UID) \
		-t $(DOCKER_IMAGE):$(DOCKER_TAG) \
		-t $(DOCKER_IMAGE):$(VERSION) .

docker-run:
	docker run -d --name hotplex \
		-p 8080:8080 \
		-v $(HOME)/.hotplex:/.hotplex \
		-v $(HOME)/.claude/settings.json:/home/hotplex/.claude/settings.json:ro \
		-v $(HOME)/.claude/projects:/home/hotplex/.claude/projects:rw \
		-v $(HOME)/projects:/home/hotplex/projects:rw \
		$(DOCKER_IMAGE):$(DOCKER_TAG)

docker-push:
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)

docker-push-tag:
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)
	docker push $(DOCKER_IMAGE):$(VERSION)

docker-buildx:
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--build-arg VERSION=$(VERSION) \
		--tag $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(DOCKER_TAG) \
		--tag $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(VERSION) \
		--push .

docker-clean:
	docker rmi $(DOCKER_IMAGE):$(DOCKER_TAG) || true

.PHONY: docker-build docker-build-tag docker-run docker-push docker-push-tag docker-buildx docker-clean
```

**Step 2: 运行测试**

```bash
make docker-build
```

**Step 3: 提交**

```bash
git add Makefile
git commit -m "feat(docker): add Docker Makefile targets"
```

---

## Task 3: 更新 GoReleaser 配置

**Files:**
- Modify: `.goreleaser.yaml`

**Step 1: 添加 Docker docks**

在 `.goreleaser.yaml` 末尾添加：

```yaml
dockers:
  - image_templates:
      - ghcr.io/hrygo/hotplex:{{ .Version }}
      - ghcr.io/hrygo/hotplex:latest
    dockerfile: Dockerfile
    build_flag_templates:
      - --build-arg VERSION={{ .Version }}
      - --build-arg HOST_UID=1000
```

**Step 2: 提交**

```bash
git add .goreleaser.yaml
git commit -m "feat(docker): add Docker images to GoReleaser"
```

---

## Task 4: 更新 GitHub Actions

**Files:**
- Modify: `.github/workflows/release.yml`

**Step 1: 添加 Docker login**

在 `release.yml` 的 `goreleaser` job 中添加：

```yaml
- name: Login to GitHub Container Registry
  uses: docker/login-action@v3
  with:
    registry: ghcr.io
    username: ${{ github.actor }}
    password: ${{ secrets.GITHUB_TOKEN }}
```

**Step 2: 提交**

```bash
git add .github/workflows/release.yml
git commit -m "feat(docker): add GHCR login to release workflow"
```

---

## Task 5: 更新文档

**Files:**
- Modify: `docs/docker-deployment.md`
- Modify: `docs/docker-deployment_zh.md`

**Step 1: 更新文档，添加新构建方式说明**

在 docker-deployment.md 开头添加：

```markdown
## 构建方式

### 1. 纯净构建 (hotplex-only)

仅包含 hotplexd 二进制，镜像最小化（约 20MB）：

```bash
# 本地构建
make docker-build

# 运行
make docker-run

# 推送到 GHCR
make docker-push-tag
```

### 2. All-in-One 构建

包含 hotplexd + Claude Code，配置文件可直接映射宿主机：

```bash
# 构建 Claude Code 版本
make docker-build-allinone TARGET=claude

# 运行（自动映射宿主机 Claude Code 配置）
make docker-run-allinone

# 或手动运行
docker run -d --name hotplex-ai \
  -p 8080:8080 \
  -v $HOME/.hotplex:/.hotplex \
  -v $HOME/.claude:/home/hotplex/.claude:ro \
  ghcr.io/hrygo/hotplex:allinone-claude-latest
```

**Volume 映射说明**：
| 宿主机 | 容器内 | 说明 |
|--------|--------|------|
| `$HOME/.claude` | `/home/hotplex/.claude` | Claude Code 配置（只读） |
| `$HOME/.hotplex` | `/.hotplex` | HotPlex 配置 |

**优点**：
- 复用宿主机 Claude Code 的 `settings.json` 配置
- 复用本地模型（Ollama 等）
- 无需在容器内重新配置

### 3. 多平台构建

使用 Buildx 构建 amd64 + arm64：

```bash
make docker-buildx          # 纯净构建多平台
make docker-buildx-allinone  # All-in-One 多平台
```

**Step 2: 提交**

```bash
git add docs/docker-deployment.md docs/docker-deployment_zh.md
git commit -m "docs(docker): update deployment guide with new build options"
```

---

## 执行选项

**Plan complete and saved to `docs/plans/2026-03-05-docker-build-release.md`.**

**Two execution options:**

1. **Subagent-Driven (this session)** - I dispatch fresh subagent per task, review between tasks, fast iteration

2. **Parallel Session (separate)** - Open new session with executing-plans, batch execution with checkpoints

**Which approach?**

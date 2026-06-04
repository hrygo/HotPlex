# Docker 最佳实践改进方案

**日期**: 2026-05-26
**分支**: feat/dual-db-support
**范围**: Dockerfile、docker-compose.yml、docker-compose.prod.yml、docker-compose.pg.yml、docker/

---

## 背景

对 HotPlex 项目的 Docker 配置进行了全面调研（目录结构、Dockerfile 构建、Compose 编排），
对标 Traefik/Consul/Dex/etcd/MinIO 等 Go 项目。结论：当前布局基本正确，需要渐进式优化。

## 改进项清单

### P0 — 必须修复

1. **postgres 添加 `shm_size: '512m'`**
   - Docker 默认 64MB，PostgreSQL 并行查询/pgvector 索引构建会 OOM
   - 文件: `docker-compose.yml`

2. **Dockerfile LABEL 迁移到 OCI 标准**
   - 当前用非标准 `version`/`git.sha`/`build.time`，迁移到 `org.opencontainers.image.*`
   - 文件: `Dockerfile`

3. **Dockerfile 多架构支持**
   - 添加 `FROM --platform=$BUILDPLATFORM` + `ARG TARGETOS`/`TARGETARCH`
   - `CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build`
   - 文件: `Dockerfile`

### P1 — 应该修复

4. **ai-tools-collector 改用 alpine base**
   - 当前用 `node:24-bookworm-slim`（~200MB），只需 curl 下载两个二进制
   - 改为 `alpine:3.21`（~7MB）
   - 文件: `Dockerfile`

5. **HEALTHCHECK 简化为 exec 形式**
   - `CMD curl -f ... || exit 1` → `CMD ["curl", "-f", "..."]`
   - `|| exit 1` 冗余（curl -f 已返回非零）
   - Dockerfile start_period 10s → 15s；compose start_period 15s → 30s
   - 文件: `Dockerfile`, `docker-compose.yml`

6. **所有服务添加 logging 轮转**
   - 当前仅 gateway 有 logging 配置
   - 为 postgres、backup、prometheus、grafana 添加相同配置
   - 文件: `docker-compose.yml`

7. **PG start_period 提升到 30s**
   - 首次初始化 + pgvector 扩展安装可能较慢
   - 文件: `docker-compose.yml`

8. **生产 overlay 移除 PG ports 映射**
   - 生产环境 PG 不应暴露到宿主机
   - 文件: `docker-compose.prod.yml`

9. **添加 docker-compose.override.yml 到 .gitignore**
   - 开发者本地定制端口/卷，不影响仓库
   - 文件: `.gitignore`

### P2 — 建议修复

10. **backup sidecar 改进**
    - 添加 `trap` 信号处理，避免容器停止时中断 pg_dump
    - 使用 `postgres:16-alpine` 镜像替代 `alpine:3.21`（已内置 pg_dump）
    - 添加 healthcheck（检查近期是否有备份文件）
    - 添加 logging 配置
    - 文件: `docker-compose.yml`

11. **Traefik 添加 network 限制**
    - `--providers.docker.network=traefik-network` 限制发现范围
    - 文件: `docker-compose.prod.yml`

12. **postgres 添加性能参数**
    - shared_buffers=256MB, work_mem=32MB, max_parallel_workers_per_gather=2
    - 文件: `docker-compose.yml`

## 不做的事情

- **目录结构重构**: 当前布局已符合业界标准，无需改动
- **基础镜像替换**: debian:bookworm-slim 是正确选择（Worker 子进程需要完整工具链）
- **distroless/scratch**: 不适用于本项目
- **compose.yaml 重命名**: docker-compose.yml 仍广泛兼容，不迁移
- **Go entrypoint**: 当前 bash entrypoint 已足够清晰
- **镜像 digest pinning**: 需要自动化更新流程，不在本次范围

# Production Deployment

## Scaling HotPlex to Enterprise Grade

Moving from local development to a production-grade deployment requires a focus on **Reliability, Scalability, and Security.** This guide outlines the best practices for deploying HotPlex in a professional environment.

---

### Deployment Strategies

#### 1. 🐳 Containerization (Recommended)
The official `hotplex` Docker image is the preferred way to deploy. It is optimized for size and security.

```bash
docker pull hrygo/hotplex:latest
docker run -p 8080:8080 -v ./config:/etc/hotplex -e HOTPLEX_STATE_DB=postgres://...
```

#### 2. ☸️ Kubernetes
For large-scale deployments, use our official **Helm Chart**. This provides built-in:
- **High Availability**: Multi-replica deployments with leader election.
- **Auto-scaling**: Scale workers based on message throughput.
- **Ingress Management**: Automated SSL termination and routing.

---

### Hardening Your Instance

In production, security is paramount:

- **External State Stores**: Move away from in-memory or SQLite. Use **PostgreSQL or Redis** for cross-node state persistence.
- **TLS Everywhere**: Always run `hotplexd` behind a reverse proxy (Nginx, Traefik) providing TLS 1.3 encryption.
- **Authentication**: Enable the `AuthHook` to integrate with your existing OIDC or LDAP provider.

---

### Resource Planning

| Load Level | CPU    | RAM   | Max Concurrent Sessions |
| :--------- | :----- | :---- | :---------------------- |
| **Small**  | 2 vCPU | 4 GB  | ~50                     |
| **Medium** | 4 vCPU | 8 GB  | ~200                    |
| **Large**  | 8 vCPU | 16 GB | ~1000+                  |

---

### Monitoring Health

Always configure a liveness probe directed at our health endpoint:

```http
GET /health
```

[View the Docker Deployment Guide on GitHub](https://github.com/hrygo/hotplex/blob/main/docs/docker-deployment.md)

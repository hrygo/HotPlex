---
name: Docker Container Operations
description: This skill should be used when the user asks to "manage Docker containers", "restart hotplex container", "check container status", "scale hotplex", "stop bot", "start bot". Provides container lifecycle management for hotplex deployment.
version: 0.1.0
---

# Docker Container Operations

Manage the lifecycle of hotplex containers running in Docker Compose deployment.

## Overview

This skill provides administrative operations for hotplex Docker containers. It interacts with the docker CLI and docker-compose to manage container lifecycle, scaling, and resource monitoring.

## Prerequisites

- Docker CLI installed on the host
- docker-compose or docker compose plugin available
- Permission to run docker commands
- HotPlex deployed via docker-compose

## Container Operations

### List All Containers

List all hotplex containers with their status:

```bash
docker compose ps
```

### Start a Container

Start a specific hotplex service:

```bash
docker compose up -d hotplex
docker compose up -d hotplex-secondary
```

### Stop a Container

Stop a specific hotplex service:

```bash
docker compose stop hotplex
```

### Restart a Container

Restart a specific hotplex service:

```bash
docker compose restart hotplex
```

### Remove a Container

Remove a stopped container:

```bash
docker compose rm hotplex
```

### View Container Logs

View real-time logs from a container:

```bash
docker compose logs -f hotplex
docker compose logs --tail=100 hotplex-secondary
```

### View Resource Usage

Check CPU and memory usage:

```bash
docker stats $(docker compose ps -q)
docker stats hotplex hotplex-secondary
```

## Scaling Operations

### Scale a Service

Scale hotplex-secondary to multiple instances:

```bash
docker compose up -d --scale hotplex-secondary=2
```

Note: Scaling requires proper configuration in docker-compose.yml for port conflicts.

## Configuration

The skill uses the docker-compose.yml file at `docker/matrix/`. All commands run in that directory.

## Troubleshooting

- If container fails to start, check logs: `docker compose logs hotplex`
- Verify container health: `docker inspect hotplex --format='{{.State.Health}}'`
- Check container networking: `docker network ls`

## Additional Resources

### Reference Files

- **`docker/matrix/docker-compose.yml`** - Container deployment configuration
- **`docker/matrix/.env.primary`** - Bot1 credentials
- **`docker/matrix/.env.secondary`** - Bot2 credentials
- **`references/docker-commands.md`** - Complete Docker CLI reference

### Related Skills

- **`hotplex-diagnostics`** - For log analysis and debugging
- **`hotplex-data-mgmt`** - For data and session management

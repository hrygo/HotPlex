# Docker Commands Reference

Complete reference for Docker CLI commands used in hotplex management.

## Docker Compose Commands

### Service Management

```bash
# Start all services
docker compose up -d

# Stop all services
docker compose down

# Restart specific service
docker compose restart hotplex

# Rebuild and start
docker compose up -d --build hotplex
```

### Logs

```bash
# Follow logs
docker compose logs -f

# Specific service
docker compose logs -f hotplex

# Last N lines
docker compose logs --tail=100 hotplex

# Since timestamp
docker compose logs --since 2024-01-01T00:00:00 hotplex

# Filter by level (JSON logs)
docker compose logs --filter "level=error" hotplex
```

### Scaling

```bash
# Scale service
docker compose up -d --scale hotplex-secondary=2

# Scale with limits
docker compose up -d --scale hotplex-secondary=2 --max-ports
```

### Configuration

```bash
# View configuration
docker compose config

# Validate
docker compose config --quiet
```

## Docker Commands

### Container Operations

```bash
# List containers
docker ps -a

# Inspect container
docker inspect hotplex

# Exec into container
docker exec -it hotplex /bin/sh

# Copy files
docker cp hotplex:/path/in/container /local/path
docker cp /local/path hotplex:/path/in/container
```

### Resource Monitoring

```bash
# Stats
docker stats
docker stats --no-stream hotplex

# Top processes
docker top hotplex

# Events
docker events
```

### Networking

```bash
# List networks
docker network ls

# Inspect network
docker network inspect hotplex_default
```

### Volumes

```bash
# List volumes
docker volume ls

# Inspect volume
docker volume inspect hotplex_hotplex-go-mod
```

## Common Issues

### Port Conflicts

```bash
# Find process using port
lsof -i :18080

# Kill process
kill $(lsof -t -i :18080)
```

### Permission Denied

```bash
# Fix ownership
sudo chown -R $(id -u):$(id -g) ~/.hotplex ~/.claude

# Or use UID from container
HOST_UID=$(id -u) docker compose up -d
```

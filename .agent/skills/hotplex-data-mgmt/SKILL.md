---
name: HotPlex Data Management
description: This skill should be used when the user asks to "manage data", "clean sessions", "cleanup markers", "view messages", "export data", "delete session". Provides data and session management for hotplex persistence layer.
version: 0.1.0
---

# HotPlex Data Management

Manage persistent data including session markers, messages, and temporary files.

## Overview

This skill provides data management capabilities for hotplex persistence layer. It handles session markers, message storage, and cleanup operations.

## Prerequisites

- HotPlex deployed and running
- Access to host's `.hotplex` directory
- Docker CLI for container file access

## Session Markers

### List Session Markers

List all persistent session markers:

```bash
ls -la ~/.hotplex/markers/
```

### Delete a Session Marker

Remove a specific session marker to prevent resumption:

```bash
rm ~/.hotplex/markers/<provider-session-id>
```

### Delete All Markers

Remove all session markers (forces fresh start):

```bash
rm ~/.hotplex/markers/*
```

## Message Storage

### Locate Message Database

Find message storage location:

```bash
docker exec hotplex ls -la /home/hotplex/.hotplex/
```

### Check Database Size

View database file sizes:

```bash
docker exec hotplex du -sh /home/hotplex/.hotplex/*.db
docker exec hotplex du -sh /home/hotplex/.hotplex/*
```

### Export Messages

Export messages from database (requires sqlite3):

```bash
docker exec hotplex sqlite3 /home/hotplex/.hotplex/messages.db "SELECT * FROM messages LIMIT 100;"
```

## Temporary Files

### Clean Claude Cache

Remove cached CLI data:

```bash
docker exec hotplex rm -rf /home/hotplex/.claude/sessions/*
```

### Clean Temporary Work Directories

Remove temporary work directories:

```bash
docker exec hotplex rm -rf /tmp/hotplex_*
```

### Clean Build Cache

Remove Go build cache:

```bash
docker exec hotplex go clean -cache
```

## Session Cleanup

### Force Stop All Sessions

Stop all running CLI processes:

```bash
docker exec hotplex pkill -f "claude\|opencode"
```

### Clean Zombie Sessions

Remove stale session markers and processes:

```bash
# Find zombie markers
docker exec hotplex find /home/hotplex/.hotplex/markers -type f -mmin +60

# Remove markers older than 24 hours
docker exec hotplex find /home/hotplex/.hotplex/markers -type f -mtime +1 -delete
```

## Backup and Restore

### Backup Data

Create backup of hotplex data:

```bash
tar -czf hotplex-backup-$(date +%Y%m%d).tar.gz \
  -C ~ .hotplex .claude
```

### Restore Data

Restore from backup:

```bash
tar -xzf hotplex-backup-20240101.tar.gz -C ~
```

## Configuration

Data directories:
- `~/.hotplex/` - Message database and session markers
- `~/.claude/` - Claude CLI session state
- `/tmp/hotplex_*/` - Temporary work directories

## Troubleshooting

### Disk Full

Check disk usage:

```bash
df -h ~
docker system df
```

### Permission Issues

Fix ownership:

```bash
sudo chown -R $(id -u):$(id -g) ~/.hotplex ~/.claude
```

## Additional Resources

### Reference Files

- **`internal/persistence/marker.go`** - Marker store implementation
- **`plugins/storage/`** - Message storage backends

### Related Skills

- **`docker-container-ops`** - For container lifecycle management
- **`hotplex-diagnostics`** - For monitoring and debugging

# HotPlex API Endpoints

Complete API reference for hotplex WebSocket and HTTP endpoints.

## Base URLs

- Primary bot: `http://localhost:18080`
- Secondary bot: `http://localhost:18081`

## WebSocket API

### Connection

```
ws://localhost:18080/ws
```

### Client Request Format

```json
{
  "request_id": 1,
  "type": "execute|stop|stats|version",
  "session_id": "session-123",
  "prompt": "Hello",
  "instructions": "Be helpful",
  "system_prompt": "Custom system prompt",
  "work_dir": "/home/hotplex/projects"
}
```

### Server Response Format

```json
{
  "request_id": 1,
  "event": "message|completed|error|stopped",
  "data": {}
}
```

## Request Types

### Execute

Start a new execution:

```json
{
  "type": "execute",
  "session_id": "my-session",
  "prompt": "List files in current directory",
  "instructions": "Use ls -la",
  "work_dir": "/home/hotplex/projects/myproject"
}
```

### Stop

Stop a running session:

```json
{
  "type": "stop",
  "session_id": "my-session",
  "reason": "user_requested"
}
```

### Stats

Get session statistics:

```json
{
  "type": "stats",
  "session_id": "my-session"
}
```

### Version

Get CLI version:

```json
{
  "type": "version"
}
```

## Response Events

### Message Event

```json
{
  "event": "message",
  "data": {
    "type": "content",
    "content": "Here are the files..."
  }
}
```

### Completed Event

```json
{
  "event": "completed",
  "data": {
    "session_id": "my-session",
    "stats": {
      "input_tokens": 1000,
      "output_tokens": 500
    }
  }
}
```

### Error Event

```json
{
  "event": "error",
  "data": {
    "message": "Execution failed: ..."
  }
}
```

### Stopped Event

```json
{
  "event": "stopped",
  "data": {
    "session_id": "my-session"
  }
}
```

## HTTP Endpoints

### Health Check

```
GET /health
```

Response:
```json
{
  "status": "ok",
  "version": "0.24.0"
}
```

### Metrics

```
GET /metrics
```

Returns Prometheus-format metrics.

## Session ID Format

Session IDs follow the format:
```
platform:userID:botUserID:channelID:threadID
```

Example:
```
slack:U123456:BOT_U0AHRCL1KCM:C123456:T123456
```

## Error Codes

| Code | Description |
|------|-------------|
| 4001 | Invalid request format |
| 4002 | Session not found |
| 4003 | Execution timeout |
| 4004 | Session already running |
| 5001 | Internal server error |

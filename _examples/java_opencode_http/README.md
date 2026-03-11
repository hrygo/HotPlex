# Java HTTP & WebSocket Client Examples

This directory contains Java client examples demonstrating how to use HotPlex's HTTP REST API and WebSocket interfaces.

## Example Files

### 1. SimpleClient.java
Simple HTTP client example demonstrating:
- REST session creation and message sending
- SSE event stream listening
- System prompt injection

### 2. HotPlexWsClient.java
Enterprise-grade WebSocket client with:
- Auto-reconnection (exponential backoff)
- Error handling and recovery
- Graceful shutdown
- Metrics collection

## Quick Start

### HTTP Client

```bash
# Start hotplexd server
go run cmd/hotplexd/main.go

# Compile and run
cd _examples/java_opencode_http
javac SimpleClient.java
java SimpleClient
```

### WebSocket Client

```bash
# Compile
javac HotPlexWsClient.java

# Run
java com.hotplex.example.HotPlexWsClient
```

## Usage as Library

```java
import com.hotplex.example.HotPlexWsClient;

HotPlexWsClient client = new HotPlexWsClient(
    "ws://localhost:8080/ws/v1/agent",
    "my-session"
);

client.connect();

String result = client.execute(
    "List files in current directory",
    "You are a helpful assistant."
);

System.out.println(result);
client.disconnect();
```

## Dependencies

- Java 11+
- No external dependencies (uses standard library only)

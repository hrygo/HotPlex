*Read this in other languages: [English](quick-start.md), [简体中文](quick-start_zh.md).*

# 快速入门指南

在 5 分钟内快速启动并运行 HotPlex。

## 前置要求

在开始之前，请确保您已安装：

1. 安装 **Go 1.25** 或更高版本（推荐）
2.  已安装并认证的 **Claude Code CLI** 或 **OpenCode CLI**

### 安装 Claude Code CLI

```bash
# macOS / Linux / WSL
curl -fsSL https://claude.ai/install.sh | bash

# 认证
claude auth
```

### 安装 OpenCode CLI

```bash
# 使用 npm
npm install -g @opencode/opencode

# 或使用 Homebrew
brew install opencode
```

---

## 选项 1：Go SDK (推荐)

### 第一步：安装

```bash
go get github.com/hrygo/hotplex
```

### 第二步：创建 `main.go`

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/hrygo/hotplex"
)

func main() {
    // 初始化引擎
    engine, err := hotplex.NewEngine(hotplex.EngineOptions{
        Timeout:        5 * time.Minute,
        PermissionMode: "bypass-permissions",
    })
    if err != nil {
        panic(err)
    }
    defer engine.Close()

    // 配置会话
    cfg := &hotplex.Config{
        WorkDir:   "/tmp/hotplex-demo",
        SessionID: "my-first-session",
    }

    // 执行提示词
    ctx := context.Background()
    err = engine.Execute(ctx, cfg, "用 Go 写一个 hello world", 
        func(eventType string, data any) error {
            if eventType == "answer" {
                fmt.Print(data)
            }
            return nil
        })
    
    if err != nil {
        fmt.Printf("Error: %v\n", err)
    }
}
```

### 第三步：运行

```bash
go run main.go
```

---

## 选项 2：独立服务端

将 HotPlex 作为独立服务端运行，支持多语言客户端。

### 第一步：构建

```bash
git clone https://github.com/hrygo/hotplex.git
cd hotplex
make build
```

### 第二步：运行

```bash
PORT=8080 ./dist/hotplexd
```

### 第三步：连接

**WebSocket (任何语言):**
```
ws://localhost:8080/ws/v1/agent
```

**OpenCode HTTP/SSE:**
```
http://localhost:8080
```

---

## 选项 3：Python SDK

### 第一步：安装

```bash
pip install hotplex
```

### 第二步：创建 `main.py`

```python
from hotplex import HotPlexClient, Config

with HotPlexClient(url="ws://localhost:8080/ws/v1/agent") as client:
    for event in client.execute_stream(
        prompt="用 Python 写一个 hello world",
        config=Config(work_dir="/tmp", session_id="py-demo")
    ):
        if event.type == "answer":
            print(event.data, end="")
```

### 第三步：运行

```bash
python main.py
```

---

## 下一步

- [架构深度解析](architecture_zh.md) - 了解 HotPlex 的工作原理
- [SDK 开发者指南](sdk-guide_zh.md) - 完整的 SDK 参考
- [代码示例](../_examples/) - 更多代码示例
- [基准测试报告](benchmark-report_zh.md) - 性能数据

---

## 常见问题

### "claude: command not found"

安装 Claude Code CLI:
```bash
curl -fsSL https://claude.ai/install.sh | bash
claude auth
```

### "Permission denied"

确保工作目录存在且可写:
```bash
mkdir -p /tmp/hotplex-demo
```

### "Session not found"

会话通过 `SessionID` 标识。在多轮对话中使用相同的 ID。

---

## 需要帮助？

- [GitHub Issues](https://github.com/hrygo/hotplex/issues)
- [Discussions](https://github.com/hrygo/hotplex/discussions)

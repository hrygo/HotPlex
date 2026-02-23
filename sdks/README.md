# HotPlex SDKs

This directory contains the client-side SDKs for various programming languages.

## 🏗 Relationship with HotPlex Core

All SDKs in this folder are **client libraries** designed to communicate with the **HotPlex Server** (`hotplexd`).

### Execution Flow:
`Your App (SDK) <---> HotPlex Server (internal/server) <---> HotPlex Engine (internal/engine) <---> AI CLI (Claude/OpenCode)`

### Key Dependencies:
- **Server Required**: These SDKs **do not** run the AI CLI tools directly. They require a running instance of the HotPlex Gateway (`hotplexd`), which is implemented in `internal/server`.
- **Protocol**: They communicate via **WebSocket** (Native HotPlex Protocol) or **HTTP/SSE** (OpenCode Compatibility Layer).

## 📂 Available SDKs

- **[Python](./python)**: A production-ready Python client with full-duplex session support.
- **[TypeScript](./typescript)**: Type-safe client for Web and Node.js environments. Supports both browser and Node.js.

## 🚀 Getting Started

1.  **Start the Backend**:
    First, build and run the HotPlex server from the root of this repository:
    ```bash
    make build
    ./dist/hotplexd
    ```

2.  **Integrate the SDK**:
    Choose the SDK for your language and point it to the server URL (default: `ws://localhost:8080/ws/v1/agent`).

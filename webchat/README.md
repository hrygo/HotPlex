# 🌐 Hotplex Web Chat

<p align="center">
  <strong>Premium Web Interface for Hotplex Worker Gateway</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Next.js-15.0-black?style=flat-square&logo=next.js" alt="Next.js">
  <img src="https://img.shields.io/badge/React-19.0-61DAFB?style=flat-square&logo=react" alt="React">
  <img src="https://img.shields.io/badge/Tailwind-4.0-38B2AC?style=flat-square&logo=tailwind-css" alt="Tailwind">
  <img src="https://img.shields.io/badge/AI_SDK-6.0-black?style=flat-square" alt="Vercel AI SDK">
</p>

---

Hotplex Web Chat is a state-of-the-art frontend implementation designed to showcase the full capabilities of the **Hotplex Worker Gateway**. Built with a focus on performance and developer experience, it provides a seamless browser-based interface for interacting with any AI coding agent.

## 🧱 Architecture

The Web Chat acts as a thin, reactive client that communicates with the Gateway over the Agent Event Protocol (AEP v1).

```mermaid
graph LR
    User([User Browser]) -->|WebSocket / AEP v1| GW[Hotplex Gateway]
    GW -->|Process Bridge| Worker[AI Coding Agent]
    Worker -->|NDJSON/SSE| GW
    GW -->|Streaming Events| User
```

## 🚀 Core Features

- 🔹 **Real-time Streaming**: Instant feedback for message deltas and tool call events.
- 🔹 **AEP v1 Native**: Full support for status synchronization, user permissions, and MCP elicitation.
- 🔹 **Adaptive UI**: Built with `@assistant-ui/react` and Tailwind 4 for a premium, responsive experience.
- 🔹 **Session Persistence**: Seamlessly resumes active sessions upon gateway reconnection.
- 🔹 **Modern Tooling**: Next.js 15 App Router, TypeScript, and Playwright E2E testing.

## ⚡ Quick Start

### 1. Requirements
Ensure the **Hotplex Gateway** is running locally:
```bash
# In the project root
make dev
```

### 2. Setup Web Chat
```bash
cd webchat
pnpm install
cp .env.example .env.local
pnpm dev
```

Visit [http://localhost:3000](http://localhost:3000) to start chatting.

## 🛠️ Configuration

Configure the application via `.env.local`:

| Variable | Description | Example |
|:---|:---|:---|
| `HOTPLEX_WS_URL` | Gateway WebSocket endpoint | `ws://localhost:8888/ws` |
| `HOTPLEX_WORKER_TYPE` | Default worker to spawn | `claude_code` |
| `HOTPLEX_AUTH_TOKEN` | JWT for authenticated access | `eyJhbGci...` |

## 💎 Development

### Available Scripts

- `pnpm dev`: Start the development server with hot-reload.
- `pnpm build`: Create a production-ready build of the application.
- `pnpm lint`: Run ESLint and TypeScript checks.
- `pnpm test:e2e`: Run end-to-end integration tests using Playwright.

### Testing
We use Playwright to ensure the WebSocket handshake and message streaming are working correctly.
```bash
pnpm test:e2e
```

## 📜 License
Distributed under the Apache License 2.0.

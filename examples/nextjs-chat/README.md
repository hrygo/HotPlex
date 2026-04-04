# Next.js Chat Example

Complete example using `@hotplex/ai-sdk-transport` with Next.js App Router.

## Setup

```bash
pnpm install
cp .env.example .env.local
pnpm dev
```

Open [http://localhost:3000](http://localhost:3000)

## Environment Variables

- `HOTPLEX_WS_URL` - WebSocket URL (default: ws://localhost:8888)
- `HOTPLEX_WORKER_TYPE` - Worker type (claude_code, opencode_cli, opencode_server)
- `HOTPLEX_AUTH_TOKEN` - Optional auth token

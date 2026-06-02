/**
 * HotPlex Gateway - Quick Start Example
 *
 * Minimal demo showing how to connect to the gateway and send a simple task.
 *
 * Usage:
 *   npx tsx examples/quickstart.ts
 *
 * Prerequisites:
 *   - HotPlex Gateway running at ws://localhost:8888/ws
 *   - Claude Code CLI installed and accessible
 */

import { HotPlexClient, WorkerType } from '../src/index.js';

// ── Configuration ────────────────────────────────────────────────────────

const GATEWAY_URL = 'ws://localhost:8888/ws';
const API_KEY = process.env.HOTPLEX_API_KEY || 'dev-api-key';

// ── Main ─────────────────────────────────────────────────────────────────

async function main() {
  console.log('🚀 HotPlex Gateway - Quick Start\n');

  const client = new HotPlexClient({
    url: GATEWAY_URL,
    workerType: WorkerType.ClaudeCode,
    apiKey: API_KEY,
  });

  // ── Event handlers ──────────────────────────────────────────────────

  client.on('delta', (data) => {
    process.stdout.write(data.content);
  });

  client.on('done', (data) => {
    console.log('\n\n✅ Task completed:', data.success);
    if (data.stats) {
      console.log(`   Duration: ${data.stats.duration_ms}ms`);
      console.log(`   Tokens: ${data.stats.total_tokens}`);
      console.log(`   Cost: $${data.stats.cost_usd}`);
    }
    client.disconnect();
  });

  client.on('error', (data) => {
    console.error('\n❌ Error:', data.code, '-', data.message);
    client.disconnect();
    process.exit(1);
  });

  // ── Graceful shutdown ───────────────────────────────────────────────

  const shutdown = () => {
    console.log('\n\nInterrupted. Disconnecting...');
    client.disconnect();
    process.exit(0);
  };
  process.on('SIGINT', shutdown);
  process.on('SIGTERM', shutdown);

  // ── Connect and run ─────────────────────────────────────────────────

  try {
    console.log('Connecting to gateway...');
    const ack = await client.connect();
    console.log(`Connected! Session: ${ack.session_id}\n`);
    console.log('Sending task...\n');

    // sendInputAsync sends the input and returns a Promise that resolves
    // when the 'done' event arrives (or rejects on 'error').
    await client.sendInputAsync(
      'Write a hello world program in Go that prints "Hello, World!" to stdout.',
    );

    console.log('\nAll done!');
  } catch (err) {
    console.error('Task failed:', err instanceof Error ? err.message : err);
    client.disconnect();
    process.exit(1);
  }
}

main();

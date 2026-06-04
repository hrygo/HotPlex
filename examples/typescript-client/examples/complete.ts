/**
 * HotPlex Gateway - Complete Example
 *
 * Full-featured demo showing all client capabilities:
 * - Session resume
 * - Streaming output with typing indicator
 * - Tool call monitoring
 * - Permission request handling
 * - Error recovery
 * - Stats display
 * - Graceful shutdown
 *
 * Usage:
 *   npx tsx examples/complete.ts
 *
 * Resume an existing session:
 *   HOTPLEX_SESSION_ID=sess_xxx npx tsx examples/complete.ts
 */

import * as readline from 'readline';
import { HotPlexClient, WorkerType, ErrorCode } from '../src/index.js';

// ── Configuration ────────────────────────────────────────────────────────

const CONFIG = {
  url: process.env.HOTPLEX_URL || 'ws://localhost:8888',
  sessionId: process.env.HOTPLEX_SESSION_ID,
  apiKey: process.env.HOTPLEX_API_KEY || 'dev-api-key',
} as const;

// ── Utility functions ────────────────────────────────────────────────────

function createTypingIndicator() {
  const frames = ['⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'];
  let i = 0;
  const interval = setInterval(() => {
    process.stdout.write(`\r${frames[i++ % frames.length]} Processing...`);
  }, 80);
  return {
    stop() {
      clearInterval(interval);
      process.stdout.write('\r' + ' '.repeat(10) + '\r');
    },
  };
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
}

function printConfig() {
  console.log('Configuration:');
  console.log(`   Gateway URL: ${CONFIG.url}`);
  if (CONFIG.sessionId) {
    console.log(`   Session: ${CONFIG.sessionId} (RESUME MODE)`);
  }
  console.log('');
}

// Tools that are auto-approved (no user confirmation needed)
const AUTO_APPROVE_TOOLS = ['read_file', 'grep', 'glob', 'bash'];

// ── Main ─────────────────────────────────────────────────────────────────

async function main() {
  // Execution flow (top to bottom):
  //   1. Create client and register ALL event handlers BEFORE connecting
  //      (avoids missing events that fire immediately after connect)
  //   2. Register shutdown handlers (SIGINT / SIGTERM)
  //   3. Connect to gateway (or resume existing session)
  //   4. Send task via sendInputAsync — returns a Promise that resolves
  //      on 'done' event or rejects on 'error' event
  console.log('═══════════════════════════════════════════════════════════════');
  console.log('      HotPlex Gateway - Complete Example');
  console.log('═══════════════════════════════════════════════════════════════\n');

  printConfig();

  const client = new HotPlexClient({
    url: CONFIG.url + '/ws',
    workerType: WorkerType.ClaudeCode,
    apiKey: CONFIG.apiKey,
    reconnect: {
      enabled: true,
      maxAttempts: 5,
      baseDelayMs: 1000,
      maxDelayMs: 30000,
    },
  });

  const typingIndicator = createTypingIndicator();
  let sessionId: string | null = null;

  // ══════════════════════════════════════════════════════════════════════
  // Event handlers — grouped by concern for readability
  // ══════════════════════════════════════════════════════════════════════

  // ── Connection lifecycle ────────────────────────────────────────────

  client.on('connected', (ack) => {
    typingIndicator.stop();
    sessionId = ack.session_id;
    console.log('\n✅ Connected to gateway');
    console.log(`   Session ID: ${sessionId}`);
    console.log(`   Worker: ${ack.server_caps.worker_type}`);
    console.log(`   Supports Resume: ${ack.server_caps.supports_resume}`);
    console.log('\n─────────────────────────────────────────────────────────────\n');
  });

  client.on('disconnected', (reason) => {
    typingIndicator.stop();
    console.log('\n📴 Disconnected:', reason);
  });

  client.on('reconnecting', (attempt) => {
    console.log(`\n🔄 Reconnecting (attempt ${attempt})...`);
  });

  // ── Session state ───────────────────────────────────────────────────

  client.on('state', (data) => {
    console.log(`\n📊 State changed: ${data.state}`);
  });

  // ── Content streaming ───────────────────────────────────────────────

  client.on('delta', (data) => {
    process.stdout.write(data.content);
  });

  client.on('message', (data) => {
    typingIndicator.stop();
    const preview = data.content.length > 100
      ? data.content.substring(0, 100) + '...'
      : data.content;
    console.log('\n📨 Full message received:');
    console.log(`   Role: ${data.role}`);
    console.log(`   Content: ${preview}`);
  });

  client.on('reasoning', (_data) => {
    // Display thinking process (commented out to reduce noise):
    // console.log('\n💭 Thinking:', data.content.substring(0, 50) + '...');
  });

  // ── Tool interaction ────────────────────────────────────────────────

  client.on('toolCall', (data) => {
    typingIndicator.stop();
    console.log('\n🔧 Tool called:');
    console.log(`   Tool: ${data.name}`);
    console.log(`   Args: ${JSON.stringify(data.input)}`);
  });

  client.on('toolResult', (data) => {
    console.log('📋 Tool result:');
    if (data.error) {
      console.log(`   ❌ Error: ${data.error}`);
    } else {
      const output = typeof data.output === 'string'
        ? data.output.substring(0, 100) + (data.output.length > 100 ? '...' : '')
        : JSON.stringify(data.output);
      console.log(`   Output: ${output}`);
    }
  });

  // ── Permissions ─────────────────────────────────────────────────────

  client.on('permissionRequest', (data) => {
    typingIndicator.stop();
    console.log('\n🔐 PERMISSION REQUEST:');
    console.log(`   Tool: ${data.tool_name}`);
    console.log(`   Description: ${data.description || 'N/A'}`);

    if (AUTO_APPROVE_TOOLS.includes(data.tool_name)) {
      console.log('   → Auto-approving (safe tool)');
      client.sendPermissionResponse(data.id, true);
    } else {
      console.log('   → Denying (potentially unsafe tool)');
      client.sendPermissionResponse(data.id, false, 'Tool not in auto-approve list');
    }
  });

  // ── Control plane ───────────────────────────────────────────────────

  client.on('throttle', (data) => {
    console.log('\n⚠️  Throttled by server');
    if (data.suggestion) {
      console.log(`   Max rate: ${data.suggestion.max_message_rate}`);
      console.log(`   Backoff: ${data.suggestion.backoff_ms}ms`);
    }
  });

  client.on('reconnect', (data) => {
    console.log('\n🔄 Server requested reconnect');
    console.log(`   Reason: ${data.reason}`);
  });

  client.on('sessionInvalid', (data) => {
    console.log('\n🚫 Session invalidated');
    console.log(`   Reason: ${data.reason}`);
    console.log(`   Recoverable: ${data.recoverable}`);
  });

  // ── Task lifecycle ──────────────────────────────────────────────────

  client.on('done', (data) => {
    typingIndicator.stop();
    console.log('\n\n═══════════════════════════════════════════════════════════════');
    console.log('                        TASK COMPLETED');
    console.log('═══════════════════════════════════════════════════════════════');
    console.log(`\n   Success: ${data.success ? '✅' : '❌'}`);

    if (data.stats) {
      console.log('\n   📈 Statistics:');
      console.log(`      Duration:    ${formatDuration(data.stats.duration_ms || 0)}`);
      console.log(`      Tool Calls:  ${data.stats.tool_calls || 0}`);
      console.log(`      Input Tokens:  ${data.stats.input_tokens?.toLocaleString() || 'N/A'}`);
      console.log(`      Output Tokens: ${data.stats.output_tokens?.toLocaleString() || 'N/A'}`);
      if (data.stats.cache_read_tokens) {
        console.log(`      Cache Hits:   ${data.stats.cache_read_tokens.toLocaleString()}`);
      }
      console.log(`      Total Tokens: ${data.stats.total_tokens?.toLocaleString() || 'N/A'}`);
      if (data.stats.cost_usd) {
        console.log(`      Cost:         $${data.stats.cost_usd.toFixed(4)}`);
      }
      if (data.stats.model) {
        console.log(`      Model:        ${data.stats.model}`);
      }
    }

    if (sessionId) {
      console.log(`\n   💾 Session ID for resume: ${sessionId}`);
      console.log(`\n   Resume with: HOTPLEX_SESSION_ID=${sessionId} npx tsx examples/complete.ts`);
    }
    console.log('═══════════════════════════════════════════════════════════════\n');

    client.disconnect();
  });

  client.on('error', (data) => {
    typingIndicator.stop();
    console.error('\n\n❌ ERROR:');
    console.error(`   Code: ${data.code}`);
    console.error(`   Message: ${data.message}`);

    if (data.code === ErrorCode.SessionBusy) {
      console.error('   Note: Session is busy, will auto-retry...');
      return; // let the client's built-in retry handle it
    }

    if (data.code === ErrorCode.Unauthorized) {
      console.error('   Note: Authentication required. Set HOTPLEX_API_KEY env var.');
    }

    if (data.details) {
      console.error('   Details:', JSON.stringify(data.details, null, 2));
    }
  });

  // ══════════════════════════════════════════════════════════════════════
  // Graceful shutdown
  // ══════════════════════════════════════════════════════════════════════

  const rl = readline.createInterface({
    input: process.stdin,
    output: process.stdout,
  });

  const shutdown = (signal: string) => {
    console.log(`\n\nReceived ${signal}. Shutting down gracefully...`);
    typingIndicator.stop();

    if (sessionId) {
      console.log('Terminating session on server...');
      client.terminate();
    }

    setTimeout(() => {
      client.disconnect();
      rl.close();
      process.exit(0);
    }, 1000);
  };

  process.on('SIGINT', () => shutdown('SIGINT'));
  process.on('SIGTERM', () => shutdown('SIGTERM'));

  // ══════════════════════════════════════════════════════════════════════
  // Connect and run
  //
  // Two connection modes:
  //   connect()  → creates a new session, gateway spawns a fresh worker
  //   resume(id) → reattaches to an existing session (worker state preserved)
  //
  // sendInputAsync(content) sends user input and returns a Promise that:
  //   resolves when 'done' event arrives (task completed)
  //   rejects  when 'error' event arrives (task failed)
  // This is the simplest way to run a task end-to-end.
  // ══════════════════════════════════════════════════════════════════════

  try {
    console.log('Connecting to gateway...');

    if (CONFIG.sessionId) {
      console.log(`Resuming session: ${CONFIG.sessionId}`);
      await client.resume(CONFIG.sessionId);
    } else {
      await client.connect();
    }

    const task = process.env.HOTPLEX_TASK ||
      'Create a simple HTTP server in Go that handles GET /health returning 200 OK ' +
      'with JSON body {"status":"ok"}. Include proper error handling and a main ' +
      'function that starts the server on port 8080.';

    console.log('\n📤 Sending task...\n');
    await client.sendInputAsync(task);
  } catch (err) {
    console.error('\n❌ Task failed:', err instanceof Error ? err.message : err);
    client.disconnect();
    process.exit(1);
  }
}

main();

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Mock WebSocket
class MockWebSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSING = 2;
  static CLOSED = 3;

  readyState = MockWebSocket.OPEN;
  url: string;
  onopen: ((event: any) => void) | null = null;
  onmessage: ((event: any) => void) | null = null;
  onerror: ((event: any) => void) | null = null;
  onclose: ((event: any) => void) | null = null;

  constructor(url: string) {
    this.url = url;
  }

  send = vi.fn();
  close = vi.fn();

  // Helper to simulate server message
  simulateMessage(data: string) {
    if (this.onmessage) {
      this.onmessage({ data });
    }
  }

  simulateClose(code = 1000, reason = '') {
    if (this.onclose) {
      this.onclose({ code, reason });
    }
  }
}

// Mock global WebSocket
vi.stubGlobal('WebSocket', MockWebSocket);

describe('BrowserHotPlexClient', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('should export BrowserHotPlexClient', async () => {
    const { BrowserHotPlexClient } = await import('../client/browser-client.js');
    expect(BrowserHotPlexClient).toBeDefined();
  });

  it('should create client with config', async () => {
    const { BrowserHotPlexClient } = await import('../client/browser-client.js');

    const client = new BrowserHotPlexClient({
      url: 'ws://localhost:8888',
      workerType: 'claude_code',
    });

    expect(client).toBeDefined();
    expect(client.connected).toBe(false);
  });

  it('should export constants', async () => {
    const {
      EventKind,
      SessionState,
      ErrorCode,
      WorkerType,
      AEP_VERSION,
    } = await import('../client/constants.js');

    expect(EventKind.MessageStart).toBe('message.start');
    expect(EventKind.MessageDelta).toBe('message.delta');
    expect(EventKind.MessageEnd).toBe('message.end');
    expect(EventKind.Done).toBe('done');
    expect(EventKind.Error).toBe('error');

    expect(SessionState.Created).toBe('created');
    expect(SessionState.Running).toBe('running');
    expect(SessionState.Idle).toBe('idle');

    expect(WorkerType.ClaudeCode).toBe('claude_code');
    expect(WorkerType.OpenCodeCLI).toBe('opencode_cli');

    expect(AEP_VERSION).toBe('aep/v1');
  });

  it('should export type definitions', async () => {
    // Types are stripped at runtime, but we can verify the module loads
    const types = await import('../client/types.js');
    // The module should exist and have expected structure
    expect(types).toBeDefined();
    // ErrorData interface should be defined (verified by TypeScript at compile time)
  });
});

describe('envelope utilities', () => {
  it('should create init envelope', async () => {
    const { createInitEnvelope } = await import('../client/envelope.js');

    const env = createInitEnvelope('sess_test', 'claude_code');

    expect(env.version).toBe('aep/v1');
    expect(env.session_id).toBe('sess_test');
    expect(env.seq).toBe(1); // seq must be ≥ 1 per AEP spec
    expect(env.timestamp).toBeGreaterThan(0); // timestamp must be positive unix-ms
    expect(env.event.type).toBe('init'); // must be 'init', not 'control'
    expect(env.event.data.worker_type).toBe('claude_code');
  });

  it('should create input envelope', async () => {
    const { createInputEnvelope } = await import('../client/envelope.js');

    const env = createInputEnvelope('sess_test', 'Hello, world!');

    expect(env.event.type).toBe('input');
    expect((env.event.data as any).content).toBe('Hello, world!');
    expect(env.seq).toBe(1); // seq must be ≥ 1 per AEP spec
  });

  it('should serialize and deserialize envelope', async () => {
    const { createInitEnvelope, serializeEnvelope, deserializeEnvelope } = await import('../client/envelope.js');

    const env = createInitEnvelope('sess_test', 'claude_code');
    const serialized = serializeEnvelope(env);
    const deserialized = deserializeEnvelope(serialized);

    expect(deserialized.version).toBe(env.version);
    expect(deserialized.session_id).toBe(env.session_id);
    expect(deserialized.event.type).toBe(env.event.type);
  });

  it('should generate unique event IDs', async () => {
    const { newEventId, newSessionId } = await import('../client/envelope.js');

    const eventId = newEventId();
    const sessionId = newSessionId();

    expect(eventId).toMatch(/^evt_/);
    expect(sessionId).toMatch(/^sess_/);
    expect(eventId).not.toBe(newEventId());
  });
});

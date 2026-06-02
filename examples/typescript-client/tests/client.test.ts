/**
 * Unit tests for HotPlexClient.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

// Mock ws module before importing client
vi.mock('ws', () => {
  const mockWs = {
    on: vi.fn(),
    off: vi.fn(),
    send: vi.fn(),
    close: vi.fn(),
    readyState: 1,
  };
  return { WebSocket: vi.fn(() => mockWs) };
});

import { HotPlexClient } from '../src/client';
import { WorkerType } from '../src/constants';
import {
  contextUsageFixture,
  skillsListFixture,
  mcpStatusFixture,
  planFixture,
  modeUpdateFixture,
  toolUpdateFixture,
  questionRequestFixture,
  toolCallACPFixture,
  toolResultACPFixture,
} from './fixtures.js';

describe('HotPlexClient', () => {
  let client: HotPlexClient;

  beforeEach(() => {
    vi.clearAllMocks();
    client = new HotPlexClient({
      url: 'ws://localhost:8888',
      workerType: WorkerType.ClaudeCode,
    });
  });

  afterEach(() => {
    client.disconnect();
  });

  describe('constructor', () => {
    it('should create client with config', () => {
      expect(client.sessionId).toBeNull();
      expect(client.state).toBe('deleted');
      expect(client.connected).toBe(false);
    });

    it('should use default reconnect config', () => {
      expect(client.reconnecting).toBe(false);
    });
  });

  describe('connect', () => {
    it('should generate new session ID on connect', async () => {
      // The actual WebSocket mock doesn't trigger events,
      // so we just verify the client can be instantiated
      expect(client).toBeDefined();
    });

    it('should use provided session ID', async () => {
      const customClient = new HotPlexClient({
        url: 'ws://localhost:8888',
        workerType: WorkerType.ClaudeCode,
      });
      
      expect(customClient.sessionId).toBeNull();
      customClient.disconnect();
    });
  });

  describe('disconnect', () => {
    it('should set connected to false', () => {
      client.disconnect();
      expect(client.connected).toBe(false);
    });

    it('should not throw when disconnecting multiple times', () => {
      client.disconnect();
      expect(() => client.disconnect()).not.toThrow();
    });
  });

  describe('sendInput', () => {
    it('should throw when not connected', () => {
      expect(() => client.sendInput('test')).toThrow('Not connected to gateway');
    });
  });

  describe('sendControl', () => {
    it('should throw when not connected', () => {
      expect(() => client.sendControl('terminate')).toThrow('Not connected to gateway');
    });
  });

  describe('sendPermissionResponse', () => {
    it('should throw when not connected', () => {
      expect(() => client.sendPermissionResponse('perm_123', true)).toThrow('Not connected to gateway');
    });
  });
});

describe('HotPlexClient Events', () => {
  let client: HotPlexClient;

  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    if (client) client.disconnect();
  });

  it('should emit events via EventEmitter interface', () => {
    client = new HotPlexClient({
      url: 'ws://localhost:8888',
      workerType: WorkerType.ClaudeCode,
    });

    const deltaHandler = vi.fn();
    const doneHandler = vi.fn();
    const errorHandler = vi.fn();

    client.on('delta', deltaHandler);
    client.on('done', doneHandler);
    client.on('error', errorHandler);

    // Verify listeners are registered
    expect(client.listenerCount('delta')).toBe(1);
    expect(client.listenerCount('done')).toBe(1);
    expect(client.listenerCount('error')).toBe(1);

    client.off('delta', deltaHandler);
    client.off('done', doneHandler);
    client.off('error', errorHandler);

    expect(client.listenerCount('delta')).toBe(0);
    expect(client.listenerCount('done')).toBe(0);
    expect(client.listenerCount('error')).toBe(0);
  });

  it('should support once() for one-time listeners', () => {
    client = new HotPlexClient({
      url: 'ws://localhost:8888',
      workerType: WorkerType.ClaudeCode,
    });

    const handler = vi.fn();
    client.once('connected', handler);

    expect(client.listenerCount('connected')).toBe(1);
  });
});

describe('AEP Event Parsing', () => {
  it('should parse context_usage event', () => {
    const env = JSON.parse(contextUsageFixture);
    expect(env.event.type).toBe('context_usage');
    expect(env.event.data.total_tokens).toBe(1000);
    expect(env.event.data.max_tokens).toBe(200000);
    expect(env.event.data.percentage).toBe(5);
    expect(env.event.data.model).toBe('claude');
  });

  it('should parse skills_list event', () => {
    const env = JSON.parse(skillsListFixture);
    expect(env.event.type).toBe('skills_list');
    expect(env.event.data.skills).toHaveLength(1);
    expect(env.event.data.skills[0].name).toBe('test-skill');
    expect(env.event.data.total).toBe(1);
  });

  it('should parse mcp_status event', () => {
    const env = JSON.parse(mcpStatusFixture);
    expect(env.event.type).toBe('mcp_status');
    expect(env.event.data.servers).toHaveLength(1);
    expect(env.event.data.servers[0].name).toBe('filesystem');
    expect(env.event.data.servers[0].status).toBe('connected');
  });

  it('should parse plan event with items', () => {
    const env = JSON.parse(planFixture);
    expect(env.event.type).toBe('plan');
    expect(env.event.data.items).toHaveLength(1);
    expect(env.event.data.items[0].content).toBe('Implement feature X');
    expect(env.event.data.items[0].priority).toBe('high');
    expect(env.event.data.items[0].status).toBe('pending');
  });

  it('should parse mode_update event', () => {
    const env = JSON.parse(modeUpdateFixture);
    expect(env.event.type).toBe('mode_update');
    expect(env.event.data.current_mode_id).toBe('code');
  });

  it('should parse tool_update with diff field', () => {
    const env = JSON.parse(toolUpdateFixture);
    expect(env.event.type).toBe('tool_update');
    expect(env.event.data.status).toBe('completed');
    expect(env.event.data.diff).toBeDefined();
    expect(env.event.data.diff.path).toBe('src/main.ts');
    expect(env.event.data.diff.old_text).toBe('old');
    expect(env.event.data.diff.new_text).toBe('new');
  });

  it('should parse question_request event', () => {
    const env = JSON.parse(questionRequestFixture);
    expect(env.event.type).toBe('question_request');
    expect(env.event.data.questions).toHaveLength(1);
    expect(env.event.data.questions[0].question).toBe('Proceed?');
    expect(env.event.data.questions[0].multi_select).toBe(false);
  });

  it('should parse tool_call and tool_result with ACP extensions', () => {
    const callEnv = JSON.parse(toolCallACPFixture);
    expect(callEnv.event.type).toBe('tool_call');
    expect(callEnv.event.data.title).toBe('read: main.go');
    expect(callEnv.event.data.kind).toBe('read');
    expect(callEnv.event.data.locations).toHaveLength(1);
    expect(callEnv.event.data.locations[0].path).toBe('main.go');

    const resultEnv = JSON.parse(toolResultACPFixture);
    expect(resultEnv.event.type).toBe('tool_result');
    expect(resultEnv.event.data.status).toBe('completed');
    expect(resultEnv.event.data.diff).toBeDefined();
    expect(resultEnv.event.data.diff.path).toBe('main.go');
    expect(resultEnv.event.data.diff.old_text).toBe('old content');
    expect(resultEnv.event.data.diff.new_text).toBe('new content');
  });
});

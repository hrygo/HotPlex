/**
 * Test fixtures for HotPlex Client SDK tests.
 */

import type { Envelope } from '../src/constants.js';

// Sample Envelopes for testing
export const sampleInitAckEnvelope: Envelope = {
  version: 'aep/v1',
  id: 'evt_test-1234',
  seq: 1,
  session_id: 'sess_test-session',
  timestamp: Date.now(),
  event: {
    type: 'init_ack',
    data: {
      session_id: 'sess_test-session',
      state: 'idle',
      server_caps: {
        protocol_version: 'aep/v1',
        worker_type: 'claude_code',
        supports_resume: true,
        supports_delta: true,
        supports_tool_call: true,
        supports_ping: true,
        max_frame_size: 32768,
        modalities: ['text', 'code'],
        tools: ['read_file', 'write_file', 'bash'],
      },
    },
  },
};

export const sampleStateEnvelope: Envelope = {
  version: 'aep/v1',
  id: 'evt_state-1234',
  seq: 2,
  session_id: 'sess_test-session',
  timestamp: Date.now(),
  event: {
    type: 'state',
    data: {
      state: 'running',
      message: '',
    },
  },
};

export const sampleDeltaEnvelope: Envelope = {
  version: 'aep/v1',
  id: 'evt_delta-1234',
  seq: 3,
  session_id: 'sess_test-session',
  timestamp: Date.now(),
  event: {
    type: 'message.delta',
    data: {
      message_id: 'msg_123',
      content: 'Hello',
    },
  },
};

export const sampleErrorEnvelope: Envelope = {
  version: 'aep/v1',
  id: 'evt_error-1234',
  seq: 4,
  session_id: 'sess_test-session',
  timestamp: Date.now(),
  event: {
    type: 'error',
    data: {
      code: 'WORKER_CRASH',
      message: 'Worker process crashed',
      event_id: 'evt_123',
    },
  },
};

export const sampleDoneEnvelope: Envelope = {
  version: 'aep/v1',
  id: 'evt_done-1234',
  seq: 5,
  session_id: 'sess_test-session',
  timestamp: Date.now(),
  event: {
    type: 'done',
    data: {
      success: true,
      stats: {
        duration_ms: 5200,
        tool_calls: 3,
        input_tokens: 1000,
        output_tokens: 500,
        total_tokens: 1500,
        cost_usd: 0.05,
        model: 'claude-sonnet-4-6',
      },
    },
  },
};

// ============================================================================
// NDJSON fixture strings for event parsing tests
// ============================================================================

export const contextUsageFixture = JSON.stringify({
  version: 'aep/v1',
  id: 'evt_cu1',
  seq: 10,
  session_id: 'sess_test',
  timestamp: 1000,
  event: { type: 'context_usage', data: { total_tokens: 1000, max_tokens: 200000, percentage: 5, model: 'claude' } }
});

export const skillsListFixture = JSON.stringify({
  version: 'aep/v1',
  id: 'evt_sl1',
  seq: 11,
  session_id: 'sess_test',
  timestamp: 1001,
  event: { type: 'skills_list', data: { skills: [{ name: 'test-skill', description: 'A test skill', source: 'project' }], total: 1 } }
});

export const mcpStatusFixture = JSON.stringify({
  version: 'aep/v1',
  id: 'evt_ms1',
  seq: 12,
  session_id: 'sess_test',
  timestamp: 1002,
  event: { type: 'mcp_status', data: { servers: [{ name: 'filesystem', status: 'connected' }] } }
});

export const planFixture = JSON.stringify({
  version: 'aep/v1',
  id: 'evt_pl1',
  seq: 13,
  session_id: 'sess_test',
  timestamp: 1003,
  event: { type: 'plan', data: { items: [{ content: 'Implement feature X', priority: 'high', status: 'pending' }] } }
});

export const modeUpdateFixture = JSON.stringify({
  version: 'aep/v1',
  id: 'evt_mu1',
  seq: 14,
  session_id: 'sess_test',
  timestamp: 1004,
  event: { type: 'mode_update', data: { current_mode_id: 'code' } }
});

export const toolUpdateFixture = JSON.stringify({
  version: 'aep/v1',
  id: 'evt_tu1',
  seq: 15,
  session_id: 'sess_test',
  timestamp: 1005,
  event: { type: 'tool_update', data: { id: 'call_123', status: 'completed', diff: { path: 'src/main.ts', old_text: 'old', new_text: 'new' } } }
});

export const questionRequestFixture = JSON.stringify({
  version: 'aep/v1',
  id: 'evt_qr1',
  seq: 16,
  session_id: 'sess_test',
  timestamp: 1006,
  event: { type: 'question_request', data: { id: 'q_1', questions: [{ question: 'Proceed?', header: 'Confirm', options: [{ label: 'Yes' }, { label: 'No' }], multi_select: false }] } }
});

export const toolCallACPFixture = JSON.stringify({
  version: 'aep/v1',
  id: 'evt_tc1',
  seq: 17,
  session_id: 'sess_test',
  timestamp: 1007,
  event: { type: 'tool_call', data: { id: 'call_1', name: 'read_file', input: { path: 'main.go' }, title: 'read: main.go', kind: 'read', locations: [{ path: 'main.go', line: 10 }] } }
});

export const toolResultACPFixture = JSON.stringify({
  version: 'aep/v1',
  id: 'evt_tr1',
  seq: 18,
  session_id: 'sess_test',
  timestamp: 1008,
  event: { type: 'tool_result', data: { id: 'call_1', output: 'file content', status: 'completed', diff: { path: 'main.go', old_text: 'old content', new_text: 'new content' } } }
});

export const mockWebSocket = {
  on: vi.fn(),
  off: vi.fn(),
  send: vi.fn(),
  close: vi.fn(),
  readyState: 1, // WebSocket.OPEN
};

export const createMockWebSocket = () => {
  const ws = {
    on: vi.fn((event, callback) => { ws[`on${event}`] = callback; }),
    off: vi.fn((event, callback) => { delete ws[`on${event}`]; }),
    send: vi.fn(),
    close: vi.fn(),
    readyState: 1,
  };
  return ws;
};

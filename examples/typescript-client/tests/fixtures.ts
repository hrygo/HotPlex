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

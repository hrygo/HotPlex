/**
 * Unit tests for envelope helpers.
 */

import { describe, it, expect } from 'vitest';
import {
  generateUUID,
  newEventId,
  newSessionId,
  createEnvelope,
  createInitEnvelope,
  createInputEnvelope,
  createPingEnvelope,
  createControlEnvelope,
  serializeEnvelope,
  deserializeEnvelope,
  isInitAck,
  isError,
  isState,
  isDone,
  isDelta,
  isControl,
} from '../src/envelope';
import { EventKind, AEP_VERSION, EVENT_ID_PREFIX, SESSION_ID_PREFIX } from '../src/constants';

describe('Envelope Helpers', () => {
  describe('generateUUID', () => {
    it('should generate a valid UUID v4 format', () => {
      const uuid = generateUUID();
      const uuidRegex = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
      expect(uuid).toMatch(uuidRegex);
    });

    it('should generate unique UUIDs', () => {
      const uuid1 = generateUUID();
      const uuid2 = generateUUID();
      expect(uuid1).not.toBe(uuid2);
    });
  });

  describe('newEventId', () => {
    it('should generate event ID with evt_ prefix', () => {
      const id = newEventId();
      expect(id).toMatch(new RegExp(`^${EVENT_ID_PREFIX}`));
    });

    it('should generate unique event IDs', () => {
      const id1 = newEventId();
      const id2 = newEventId();
      expect(id1).not.toBe(id2);
    });
  });

  describe('newSessionId', () => {
    it('should generate session ID with sess_ prefix', () => {
      const id = newSessionId();
      expect(id).toMatch(new RegExp(`^${SESSION_ID_PREFIX}`));
    });
  });

  describe('createEnvelope', () => {
    it('should create envelope with correct structure', () => {
      const env = createEnvelope('evt_123', 'sess_456', 1, EventKind.Input, { content: 'test' });
      
      expect(env.version).toBe(AEP_VERSION);
      expect(env.id).toBe('evt_123');
      expect(env.session_id).toBe('sess_456');
      expect(env.seq).toBe(1);
      expect(env.event.type).toBe(EventKind.Input);
      expect(env.event.data).toEqual({ content: 'test' });
    });

    it('should include timestamp', () => {
      const before = Date.now();
      const env = createEnvelope('evt_123', 'sess_456', 1, EventKind.Input, {});
      const after = Date.now();
      
      expect(env.timestamp).toBeGreaterThanOrEqual(before);
      expect(env.timestamp).toBeLessThanOrEqual(after);
    });

    it('should set priority when provided', () => {
      const env = createEnvelope('evt_123', 'sess_456', 1, EventKind.Control, {}, 'control');
      expect(env.priority).toBe('control');
    });
  });

  describe('createInitEnvelope', () => {
    it('should create init envelope with correct structure', () => {
      const env = createInitEnvelope(undefined, 'claude_code', undefined, 'test-token');
      
      expect(env.version).toBe(AEP_VERSION);
      expect(env.event.type).toBe(EventKind.Control);
      expect(env.event.data.version).toBe(AEP_VERSION);
      expect(env.event.data.worker_type).toBe('claude_code');
      expect(env.event.data.auth?.token).toBe('test-token');
      expect(env.event.data.client_caps?.supports_delta).toBe(true);
    });

    it('should include session_id when provided', () => {
      const env = createInitEnvelope('sess_existing', 'claude_code');
      expect(env.event.data.session_id).toBe('sess_existing');
    });
  });

  describe('createInputEnvelope', () => {
    it('should create input envelope with content', () => {
      const env = createInputEnvelope('sess_123', 'Hello world');
      
      expect(env.session_id).toBe('sess_123');
      expect(env.event.type).toBe(EventKind.Input);
      expect(env.event.data.content).toBe('Hello world');
    });

    it('should include metadata when provided', () => {
      const env = createInputEnvelope('sess_123', 'Hello', { key: 'value' });
      expect(env.event.data.metadata).toEqual({ key: 'value' });
    });
  });

  describe('createPingEnvelope', () => {
    it('should create ping envelope', () => {
      const env = createPingEnvelope('sess_123');
      
      expect(env.session_id).toBe('sess_123');
      expect(env.event.type).toBe(EventKind.Ping);
      expect(env.event.data).toEqual({});
    });
  });

  describe('createControlEnvelope', () => {
    it('should create terminate control envelope', () => {
      const env = createControlEnvelope('sess_123', 'terminate');
      expect(env.event.data.action).toBe('terminate');
    });

    it('should create delete control envelope', () => {
      const env = createControlEnvelope('sess_123', 'delete');
      expect(env.event.data.action).toBe('delete');
    });
  });

  describe('serializeEnvelope / deserializeEnvelope', () => {
    it('should serialize and deserialize envelope correctly', () => {
      const original = createEnvelope('evt_123', 'sess_456', 1, EventKind.Input, { content: 'test' });
      const serialized = serializeEnvelope(original);
      const deserialized = deserializeEnvelope(serialized.trim());
      
      expect(deserialized.id).toBe(original.id);
      expect(deserialized.session_id).toBe(original.session_id);
      expect(deserialized.seq).toBe(original.seq);
      expect(deserialized.event.type).toBe(original.event.type);
      expect(deserialized.event.data).toEqual(original.event.data);
    });

    it('should handle NDJSON format with trailing newline', () => {
      const env = createEnvelope('evt_123', 'sess_456', 1, EventKind.Input, {});
      const line = serializeEnvelope(env);
      expect(line.endsWith('\n')).toBe(true);
    });
  });

  describe('isInitAck', () => {
    it('should return true for init_ack envelopes', () => {
      const env = { ...createEnvelope('evt_1', 'sess_1', 1, 'init_ack', {}), event: { type: 'init_ack', data: {} } };
      expect(isInitAck(env)).toBe(true);
    });

    it('should return false for other envelopes', () => {
      const env = createEnvelope('evt_1', 'sess_1', 1, EventKind.Input, {});
      expect(isInitAck(env)).toBe(false);
    });
  });

  describe('isError', () => {
    it('should return true for error envelopes', () => {
      const env = createEnvelope('evt_1', 'sess_1', 1, EventKind.Error, {});
      expect(isError(env)).toBe(true);
    });
  });

  describe('isDelta', () => {
    it('should return true for message.delta envelopes', () => {
      const env = createEnvelope('evt_1', 'sess_1', 1, EventKind.MessageDelta, {});
      expect(isDelta(env)).toBe(true);
    });
  });

  describe('isDone', () => {
    it('should return true for done envelopes', () => {
      const env = createEnvelope('evt_1', 'sess_1', 1, EventKind.Done, {});
      expect(isDone(env)).toBe(true);
    });
  });

  describe('isControl', () => {
    it('should return true for control envelopes', () => {
      const env = createEnvelope('evt_1', 'sess_1', 1, EventKind.Control, {});
      expect(isControl(env)).toBe(true);
    });
  });
});

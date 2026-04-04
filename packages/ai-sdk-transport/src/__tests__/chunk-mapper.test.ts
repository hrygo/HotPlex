import { describe, it, expect, vi, beforeEach } from 'vitest';
import {
  mapMessageStart,
  mapMessageDelta,
  mapMessageEnd,
  mapToolCall,
  mapToolResult,
  mapReasoning,
  mapStep,
  mapDone,
  mapErrorToDataStream,
  type DataStreamWriter,
} from '../transport/chunk-mapper.js';

describe('chunk-mapper', () => {
  let mockWriter: DataStreamWriter;

  beforeEach(() => {
    mockWriter = {
      writeData: vi.fn(),
    };
  });

  describe('mapMessageStart', () => {
    it('should write text-start event', () => {
      mapMessageStart(mockWriter, { id: 'msg_123', role: 'assistant', content_type: 'text' });

      expect(mockWriter.writeData).toHaveBeenCalledWith({
        type: 'text-start',
        id: 'msg_123',
      });
    });

    it('should ignore empty data', () => {
      mapMessageStart(mockWriter, { id: '', role: 'assistant', content_type: 'text' });
      expect(mockWriter.writeData).not.toHaveBeenCalled();
    });

    it('should ignore null data', () => {
      mapMessageStart(mockWriter, null as any);
      expect(mockWriter.writeData).not.toHaveBeenCalled();
    });
  });

  describe('mapMessageDelta', () => {
    it('should write text-delta event', () => {
      mapMessageDelta(mockWriter, { message_id: 'msg_123', content: 'Hello' });

      expect(mockWriter.writeData).toHaveBeenCalledWith({
        type: 'text-delta',
        id: 'msg_123',
        delta: 'Hello',
      });
    });

    it('should ignore empty content', () => {
      mapMessageDelta(mockWriter, { message_id: 'msg_123', content: '' });
      expect(mockWriter.writeData).not.toHaveBeenCalled();
    });
  });

  describe('mapMessageEnd', () => {
    it('should write text-end event', () => {
      mapMessageEnd(mockWriter, { message_id: 'msg_123' });

      expect(mockWriter.writeData).toHaveBeenCalledWith({
        type: 'text-end',
        id: 'msg_123',
      });
    });
  });

  describe('mapToolCall', () => {
    it('should write tool-input-start and tool-input-delta events', () => {
      const data = {
        id: 'tool_1',
        name: 'read_file',
        input: { path: '/test.txt' },
      };

      mapToolCall(mockWriter, data);

      expect(mockWriter.writeData).toHaveBeenCalledTimes(2);
      expect(mockWriter.writeData).toHaveBeenNthCalledWith(1, {
        type: 'tool-input-start',
        toolCallId: 'tool_1',
        toolName: 'read_file',
      });
      expect(mockWriter.writeData).toHaveBeenNthCalledWith(2, {
        type: 'tool-input-delta',
        toolCallId: 'tool_1',
        input: { path: '/test.txt' },
      });
    });
  });

  describe('mapToolResult', () => {
    it('should write tool-result with output', () => {
      mapToolResult(mockWriter, { id: 'tool_1', output: { content: 'file contents' } });

      expect(mockWriter.writeData).toHaveBeenCalledWith({
        type: 'tool-result',
        toolCallId: 'tool_1',
        result: { content: 'file contents' },
      });
    });

    it('should write tool-result with error', () => {
      mapToolResult(mockWriter, { id: 'tool_1', output: null, error: 'File not found' });

      expect(mockWriter.writeData).toHaveBeenCalledWith({
        type: 'tool-result',
        toolCallId: 'tool_1',
        result: 'File not found',
      });
    });
  });

  describe('mapReasoning', () => {
    it('should write reasoning-delta event', () => {
      mapReasoning(mockWriter, { id: 'reason_1', content: 'Let me think...' });

      expect(mockWriter.writeData).toHaveBeenCalledWith({
        type: 'reasoning-delta',
        id: 'reason_1',
        delta: 'Let me think...',
      });
    });
  });

  describe('mapStep', () => {
    it('should write start-step event', () => {
      mapStep(mockWriter, { id: 'step_1', step_type: 'tool_use', parent_id: 'parent_1' });

      expect(mockWriter.writeData).toHaveBeenCalledWith({
        type: 'start-step',
        stepType: 'tool_use',
        parentId: 'parent_1',
      });
    });
  });

  describe('mapDone', () => {
    it('should write finish with stop reason on success', () => {
      mapDone(mockWriter, { success: true });

      expect(mockWriter.writeData).toHaveBeenCalledWith({
        type: 'finish',
        reason: 'stop',
      });
    });

    it('should write finish with error reason on failure', () => {
      mapDone(mockWriter, { success: false });

      expect(mockWriter.writeData).toHaveBeenCalledWith({
        type: 'finish',
        reason: 'error',
      });
    });
  });

  describe('mapErrorToDataStream', () => {
    it('should write error event', () => {
      mapErrorToDataStream(mockWriter, { code: 'WORKER_CRASH', message: 'Worker exited' });

      expect(mockWriter.writeData).toHaveBeenCalledWith({
        type: 'error',
        error: {
          code: 'WORKER_CRASH',
          message: 'AI worker crashed unexpectedly. Please retry.',
        },
      });
    });

    it('should ignore SESSION_BUSY errors', () => {
      mapErrorToDataStream(mockWriter, { code: 'SESSION_BUSY', message: 'Session busy' });

      expect(mockWriter.writeData).not.toHaveBeenCalled();
    });

    it('should use default message for unknown codes', () => {
      mapErrorToDataStream(mockWriter, { code: 'UNKNOWN_CODE', message: 'Custom error' });

      expect(mockWriter.writeData).toHaveBeenCalledWith({
        type: 'error',
        error: {
          code: 'UNKNOWN_CODE',
          message: 'Custom error',
        },
      });
    });
  });
});

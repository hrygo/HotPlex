/**
 * Client-side BrowserHotPlexClient for direct WebSocket connections.
 */

export { BrowserHotPlexClient } from './browser-client.js';
export type { BrowserClientEvents } from './browser-client.js';

// Re-export types and constants
export type {
  HotPlexClientConfig,
  ReconnectConfig,
  HeartbeatConfig,
  ClientState,
  Envelope,
  Event,
  ErrorData,
  StateData,
  InputData,
  MessageStartData,
  MessageDeltaData,
  MessageEndData,
  ToolCallData,
  ToolResultData,
  ReasoningData,
  StepData,
  PermissionRequestData,
  DoneData,
} from './types.js';

export {
  EventKind,
  SessionState,
  ErrorCode,
  ControlAction,
  WorkerType,
  AEP_VERSION,
} from './constants.js';

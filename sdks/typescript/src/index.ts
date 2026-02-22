export { HotPlexClient } from './client';
export type { Config, ClientConfig } from './config';
export { defaultClientConfig } from './config';
export type { Event, EventMeta, SessionStats, EventType } from './events';
export { parseEvent, parseSessionStats } from './events';
export {
  HotPlexError,
  ConnectionError,
  TimeoutError,
  ExecutionError,
  DangerBlockedError,
  SessionError,
  SessionNotFoundError,
  SessionDeadError,
} from './errors';

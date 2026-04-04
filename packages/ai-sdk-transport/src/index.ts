/**
 * @hotplex/ai-sdk-transport
 *
 * AI SDK ChatTransport adapter for HotPlex Worker Gateway (AEP v1 over WebSocket).
 *
 * @example
 * ```typescript
 * // Server-side API route (Next.js App Router)
 * import { createHotPlexHandler } from '@hotplex/ai-sdk-transport/server';
 *
 * export async function POST(req: Request) {
 *   const body = await req.json();
 *   return createHotPlexHandler({
 *     url: process.env.HOTPLEX_WS_URL!,
 *     workerType: 'claude_code',
 *   })(body);
 * }
 * ```
 *
 * @example
 * ```tsx
 * // Client-side component
 * import { useChat } from '@ai-sdk/react';
 *
 * function Chat() {
 *   const { messages, isLoading, input, handleInputChange, handleSubmit } = useChat({
 *     api: '/api/chat',
 *   });
 *
 *   return (
 *     <form onSubmit={handleSubmit}>
 *       <input value={input} onChange={handleInputChange} />
 *       <button type="submit">Send</button>
 *     </form>
 *   );
 * }
 * ```
 */

// Transport utilities
export { createAepStream, createDataStreamWriter } from './transport/stream-controller.js';
export { mapAepToDataStream, mapErrorToDataStream } from './transport/chunk-mapper.js';
export type { DataStreamWriter } from './transport/chunk-mapper.js';

// Client
export { BrowserHotPlexClient } from './client/browser-client.js';
export type { BrowserClientEvents } from './client/browser-client.js';

// Constants
export {
  EventKind,
  SessionState,
  ErrorCode,
  ControlAction,
  WorkerType,
  AEP_VERSION,
} from './client/constants.js';

// Types
export type {
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
  HotPlexClientConfig,
  ReconnectConfig,
  HeartbeatConfig,
  ClientState,
} from './client/types.js';

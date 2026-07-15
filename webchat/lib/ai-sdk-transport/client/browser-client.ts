/**
 * BrowserHotPlexClient - Browser-native WebSocket client for HotPlex Worker Gateway.
 *
 * Adapted from examples/typescript-client/src/client.ts for browser environments.
 * Uses native WebSocket instead of the 'ws' Node.js package.
 */

import { EventEmitter } from 'eventemitter3';
import { logger } from '@/lib/logger';
import {
  EventKind,
  SessionState,
  ErrorCode,
  ControlAction,
  ProtocolConstants,
  WorkerStdioCommand,
} from './constants';
import type {
  HotPlexClientConfig,
  ReconnectConfig,
  HeartbeatConfig,
  Envelope,
  InitAckData,
  ErrorData,
  StateData,
  InputAckData,
  MessageDeltaData,
  MessageData,
  MessageStartData,
  MessageEndData,
  ToolCallData,
  ToolResultData,
  DoneData,
  PermissionRequestData,
  PermissionResponseData,
  QuestionRequestData,
  QuestionResponseData,
  ElicitationRequestData,
  ElicitationResponseData,
  ReasoningData,
  StepData,
  PongData,
  ControlData,
  ContextUsageData,
} from './types';
import {
  createInitEnvelope,
  createInputEnvelope,
  createPingEnvelope,
  createControlEnvelope,
  createPermissionResponseEnvelope,
  createQuestionResponseEnvelope,
  createElicitationResponseEnvelope,
  createWorkerCommandEnvelope,
  serializeEnvelope,
  deserializeEnvelope,
  isInitAck,
  newEventId,
} from './envelope';

// ============================================================================
// Event Types
// ============================================================================

export interface BrowserClientEvents {
  connected: (ack: InitAckData) => void;
  disconnected: (reason: string) => void;
  reconnecting: (attempt: number) => void;
  reconnect_failed: (attempt: number) => void;
  sessionAlreadyConnected: (data: ErrorData, env: Envelope) => void;
  delta: (data: MessageDeltaData, env: Envelope) => void;
  message: (data: MessageData, env: Envelope) => void;
  messageStart: (data: MessageStartData, env: Envelope) => void;
  messageEnd: (data: MessageEndData, env: Envelope) => void;
  toolCall: (data: ToolCallData, env: Envelope) => void;
  toolResult: (data: ToolResultData, env: Envelope) => void;
  done: (data: DoneData, env: Envelope) => void;
  error: (data: ErrorData, env: Envelope) => void;
  state: (data: StateData, env: Envelope) => void;
  inputAck: (data: InputAckData, env: Envelope) => void;
  reasoning: (data: ReasoningData, env: Envelope) => void;
  step: (data: StepData, env: Envelope) => void;
  permissionRequest: (data: PermissionRequestData, env: Envelope) => void;
  permissionResponse: (data: PermissionResponseData, env: Envelope) => void;
  questionRequest: (data: QuestionRequestData, env: Envelope) => void;
  questionResponse: (data: QuestionResponseData, env: Envelope) => void;
  elicitationRequest: (data: ElicitationRequestData, env: Envelope) => void;
  elicitationResponse: (data: ElicitationResponseData, env: Envelope) => void;
  reconnect: (data: ControlData, env: Envelope) => void;
  sessionInvalid: (data: ControlData, env: Envelope) => void;
  throttle: (data: ControlData, env: Envelope) => void;
  pong: (data: PongData, env: Envelope) => void;
  contextUsage: (data: ContextUsageData, env: Envelope) => void;
  skillsList: (data: import('./types').SkillsListData, env: Envelope) => void;
}

// ============================================================================
// Constants
// ============================================================================

const DEFAULT_RECONNECT_CONFIG = {
  enabled: true,
  maxAttempts: 10,
  baseDelayMs: 3000,                    // 3 seconds — avoid reconnection storms
  maxDelayMs: ProtocolConstants.ReconnectMaxDelayMs,
};

const DEFAULT_HEARTBEAT_CONFIG = {
  pingIntervalMs: ProtocolConstants.PingPeriodMs,
  pongTimeoutMs: ProtocolConstants.PongWaitMs,
  maxMissedPongs: ProtocolConstants.MaxMissedPongs,
};

// ============================================================================
// BrowserHotPlexClient
// ============================================================================

export class BrowserHotPlexClient extends EventEmitter<BrowserClientEvents> {
  private ws: WebSocket | null = null;
  private config: HotPlexClientConfig;
  private reconnectConfig: Required<ReconnectConfig>;
  private heartbeatConfig: Required<HeartbeatConfig>;

  private _sessionId: string | null = null;
  private _state: SessionState = SessionState.Deleted;
  private _connected: boolean = false;
  private _connecting: boolean = false;
  private _reconnecting: boolean = false;

  private reconnectAttempt = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private shouldReconnect = true;

  private pingTimer: ReturnType<typeof setTimeout> | null = null;
  private pongTimer: ReturnType<typeof setTimeout> | null = null;
  private missedPongs = 0;
  private lastPongTime = 0;

  // Hard cap on how long a pending input may wait for a terminal outcome.
  private static readonly INPUT_SETTLE_TIMEOUT_MS = 300_000;
  // A tombstoned (unknown-outcome) input blocks new sends for at most this long
  // before being force-cleared, so an ambiguous outcome can never lock the chat.
  private static readonly TOMBSTONE_GRACE_MS = 120_000;

  private pendingInput: {
    content: string;
    clientMessageId: string;
    retryable: boolean;
    tombstone: boolean;
    resolve: () => void;
    reject: (err: Error) => void;
  } | null = null;
  private inputSettleTimer: ReturnType<typeof setTimeout> | null = null;
  private inputTombstoneTimer: ReturnType<typeof setTimeout> | null = null;
  private pendingConnectReject: ((err: Error) => void) | null = null;

  private closed = false;

  constructor(config: HotPlexClientConfig) {
    super();

    this.config = {
      url: config.url,
      workerType: config.workerType,
      apiKey: config.apiKey,
      authToken: config.authToken,
      workspaceId: config.workspaceId,
      initConfig: config.initConfig,
      reconnect: config.reconnect ?? { enabled: true },
      heartbeat: config.heartbeat ?? {},
    };

    const reconnect = this.config.reconnect!;
    this.reconnectConfig = {
      enabled: reconnect.enabled,
      maxAttempts: reconnect.maxAttempts ?? DEFAULT_RECONNECT_CONFIG.maxAttempts,
      baseDelayMs: reconnect.baseDelayMs ?? DEFAULT_RECONNECT_CONFIG.baseDelayMs,
      maxDelayMs: reconnect.maxDelayMs ?? DEFAULT_RECONNECT_CONFIG.maxDelayMs,
    };

    const heartbeat = this.config.heartbeat;
    this.heartbeatConfig = {
      pingIntervalMs: heartbeat?.pingIntervalMs ?? DEFAULT_HEARTBEAT_CONFIG.pingIntervalMs,
      pongTimeoutMs: heartbeat?.pongTimeoutMs ?? DEFAULT_HEARTBEAT_CONFIG.pongTimeoutMs,
      maxMissedPongs: heartbeat?.maxMissedPongs ?? DEFAULT_HEARTBEAT_CONFIG.maxMissedPongs,
    };
  }

  // ============================================================================
  // Public Getters
  // ============================================================================

  get sessionId(): string | null { return this._sessionId; }
  get state(): SessionState { return this._state; }
  get connected(): boolean { return this._connected; }
  /** True while a connection handshake is in progress (awaiting init_ack). */
  get connecting(): boolean { return this._connecting; }
  get reconnecting(): boolean { return this._reconnecting; }

  // ============================================================================
  // Connection Lifecycle
  // ============================================================================

  async connect(sessionId?: string): Promise<InitAckData> {
    this.closed = false;
    this.shouldReconnect = true;
    const id = sessionId || this._sessionId || undefined;
    if (id) {
      this._sessionId = id;
    }
    return this._doConnect(id);
  }

  async resume(existingSessionId: string): Promise<InitAckData> {
    this.closed = false;
    this.shouldReconnect = true;
    this._sessionId = existingSessionId;
    return this._doConnect(existingSessionId);
  }

  private _doConnect(sessionId: string | undefined): Promise<InitAckData> {
    // Prevent zombie connections: if the client was explicitly closed via
    // disconnect(), do not create a new WebSocket. This guards against a
    // race where a pending reconnect timer fires after React effect cleanup
    // has already torn down the component.
    if (this.closed) {
      return Promise.reject(new Error('Client is closed'));
    }

    this._connecting = true;
    return new Promise((resolve, reject) => {
      this.pendingConnectReject = reject;
      try {
        const prevWs = this.ws;
        if (prevWs) {
          // Detach handler AND close the socket to avoid server having two active connections.
          // The close event from prevWs is already handled by the `activeWs !== this.ws`
          // guard in the addEventListener close handler, so no reconnection storm occurs.
          prevWs.onclose = null;
          prevWs.close();
        }

        // Build URL without api_key — auth is passed via auth.token in init envelope.
        const url = this.config.url;

        this.ws = new WebSocket(url);

        const initEnv = createInitEnvelope(
          sessionId,
          this.config.workerType,
          this.config.initConfig,
          this.config.authToken,
          this.config.workspaceId
        );

        const onOpen = () => {
          if (!this.ws) return;
          this.ws.send(serializeEnvelope(initEnv));
        };

        const onMessage = (event: MessageEvent) => {
          const line = (typeof event.data === 'string' ? event.data : '').trim();
          if (!line) return;

          try {
            const env = deserializeEnvelope(line);
            this._handleMessage(env, resolve, reject);
          } catch (err) {
            this.emit('error', { code: ErrorCode.InvalidMessage, message: String(err) } as ErrorData, {} as Envelope);
          }
        };

        const onError = () => {
          // WebSocket error events don't carry useful info; close event follows
        };

        const activeWs = this.ws;

        this.ws.addEventListener('open', onOpen);
        this.ws.addEventListener('message', onMessage);
        this.ws.addEventListener('error', onError);
        this.ws.addEventListener('close', (event: CloseEvent) => {
          if (activeWs !== this.ws) return;
          this._handleClose(event.code, event.reason || 'Connection closed');
        });
      } catch (err) {
        this._connecting = false;
        this.pendingConnectReject = null;
        reject(err);
      }
    });
  }

  private _handleMessage(env: Envelope, resolve: (ack: InitAckData) => void, reject: (err: Error) => void): void {
    const { event, session_id } = env;

    if (isInitAck(env)) {
      const ackData = event.data as unknown as InitAckData;
      const wasReconnecting = this._reconnecting;

      // Handle handshake-level errors
      if (ackData.error || ackData.code) {
        const errorMsg = ackData.error || `Handshake failed with code: ${ackData.code}`;

        if (ackData.code === ErrorCode.SessionNotFound) {
          // Session was deleted on server — retry with the original session ID.
          // Server uses DeriveSessionKey(userID, workerType, clientSessionID, workDir)
          // to map client session_id to a deterministic UUIDv5. By retrying with the
          // same session ID, the auto-created session always maps to the same server-side
          // session key, providing stable worker-to-session consistency.
          const retryId = this._sessionId;
          this._sessionId = null;
          this._doConnect(retryId ?? undefined).then(resolve, reject);
          return;
        }

        // Fatal errors shouldn't trigger reconnect
        if (ackData.code === ErrorCode.Unauthorized ||
            ackData.code === ErrorCode.AuthRequired ||
            ackData.code === ErrorCode.SessionAlreadyConnected) {
          this.shouldReconnect = false;
        }

        if (ackData.code === ErrorCode.SessionAlreadyConnected) {
          const errorData = {
            code: ErrorCode.SessionAlreadyConnected,
            message: errorMsg,
          } as ErrorData;
          logger.info('BrowserClient', 'Session already connected', { message: errorMsg });
          this._stopHeartbeat();
          this._clearReconnectTimer();
          this.emit('sessionAlreadyConnected', errorData, env);
          this.disconnect();
          reject(new Error(errorMsg));
          return;
        }

        logger.error('BrowserClient', 'Handshake error', { message: errorMsg });
        this.emit('error', {
          code: ackData.code || ErrorCode.InternalError,
          message: errorMsg
        } as ErrorData, env);

        this.disconnect();
        reject(new Error(errorMsg));
        return;
      }

      this._sessionId = session_id;
      this._connected = true;
      this._connecting = false;
      this._reconnecting = false;
      this.reconnectAttempt = 0;
      this.pendingConnectReject = null;

      if (ackData.state) {
        this._state = ackData.state;
      }

      this._startHeartbeat();

      if (this.reconnectTimer) {
        clearTimeout(this.reconnectTimer);
        this.reconnectTimer = null;
      }

      if (wasReconnecting && this.pendingInput?.retryable) {
        this._sendInputWithID(this.pendingInput.content, this.pendingInput.clientMessageId);
      }

      this.emit('connected', ackData);
      resolve(ackData);
      return;
    }

    this._routeEvent(env);
  }

  private _routeEvent(env: Envelope): void {
    const { event } = env;

    switch (event.type) {
      case EventKind.Error: {
        const errData = event.data as ErrorData;
        if (!event.data || Object.keys(event.data).length === 0) {
          logger.warn('BrowserClient', 'Received empty error data', { envId: env?.id });
        }
        
        if (errData.code === ErrorCode.SessionAlreadyConnected) {
          this.shouldReconnect = false;
          this._stopHeartbeat();
          this._clearReconnectTimer();
          this.emit('sessionAlreadyConnected', errData, env);
          this.emit('error', errData, env);
          this.disconnect();
          break;
        }

        // Fatal errors shouldn't trigger reconnect
        if (errData.code === ErrorCode.SessionNotFound) {
          this.shouldReconnect = false;
        }

        this.emit('error', errData, env);
        // Every gateway error is a definitive outcome for the submitted input.
        // In particular, retrying SESSION_BUSY at a fixed interval creates a
        // request storm while another turn owns the session.
        this._settlePending({
          kind: 'reject',
          error: new Error(errData.code || errData.message || 'Gateway error'),
          force: true,
        });
        break;
      }

      case EventKind.State:
        this._state = (event.data as StateData).state;
        this.emit('state', event.data as StateData, env);
        break;

      case EventKind.InputAck: {
        const ackData = event.data as InputAckData;
        if (this.pendingInput?.clientMessageId === ackData.client_message_id &&
            ackData.status !== 'accepted') {
          this.pendingInput.retryable = false;
          if (ackData.status === 'delivered') {
            // A delivered outcome resolves the pending input — including the
            // duplicate-delivered replay on reconnect — so the client never
            // waits for a Done that the deduplicated path will not send.
            this._settlePending({ kind: 'resolve' });
          } else if (ackData.status === 'unknown') {
            this._settlePending({
              kind: 'reject',
              error: new Error(ackData.error_code || ackData.status.toUpperCase()),
              tombstone: true,
            });
          } else if (ackData.status === 'failed') {
            this._settlePending({
              kind: 'reject',
              error: new Error(ackData.error_code || ackData.status.toUpperCase()),
              force: true,
            });
          }
        }
        this.emit('inputAck', ackData, env);
        break;
      }

      case EventKind.Done:
        this.emit('done', event.data as DoneData, env);
        this._settlePending({ kind: 'resolve' });
        break;

      case EventKind.MessageDelta:
        this.emit('delta', event.data as MessageDeltaData, env);
        break;

      case EventKind.Message:
        this.emit('message', event.data as MessageData, env);
        break;

      case EventKind.MessageStart:
        this.emit('messageStart', event.data as MessageStartData, env);
        break;

      case EventKind.MessageEnd:
        this.emit('messageEnd', event.data as MessageEndData, env);
        break;

      case EventKind.ToolCall:
        this.emit('toolCall', event.data as ToolCallData, env);
        break;

      case EventKind.ToolResult:
        this.emit('toolResult', event.data as ToolResultData, env);
        break;

      case EventKind.Reasoning:
        this.emit('reasoning', event.data as ReasoningData, env);
        break;

      case EventKind.Step:
        this.emit('step', event.data as StepData, env);
        break;

      case EventKind.PermissionRequest:
        this.emit('permissionRequest', event.data as PermissionRequestData, env);
        break;

      case EventKind.PermissionResponse:
        this.emit('permissionResponse', event.data as PermissionResponseData, env);
        break;

      case EventKind.QuestionRequest:
        this.emit('questionRequest', event.data as QuestionRequestData, env);
        break;

      case EventKind.QuestionResponse:
        this.emit('questionResponse', event.data as QuestionResponseData, env);
        break;

      case EventKind.ElicitationRequest:
        this.emit('elicitationRequest', event.data as ElicitationRequestData, env);
        break;

      case EventKind.ElicitationResponse:
        this.emit('elicitationResponse', event.data as ElicitationResponseData, env);
        break;

      case EventKind.Pong:
        this.missedPongs = 0;
        this.lastPongTime = Date.now();
        this.emit('pong', event.data as PongData, env);
        break;

      case EventKind.Control:
        this._handleControlMessage(event.data as ControlData, env);
        break;

      case EventKind.ContextUsage:
        this.emit('contextUsage', event.data as ContextUsageData, env);
        break;

      case EventKind.SkillsList:
        this.emit('skillsList', event.data as import('./types').SkillsListData, env);
        break;
    }
  }

  private _handleControlMessage(data: ControlData, env: Envelope): void {
    switch (data.action) {
      case ControlAction.Reconnect:
        this.emit('reconnect', data, env);
        if (this.reconnectConfig.enabled) {
          this._scheduleReconnect();
        }
        break;

      case ControlAction.SessionInvalid:
        this.emit('sessionInvalid', data, env);
        this.shouldReconnect = false;
        this.disconnect();
        break;

      case ControlAction.Throttle:
        this.emit('throttle', data, env);
        break;

      case ControlAction.Terminate:
        this.emit('reconnect', data, env);
        this.shouldReconnect = false;
        this.disconnect();
        break;

      case ControlAction.Delete:
        this.shouldReconnect = false;
        this.disconnect();
        break;
    }
  }

  // ============================================================================
  // Sending Messages
  // ============================================================================

  private _send(env: Envelope<unknown>): void {
    if (!this._sessionId || !this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new Error('Not connected to gateway');
    }
    this.ws.send(serializeEnvelope(env));
  }

  sendInput(content: string): string {
    if (this.pendingInput) {
      throw new Error('Input already pending');
    }
    const pending = {
      content,
      clientMessageId: newEventId(),
      retryable: true,
      tombstone: false,
      resolve: () => undefined,
      reject: () => undefined,
    };
    this.pendingInput = pending;
    this._armInputSettleTimer();
    try {
      this._sendInputWithID(content, pending.clientMessageId);
    } catch (err) {
      if (this.pendingInput === pending) {
        this._clearInputTimers();
        this.pendingInput = null;
      }
      throw err;
    }
    return pending.clientMessageId;
  }

  async sendInputAsync(content: string): Promise<void> {
    if (this.pendingInput) {
      throw new Error('Input already pending');
    }

    return new Promise((resolve, reject) => {
      const clientMessageId = newEventId();
      const pending = { content, clientMessageId, retryable: true, tombstone: false, resolve, reject };
      this.pendingInput = pending;
      this._armInputSettleTimer();
      try {
        this._sendInputWithID(content, clientMessageId);
      } catch (err) {
        if (this.pendingInput === pending) {
          this._clearInputTimers();
          this.pendingInput = null;
        }
        reject(err instanceof Error ? err : new Error(String(err)));
        return;
      }
    });
  }

  private _sendInputWithID(content: string, clientMessageId: string): void {
    const env = createInputEnvelope(this._sessionId!, content, undefined, clientMessageId);
    this._send(env);
  }

  sendPermissionResponse(permissionId: string, allowed: boolean, reason?: string): void {
    const env = createPermissionResponseEnvelope(this._sessionId!, permissionId, allowed, reason);
    this._send(env);
  }

  sendQuestionResponse(questionId: string, answers: Record<string, string | string[]>): void {
    const env = createQuestionResponseEnvelope(this._sessionId!, questionId, answers);
    this._send(env);
  }

  sendElicitationResponse(elicitationId: string, action: 'accept' | 'decline' | 'cancel', content?: Record<string, unknown>): void {
    const env = createElicitationResponseEnvelope(this._sessionId!, elicitationId, action, content);
    this._send(env);
  }

  sendControl(action: 'terminate' | 'delete' | 'stop'): void {
    const env = createControlEnvelope(this._sessionId!, action);
    this._send(env);
    if (action === 'stop') {
      this._settlePending({ kind: 'resolve' });
    }
  }

  sendWorkerCommand(command: typeof WorkerStdioCommand[keyof typeof WorkerStdioCommand], args?: string, extra?: Record<string, unknown>): void {
    const env = createWorkerCommandEnvelope(this._sessionId!, command, args, extra);
    this._send(env);
  }

  disconnect(): void {
    this.closed = true;
    this.shouldReconnect = false;

    this._stopHeartbeat();
    this._clearReconnectTimer();
    this._clearInputTimers();

    if (this.pendingConnectReject) {
      this.pendingConnectReject(new Error('Client disconnected'));
      this.pendingConnectReject = null;
    }

    this._settlePending({ kind: 'reject', error: new Error('Client disconnected'), force: true });

    if (this.ws) {
      const ws = this.ws;
      this.ws = null;
      if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
        ws.close(1000, 'Client disconnect');
      }
    }

    this._connected = false;
    this._connecting = false;
    this.emit('disconnected', 'Client initiated disconnect');
  }

  // ============================================================================
  // Heartbeat
  // ============================================================================

  private _startHeartbeat(): void {
    this._stopHeartbeat();
    this.missedPongs = 0;
    this.lastPongTime = Date.now();

    this.pingTimer = setInterval(() => {
      this._sendPing();
    }, this.heartbeatConfig.pingIntervalMs);
  }

  private _stopHeartbeat(): void {
    if (this.pingTimer) {
      clearInterval(this.pingTimer);
      this.pingTimer = null;
    }
    if (this.pongTimer) {
      clearTimeout(this.pongTimer);
      this.pongTimer = null;
    }
  }

  private _sendPing(): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN || !this._sessionId) {
      return;
    }

    const env = createPingEnvelope(this._sessionId);
    this.ws.send(serializeEnvelope(env));

    if (this.pongTimer) {
      clearTimeout(this.pongTimer);
    }
    this.pongTimer = setTimeout(() => {
      this.pongTimer = null;
      const timeSinceLastPong = Date.now() - this.lastPongTime;
      if (timeSinceLastPong >= this.heartbeatConfig.pongTimeoutMs) {
        this.missedPongs++;

        if (this.missedPongs >= this.heartbeatConfig.maxMissedPongs) {
          this._handleClose(4000, 'Heartbeat timeout');
        }
      }
    }, this.heartbeatConfig.pongTimeoutMs);
  }

  // ============================================================================
  // Reconnection
  // ============================================================================

  private _scheduleReconnect(): void {
    if (!this.shouldReconnect || this.closed || this.reconnectAttempt >= this.reconnectConfig.maxAttempts) {
      return;
    }

    this._reconnecting = true;
    this.reconnectAttempt++;

    const delay = Math.min(
      this.reconnectConfig.baseDelayMs * Math.pow(2, this.reconnectAttempt - 1),
      this.reconnectConfig.maxDelayMs,
    );

    this.emit('reconnecting', this.reconnectAttempt);

    this.reconnectTimer = setTimeout(async () => {
      if (!this._sessionId || this.closed) return;

      try {
        await this._doConnect(this._sessionId);
      } catch {
        this._handleClose(4001, 'Reconnect failed');
      }
    }, delay);
  }

  private _clearReconnectTimer(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }

  private _handleClose(_code: number, reason: string): void {
    this._stopHeartbeat();

    const wasConnected = this._connected;
    this._connected = false;

    if (this.ws) {
      const old = this.ws;
      this.ws = null;
      if (old.readyState === WebSocket.OPEN || old.readyState === WebSocket.CONNECTING) {
        old.close(1000, reason);
      }
    }

    if (!wasConnected && !this._reconnecting) {
      // Connection closed before or during handshake — reject pending connect
      if (this._connecting && this.pendingConnectReject) {
        this._connecting = false;
        this.pendingConnectReject(new Error(`WebSocket closed during handshake: ${reason}`));
        this.pendingConnectReject = null;
      }
      return;
    }

    if (this.shouldReconnect && !this.closed && this.reconnectAttempt < this.reconnectConfig.maxAttempts) {
      this._scheduleReconnect();
    } else if (this.shouldReconnect && !this.closed && this.reconnectAttempt >= this.reconnectConfig.maxAttempts) {
      // Reconnect attempts exhausted — emit a terminal failure so the UI can
      // stop promising "reconnecting…" forever (this path was previously silent).
      // Note: 'reconnect_failed' is terminal and implies disconnection; it is
      // intentionally NOT accompanied by a separate 'disconnected' event.
      // Subscribers that track connection state must handle 'reconnect_failed'
      // as well (the runtime adapter does — hotplex-runtime-adapter.ts). Kept
      // singular to preserve observed event ordering; add a concurrent
      // 'disconnected' emit here if a future consumer listens only to that event.
      this._reconnecting = false;
      this._clearInputTimers();
      this._settlePending({ kind: 'reject', error: new Error('Reconnect failed'), force: true });
      this.emit('reconnect_failed', this.reconnectAttempt);
    } else if (!this.shouldReconnect || this.closed) {
      this._clearInputTimers();
      this._settlePending({ kind: 'reject', error: new Error(reason || 'Disconnected'), force: true });
      this.emit('disconnected', reason);
    }
  }

  // ─── Pending-input lifecycle ───────────────────────────────────────────────
  //
  // _settlePending is the single point that clears or tombstones a pending
  // input. Every terminal path (terminal InputAck, Done, non-busy Error,
  // disconnect, reconnect failure, settle/grace timeout) routes through it, so
  // the client can never be permanently locked out of sending.

  private _clearInputTimers(): void {
    if (this.inputSettleTimer) {
      clearTimeout(this.inputSettleTimer);
      this.inputSettleTimer = null;
    }
    if (this.inputTombstoneTimer) {
      clearTimeout(this.inputTombstoneTimer);
      this.inputTombstoneTimer = null;
    }
  }

  private _armInputSettleTimer(): void {
    this.inputSettleTimer = setTimeout(() => {
      this.inputSettleTimer = null;
      // Hard cap: force-clear regardless of tombstone so a missing terminal
      // event can never block the chat forever.
      this._settlePending({ kind: 'reject', error: new Error('Input timeout'), force: true });
    }, BrowserHotPlexClient.INPUT_SETTLE_TIMEOUT_MS);
  }

  private _settlePending(outcome:
    | { kind: 'resolve' }
    | { kind: 'reject'; error: Error; tombstone?: boolean; force?: boolean },
  ): void {
    const pending = this.pendingInput;
    if (!pending) {
      return;
    }

    // A tombstoned input (unknown outcome) stays sticky — it blocks new sends
    // to avoid double side-effects while the worker may still be processing.
    // Only Done (resolve), a definitive event (force), or the grace timer may
    // clear it. Other rejects during the window still deliver the error to the
    // already-settled promise (a no-op) without clearing pendingInput.
    if (pending.tombstone && outcome.kind === 'reject' && !outcome.force) {
      pending.reject(outcome.error);
      return;
    }

    this._clearInputTimers();
    if (outcome.kind === 'resolve') {
      pending.resolve();
    } else {
      pending.reject(outcome.error);
    }

    if (outcome.kind === 'reject' && outcome.tombstone) {
      pending.tombstone = true;
      // Bounded escape: if no Done arrives within the grace window, force-clear
      // so an ambiguous outcome can never lock the chat permanently.
      this.inputTombstoneTimer = setTimeout(() => {
        this.inputTombstoneTimer = null;
        this._settlePending({ kind: 'reject', error: new Error('Input outcome timeout'), force: true });
      }, BrowserHotPlexClient.TOMBSTONE_GRACE_MS);
      return; // keep pendingInput as a sticky tombstone
    }

    this.pendingInput = null;
  }
}

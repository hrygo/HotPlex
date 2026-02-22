import WebSocket from 'ws';
import { Config, ClientConfig, defaultClientConfig } from './config';
import { Event, EventType, parseEvent } from './events';
import {
  ConnectionError,
  TimeoutError,
  ExecutionError,
  DangerBlockedError,
} from './errors';

type EventCallback = (event: Event) => void;

export class HotPlexClient {
  private config: ClientConfig;
  private ws: WebSocket | null = null;
  private connected = false;
  private requestId = 0;
  private eventQueue: Event[] = [];
  private resolvers: ((value: Event | null) => void)[] = [];

  constructor(config: Partial<ClientConfig> = {}) {
    this.config = { ...defaultClientConfig, ...config };
  }

  connect(): Promise<void> {
    return new Promise((resolve, reject) => {
      if (this.connected) {
        resolve();
        return;
      }

      const headers: Record<string, string> = {};
      if (this.config.apiKey) {
        headers['Authorization'] = `Bearer ${this.config.apiKey}`;
      }

      this.ws = new WebSocket(this.config.url, { headers });

      this.ws.on('open', () => {
        this.connected = true;
        resolve();
      });

      this.ws.on('message', (data: WebSocket.Data) => {
        try {
          const parsed = JSON.parse(data.toString()) as Record<string, unknown>;
          const event = parseEvent(parsed);
          this.eventQueue.push(event);
          const resolver = this.resolvers.shift();
          if (resolver) {
            resolver(this.eventQueue.shift() || null);
          }
        } catch (e) {
          console.error('Failed to parse message:', e);
        }
      });

      this.ws.on('close', () => {
        this.connected = false;
        while (this.resolvers.length > 0) {
          const resolver = this.resolvers.shift();
          if (resolver) resolver(null);
        }
      });

      this.ws.on('error', (err) => {
        reject(new ConnectionError(`Connection failed: ${err.message}`));
      });
    });
  }

  close(): void {
    this.connected = false;
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
  }

  async execute(
    prompt: string,
    config: Config,
    onEvent?: EventCallback,
    timeout?: number
  ): Promise<Event[]> {
    if (!this.connected) {
      await this.connect();
    }

    const timeoutMs = timeout || this.config.timeout;
    const events: Event[] = [];
    const startTime = Date.now();

    this.requestId++;
    const request = {
      request_id: this.requestId,
      type: 'execute',
      prompt,
      config,
    };

    this.ws!.send(JSON.stringify(request));

    while (true) {
      const elapsed = Date.now() - startTime;
      if (elapsed > timeoutMs) {
        throw new TimeoutError(`Execution timed out after ${timeoutMs}ms`);
      }

      const event = await this.waitForEvent(timeoutMs - elapsed);
      if (!event) break;

      events.push(event);
      if (onEvent) onEvent(event);

      if (event.type === 'session_stats') break;
      if (event.type === 'error') throw new ExecutionError(String(event.data));
      if (event.type === 'danger_block') throw new DangerBlockedError(String(event.data));
    }

    return events;
  }

  async *executeStream(
    prompt: string,
    config: Config,
    timeout?: number
  ): AsyncGenerator<Event> {
    if (!this.connected) {
      await this.connect();
    }

    const timeoutMs = timeout || this.config.timeout;
    const startTime = Date.now();

    this.requestId++;
    const request = {
      request_id: this.requestId,
      type: 'execute',
      prompt,
      config,
    };

    this.ws!.send(JSON.stringify(request));

    while (true) {
      const elapsed = Date.now() - startTime;
      if (elapsed > timeoutMs) {
        throw new TimeoutError(`Execution timed out after ${timeoutMs}ms`);
      }

      const event = await this.waitForEvent(timeoutMs - elapsed);
      if (!event) break;

      yield event;

      if (event.type === 'session_stats') break;
      if (event.type === 'error') throw new ExecutionError(String(event.data));
      if (event.type === 'danger_block') throw new DangerBlockedError(String(event.data));
    }
  }

  private waitForEvent(timeoutMs: number): Promise<Event | null> {
    return new Promise((resolve) => {
      if (this.eventQueue.length > 0) {
        resolve(this.eventQueue.shift() || null);
        return;
      }

      this.resolvers.push(resolve);

      setTimeout(() => {
        const index = this.resolvers.indexOf(resolve);
        if (index >= 0) {
          this.resolvers.splice(index, 1);
          resolve(null);
        }
      }, timeoutMs);
    });
  }
}

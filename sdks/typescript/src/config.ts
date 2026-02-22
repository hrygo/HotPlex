export interface Config {
  work_dir: string;
  session_id: string;
  task_instructions?: string;
}

export interface ClientConfig {
  url: string;
  timeout: number;
  reconnect: boolean;
  reconnectAttempts: number;
  reconnectDelay: number;
  logLevel: 'debug' | 'info' | 'warn' | 'error';
  apiKey?: string;
}

export const defaultClientConfig: ClientConfig = {
  url: 'ws://localhost:8080/ws/v1/agent',
  timeout: 300000,
  reconnect: true,
  reconnectAttempts: 5,
  reconnectDelay: 1000,
  logLevel: 'info',
};

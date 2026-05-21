/**
 * Admin panel type definitions.
 *
 * Covers auth, bot configs, sessions, cron jobs, and gateway stats.
 */

// --- Auth ---

export interface AdminConnection {
  url: string;
  token: string;
}

// --- Bot ---

export interface BotConfigEntry {
  name: string;
  platform: string;
  bot_id: string;
  status: string;
  connected_at?: string;
  config?: BotConfigAttrs;
  agent_configs?: AgentConfigSummary;
}

export interface BotConfigAttrs {
  worker_type?: string;
  work_dir?: string;
  dm_policy?: string;
  group_policy?: string;
  require_mention?: boolean;
  allow_from?: string[];
  allow_dm_from?: string[];
  allow_group_from?: string[];
  stt?: { provider?: string };
  tts?: { provider?: string; voice?: string };
}

export interface AgentConfigSummary {
  soul?: AgentConfigMeta;
  agents?: AgentConfigMeta;
  skills?: AgentConfigMeta;
  user?: AgentConfigMeta;
  memory?: AgentConfigMeta;
}

export interface AgentConfigMeta {
  source: string;
  size: number;
}

export interface AgentConfigFile {
  content: string;
  source: string;
  size: number;
  file: string;
}

// --- Session ---

export interface AdminSessionInfo {
  id: string;
  user_id: string;
  state: string;
  created_at: string;
  updated_at: string;
  worker_type?: string;
  work_dir?: string;
  title?: string;
  turn_count?: number;
}

// --- Cron ---

export interface CronJob {
  id: string;
  name: string;
  schedule: string;
  message: string;
  bot_id: string;
  owner_id: string;
  enabled: boolean;
  max_runs?: number;
  runs_count?: number;
  next_run_at?: string;
  last_run_at?: string;
  expires_at?: string;
}

// --- API Key ---

export interface APIKeyUser {
  id?: number;
  api_key: string;
  user_id: string;
  description?: string;
  created_at?: string;
  updated_at?: string;
}

// --- Stats ---

export interface GatewayStats {
  uptime_seconds: number;
  total_sessions: number;
  active_sessions: number;
}

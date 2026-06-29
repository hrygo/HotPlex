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

export interface CronSchedule {
  kind: 'at' | 'every' | 'cron';
  at?: string;
  every_ms?: number;
  expr?: string;
  tz?: string;
}

export interface CronPayload {
  kind: string;
  message: string;
  target_session_id?: string;
  allowed_tools?: string[];
  worker_type?: string;
}

export interface CronJobState {
  next_run_at_ms?: number;
  last_run_at_ms?: number;
  run_count?: number;
  last_status?: string;
  consecutive_errors?: number;
}

export interface CronJob {
  id: string;
  name: string;
  description?: string;
  schedule: CronSchedule;
  payload: CronPayload;
  enabled: boolean;
  bot_id?: string;
  owner_id?: string;
  max_runs?: number;
  expires_at?: string;
  state?: CronJobState;
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

// --- Workspace (admin console, issue #807) ---

// AdminWorkspace is the admin console projection of a workspace: the full row
// plus the owner's readable identity (display_name + username) joined server-side
// (GET /admin/workspaces). permission_mode "" = no explicit override (config default).
export interface AdminWorkspace {
  id: string;
  owner_user_id: string;
  owner_display_name: string;
  owner_username: string;
  name: string;
  work_dir: string;
  agent_config_overrides: string;
  worker_preference: string;
  permission_mode: string;
  status: string;
  created_at: number;
  updated_at: number;
}

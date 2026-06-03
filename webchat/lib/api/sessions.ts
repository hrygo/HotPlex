/**
 * Gateway API client for session management.
 *
 * These endpoints are on the same port as WebSocket (gateway :8888).
 * Authentication strategy:
 *   - Same-origin (embedded webchat): credentials: 'same-origin' (cookie auth)
 *   - Cross-origin (external deployment): X-API-Key header
 */

import { httpBase, apiKey, isSameOrigin } from "@/lib/config";

const BASE = httpBase();

// Build auth options: same-origin uses cookie auth (credentials: same-origin),
// cross-origin deployments continue using X-API-Key header.
function authHeaders(): Record<string, string> {
  if (isSameOrigin()) return {};
  return apiKey ? { 'X-API-Key': apiKey } : {};
}

function authOpts(): RequestInit {
  if (isSameOrigin()) return { credentials: 'same-origin' as RequestCredentials };
  return {};
}

// Merge auth headers with custom headers.
function withAuth(headers?: Record<string, string>): Record<string, string> {
  return { ...authHeaders(), ...headers };
}

export interface SessionInfo {
  id: string;
  user_id: string;
  owner_id?: string;
  bot_id?: string;
  worker_type: string;
  state: SessionState;
  created_at: string;
  updated_at: string;
  expires_at?: string;
  idle_expires_at?: string;
  turn_count?: number;
  work_dir?: string;
  title?: string;
}

export type SessionState = 'created' | 'running' | 'idle' | 'terminated' | 'deleted';

export interface ListSessionsResponse {
  sessions: SessionInfo[];
  limit: number;
  offset: number;
}

export interface ConversationRecord {
  id: number;
  session_id: string;
  generation: number;
  turn_num: number;
  seq: number;
  role: string;
  content: string;
  platform: string;
  user_id: string;
  model: string;
  success: boolean | null;
  source: string;
  tools: Record<string, number> | null;
  tool_call_count: number;
  tokens_in: number;
  tokens_input: number;
  tokens_cache_write: number;
  tokens_cache_read: number;
  tokens_out: number;
  duration_ms: number;
  cost_usd: number;
  created_at: number;
}

export interface GetHistoryResponse {
  records: ConversationRecord[];
  has_more: boolean;
}

export class AuthError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'AuthError';
  }
}

function throwIfAuthError(prefix: string, status: number): never | void {
  if (status === 401) {
    throw new AuthError(
      `${prefix} failed: 401 — Authentication failed. Check your API key configuration or consult the documentation.`
    );
  }
}

export async function listSessions(limit = 20, offset = 0, signal?: AbortSignal): Promise<ListSessionsResponse> {
  const res = await fetch(
    `${BASE}/api/sessions?limit=${limit}&offset=${offset}`,
    { headers: withAuth({ 'Content-Type': 'application/json' }), ...authOpts(), signal }
  );
  throwIfAuthError('listSessions', res.status);
  if (!res.ok) throw new Error(`listSessions failed: ${res.status}`);
  return res.json();
}

export interface CreateSessionOptions {
  workerType?: string;
  title: string;
  workDir?: string;
}

export async function createSession(opts: CreateSessionOptions, signal?: AbortSignal): Promise<{ session_id: string }> {
  const workerType = opts.workerType ?? 'claude_code';
  let url = `${BASE}/api/sessions?worker_type=${encodeURIComponent(workerType)}&title=${encodeURIComponent(opts.title)}`;
  if (opts.workDir) {
    url += `&work_dir=${encodeURIComponent(opts.workDir)}`;
  }
  const res = await fetch(url, { method: 'POST', headers: authHeaders(), ...authOpts(), signal });
  throwIfAuthError('createSession', res.status);
  if (!res.ok) {
    const body = await res.text().catch(() => '');
    throw new Error(body || `createSession failed: ${res.status}`);
  }
  return res.json();
}

export async function deleteSession(id: string, signal?: AbortSignal): Promise<void> {
  const res = await fetch(
    `${BASE}/api/sessions/${id}`,
    { method: 'DELETE', headers: authHeaders(), ...authOpts(), signal }
  );
  throwIfAuthError('deleteSession', res.status);
  if (!res.ok) throw new Error(`deleteSession failed: ${res.status}`);
}

export async function getSessionHistory(
  sessionId: string,
  options?: { beforeId?: number; limit?: number; signal?: AbortSignal }
): Promise<GetHistoryResponse> {
  if (!sessionId?.trim()) {
    throw new Error('getSessionHistory: empty session ID');
  }
  const limit = options?.limit ?? 50;
  let url = `${BASE}/api/sessions/${sessionId}/history?limit=${limit}`;
  if (options?.beforeId) {
    url += `&before_id=${options.beforeId}`;
  }
  const res = await fetch(url, { headers: withAuth({ 'Content-Type': 'application/json' }), ...authOpts(), signal: options?.signal });
  throwIfAuthError('getSessionHistory', res.status);
  if (!res.ok) throw new Error(`getSessionHistory failed: ${res.status}`);
  return res.json();
}

export function formatRelativeTime(dateStr: string): string {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMin = Math.floor(diffMs / 60000);
  const diffHour = Math.floor(diffMs / 3600000);
  const diffDay = Math.floor(diffMs / 86400000);

  const timeStr = date.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false });

  if (diffMin < 1) return `Just now ${timeStr}`;
  if (diffMin < 60) return `${diffMin}m ago`;
  if (diffHour < 24) return `Today ${timeStr}`;
  if (diffDay === 1) return `Yesterday ${timeStr}`;
  if (diffDay < 7) return `${diffDay}d ago`;
  return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}

export function stateLabel(state: SessionState): string {
  // NOTE: Labels are intentionally English. Do not translate to Chinese —
  // the rest of the UI (bot feedback, commands, header) is English too.
  // If i18n is needed, use a proper i18n framework rather than hardcoded translations.
  const map: Record<SessionState, string> = {
    created: 'Created',
    running: 'Running',
    idle: 'Idle',
    terminated: 'Terminated',
    deleted: 'Deleted',
  };
  return map[state] ?? state;
}

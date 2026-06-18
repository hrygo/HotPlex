import { httpBase, apiKey, isSameOrigin } from "@/lib/config";

const BASE = httpBase();

function authHeaders(): Record<string, string> {
  if (isSameOrigin()) return {};
  return apiKey ? { 'X-API-Key': apiKey } : {};
}

function authOpts(): RequestInit {
  if (isSameOrigin()) return { credentials: 'same-origin' as RequestCredentials };
  return {};
}

function withAuth(headers?: Record<string, string>): Record<string, string> {
  return { ...authHeaders(), ...headers };
}

export interface Workspace {
  id: string;
  name: string;
  work_dir: string;
  owner_user_id: string;
  worker_preference: string;
  agent_config_overrides: Record<string, string>;
  status: string;
  created_at: number;
  updated_at: number;
}

export interface ListWorkspacesResponse {
  workspaces: Workspace[];
  limit: number;
  offset: number;
}

export async function listWorkspaces(limit = 100, offset = 0, signal?: AbortSignal): Promise<ListWorkspacesResponse> {
  const res = await fetch(
    `${BASE}/api/workspaces?limit=${limit}&offset=${offset}`,
    { headers: withAuth({ 'Content-Type': 'application/json' }), ...authOpts(), signal }
  );
  if (!res.ok) throw new Error(`listWorkspaces failed: ${res.status}`);
  return res.json();
}

export async function createWorkspace(name: string, workDir: string, signal?: AbortSignal): Promise<Workspace> {
  const res = await fetch(
    `${BASE}/api/workspaces`,
    {
      method: 'POST',
      headers: withAuth({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({ name, work_dir: workDir }),
      ...authOpts(),
      signal,
    }
  );
  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || `createWorkspace failed: ${res.status}`);
  }
  return res.json();
}

export async function getWorkspace(id: string, signal?: AbortSignal): Promise<Workspace> {
  const res = await fetch(
    `${BASE}/api/workspaces/${id}`,
    { headers: withAuth({ 'Content-Type': 'application/json' }), ...authOpts(), signal }
  );
  if (!res.ok) throw new Error(`getWorkspace failed: ${res.status}`);
  return res.json();
}

export interface UpdateWorkspaceOptions {
  name?: string;
  workerPreference?: string;
  agentConfigOverrides?: Record<string, string>;
}

export async function updateWorkspace(id: string, opts: UpdateWorkspaceOptions, signal?: AbortSignal): Promise<Workspace> {
  const body: any = {};
  if (opts.name !== undefined) body.name = opts.name;
  if (opts.workerPreference !== undefined) body.worker_preference = opts.workerPreference;
  if (opts.agentConfigOverrides !== undefined) body.agent_config_overrides = opts.agentConfigOverrides;

  const res = await fetch(
    `${BASE}/api/workspaces/${id}`,
    {
      method: 'PATCH',
      headers: withAuth({ 'Content-Type': 'application/json' }),
      body: JSON.stringify(body),
      ...authOpts(),
      signal,
    }
  );
  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || `updateWorkspace failed: ${res.status}`);
  }
  return res.json();
}

export async function deleteWorkspace(id: string, signal?: AbortSignal): Promise<void> {
  const res = await fetch(
    `${BASE}/api/workspaces/${id}`,
    { method: 'DELETE', headers: authHeaders(), ...authOpts(), signal }
  );
  if (!res.ok) throw new Error(`deleteWorkspace failed: ${res.status}`);
}

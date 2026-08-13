import { BASE, authHeaders, authOpts, withAuth, extractApiError } from "@/lib/api/client";
import { parseApiError, ApiError } from "@/lib/api/errors";

export interface Workspace {
  id: string;
  name: string;
  work_dir: string;
  owner_user_id: string;
  worker_preference: string;
  permission_mode: string;
  agent_config_overrides: Record<string, string>;
  status: string;
  created_at: number;
  updated_at: number;
}

export interface ListWorkspacesResponse {
  workspaces: Workspace[];
  // workspace_root 是服务端 workspace 沙箱根（绝对路径，HotplexHome()/workspaces/<segment>），
  // 前端路径构造的唯一事实源。旧后端缺失时前端应降级禁用创建（spec Root-HotplexHome §5.1.5）。
  workspace_root: string;
  limit: number;
  offset: number;
}

// Backend stores agent_config_overrides as a JSON *string* column
// (Workspace.AgentConfigOverrides is `string`, validated by
// agentconfig.ValidateOverrides which json.Unmarshal's it). The frontend
// works with a parsed object, so we (de)serialize at this API boundary:
// JSON.stringify on write, JSON.parse on read. Mismatching this causes the
// PATCH to 400 ("cannot unmarshal object into string") and GET to feed a raw
// JSON string into the editor (PR #762 review P0).
// parseOverrides 规范化后端存储的 overrides（JSON 字符串或已解析对象）为
// Record<string,string>。仅接受纯对象（排除数组/null）、强制值为字符串，
// 防御历史脏数据（手动 SQL 植入的非对象/非字符串值）在首次 save 时触发
// INVALID_CONFIG_JSON 400（PR #762 review P3：纯防御性，正常写入路径不会
// 产生脏数据，后端 ValidateOverrides 已在写入时双重拦截）。
function parseOverrides(raw: unknown): Record<string, string> {
  const normalize = (obj: unknown): Record<string, string> => {
    if (!obj || typeof obj !== 'object' || Array.isArray(obj)) return {};
    const out: Record<string, string> = {};
    for (const [k, v] of Object.entries(obj as Record<string, unknown>)) {
      // ValidateOverrides 要求键值均为字符串 markdown；丢弃 null/对象/数组值。
      if (v === null || v === undefined || typeof v === 'object') continue;
      out[k] = String(v);
    }
    return out;
  };
  if (typeof raw === 'string') {
    if (!raw) return {};
    try {
      return normalize(JSON.parse(raw));
    } catch {
      return {};
    }
  }
  return normalize(raw);
}

function normalizeWorkspace(raw: any): Workspace {
  return { ...raw, agent_config_overrides: parseOverrides(raw?.agent_config_overrides) };
}

export async function listWorkspaces(limit = 100, offset = 0, signal?: AbortSignal): Promise<ListWorkspacesResponse> {
  const res = await fetch(
    `${BASE}/api/workspaces?limit=${limit}&offset=${offset}`,
    { headers: withAuth({ 'Content-Type': 'application/json' }), ...authOpts(), signal }
  );
  if (!res.ok) throw new Error(await extractApiError(res, `listWorkspaces failed: ${res.status}`));
  const data = await res.json();
  return {
    ...data,
    workspaces: (data.workspaces ?? []).map(normalizeWorkspace),
  };
}

export async function createWorkspace(name: string, workDir: string, permissionMode?: string, signal?: AbortSignal): Promise<Workspace> {
  const res = await fetch(
    `${BASE}/api/workspaces`,
    {
      method: 'POST',
      headers: withAuth({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({ name, work_dir: workDir, ...(permissionMode ? { permission_mode: permissionMode } : {}) }),
      ...authOpts(),
      signal,
    }
  );
  if (!res.ok) throw new Error(await extractApiError(res, `createWorkspace failed: ${res.status}`));
  return normalizeWorkspace(await res.json());
}

export async function getWorkspace(id: string, signal?: AbortSignal): Promise<Workspace> {
  const res = await fetch(
    `${BASE}/api/workspaces/${id}`,
    { headers: withAuth({ 'Content-Type': 'application/json' }), ...authOpts(), signal }
  );
  if (!res.ok) throw new Error(await extractApiError(res, `getWorkspace failed: ${res.status}`));
  return normalizeWorkspace(await res.json());
}

export interface UpdateWorkspaceOptions {
  name?: string;
  workerPreference?: string;
  permissionMode?: string;
  agentConfigOverrides?: Record<string, string>;
  workDir?: string;
}

export async function updateWorkspace(id: string, opts: UpdateWorkspaceOptions, signal?: AbortSignal): Promise<Workspace> {
  const body: any = {};
  if (opts.name !== undefined) body.name = opts.name;
  if (opts.workerPreference !== undefined) body.worker_preference = opts.workerPreference;
  if (opts.permissionMode !== undefined) body.permission_mode = opts.permissionMode;
  if (opts.workDir !== undefined) body.work_dir = opts.workDir;
  // Backend expects a JSON string, not an object (see parseOverrides note).
  if (opts.agentConfigOverrides !== undefined) body.agent_config_overrides = JSON.stringify(opts.agentConfigOverrides);

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
    // 409 = optimistic-concurrency CAS loss (P3-1 backend): workspace was
    // modified between the caller's Get and this PATCH. Surface a friendly
    // message + the WORKSPACE_VERSION_MISMATCH code so UIs can re-fetch+retry.
    const info = await parseApiError(res);
    if (res.status === 409) {
      throw new ApiError(
        { ...info, code: info.code || 'WORKSPACE_VERSION_MISMATCH' },
        'Workspace was modified concurrently — please reopen Settings and retry.',
      );
    }
    throw new ApiError(info);
  }
  return normalizeWorkspace(await res.json());
}

export async function deleteWorkspace(id: string, signal?: AbortSignal): Promise<void> {
  const res = await fetch(
    `${BASE}/api/workspaces/${id}`,
    { method: 'DELETE', headers: authHeaders(), ...authOpts(), signal }
  );
  if (!res.ok) throw new Error(await extractApiError(res, `deleteWorkspace failed: ${res.status}`));
}

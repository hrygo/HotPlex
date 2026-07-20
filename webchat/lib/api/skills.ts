/**
 * User-facing Skills API (issue #910).
 *
 * /api/skills — merged list (global + caller's workspaces + external read-only),
 * each entry carrying `managed` (true = under .agents/skills, UI-manageable).
 * /api/workspaces/{wid}/skills — workspace-scoped skill CRUD (zip install).
 *
 * Auth: same cookie/api-key channel as the rest of the user REST API (client.ts).
 */

import { BASE, authHeaders, authOpts, withAuth, extractApiError } from '@/lib/api/client';

export interface Skill {
  name: string;
  description: string;
  source: string; // "global" (home) | "project" (workspace)
  managed: boolean; // true = .agents/skills (writable); false = external read-only
}

export interface SkillListResponse {
  skills: Skill[];
  total: number;
}

export interface SkillDetail {
  name: string;
  description: string;
  source: string;
  managed: boolean;
  body?: string; // SKILL.md full text (scoped detail endpoints)
  files?: string[];
}

export interface SkillInstallResult {
  name: string;
  description: string;
  source: string;
  managed: boolean;
  body: string;
  files: string[];
  warning?: string; // non-empty = workspace install shadows a global same-name skill
}

export async function listSkills(signal?: AbortSignal): Promise<SkillListResponse> {
  const res = await fetch(`${BASE}/api/skills`, { headers: withAuth(), ...authOpts(), signal });
  if (!res.ok) throw new Error(await extractApiError(res, `listSkills failed: ${res.status}`));
  return res.json();
}

export async function getSkill(name: string, signal?: AbortSignal): Promise<Skill> {
  const res = await fetch(`${BASE}/api/skills/${encodeURIComponent(name)}`, {
    headers: withAuth(),
    ...authOpts(),
    signal,
  });
  if (!res.ok) throw new Error(await extractApiError(res, `getSkill failed: ${res.status}`));
  return res.json();
}

// installWorkspaceSkill uploads a skill zip into a workspace's .agents/skills.
// Note: do NOT set Content-Type — the browser sets the multipart boundary.
export async function installWorkspaceSkill(
  workspaceId: string,
  file: File,
  replace = false,
  signal?: AbortSignal,
): Promise<SkillInstallResult> {
  const fd = new FormData();
  fd.append('file', file);
  const url = `${BASE}/api/workspaces/${encodeURIComponent(workspaceId)}/skills${replace ? '?replace=true' : ''}`;
  const res = await fetch(url, {
    method: 'POST',
    headers: authHeaders(),
    body: fd,
    ...authOpts(),
    signal,
  });
  if (!res.ok) throw new Error(await extractApiError(res, `installSkill failed: ${res.status}`));
  return res.json();
}

export async function getWorkspaceSkill(
  workspaceId: string,
  name: string,
  signal?: AbortSignal,
): Promise<SkillDetail> {
  const res = await fetch(
    `${BASE}/api/workspaces/${encodeURIComponent(workspaceId)}/skills/${encodeURIComponent(name)}`,
    { headers: withAuth(), ...authOpts(), signal },
  );
  if (!res.ok) throw new Error(await extractApiError(res, `getWorkspaceSkill failed: ${res.status}`));
  return res.json();
}

export async function deleteWorkspaceSkill(
  workspaceId: string,
  name: string,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch(
    `${BASE}/api/workspaces/${encodeURIComponent(workspaceId)}/skills/${encodeURIComponent(name)}`,
    { method: 'DELETE', headers: authHeaders(), ...authOpts(), signal },
  );
  if (!res.ok) throw new Error(await extractApiError(res, `deleteWorkspaceSkill failed: ${res.status}`));
}

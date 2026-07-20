/**
 * Admin Skills API (issue #910).
 *
 * /admin/api/skills — global skill management (list/install/read/delete). Rides
 * the admin dual-channel (adminFetch: Bearer or cookie-session admin). The zip
 * install is multipart, which adminFetch can't carry (it forces
 * Content-Type: application/json), so installAdminSkill uses a dedicated
 * adminUpload that mirrors the Bearer/cookie selection without that header.
 */

import { adminFetch, getStoredAdminConnection } from './admin-client';
import { adminUrl } from '@/lib/config';
import { authOpts } from './client';
import { parseApiError } from './errors';

export interface AdminSkill {
  name: string;
  description: string;
  source: string; // "global" (home)
  managed: boolean;
}

export interface AdminSkillListResponse {
  skills: AdminSkill[];
  total: number;
}

export interface AdminSkillDetail {
  name: string;
  description: string;
  source: string;
  managed: boolean;
  body: string;
  files: string[];
}

export interface AdminSkillInstallResult extends AdminSkillDetail {
  warning?: string;
}

export async function listAdminSkills(): Promise<AdminSkillListResponse> {
  return adminFetch<AdminSkillListResponse>('/admin/api/skills');
}

export async function getAdminSkill(name: string): Promise<AdminSkillDetail> {
  return adminFetch<AdminSkillDetail>(`/admin/api/skills/${encodeURIComponent(name)}`);
}

export async function deleteAdminSkill(name: string): Promise<void> {
  await adminFetch<void>(`/admin/api/skills/${encodeURIComponent(name)}`, { method: 'DELETE' });
}

// installAdminSkill uploads a global skill zip. Multipart body must not carry a
// manual Content-Type (browser sets the boundary); adminUpload selects Bearer
// vs cookie like adminFetch but omits the JSON content type.
export async function installAdminSkill(file: File, replace = false): Promise<AdminSkillInstallResult> {
  const fd = new FormData();
  fd.append('file', file);
  const path = `/admin/api/skills${replace ? '?replace=true' : ''}`;
  return adminUpload<AdminSkillInstallResult>(path, fd);
}

async function adminUpload<T>(path: string, body: FormData): Promise<T> {
  const conn = getStoredAdminConnection();
  const res = conn
    ? await fetch(`${conn.url}${path}`, { method: 'POST', headers: { Authorization: `Bearer ${conn.token}` }, body })
    : await fetch(`${adminUrl}${path}`, { method: 'POST', ...authOpts(), body });

  if (!res.ok) {
    const info = await parseApiError(res);
    const message = info.message || info.code || `Admin skill upload failed: ${res.status}`;
    const err = new Error(message);
    (err as any).status = info.status;
    if (info.code) (err as any).code = info.code;
    throw err;
  }
  if (res.status === 204) return undefined as unknown as T;
  return res.json();
}

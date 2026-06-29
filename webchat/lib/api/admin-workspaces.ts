/**
 * Admin Workspaces API (issue #807).
 *
 * /admin/workspaces is the admin console global view: list every workspace across
 * owners (with readable owner identity) + inline permission_mode edit. Distinct
 * from /api/workspaces (user self-service, own workspaces only) — these endpoints
 * ride the admin dual-channel (adminFetch: Bearer or cookie-session admin).
 */

import { adminFetch } from './admin-client';
import type { AdminWorkspace } from '@/lib/types/admin';
import type { Workspace } from './workspaces';

interface AdminWorkspaceListResponse {
  workspaces: AdminWorkspace[];
}

export async function listAdminWorkspaces(): Promise<AdminWorkspace[]> {
  const r = await adminFetch<AdminWorkspaceListResponse>('/admin/workspaces');
  return r.workspaces;
}

// updateAdminWorkspacePermissionMode changes a single workspace's permission_mode.
// permissionMode "" clears the override (falls back to the config global default).
// The PATCH write is admin-audited server-side (workspace.permission_mode.update).
export function updateAdminWorkspacePermissionMode(
  id: string,
  permissionMode: string,
): Promise<Workspace> {
  return adminFetch<Workspace>(`/admin/workspaces/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify({ permission_mode: permissionMode }),
  });
}

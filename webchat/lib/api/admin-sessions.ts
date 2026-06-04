/**
 * Admin Session API client.
 *
 * List, terminate, and delete sessions via the admin endpoints.
 */

import { adminFetch } from './admin-client';
import type { AdminSessionInfo } from '@/lib/types/admin';

export function listSessions(
  limit = 50,
  offset = 0,
): Promise<{ sessions: AdminSessionInfo[] }> {
  return adminFetch<{ sessions: AdminSessionInfo[] }>(
    `/admin/sessions?limit=${limit}&offset=${offset}`,
  );
}

export function terminateSession(id: string): Promise<void> {
  return adminFetch<void>(`/admin/sessions/${encodeURIComponent(id)}/terminate`, {
    method: 'POST',
  });
}

export function deleteSession(id: string): Promise<void> {
  return adminFetch<void>(`/admin/sessions/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
}

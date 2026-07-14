/**
 * Admin Session API client.
 *
 * List, terminate, and delete sessions via the admin endpoints.
 */

import { adminFetch } from './admin-client';
import type { AdminSessionInfo, AuditIdentityLink } from '@/lib/types/admin';

export type SessionListResponse = {
  sessions: AdminSessionInfo[];
  // user_id → resolved identity, so the UI can show display names instead of
  // raw IDs. Absent when no identity provider is configured (idp == nil); the
  // page falls back to the truncated user_id.
  identity_links?: Record<string, AuditIdentityLink>;
  limit: number;
  offset: number;
};

export function listSessions(
  limit = 50,
  offset = 0,
): Promise<SessionListResponse> {
  return adminFetch<SessionListResponse>(
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

import type { ActivityStats, AuditActivityResponse } from '@/lib/types/admin';

import { adminFetch, getStoredAdminConnection } from './admin-client';
import { BASE, authOpts } from './client';
import { parseApiError } from './errors';

export interface ActivityFilters {
  userId?: string;
  principalUserId?: string;
  action?: string;
  actionPrefix?: string;
  outcome?: string;
  platform?: string;
  sessionId?: string;
  resourceType?: string;
  from?: string;
  to?: string;
  limit?: number;
  offset?: number;
}

function activityParams(filters: ActivityFilters, format?: 'json' | 'csv'): string {
  const params = new URLSearchParams();
  if (filters.userId) params.set('user_id', filters.userId);
  if (filters.principalUserId) params.set('principal_user_id', filters.principalUserId);
  if (filters.action) params.set('action', filters.action);
  if (filters.actionPrefix) params.set('action_prefix', filters.actionPrefix);
  if (filters.outcome) params.set('outcome', filters.outcome);
  if (filters.platform) params.set('platform', filters.platform);
  if (filters.sessionId) params.set('session_id', filters.sessionId);
  if (filters.resourceType) params.set('resource_type', filters.resourceType);
  if (filters.from) params.set('from', new Date(filters.from).toISOString());
  if (filters.to) params.set('to', new Date(filters.to).toISOString());
  if (filters.limit) params.set('limit', String(filters.limit));
  if (filters.offset) params.set('offset', String(filters.offset));
  if (format) params.set('format', format);
  const query = params.toString();
  return query ? `?${query}` : '';
}

function activityBasePath(): string {
  return getStoredAdminConnection() ? '/admin/activity' : '/api/admin/activity';
}

export async function listActivity(filters: ActivityFilters): Promise<AuditActivityResponse> {
  return adminFetch<AuditActivityResponse>(`${activityBasePath()}${activityParams(filters)}`);
}

export async function listActivityStats(filters: ActivityFilters): Promise<ActivityStats> {
  return adminFetch<ActivityStats>(`${activityBasePath()}/stats${activityParams(filters)}`);
}

export async function downloadActivity(filters: ActivityFilters, format: 'json' | 'csv'): Promise<void> {
  const conn = getStoredAdminConnection();
  const path = `${conn ? '/admin/activity' : '/api/admin/activity'}/export${activityParams(filters, format)}`;
  const res = conn
    ? await fetch(`${conn.url}${path}`, {
        headers: { Authorization: `Bearer ${conn.token}` },
      })
    : await fetch(`${BASE}${path}`, authOpts());

  if (!res.ok) {
    const info = await parseApiError(res);
    throw new Error(info.message || info.raw || `Export failed: ${res.status}`);
  }

  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `activity-${new Date().toISOString().replace(/[:.]/g, '-')}.${format}`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

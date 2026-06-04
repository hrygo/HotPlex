/**
 * Admin Cron API client.
 *
 * List, update, delete, and trigger cron jobs via the admin endpoints.
 */

import { adminFetch } from './admin-client';
import type { CronJob } from '@/lib/types/admin';

export function listCronJobs(): Promise<CronJob[]> {
  return adminFetch<CronJob[]>('/admin/cron/jobs');
}

export function updateCronJob(id: string, updates: Partial<CronJob>): Promise<void> {
  return adminFetch<void>(`/admin/cron/jobs/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(updates),
  });
}

export function deleteCronJob(id: string): Promise<void> {
  return adminFetch<void>(`/admin/cron/jobs/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
}

export function triggerCronJob(id: string): Promise<void> {
  return adminFetch<void>(`/admin/cron/jobs/${encodeURIComponent(id)}/run`, {
    method: 'POST',
  });
}

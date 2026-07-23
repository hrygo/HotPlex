/**
 * Admin Cron API client.
 *
 * List, create, update, delete, trigger, and history for cron jobs via admin endpoints.
 */

import { adminFetch } from './admin-client';
import type { CronJob, CronRunHistoryItem } from '@/lib/types/admin';

export function listCronJobs(): Promise<CronJob[]> {
  return adminFetch<CronJob[]>('/admin/cron/jobs');
}

export function createCronJob(job: Record<string, unknown>): Promise<void> {
  return adminFetch<void>('/admin/cron/jobs', {
    method: 'POST',
    body: JSON.stringify(job),
  });
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

export function getCronRunHistory(id: string): Promise<CronRunHistoryItem[]> {
  return adminFetch<CronRunHistoryItem[]>(`/admin/cron/jobs/${encodeURIComponent(id)}/runs`);
}

import { adminFetch } from './admin-client';
import type { APIKeyUser } from '@/lib/types/admin';

export function listAPIKeys(): Promise<APIKeyUser[]> {
  return adminFetch<APIKeyUser[]>('/admin/api-keys');
}

export function createAPIKey(body: {
  user_id: string;
  description?: string;
}): Promise<APIKeyUser> {
  return adminFetch<APIKeyUser>('/admin/api-keys', {
    method: 'POST',
    body: JSON.stringify(body),
  });
}

export function deleteAPIKey(id: number): Promise<void> {
  return adminFetch<void>(`/admin/api-keys/${id}`, {
    method: 'DELETE',
  });
}

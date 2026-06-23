/**
 * Admin API client.
 *
 * Provides connection management and a core fetch wrapper for the
 * admin endpoints (/admin/*). Credentials are persisted in localStorage.
 */

import type { AdminConnection } from '@/lib/types/admin';

const STORAGE_URL_KEY = 'hotplex_admin_url';
const STORAGE_TOKEN_KEY = 'hotplex_admin_token';

// ---------------------------------------------------------------------------
// Connection persistence
// ---------------------------------------------------------------------------

export function getStoredAdminConnection(): AdminConnection | null {
  if (typeof window === 'undefined') return null;
  const url = localStorage.getItem(STORAGE_URL_KEY);
  const token = localStorage.getItem(STORAGE_TOKEN_KEY);
  if (!url || !token) return null;
  return { url, token };
}

export function storeAdminConnection(conn: AdminConnection): void {
  if (typeof window === 'undefined') return;
  localStorage.setItem(STORAGE_URL_KEY, conn.url);
  localStorage.setItem(STORAGE_TOKEN_KEY, conn.token);
}

export function clearAdminConnection(): void {
  if (typeof window === 'undefined') return;
  localStorage.removeItem(STORAGE_URL_KEY);
  localStorage.removeItem(STORAGE_TOKEN_KEY);
}

// ---------------------------------------------------------------------------
// Core fetch wrapper
// ---------------------------------------------------------------------------

interface AdminFetchOptions extends RequestInit {
  conn?: AdminConnection;
}

export async function adminFetch<T>(
  path: string,
  options?: AdminFetchOptions,
): Promise<T> {
  const conn = options?.conn ?? getStoredAdminConnection();
  if (!conn) {
    throw new Error('Admin connection not configured');
  }

  const url = `${conn.url}${path}`;
  const headers: Record<string, string> = {
    Authorization: `Bearer ${conn.token}`,
    'Content-Type': 'application/json',
  };

  const res = await fetch(url, {
    ...options,
    headers: options?.headers
      ? { ...headers, ...(options.headers as Record<string, string>) }
      : headers,
  });

  if (res.status === 401) {
    clearAdminConnection();
    throw new Error('Admin authentication failed (401)');
  }

  if (!res.ok) {
    const body = await res.text().catch(() => '');
    // P2.8: admin API now returns JSON error envelope {"error":{code,message}}.
    // Parse it to surface the human-readable message; fall back to raw body for
    // non-JSON responses (backward compat with any plain-text error path).
    let message = body || `Admin request failed: ${res.status}`;
    let code: string | undefined;
    try {
      const envelope = JSON.parse(body);
      if (envelope?.error?.message) {
        message = envelope.error.message;
        code = envelope.error.code;
      }
    } catch {
      // not JSON — keep raw body as message
    }
    const err = new Error(message);
    (err as any).status = res.status;
    if (code) (err as any).code = code;
    throw err;
  }

  if (res.status === 204 || res.status === 202) {
    return undefined as unknown as T;
  }

  return res.json();
}

// ---------------------------------------------------------------------------
// Connection test
// ---------------------------------------------------------------------------

export async function testConnection(conn: AdminConnection): Promise<boolean> {
  try {
    const url = `${conn.url}/admin/health`;
    const res = await fetch(url, {
      headers: { Authorization: `Bearer ${conn.token}` },
    });
    return res.ok;
  } catch {
    return false;
  }
}

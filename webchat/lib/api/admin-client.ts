/**
 * Admin API client.
 *
 * Provides connection management and a core fetch wrapper for the
 * admin endpoints (/admin/*). Credentials are persisted in localStorage.
 */

import type { AdminConnection } from '@/lib/types/admin';

import { parseApiError } from './errors';

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
    // Parse the envelope so the caller gets the real code/message (e.g.
    // UNAUTHORIZED vs INVALID_CREDENTIALS) instead of a generic string —
    // avoids silently dropping future 401-scoped codes (PR #774 review P2).
    const info = await parseApiError(res);
    const err = new Error(info.message || info.code || 'Admin authentication failed (401)');
    (err as any).status = 401;
    if (info.code) (err as any).code = info.code;
    throw err;
  }

  if (!res.ok) {
    const info = await parseApiError(res);
    const message = info.message || info.raw || `Admin request failed: ${res.status}`;
    const err = new Error(message);
    (err as any).status = info.status;
    if (info.code) (err as any).code = info.code;
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

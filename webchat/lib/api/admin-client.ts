/**
 * Admin API client.
 *
 * Two credential channels (issue #788 A2):
 *   1. Bearer token — standalone /admin pages (remote gateway operations).
 *      Connection { url, token } persisted in localStorage.
 *   2. Cookie session — embedded webchat where the admin is already logged in
 *      via the chat cookie. The backend AdminAPI.Middleware falls back from
 *      Bearer to cookie-session auth, so same-origin requests carrying the
 *      session cookie are accepted when role==admin && status==active.
 *
 * adminFetch auto-selects: Bearer when a stored connection exists, otherwise
 * the cookie channel (same-origin).
 */

import type { AdminConnection } from '@/lib/types/admin';

import { authOpts } from './client';
import { parseApiError } from './errors';
import { adminUrl } from '@/lib/config';

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
  if (conn) {
    return adminFetchBearer<T>(conn, path, options);
  }
  return adminFetchCookie<T>(path, options);
}

// Bearer-token channel: standalone admin connection (remote operations).
async function adminFetchBearer<T>(
  conn: AdminConnection,
  path: string,
  options?: AdminFetchOptions,
): Promise<T> {
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

  return parseResponseJson<T>(res);
}

async function parseResponseJson<T>(res: Response): Promise<T> {
  if (res.status === 204 || res.status === 202) {
    return undefined as unknown as T;
  }
  const text = await res.text();
  if (!text || !text.trim()) {
    return undefined as unknown as T;
  }
  try {
    return JSON.parse(text);
  } catch {
    return text as unknown as T;
  }
}

// Cookie channel: embedded webchat (same-origin session cookie). Backend
// AdminAPI.Middleware authenticates via the chat session when no Bearer token
// is present (issue #788 A2).
async function adminFetchCookie<T>(
  path: string,
  options?: AdminFetchOptions,
): Promise<T> {
  const res = await fetch(`${adminUrl}${path}`, {
    ...options,
    ...authOpts(),
    headers: {
      'Content-Type': 'application/json',
      ...((options?.headers as Record<string, string> | undefined) ?? {}),
    },
  });

  if (!res.ok) {
    // Cookie-channel !ok (typically 401) means the chat session expired or was
    // revoked. TODO(issue #788 follow-up): signal AdminShell to re-probe the
    // channel (getMe → redirect to /) so the admin isn't left looking at
    // per-request error toasts. Until then the caller surfaces a normal error.
    const info = await parseApiError(res);
    const message = info.message || info.raw || `Admin request failed: ${res.status}`;
    const err = new Error(message);
    (err as any).status = info.status;
    if (info.code) (err as any).code = info.code;
    throw err;
  }

  return parseResponseJson<T>(res);
}

// ---------------------------------------------------------------------------
// Connection test (login page — always Bearer, validates a candidate token)
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

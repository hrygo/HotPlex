/**
 * Shared webchat API client helpers.
 *
 * Authentication strategy (cookie-based, spec §8/§11):
 *   - Same-origin (embedded webchat): credentials: 'same-origin' (cookie auth)
 *   - Cross-origin (external frontend): credentials: 'include' (cookie auth)
 *     + optional X-API-Key header (alongside cookie)
 *
 * Extracted from the byte-identical copies previously duplicated across
 * auth.ts / workspaces.ts / sessions.ts. New cookie-based API clients should
 * import from here instead of re-declaring these helpers.
 */
import { httpBase, apiKey, isSameOrigin } from "@/lib/config";

export const BASE = httpBase();

// Auth headers: X-API-Key attached only in cross-origin mode (optional).
export function authHeaders(): Record<string, string> {
  if (isSameOrigin()) return {};
  return apiKey ? { 'X-API-Key': apiKey } : {};
}

// Auth request options: cookie credentials propagation.
export function authOpts(): RequestInit {
  if (isSameOrigin()) return { credentials: 'same-origin' as RequestCredentials };
  // Cross-origin: include cookies so the cookie-based login session works.
  return { credentials: 'include' as RequestCredentials };
}

// Merge auth headers with custom headers (caller-provided headers win).
export function withAuth(headers?: Record<string, string>): Record<string, string> {
  return { ...authHeaders(), ...headers };
}

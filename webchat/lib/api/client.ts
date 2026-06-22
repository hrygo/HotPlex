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

// Parse the backend's structured error envelope {error:{code,message}}
// (written by gateway writeAppError). Returns the most specific message
// available, falling back to a status-derived string when the body is not
// JSON or lacks the envelope. Centralizing this fixes the prior mismatch
// where some calls read the non-existent top-level `message` field (→ the
// semantic error code like USER_DISABLED/FORBIDDEN was discarded in favor
// of a generic "Create invitation failed: 403").
export async function extractApiError(res: Response, fallback: string): Promise<string> {
  const errData = await res.json().catch(() => ({}));
  return errData?.error?.code || errData?.error?.message || fallback;
}

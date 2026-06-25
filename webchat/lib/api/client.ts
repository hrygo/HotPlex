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
import { parseApiError } from "./errors";

export const BASE = httpBase();

// hasSessionCookie reports whether a WebChat UI login cookie is present.
// Used to decide cookie-vs-api-key identity in cross-origin mode (see below).
function hasSessionCookie(): boolean {
  if (typeof document === 'undefined') return false;
  return document.cookie.split(';').some((c) => c.trim().startsWith('webchat_session='));
}

// Auth headers: X-API-Key attached only in cross-origin mode, and ONLY when no
// cookie session exists.
//
// Why: the backend resolves identity api-key-first (security.AuthenticateRequest).
// In cross-origin mode the browser cannot put an X-API-Key on the WebSocket
// upgrade, so WS authenticates via cookie while REST authenticates via the
// api-key. When a human is logged in (cookie present) AND an env api-key is
// configured, this splits REST (api-key user) and WS (cookie user) into two
// different identities — the REST workspace list returns workspaces the WS
// user does not own, and every handshake fails with "workspace access denied".
//
// Preferring the cookie when a session exists keeps REST and WS on the same
// (logged-in) identity. The env api-key remains the fallback for non-interactive
// embedded use where no human ever logs in.
export function authHeaders(): Record<string, string> {
  if (isSameOrigin()) return {};
  if (hasSessionCookie()) return {};
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
  const info = await parseApiError(res);
  return info.code || info.message || fallback;
}

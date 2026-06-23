/**
 * Shared backend error-envelope parser.
 *
 * Both the gateway cookie client (client.ts) and the admin Bearer client
 * (admin-client.ts) talk to endpoints that emit the same JSON envelope
 * {"error":{"code":...,"message":...}} written by web.WriteAppError. Parsing
 * it in one place keeps the two clients from drifting apart (PR #774 review
 * P3: admin-client and client.ts had opposite extraction priorities). The
 * parser is priority-agnostic — each caller decides whether to surface code
 * or message first for its audience.
 */

export interface ApiErrorInfo {
  status: number;
  code?: string;
  message?: string;
  /** Raw response body, used as a fallback when the body is not JSON. */
  raw: string;
}

export async function parseApiError(res: Response): Promise<ApiErrorInfo> {
  const raw = await res.text().catch(() => '');
  let code: string | undefined;
  let message: string | undefined;
  const body = raw.trim();
  if (body) {
    try {
      const env = JSON.parse(body);
      if (env?.error && typeof env.error === 'object') {
        code = env.error.code;
        message = env.error.message;
      } else if (typeof env?.error === 'string') {
        // Legacy plain-text shape (pre-P2.8 admin): {"error":"not found"}
        message = env.error;
      }
    } catch {
      // not JSON — raw body becomes the message fallback
    }
  }
  return { status: res.status, code, message, raw };
}

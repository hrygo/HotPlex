/**
 * Unified time formatting helpers for the webchat frontend.
 *
 * IMPORTANT — input units:
 *   - `number`  → treated as **milliseconds** (JS convention, `Date.getTime()`).
 *                 Backend unix *seconds* MUST be multiplied by 1000 at the
 *                 call site before being passed in (e.g. `formatDate(created_at * 1000)`).
 *                 Missing the `* 1000` produced 1970 dates in PR #762 — do not skip it.
 *   - `string`  → parsed as an ISO 8601 timestamp via `new Date(s)`.
 *   - `Date`    → used directly.
 *
 * Any value that cannot be resolved to a valid date (null/undefined/empty
 * string/non-finite number/invalid Date) yields the sentinel `'—'` so callers
 * can render it inline without extra null-checks.
 */

export type TimeInput = number | string | Date | undefined | null;

const FALLBACK = '—';

/**
 * Coerce a heterogeneous time value into a `Date`, or `null` if invalid.
 *
 * - `Date`      → returned as-is when valid, else null.
 * - `number`    → treated as **milliseconds**; `<= 0` or non-finite → null.
 * - `string`    → parsed via `new Date(s)`; empty / invalid → null.
 */
function toDate(value: TimeInput): Date | null {
  if (value instanceof Date) {
    return Number.isNaN(value.getTime()) ? null : value;
  }
  if (typeof value === 'number') {
    if (!Number.isFinite(value) || value <= 0) return null;
    const d = new Date(value);
    return Number.isNaN(d.getTime()) ? null : d;
  }
  if (typeof value === 'string') {
    if (value.length === 0) return null;
    const d = new Date(value);
    return Number.isNaN(d.getTime()) ? null : d;
  }
  return null;
}

/**
 * Calendar date: `toLocaleDateString(undefined, {year, month:'short', day})`.
 * Example: "Jun 19, 2026".
 */
export function formatDate(value: TimeInput): string {
  const d = toDate(value);
  if (!d) return FALLBACK;
  return d.toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  });
}

/**
 * Full date + time: `toLocaleString(undefined, {year, month:'short', day, hour:'2-digit', minute:'2-digit'})`.
 * Example: "Jun 19, 2026, 14:30".
 */
export function formatDateTime(value: TimeInput): string {
  const d = toDate(value);
  if (!d) return FALLBACK;
  return d.toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

/**
 * Relative time, falling back to absolute formats for old / future timestamps.
 *
 *   diff >= 0 (past):
 *     <60s   → "just now"
 *     <60m   → "${m}m ago"
 *     <24h   → "${h}h ago"
 *     <7d    → "${d}d ago"
 *     else   → formatDate(value)
 *
 *   diff < 0 (future):
 *     → formatDateTime(value)
 */
export function formatRelative(value: TimeInput): string {
  const d = toDate(value);
  if (!d) return FALLBACK;

  const diffMs = Date.now() - d.getTime();
  if (diffMs < 0) {
    return formatDateTime(d);
  }

  const diffSec = Math.floor(diffMs / 1000);
  const diffMin = Math.floor(diffMs / 60000);
  const diffHour = Math.floor(diffMs / 3600000);
  const diffDay = Math.floor(diffMs / 86400000);

  if (diffSec < 60) return 'just now';
  if (diffMin < 60) return `${diffMin}m ago`;
  if (diffHour < 24) return `${diffHour}h ago`;
  if (diffDay < 7) return `${diffDay}d ago`;
  return formatDate(d);
}

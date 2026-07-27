/**
 * Webchat configuration — single source of truth.
 *
 * All HOTPLEX_WEBCHAT_* env vars are defined here.
 * To add a new config:
 *   1. Add the env var to `.env.example` / `.env.local`
 *   2. Add the field here with a sensible default
 *   3. `next.config.mjs` auto-forwards all HOTPLEX_WEBCHAT_* vars to the client
 */

import type { WorkerType } from "@/lib/ai-sdk-transport/client/constants";

// -- Gateway -----------------------------------------------------------

function browserReachableUrl(rawUrl: string): string {
  if (typeof window === "undefined") return rawUrl;
  try {
    const url = new URL(rawUrl);
    if (url.hostname === "0.0.0.0" || url.hostname === "::") {
      url.hostname = window.location.hostname || "localhost";
      return url.toString();
    }
  } catch {
    // Keep malformed values unchanged so connection errors expose the bad config.
  }
  return rawUrl;
}

function resolveWsUrl(): string {
  const envUrl = process.env.HOTPLEX_WEBCHAT_WS_URL;
  if (envUrl) return browserReachableUrl(envUrl);
  // Auto-detect when served from the same Go binary (zero-config).
  if (typeof window !== "undefined") {
    const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
    // If running on next dev server (port 3000), default fallback should target gateway port 8888
    if (window.location.port === "3000") {
      return `${proto}//${window.location.hostname}:8888/ws`;
    }
    return `${proto}//${window.location.host}/ws`;
  }
  return "ws://localhost:8888/ws";
}

export const wsUrl: string = resolveWsUrl();

export const workerType: WorkerType =
  (process.env.HOTPLEX_WEBCHAT_WORKER_TYPE as WorkerType) ?? "claude_code";

export const apiKey: string =
  process.env.HOTPLEX_WEBCHAT_API_KEY ?? "";

// isSameOrigin returns true when the webchat is served from the same origin
// as the gateway (embedded via go:embed). In this mode, cookie auth is used
// instead of X-API-Key header, so no build-time secret is needed.
export function isSameOrigin(): boolean {
  return typeof window !== "undefined" && !process.env.HOTPLEX_WEBCHAT_WS_URL;
}

// -- Per-session init config -------------------------------------------

export const workDir: string =
  process.env.HOTPLEX_WEBCHAT_WORK_DIR ?? "";

export const rawAllowedTools: string =
  process.env.HOTPLEX_WEBCHAT_ALLOWED_TOOLS ?? "";

export const allowedTools: string[] = rawAllowedTools
  ? rawAllowedTools.split(",").map((s) => s.trim()).filter(Boolean)
  : [];

// -- Derived -----------------------------------------------------------

export type ConnectionState = 'connected' | 'connecting' | 'reconnecting' | 'disconnected' | 'already_connected';

export function httpBase(): string {
  return (
    wsUrl
      .replace(/^ws:\/\//, "http://")
      .replace(/^wss:\/\//, "https://")
      .replace(/\/ws\/?$/, "")
  );
}

// -- Admin -----------------------------------------------------------------

export const adminUrl: string =
  browserReachableUrl(process.env.HOTPLEX_WEBCHAT_ADMIN_URL ?? 'http://localhost:9999');

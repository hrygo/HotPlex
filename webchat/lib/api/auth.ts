import { httpBase, apiKey, isSameOrigin } from "@/lib/config";

const BASE = httpBase();

function authHeaders(): Record<string, string> {
  if (isSameOrigin()) return {};
  return apiKey ? { 'X-API-Key': apiKey } : {};
}

function authOpts(): RequestInit {
  if (isSameOrigin()) return { credentials: 'same-origin' as RequestCredentials };
  // Cross-origin: include cookies so the cookie-based login session works.
  return { credentials: 'include' as RequestCredentials };
}

function withAuth(headers?: Record<string, string>): Record<string, string> {
  return { ...authHeaders(), ...headers };
}

export interface User {
  id: string;
  username: string;
  role: 'admin' | 'user';
  status: 'active' | 'disabled';
  display_name?: string;
  created_at: number;
  updated_at: number;
  last_login_at?: number;
}

export interface Invitation {
  id: string;
  code: string;
  created_by: string;
  role: 'admin' | 'user';
  expires_at: number;
  created_at?: number;
  used_at?: number;
}

export interface OAuthProvider {
  name: string;
  display_name: string;
}

export interface LoginResult {
  user_id: string;
  first_login: boolean;
}

// BootstrapStatus: whether any admin exists. Drives the first-time-setup guide
// on the login page. Public endpoint (no auth).
export async function getBootstrapStatus(signal?: AbortSignal): Promise<boolean> {
  const res = await fetch(`${BASE}/api/auth/bootstrap-status`, {
    ...authOpts(),
    signal,
  });
  if (!res.ok) {
    // Degrade: treat unreachable as "bootstrapped" so the normal login form
    // shows (better than blocking login on a transient backend error). A 5xx
    // (e.g. DB outage) is logged so it is not silently masked.
    if (res.status >= 500) {
      console.warn('bootstrap-status check failed', res.status);
    }
    return true;
  }
  const data = await res.json();
  return Boolean(data?.bootstrapped);
}

// Me (Get current profile)
export async function getMe(signal?: AbortSignal): Promise<User> {
  const res = await fetch(`${BASE}/api/auth/me`, {
    headers: withAuth({ 'Content-Type': 'application/json' }),
    ...authOpts(),
    signal,
  });
  if (!res.ok) {
    throw new Error(`getMe failed: ${res.status}`);
  }
  return res.json();
}

// Login
export async function login(username: string, password: string, signal?: AbortSignal): Promise<LoginResult> {
  const res = await fetch(`${BASE}/api/auth/login`, {
    method: 'POST',
    headers: withAuth({ 'Content-Type': 'application/json' }),
    body: JSON.stringify({ username, password }),
    ...authOpts(),
    signal,
  });
  if (!res.ok) {
    const errData = await res.json().catch(() => ({}));
    throw new Error(errData?.error?.code || errData?.error?.message || `Login failed: ${res.status}`);
  }
  return res.json();
}

// Logout
export async function logout(signal?: AbortSignal): Promise<void> {
  const res = await fetch(`${BASE}/api/auth/logout`, {
    method: 'POST',
    headers: authHeaders(),
    ...authOpts(),
    signal,
  });
  if (!res.ok) {
    throw new Error(`Logout failed: ${res.status}`);
  }
}

// Accept Invite
export async function acceptInvite(code: string, username: string, password: string, signal?: AbortSignal): Promise<LoginResult> {
  const res = await fetch(`${BASE}/api/auth/accept-invite`, {
    method: 'POST',
    headers: withAuth({ 'Content-Type': 'application/json' }),
    body: JSON.stringify({ code, username, password }),
    ...authOpts(),
    signal,
  });
  if (!res.ok) {
    const errData = await res.json().catch(() => ({}));
    throw new Error(errData?.error?.code || errData?.error?.message || `Accept invite failed: ${res.status}`);
  }
  return res.json();
}

// OAuth Providers
export async function getOAuthProviders(signal?: AbortSignal): Promise<OAuthProvider[]> {
  const res = await fetch(`${BASE}/api/auth/oauth/providers`, {
    headers: withAuth({ 'Content-Type': 'application/json' }),
    ...authOpts(),
    signal,
  });
  if (!res.ok) {
    // If not configured or failed, degrade gracefully to empty list
    return [];
  }
  return res.json();
}

// Admin: Create Invitation
export async function adminCreateInvitation(role: 'admin' | 'user', ttlSeconds?: number, signal?: AbortSignal): Promise<Invitation> {
  const res = await fetch(`${BASE}/api/admin/invitations`, {
    method: 'POST',
    headers: withAuth({ 'Content-Type': 'application/json' }),
    body: JSON.stringify({ role, ttl: ttlSeconds }),
    ...authOpts(),
    signal,
  });
  if (!res.ok) {
    const errData = await res.json().catch(() => ({}));
    throw new Error(errData.message || `Create invitation failed: ${res.status}`);
  }
  return res.json();
}

// Admin: List Invitations
export interface ListInvitationsResponse {
  invitations: Invitation[];
  limit: number;
  offset: number;
}

export async function adminListInvitations(limit = 100, offset = 0, signal?: AbortSignal): Promise<ListInvitationsResponse> {
  const res = await fetch(`${BASE}/api/admin/invitations?limit=${limit}&offset=${offset}`, {
    headers: withAuth({ 'Content-Type': 'application/json' }),
    ...authOpts(),
    signal,
  });
  if (!res.ok) {
    const errData = await res.json().catch(() => ({}));
    throw new Error(errData.message || `List invitations failed: ${res.status}`);
  }
  return res.json();
}

// Admin: Delete Invitation
export async function adminDeleteInvitation(id: string, signal?: AbortSignal): Promise<void> {
  const res = await fetch(`${BASE}/api/admin/invitations/${id}`, {
    method: 'DELETE',
    headers: authHeaders(),
    ...authOpts(),
    signal,
  });
  if (!res.ok) {
    const errData = await res.json().catch(() => ({}));
    throw new Error(errData.message || `Delete invitation failed: ${res.status}`);
  }
}

// Admin: List Users
export interface ListUsersResponse {
  users: User[];
  limit: number;
  offset: number;
}

export async function adminListUsers(limit = 100, offset = 0, signal?: AbortSignal): Promise<ListUsersResponse> {
  const res = await fetch(`${BASE}/api/admin/users?limit=${limit}&offset=${offset}`, {
    headers: withAuth({ 'Content-Type': 'application/json' }),
    ...authOpts(),
    signal,
  });
  if (!res.ok) {
    const errData = await res.json().catch(() => ({}));
    throw new Error(errData.message || `List users failed: ${res.status}`);
  }
  return res.json();
}

// Admin: Update User Status
export async function adminUpdateUserStatus(id: string, status: 'active' | 'disabled', signal?: AbortSignal): Promise<void> {
  const res = await fetch(`${BASE}/api/admin/users/${id}`, {
    method: 'PATCH',
    headers: withAuth({ 'Content-Type': 'application/json' }),
    body: JSON.stringify({ status }),
    ...authOpts(),
    signal,
  });
  if (!res.ok) {
    const errData = await res.json().catch(() => ({}));
    throw new Error(errData.message || `Update user status failed: ${res.status}`);
  }
}

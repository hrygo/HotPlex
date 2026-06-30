'use client';

import { useState, useEffect, useCallback } from 'react';
import {
  getStoredAdminConnection,
  storeAdminConnection,
  clearAdminConnection,
  testConnection,
} from '@/lib/api/admin-client';
import type { AdminConnection } from '@/lib/types/admin';

export type AuthState = 'checking' | 'authenticated' | 'unauthenticated';

// enabled gates the mount-time token probe. AdminShell passes false until it
// resolves the cookie channel, so embedded webchat admins (cookie-admin) never
// pay for a redundant testConnection that 401s against a stale stored token
// (issue #788 review P1-3). state stays 'checking' while disabled.
export function useAdminAuth(enabled = true) {
  const [state, setState] = useState<AuthState>('checking');
  const [conn, setConn] = useState<AdminConnection | null>(null);

  // Check localStorage on mount, then probe the server to confirm the token
  // is still valid (P2.12). Without this probe, a stale token in localStorage
  // would set state='authenticated' and AdminShell would render an empty shell
  // (every API call 401). testConnection hits /admin/health which requires
  // health:read scope, so it doubles as a scope sanity check.
  useEffect(() => {
    if (!enabled) return; // stay 'checking'; AdminShell re-enables when needed
    const stored = getStoredAdminConnection();
    if (!stored) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- gate: no stored creds → unauthenticated
      setState('unauthenticated');
      return;
    }
    testConnection(stored).then((ok) => {
      if (ok) {
        setConn(stored);
        setState('authenticated');
      } else {
        clearAdminConnection();
        setConn(null);
        setState('unauthenticated');
      }
    });
  }, [enabled]);

  const login = useCallback(async (url: string, token: string): Promise<boolean> => {
    const candidate: AdminConnection = { url, token };
    const ok = await testConnection(candidate);
    if (ok) {
      storeAdminConnection(candidate);
      setConn(candidate);
      setState('authenticated');
      return true;
    }
    return false;
  }, []);

  const logout = useCallback(() => {
    clearAdminConnection();
    setConn(null);
    setState('unauthenticated');
  }, []);

  return { state, conn, login, logout };
}

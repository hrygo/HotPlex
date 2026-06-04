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

export function useAdminAuth() {
  const [state, setState] = useState<AuthState>('checking');
  const [conn, setConn] = useState<AdminConnection | null>(null);

  // Check localStorage on mount
  useEffect(() => {
    const stored = getStoredAdminConnection();
    if (stored) {
      setConn(stored);
      setState('authenticated');
    } else {
      setState('unauthenticated');
    }
  }, []);

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

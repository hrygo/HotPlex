'use client';

import { useState, useEffect } from 'react';
import { getMe, type User } from '@/lib/api/auth';
import { formatDateTime } from '@/lib/utils/format-time';

export default function AccountPage() {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    getMe()
      .then((u) => { if (!cancelled) setUser(u); })
      .catch((err) => { if (!cancelled) setError(err instanceof Error ? err.message : String(err)); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, []);

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-screen bg-[var(--bg-base)]">
        <div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
      </div>
    );
  }

  if (error || !user) {
    return (
      <div className="min-h-screen bg-[var(--bg-base)] p-6 flex items-center justify-center">
        <div className="max-w-md text-center">
          <p className="text-sm text-[var(--accent-coral)] mb-2">{error || 'Failed to load profile'}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-[var(--bg-base)] p-6">
      <div className="max-w-2xl mx-auto">
        <h1 className="text-xl font-display font-bold text-[var(--text-primary)] mb-1">Account</h1>
        <p className="text-xs text-[var(--text-faint)] mb-6">Your account profile</p>

        <div className="rounded-[var(--radius-md)] border border-[var(--border-default)] bg-[var(--bg-elevated)] p-6 space-y-4">
          {/* Avatar + name */}
          <div className="flex items-center gap-4">
            <div className="w-14 h-14 rounded-full bg-[var(--accent-gold)]/10 border border-[var(--accent-gold)] flex items-center justify-center font-display font-black text-xl text-[var(--accent-gold)] uppercase">
              {user.username.slice(0, 2)}
            </div>
            <div className="min-w-0">
              <h2 className="font-display font-bold text-base text-[var(--text-primary)] truncate">
                {user.display_name || user.username}
              </h2>
              <span className="inline-flex items-center gap-1.5 mt-1 px-2 py-0.5 rounded-full text-[10px] font-mono font-semibold bg-[var(--bg-hover)] text-[var(--text-secondary)] border border-[var(--border-subtle)] uppercase">
                {user.role}
              </span>
            </div>
          </div>

          {/* Fields */}
          <div className="border-t border-[var(--border-subtle)] pt-4 space-y-3 text-xs font-mono">
            <div className="flex justify-between gap-4">
              <span className="text-[var(--text-faint)] font-semibold uppercase">Account ID</span>
              <span className="text-[var(--text-primary)] select-all text-right break-all">{user.id}</span>
            </div>
            <div className="flex justify-between gap-4">
              <span className="text-[var(--text-faint)] font-semibold uppercase">Username</span>
              <span className="text-[var(--text-primary)]">{user.username}</span>
            </div>
            <div className="flex justify-between gap-4">
              <span className="text-[var(--text-faint)] font-semibold uppercase">Status</span>
              <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] font-bold ${
                user.status === 'active'
                  ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
                  : 'bg-rose-500/10 text-rose-400 border border-rose-500/20'
              }`}>
                <span className={`w-1 h-1 rounded-full ${user.status === 'active' ? 'bg-emerald-400' : 'bg-rose-400'}`} />
                {user.status}
              </span>
            </div>
            <div className="flex justify-between gap-4">
              <span className="text-[var(--text-faint)] font-semibold uppercase">Created</span>
              <span className="text-[var(--text-primary)]">{formatDateTime(user.created_at * 1000)}</span>
            </div>
            <div className="flex justify-between gap-4">
              <span className="text-[var(--text-faint)] font-semibold uppercase">Last Login</span>
              <span className="text-[var(--text-primary)]">{formatDateTime((user.last_login_at ?? 0) * 1000)}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

'use client';

import { useState, useEffect, useCallback, useRef } from 'react';
import {
  adminListUsers,
  adminUpdateUserStatus,
  adminListInvitations,
  adminCreateInvitation,
  adminDeleteInvitation,
  type User,
  type Invitation,
} from '@/lib/api/auth';

const DEFAULT_INVITE_TTL = 7 * 24 * 3600; // 7 days, matches backend default

export function MembersTab() {
  const [users, setUsers] = useState<User[]>([]);
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actionMsg, setActionMsg] = useState<{ kind: 'ok' | 'err'; text: string } | null>(null);

  const flash = (kind: 'ok' | 'err', text: string) => {
    setActionMsg({ kind, text });
    setTimeout(() => setActionMsg(null), 2500);
  };

  const abortRef = useRef<AbortController | null>(null);

  // AbortController guards against (a) rapid re-loads racing stale responses
  // onto fresh state and (b) in-flight requests resolving after unmount (PR #779
  // review P2-1). Each load() aborts the previous; unmount aborts whatever remains.
  const load = useCallback(async () => {
    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    setLoading(true);
    setError(null);
    try {
      const [u, inv] = await Promise.all([
        adminListUsers(100, 0, ctrl.signal),
        adminListInvitations(100, 0, ctrl.signal),
      ]);
      if (ctrl.signal.aborted) return;
      setUsers(u.users || []);
      setInvitations(inv.invitations || []);
    } catch (err) {
      if (ctrl.signal.aborted) return;
      setError(err instanceof Error ? err.message : 'Load failed');
    } finally {
      if (!ctrl.signal.aborted) setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
    return () => abortRef.current?.abort();
  }, [load]);

  const handleToggleUser = async (user: User) => {
    const next = user.status === 'active' ? 'disabled' : 'active';
    try {
      await adminUpdateUserStatus(user.id, next);
      flash('ok', `${user.username} → ${next}`);
      load();
    } catch (err) {
      flash('err', err instanceof Error ? err.message : 'Update failed');
    }
  };

  const handleCreateInvitation = async () => {
    try {
      await adminCreateInvitation('user', DEFAULT_INVITE_TTL);
      flash('ok', 'Invitation created');
      load();
    } catch (err) {
      flash('err', err instanceof Error ? err.message : 'Create failed');
    }
  };

  const handleDeleteInvitation = async (id: string) => {
    try {
      await adminDeleteInvitation(id);
      flash('ok', 'Invitation deleted');
      load();
    } catch (err) {
      flash('err', err instanceof Error ? err.message : 'Delete failed');
    }
  };

  if (loading) {
    return <div className="text-center py-8 text-sm text-[var(--text-muted)]">Loading…</div>;
  }
  if (error) {
    return <div className="text-center py-8 text-sm text-[var(--accent-coral)] font-bold">{error}</div>;
  }

  return (
    <div className="space-y-6">
      {actionMsg && (
        <div className={`px-3 py-2 rounded-[var(--radius-md)] border text-xs font-bold ${actionMsg.kind === 'ok' ? 'bg-[var(--accent-emerald)]/10 border-[var(--accent-emerald)]/20 text-[var(--accent-emerald)]' : 'bg-[var(--accent-coral)]/10 border-[var(--accent-coral)]/20 text-[var(--accent-coral)]'}`}>
          {actionMsg.text}
        </div>
      )}

      {/* Users */}
      <section>
        <h3 className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest mb-2">Users</h3>
        <div className="space-y-1">
          {users.map((u) => (
            <div key={u.id} className="flex items-center justify-between py-2 px-3 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-subtle)] gap-3">
              <div className="min-w-0">
                <div className="text-sm font-bold text-[var(--text-primary)] truncate">{u.username}{u.display_name ? ` · ${u.display_name}` : ''}</div>
                <div className="text-[10px] text-[var(--text-muted)] font-mono">{u.role} · {u.status}</div>
              </div>
              <button
                onClick={() => handleToggleUser(u)}
                className={`px-3 py-1 rounded-md text-[10px] font-bold transition-all flex-shrink-0 ${u.status === 'active' ? 'bg-[var(--accent-coral)]/10 text-[var(--accent-coral)] hover:bg-[var(--accent-coral)]/20' : 'bg-[var(--accent-emerald)]/10 text-[var(--accent-emerald)] hover:bg-[var(--accent-emerald)]/20'}`}
              >
                {u.status === 'active' ? 'Disable' : 'Enable'}
              </button>
            </div>
          ))}
          {users.length === 0 && <p className="text-xs text-[var(--text-muted)] py-2">No users</p>}
        </div>
      </section>

      {/* Invitations */}
      <section>
        <div className="flex items-center justify-between mb-2">
          <h3 className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest">Invitations</h3>
          <button
            onClick={handleCreateInvitation}
            className="px-3 py-1 rounded-md bg-[var(--accent-gold)] text-black text-[10px] font-bold hover:bg-[var(--accent-gold-bright)] transition-all"
          >
            + New
          </button>
        </div>
        <div className="space-y-1">
          {invitations.map((inv) => {
            const used = !!inv.used_at;
            const expired = !used && inv.expires_at * 1000 < Date.now();
            const state = used ? 'used' : expired ? 'expired' : 'active';
            return (
              <div key={inv.id} className="flex items-center justify-between py-2 px-3 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-subtle)] gap-3">
                <div className="min-w-0">
                  <div className="text-sm font-mono font-bold text-[var(--text-primary)] truncate">{inv.code}</div>
                  <div className="text-[10px] text-[var(--text-muted)]">
                    {inv.role} · {state}{!used && !expired ? ` · expires ${new Date(inv.expires_at * 1000).toLocaleDateString()}` : ''}
                  </div>
                </div>
                <button
                  onClick={() => handleDeleteInvitation(inv.id)}
                  className="px-2 py-1 rounded-md text-[10px] font-bold text-[var(--text-muted)] hover:text-[var(--accent-coral)] hover:bg-[var(--bg-hover)] transition-all flex-shrink-0"
                >
                  Delete
                </button>
              </div>
            );
          })}
          {invitations.length === 0 && <p className="text-xs text-[var(--text-muted)] py-2">No invitations</p>}
        </div>
      </section>
    </div>
  );
}

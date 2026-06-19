'use client';

import { useState, useEffect, useRef } from 'react';
import {
  getMe,
  adminListUsers,
  adminUpdateUserStatus,
  adminListInvitations,
  adminCreateInvitation,
  adminDeleteInvitation,
  type User,
  type Invitation,
} from '@/lib/api/auth';
import { useAdminUI } from '@/context/admin-ui-context';
import { formatDate } from '@/lib/utils/format-time';
import { useResource } from '@/hooks/use-resource';
import { LoadingState, ErrorState } from '@/components/admin/resource-states';

const selectClass =
  'w-full rounded-[var(--radius-sm)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] px-3 py-2 text-sm text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors appearance-none';

const labelClass =
  'block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5';

export default function MembersPage() {
  const { confirm, showToast } = useAdminUI();

  const [currentUser, setCurrentUser] = useState<User | null>(null);
  const [userLoaded, setUserLoaded] = useState(false);

  // Fetch users/invitations only once currentUser resolves as admin. The
  // fetcher returns an empty array while the role is unknown or non-admin,
  // and the deps array re-triggers a real fetch when the role flips to admin.
  const { data: usersData, loading: usersLoading, error: usersError, reload: reloadUsers } =
    useResource<User[]>(async () => {
      if (currentUser?.role !== 'admin') return [];
      const res = await adminListUsers();
      return res.users ?? [];
    }, [currentUser?.role]);
  const users = usersData ?? [];

  const { data: invitesData, loading: invitesLoading, error: invitesError, reload: reloadInvites } =
    useResource<Invitation[]>(async () => {
      if (currentUser?.role !== 'admin') return [];
      const res = await adminListInvitations();
      return res.invitations ?? [];
    }, [currentUser?.role]);
  const invitations = invitesData ?? [];

  const [inviteRole, setInviteRole] = useState<'user' | 'admin'>('user');
  const [inviteTTL, setInviteTTL] = useState(86400);
  const [generatedCode, setGeneratedCode] = useState('');
  const [creatingInvite, setCreatingInvite] = useState(false);
  const [inviteError, setInviteError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const copyTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const [togglingId, setTogglingId] = useState<string | null>(null);
  const [revokingId, setRevokingId] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    getMe()
      .then((u) => { if (!cancelled) setCurrentUser(u); })
      .catch(() => {})
      .finally(() => { if (!cancelled) setUserLoaded(true); });
    return () => { cancelled = true; };
  }, []);

  // Clear the copy-reset timer on unmount to avoid setState after unmount.
  useEffect(() => () => { if (copyTimer.current) clearTimeout(copyTimer.current); }, []);

  const handleToggle = async (u: User) => {
    const next: 'active' | 'disabled' = u.status === 'active' ? 'disabled' : 'active';
    const isDisable = next === 'disabled';
    const ok = await confirm(
      isDisable ? 'Disable user' : 'Enable user',
      isDisable
        ? `Disable "${u.username}"? They will lose access immediately.`
        : `Enable "${u.username}"? They will regain access.`,
      { destructive: isDisable, confirmLabel: isDisable ? 'Disable' : 'Enable' },
    );
    if (!ok) return;
    setTogglingId(u.id);
    try {
      await adminUpdateUserStatus(u.id, next);
      await reloadUsers();
      showToast(`${u.username} ${next}`, 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to update user', 'error');
    } finally {
      setTogglingId(null);
    }
  };

  const handleRevoke = async (id: string) => {
    const ok = await confirm(
      'Revoke invitation',
      'Revoke this invitation key? It can no longer be used to register.',
      { destructive: true, confirmLabel: 'Revoke' },
    );
    if (!ok) return;
    setRevokingId(id);
    try {
      await adminDeleteInvitation(id);
      await reloadInvites();
      showToast('Invitation revoked', 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to revoke invitation', 'error');
    } finally {
      setRevokingId(null);
    }
  };

  const handleCreateInvite = async () => {
    setCreatingInvite(true);
    setInviteError(null);
    setGeneratedCode('');
    try {
      const inv = await adminCreateInvitation(inviteRole, inviteTTL);
      setGeneratedCode(inv.code);
      showToast('Invitation generated', 'success');
      await reloadInvites();
    } catch (err) {
      setInviteError(err instanceof Error ? err.message : 'Failed to create invitation');
    } finally {
      setCreatingInvite(false);
    }
  };

  const copyCode = async (code: string) => {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      if (copyTimer.current) clearTimeout(copyTimer.current);
      copyTimer.current = setTimeout(() => setCopied(false), 2000);
    } catch {
      showToast('Failed to copy to clipboard', 'error');
    }
  };

  if (!userLoaded) {
    return (
      <div className="flex items-center justify-center min-h-screen bg-[var(--bg-base)]">
        <div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
      </div>
    );
  }

  if (currentUser?.role !== 'admin') {
    return (
      <div className="min-h-screen bg-[var(--bg-base)] p-6 flex items-center justify-center">
        <div className="max-w-md text-center">
          <h1 className="text-xl font-display font-bold text-[var(--text-primary)] mb-2">Admins Only</h1>
          <p className="text-sm text-[var(--text-faint)]">
            Member and invitation management requires administrator privileges.
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-[var(--bg-base)] p-6">
      <div className="max-w-5xl mx-auto">
        {/* Header */}
        <div className="flex items-center justify-between mb-6">
          <div>
            <h1 className="text-xl font-display font-bold text-[var(--text-primary)]">Members</h1>
            <p className="text-xs text-[var(--text-faint)] mt-1">Manage team members and invitation keys</p>
          </div>
          <button
            onClick={() => { reloadUsers(); reloadInvites(); }}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] text-[var(--text-faint)] hover:text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] transition-colors text-[11px] font-bold uppercase tracking-wider"
          >
            Refresh
          </button>
        </div>

        {/* Active Members */}
        <section className="mb-8">
          <h2 className="text-xs font-bold text-[var(--text-faint)] uppercase tracking-wider mb-3">Active Members</h2>
          {usersError && (
            <div className="mb-3 rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] p-3 text-xs text-[var(--accent-coral)]">
              {usersError}
            </div>
          )}
          {usersLoading ? (
            <p className="text-xs text-[var(--text-faint)] py-6 text-center">Loading members...</p>
          ) : (
            <div className="rounded-[var(--radius-md)] border border-[var(--border-subtle)] overflow-hidden bg-[var(--bg-surface)]">
              <table className="w-full text-left border-collapse text-xs">
                <thead>
                  <tr className="bg-[var(--bg-elevated)] border-b border-[var(--border-subtle)] text-[var(--text-faint)] uppercase tracking-wider font-bold">
                    <th className="p-3">Username</th>
                    <th className="p-3">Role</th>
                    <th className="p-3">Status</th>
                    <th className="p-3">Created</th>
                    <th className="p-3 text-right">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {users.map((u) => (
                    <tr key={u.id} className="border-b border-[var(--border-subtle)] last:border-0 hover:bg-[var(--bg-hover)]">
                      <td className="p-3 font-semibold text-[var(--text-primary)]">
                        {u.display_name ? (
                          <span>
                            {u.display_name} <span className="text-[var(--text-faint)] font-normal">@{u.username}</span>
                          </span>
                        ) : (
                          u.username
                        )}
                      </td>
                      <td className="p-3 font-mono text-[var(--text-muted)] uppercase">{u.role}</td>
                      <td className="p-3">
                        <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] font-bold ${
                          u.status === 'active'
                            ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
                            : 'bg-rose-500/10 text-rose-400 border border-rose-500/20'
                        }`}>
                          <span className={`w-1 h-1 rounded-full ${u.status === 'active' ? 'bg-emerald-400' : 'bg-rose-400'}`} />
                          {u.status}
                        </span>
                      </td>
                      <td className="p-3 text-[var(--text-faint)] font-mono">{formatDate(u.created_at * 1000)}</td>
                      <td className="p-3 text-right">
                        {u.id !== currentUser?.id && (
                          <button
                            onClick={() => handleToggle(u)}
                            disabled={togglingId === u.id}
                            className={`px-2.5 py-1 rounded text-[10px] font-bold uppercase tracking-wider transition-all border disabled:opacity-50 ${
                              u.status === 'active'
                                ? 'border-rose-500/30 text-rose-400 hover:bg-rose-500/10'
                                : 'border-emerald-500/30 text-emerald-400 hover:bg-emerald-500/10'
                            }`}
                          >
                            {togglingId === u.id ? '...' : u.status === 'active' ? 'Disable' : 'Enable'}
                          </button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>

        {/* Invitations */}
        <section className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          {/* Generate */}
          <div>
            <h2 className="text-xs font-bold text-[var(--text-faint)] uppercase tracking-wider mb-3">Generate Invitation</h2>
            <form
              onSubmit={(e) => { e.preventDefault(); handleCreateInvite(); }}
              className="rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-5 space-y-4"
            >
              <div>
                <label className={labelClass}>Role</label>
                <select
                  value={inviteRole}
                  onChange={(e) => setInviteRole(e.target.value as 'user' | 'admin')}
                  className={selectClass}
                >
                  <option value="user">User (normal access)</option>
                  <option value="admin">Admin (full privilege)</option>
                </select>
              </div>
              <div>
                <label className={labelClass}>Lifespan</label>
                <select
                  value={inviteTTL}
                  onChange={(e) => setInviteTTL(Number(e.target.value))}
                  className={selectClass}
                >
                  <option value={3600}>1 Hour</option>
                  <option value={86400}>24 Hours</option>
                  <option value={604800}>7 Days</option>
                  <option value={2592000}>30 Days</option>
                </select>
              </div>

              {inviteError && (
                <p className="text-[11px] text-[var(--accent-coral)]">{inviteError}</p>
              )}

              <button
                type="submit"
                disabled={creatingInvite}
                className="w-full px-4 py-2 rounded-[var(--radius-sm)] text-xs font-bold uppercase tracking-wider bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {creatingInvite ? 'Generating...' : 'Generate Invite'}
              </button>

              {generatedCode && (
                <div className="rounded-[var(--radius-sm)] bg-[var(--bg-elevated)] border border-[rgba(16,185,129,0.3)] p-3">
                  <p className="text-[10px] text-emerald-400 font-bold uppercase tracking-wide mb-1.5">Registration Key</p>
                  <div className="flex gap-2 items-center">
                    <code className="flex-1 text-xs text-[var(--text-primary)] bg-[var(--bg-surface)] p-2 rounded border border-[var(--border-subtle)] select-all truncate font-mono">
                      {generatedCode}
                    </code>
                    <button
                      onClick={() => copyCode(generatedCode)}
                      className="px-2.5 py-2 text-[10px] font-bold uppercase tracking-wider bg-emerald-500 text-black rounded hover:bg-emerald-400 transition-colors"
                    >
                      {copied ? 'Copied' : 'Copy'}
                    </button>
                  </div>
                </div>
              )}
            </form>
          </div>

          {/* Pending */}
          <div>
            <h2 className="text-xs font-bold text-[var(--text-faint)] uppercase tracking-wider mb-3">Pending Invitations</h2>
            <div className="rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-3 min-h-[120px]">
              {invitesError ? (
                <p className="text-xs text-[var(--accent-coral)] py-6 text-center">{invitesError}</p>
              ) : invitesLoading ? (
                <p className="text-xs text-[var(--text-faint)] py-6 text-center">Loading invitations...</p>
              ) : invitations.length === 0 ? (
                <p className="text-xs text-[var(--text-faint)] py-6 text-center">No pending invitation keys.</p>
              ) : (
                <div className="space-y-2">
                  {invitations.map((i) => (
                    <div
                      key={i.id}
                      className="flex items-center justify-between bg-[var(--bg-elevated)] border border-[var(--border-subtle)] p-2.5 rounded-[var(--radius-sm)] text-xs font-mono"
                    >
                      <div className="min-w-0 flex-1 pr-3">
                        <div className="text-[var(--text-primary)] font-bold truncate select-all">{i.code}</div>
                        <div className="text-[9px] text-[var(--text-faint)] uppercase mt-0.5">
                          Role: {i.role} · Expires: {formatDate(i.expires_at * 1000)}
                        </div>
                      </div>
                      <button
                        onClick={() => handleRevoke(i.id)}
                        disabled={revokingId === i.id}
                        className="text-[10px] font-bold text-[var(--accent-coral)] hover:underline flex-shrink-0 disabled:opacity-50"
                      >
                        {revokingId === i.id ? '...' : 'Revoke'}
                      </button>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        </section>
      </div>
    </div>
  );
}

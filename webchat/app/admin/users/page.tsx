'use client';

import { useState, useEffect, useCallback, useRef } from 'react';
import {
  adminListUsers,
  adminUpdateUserStatus,
  adminResetUserPassword,
  adminListInvitations,
  adminCreateInvitation,
  adminDeleteInvitation,
  logout,
  type User,
  type Invitation,
} from '@/lib/api/auth';
import { useTranslation } from 'react-i18next';

const DEFAULT_INVITE_TTL = 7 * 24 * 3600;

interface ResetModalState {
  isOpen: boolean;
  user: User | null;
  newPassword: string;
  confirmPassword: string;
  error: string | null;
  submitting: boolean;
}

export default function AdminUsersPage() {
  const { t } = useTranslation(['admin', 'common', 'chat']);
  const [users, setUsers] = useState<User[]>([]);
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [actionMsg, setActionMsg] = useState<{ kind: 'ok' | 'err'; text: string } | null>(null);
  const [busyUserId, setBusyUserId] = useState<string | null>(null);
  const [copiedInviteId, setCopiedInviteId] = useState<string | null>(null);

  // Reset password modal state
  const [resetModal, setResetModal] = useState<ResetModalState>({
    isOpen: false,
    user: null,
    newPassword: '',
    confirmPassword: '',
    error: null,
    submitting: false,
  });

  const flashTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const copyTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  const flash = (kind: 'ok' | 'err', text: string) => {
    setActionMsg({ kind, text });
    if (flashTimer.current) clearTimeout(flashTimer.current);
    flashTimer.current = setTimeout(() => setActionMsg(null), 3000);
  };

  const loadData = useCallback(async () => {
    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    setLoading(true);
    setError(null);
    try {
      const [uRes, invRes] = await Promise.all([
        adminListUsers(200, 0, ctrl.signal),
        adminListInvitations(200, 0, ctrl.signal),
      ]);
      if (ctrl.signal.aborted) return;
      setUsers(uRes.users || []);
      setInvitations(invRes.invitations || []);
    } catch (err) {
      if (ctrl.signal.aborted) return;
      setError(err instanceof Error ? err.message : 'Failed to load user data');
    } finally {
      if (!ctrl.signal.aborted) setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadData();
    return () => {
      abortRef.current?.abort();
      if (flashTimer.current) clearTimeout(flashTimer.current);
      if (copyTimer.current) clearTimeout(copyTimer.current);
    };
  }, [loadData]);

  const handleToggleUserStatus = async (user: User) => {
    const nextStatus = user.status === 'active' ? 'disabled' : 'active';
    setBusyUserId(user.id);
    try {
      await adminUpdateUserStatus(user.id, nextStatus);
      flash('ok', t('admin:users.reset_modal.success'));
      loadData();
    } catch (err) {
      flash('err', err instanceof Error ? err.message : 'Update user status failed');
    } finally {
      setBusyUserId(null);
    }
  };

  const openResetModal = (user: User) => {
    setResetModal({
      isOpen: true,
      user,
      newPassword: '',
      confirmPassword: '',
      error: null,
      submitting: false,
    });
  };

  const closeResetModal = () => {
    setResetModal({
      isOpen: false,
      user: null,
      newPassword: '',
      confirmPassword: '',
      error: null,
      submitting: false,
    });
  };

  const handleResetPasswordSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!resetModal.user) return;

    if (resetModal.newPassword.length < 6) {
      setResetModal((prev) => ({
        ...prev,
        error: t('admin:users.reset_modal.min_length_hint'),
      }));
      return;
    }

    if (resetModal.newPassword !== resetModal.confirmPassword) {
      setResetModal((prev) => ({
        ...prev,
        error: t('admin:users.reset_modal.password_mismatch'),
      }));
      return;
    }

    setResetModal((prev) => ({ ...prev, submitting: true, error: null }));
    try {
      await adminResetUserPassword(resetModal.user.id, resetModal.newPassword);
      flash(
        'ok',
        `${t('admin:users.reset_modal.success')} (${resetModal.user.username})`
      );
      closeResetModal();
    } catch (err) {
      setResetModal((prev) => ({
        ...prev,
        submitting: false,
        error: err instanceof Error ? err.message : t('admin:users.reset_modal.failed'),
      }));
    }
  };

  const handleCreateInvitation = async () => {
    try {
      await adminCreateInvitation('user', DEFAULT_INVITE_TTL);
      flash('ok', t('chat:settings.success.invite_generated'));
      loadData();
    } catch (err) {
      flash('err', err instanceof Error ? err.message : t('chat:settings.error.create_failed'));
    }
  };

  const handleDeleteInvitation = async (id: string) => {
    try {
      await adminDeleteInvitation(id);
      flash('ok', t('chat:settings.success.invite_deleted'));
      loadData();
    } catch (err) {
      flash('err', err instanceof Error ? err.message : t('chat:settings.error.delete_failed'));
    }
  };

  const handleCopyInviteCode = (id: string, code: string) => {
    navigator.clipboard.writeText(code);
    setCopiedInviteId(id);
    if (copyTimer.current) clearTimeout(copyTimer.current);
    copyTimer.current = setTimeout(() => setCopiedInviteId(null), 1500);
  };

  const filteredUsers = users.filter((u) => {
    if (!searchQuery.trim()) return true;
    const q = searchQuery.toLowerCase();
    return (
      u.username.toLowerCase().includes(q) ||
      (u.display_name && u.display_name.toLowerCase().includes(q)) ||
      u.id.toLowerCase().includes(q)
    );
  });

  const formatDate = (ts?: number) => {
    if (!ts) return t('admin:users.never_logged_in');
    return new Date(ts * 1000).toLocaleString();
  };

  return (
    <div className="min-h-screen bg-[var(--bg-base)] p-6 space-y-6 text-[var(--text-primary)]">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 border-b border-[var(--border-subtle)] pb-5">
        <div>
          <h1 className="text-xl font-bold font-[family-name:var(--font-display)] tracking-tight">
            {t('admin:users.title')}
          </h1>
          <p className="text-xs text-[var(--text-muted)] mt-1">
            {t('admin:users.subtitle')}
          </p>
        </div>
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={handleCreateInvitation}
            className="px-3.5 py-2 rounded-lg bg-[var(--accent-gold)] text-black text-xs font-bold hover:bg-[var(--accent-gold-bright)] transition-all flex items-center gap-1.5 shadow-sm active:scale-[0.98] cursor-pointer"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M12 4v16m8-8H4" />
            </svg>
            {t('admin:users.action.generate_invite')}
          </button>
        </div>
      </div>

      {/* Action Banner Toast */}
      {actionMsg && (
        <div
          className={`flex items-center gap-2 px-4 py-3 rounded-lg border text-xs font-bold transition-all animate-fade-in-up ${
            actionMsg.kind === 'ok'
              ? 'bg-[rgba(52,211,153,0.08)] border-[rgba(52,211,153,0.2)] text-[var(--accent-emerald)]'
              : 'bg-[rgba(244,63,94,0.08)] border-[rgba(244,63,94,0.2)] text-[var(--accent-coral)]'
          }`}
        >
          {actionMsg.kind === 'ok' ? (
            <svg className="w-4 h-4 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
            </svg>
          ) : (
            <svg className="w-4 h-4 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
            </svg>
          )}
          <span>{actionMsg.text}</span>
        </div>
      )}

      {/* Search Bar & Summary */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div className="relative flex-1 max-w-md">
          <input
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder={t('admin:users.search_placeholder')}
            className="w-full pl-9 pr-4 py-2 text-xs bg-[var(--bg-surface)] border border-[var(--border-subtle)] rounded-lg text-[var(--text-primary)] placeholder-[var(--text-muted)] focus:outline-none focus:border-[var(--accent-gold)] transition-colors"
          />
          <svg className="w-4 h-4 absolute left-3 top-2.5 text-[var(--text-muted)] pointer-events-none" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
          </svg>
        </div>
        <div className="text-xs font-mono text-[var(--text-muted)]">
          {t('admin:users.user_count', { count: filteredUsers.length })}
        </div>
      </div>

      {/* Loading & Main Users Table */}
      {loading ? (
        <div className="flex items-center justify-center py-20">
          <div className="w-7 h-7 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
        </div>
      ) : error ? (
        <div className="p-4 rounded-lg bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] text-xs text-[var(--accent-coral)] font-bold">
          {error}
        </div>
      ) : (
        <div className="border border-[var(--border-subtle)] rounded-xl bg-[var(--bg-surface)] overflow-hidden shadow-sm">
          <div className="overflow-x-auto">
            <table className="w-full text-left border-collapse">
              <thead>
                <tr className="border-b border-[var(--border-subtle)] bg-[var(--bg-elevated)]/30 text-[10px] font-mono font-bold uppercase tracking-wider text-[var(--text-muted)]">
                  <th className="py-3 px-4">{t('admin:users.table.user')}</th>
                  <th className="py-3 px-4">{t('admin:users.table.role')}</th>
                  <th className="py-3 px-4">{t('admin:users.table.status')}</th>
                  <th className="py-3 px-4">{t('admin:users.table.last_login')}</th>
                  <th className="py-3 px-4">{t('admin:users.table.created_at')}</th>
                  <th className="py-3 px-4 text-right">{t('admin:users.table.actions')}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--border-subtle)] text-xs">
                {filteredUsers.map((u) => (
                  <tr key={u.id} className="hover:bg-[var(--bg-hover)]/40 transition-colors">
                    <td className="py-3.5 px-4">
                      <div className="font-bold text-[var(--text-primary)]">
                        {u.display_name || u.username}
                      </div>
                      <div className="text-[10px] font-mono text-[var(--text-muted)]">
                        @{u.username} · ID: {u.id}
                      </div>
                    </td>
                    <td className="py-3.5 px-4">
                      <span className="capitalize font-mono text-[11px] text-[var(--text-primary)]">
                        {t(('common:role.' + u.role) as any, { defaultValue: u.role })}
                      </span>
                    </td>
                    <td className="py-3.5 px-4">
                      <span
                        className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] font-bold border ${
                          u.status === 'active'
                            ? 'bg-[var(--accent-emerald)]/10 text-[var(--accent-emerald)] border-[var(--accent-emerald)]/20'
                            : 'bg-[var(--accent-coral)]/10 text-[var(--accent-coral)] border-[var(--accent-coral)]/20'
                        }`}
                      >
                        <span
                          className={`w-1.5 h-1.5 rounded-full ${
                            u.status === 'active' ? 'bg-[var(--accent-emerald)]' : 'bg-[var(--accent-coral)]'
                          }`}
                        />
                        {t(('common:status.' + u.status) as any, { defaultValue: u.status })}
                      </span>
                    </td>
                    <td className="py-3.5 px-4 text-[11px] font-mono text-[var(--text-muted)]">
                      {formatDate(u.last_login_at)}
                    </td>
                    <td className="py-3.5 px-4 text-[11px] font-mono text-[var(--text-muted)]">
                      {formatDate(u.created_at)}
                    </td>
                    <td className="py-3.5 px-4 text-right">
                      <div className="flex items-center justify-end gap-2">
                        <button
                          type="button"
                          onClick={() => openResetModal(u)}
                          className="px-2.5 py-1 rounded text-[11px] font-bold bg-[var(--bg-elevated)] border border-[var(--border-subtle)] hover:border-[var(--border-default)] hover:text-[var(--accent-gold)] transition-colors cursor-pointer"
                        >
                          {t('admin:users.action.reset_password')}
                        </button>
                        <button
                          type="button"
                          disabled={busyUserId === u.id}
                          onClick={() => handleToggleUserStatus(u)}
                          className={`px-2.5 py-1 rounded text-[11px] font-bold border transition-colors cursor-pointer disabled:opacity-40 ${
                            u.status === 'active'
                              ? 'bg-[var(--accent-coral)]/10 text-[var(--accent-coral)] border-[var(--accent-coral)]/20 hover:bg-[var(--accent-coral)]/20'
                              : 'bg-[var(--accent-emerald)]/10 text-[var(--accent-emerald)] border-[var(--accent-emerald)]/20 hover:bg-[var(--accent-emerald)]/20'
                          }`}
                        >
                          {u.status === 'active'
                            ? t('admin:users.action.disable')
                            : t('admin:users.action.enable')}
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}

                {filteredUsers.length === 0 && (
                  <tr>
                    <td colSpan={6} className="py-12 text-center text-xs text-[var(--text-muted)]">
                      {searchQuery ? '没有找到符合条件的用户账号' : '尚无用户注册'}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Invitations Section */}
      <div className="pt-4 border-t border-[var(--border-subtle)] space-y-3">
        <h2 className="text-xs font-mono font-bold text-[var(--text-muted)] uppercase tracking-wider">
          {t('chat:settings.title.invite_codes', { count: invitations.length })}
        </h2>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
          {invitations.map((inv) => {
            const used = !!inv.used_at;
            const expired = !used && (inv.is_expired ?? inv.expires_at * 1000 < Date.now());
            const state = used ? 'used' : expired ? 'expired' : 'active';
            const isCopied = copiedInviteId === inv.id;

            return (
              <div
                key={inv.id}
                className="flex items-center justify-between p-3 rounded-lg bg-[var(--bg-surface)] border border-[var(--border-subtle)] hover:border-[var(--border-default)] transition-colors gap-3"
              >
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="font-mono font-bold text-xs bg-[var(--bg-elevated)] border border-[var(--border-subtle)] px-2 py-0.5 rounded select-all truncate">
                      {inv.code}
                    </span>
                    <button
                      type="button"
                      onClick={() => handleCopyInviteCode(inv.id, inv.code)}
                      className="p-1 text-[var(--text-muted)] hover:text-[var(--text-primary)] transition-colors cursor-pointer"
                    >
                      {isCopied ? (
                        <svg className="w-3.5 h-3.5 text-[var(--accent-emerald)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
                        </svg>
                      ) : (
                        <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 5H6a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2v-1M8 5a2 2 0 002 2h2a2 2 0 002-2M8 5a2 2 0 002 2h2a2 2 0 002-2M8 5a2 2 0 012-2h2a2 2 0 012 2m0 0h2a2 2 0 012 2v3m2 4H10m0 0l3-3m-3 3l3 3" />
                        </svg>
                      )}
                    </button>
                  </div>
                  <div className="flex items-center gap-2 mt-1 text-[10px] font-mono text-[var(--text-muted)]">
                    <span className="capitalize">{inv.role}</span>
                    <span>·</span>
                    <span
                      className={`px-1.5 py-0.2 rounded text-[8px] font-bold uppercase border ${
                        state === 'active'
                          ? 'bg-[var(--accent-gold)]/10 text-[var(--accent-gold)] border-[var(--accent-gold)]/20'
                          : state === 'used'
                          ? 'bg-[var(--accent-emerald)]/10 text-[var(--accent-emerald)] border-[var(--accent-emerald)]/20'
                          : 'bg-[var(--accent-coral)]/10 text-[var(--accent-coral)] border-[var(--accent-coral)]/20'
                      }`}
                    >
                      {state}
                    </span>
                  </div>
                </div>
                <button
                  type="button"
                  onClick={() => handleDeleteInvitation(inv.id)}
                  className="p-1.5 text-xs text-[var(--text-muted)] hover:text-[var(--accent-coral)] transition-colors cursor-pointer"
                >
                  <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                  </svg>
                </button>
              </div>
            );
          })}
        </div>
      </div>

      {/* Reset Password Modal */}
      {resetModal.isOpen && resetModal.user && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-xs animate-fade-in">
          <div className="w-full max-w-md bg-[var(--bg-surface)] border border-[var(--border-subtle)] rounded-xl shadow-2xl overflow-hidden p-6 space-y-4">
            <div>
              <h3 className="text-base font-bold text-[var(--text-primary)]">
                {t('admin:users.reset_modal.title')}
              </h3>
              <p className="text-xs text-[var(--text-muted)] mt-1">
                {t('admin:users.reset_modal.subtitle', { username: resetModal.user.username })}
              </p>
            </div>

            {resetModal.error && (
              <div className="p-3 rounded-lg bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.2)] text-xs text-[var(--accent-coral)] font-bold">
                {resetModal.error}
              </div>
            )}

            <form onSubmit={handleResetPasswordSubmit} className="space-y-4">
              <div>
                <label className="block text-xs font-bold text-[var(--text-primary)] mb-1">
                  {t('admin:users.reset_modal.new_password_label')}
                </label>
                <input
                  type="password"
                  required
                  value={resetModal.newPassword}
                  onChange={(e) => setResetModal((prev) => ({ ...prev, newPassword: e.target.value }))}
                  placeholder={t('admin:users.reset_modal.password_placeholder')}
                  className="w-full px-3 py-2 text-xs bg-[var(--bg-elevated)] border border-[var(--border-subtle)] rounded-lg text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)]"
                />
              </div>

              <div>
                <label className="block text-xs font-bold text-[var(--text-primary)] mb-1">
                  {t('admin:users.reset_modal.confirm_password_label')}
                </label>
                <input
                  type="password"
                  required
                  value={resetModal.confirmPassword}
                  onChange={(e) => setResetModal((prev) => ({ ...prev, confirmPassword: e.target.value }))}
                  placeholder={t('admin:users.reset_modal.confirm_placeholder')}
                  className="w-full px-3 py-2 text-xs bg-[var(--bg-elevated)] border border-[var(--border-subtle)] rounded-lg text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)]"
                />
              </div>

              <div className="flex items-center justify-end gap-3 pt-2">
                <button
                  type="button"
                  onClick={closeResetModal}
                  disabled={resetModal.submitting}
                  className="px-4 py-2 rounded-lg text-xs font-bold border border-[var(--border-subtle)] text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)] transition-colors cursor-pointer"
                >
                  {t('common:action.cancel')}
                </button>
                <button
                  type="submit"
                  disabled={resetModal.submitting}
                  className="px-4 py-2 rounded-lg text-xs font-bold bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-colors cursor-pointer disabled:opacity-50"
                >
                  {resetModal.submitting
                    ? t('admin:users.reset_modal.resetting')
                    : t('admin:users.action.reset_password')}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}

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
import { TabPanel } from './tab-panel';
import { useTranslation } from 'react-i18next';

const DEFAULT_INVITE_TTL = 7 * 24 * 3600; // 7 days, matches backend default

interface MembersTabProps {
  currentUser: User | null;
}

export function MembersTab({ currentUser }: MembersTabProps) {
  const { t } = useTranslation(['chat', 'common', 'admin']);
  const [users, setUsers] = useState<User[]>([]);
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actionMsg, setActionMsg] = useState<{ kind: 'ok' | 'err'; text: string } | null>(null);
  const [busyUserId, setBusyUserId] = useState<string | null>(null);
  const [copiedInviteId, setCopiedInviteId] = useState<string | null>(null);

  // Reset password modal state
  const [resetModal, setResetModal] = useState<{
    isOpen: boolean;
    user: User | null;
    newPassword: string;
    confirmPassword: string;
    error: string | null;
    submitting: boolean;
  }>({
    isOpen: false,
    user: null,
    newPassword: '',
    confirmPassword: '',
    error: null,
    submitting: false,
  });

  // Flash timer ref so unmount clears the pending setState (PR #779 review P3-8).
  const flashTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const copyTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const flash = (kind: 'ok' | 'err', text: string) => {
    setActionMsg({ kind, text });
    if (flashTimer.current) clearTimeout(flashTimer.current);
    flashTimer.current = setTimeout(() => setActionMsg(null), 2500);
  };

  const abortRef = useRef<AbortController | null>(null);

  // AbortController guards against concurrent loads racing or resolving after unmount
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
    return () => {
      abortRef.current?.abort();
      if (flashTimer.current) clearTimeout(flashTimer.current);
      if (copyTimer.current) clearTimeout(copyTimer.current);
    };
  }, [load]);

  const handleToggleUser = async (user: User) => {
    if (currentUser?.id === user.id && user.status === 'active') {
      if (!window.confirm(t('chat:settings.confirm.disable_self'))) {
        return;
      }
    }
    const next = user.status === 'active' ? 'disabled' : 'active';
    setBusyUserId(user.id);
    try {
      await adminUpdateUserStatus(user.id, next);
      if (currentUser?.id === user.id && next === 'disabled') {
        try { await logout(); } catch { /* session already invalidated */ }
        window.location.replace('/login');
        return;
      }
      flash('ok', t('chat:settings.success.status_updated', { username: user.username, status: t(('common:status.' + next) as any) }));
      load();
    } catch (err) {
      flash('err', err instanceof Error ? err.message : t('chat:settings.error.update_failed'));
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

  const handleResetPasswordSubmit = async (e: React.SyntheticEvent) => {
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
      flash('ok', `${t('admin:users.reset_modal.success')} (${resetModal.user.username})`);
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
      load();
    } catch (err) {
      flash('err', err instanceof Error ? err.message : t('chat:settings.error.create_failed'));
    }
  };

  const handleDeleteInvitation = async (id: string) => {
    try {
      await adminDeleteInvitation(id);
      flash('ok', t('chat:settings.success.invite_deleted'));
      load();
    } catch (err) {
      flash('err', err instanceof Error ? err.message : t('chat:settings.error.delete_failed'));
    }
  };

  const handleCopyInvite = (id: string, code: string) => {
    navigator.clipboard.writeText(code);
    setCopiedInviteId(id);
    if (copyTimer.current) clearTimeout(copyTimer.current);
    copyTimer.current = setTimeout(() => setCopiedInviteId(null), 1500);
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <div className="w-6 h-6 border-2 border-[var(--accent-gold)] stroke-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
      </div>
    );
  }
  if (error) {
    return (
      <div className="flex gap-2 px-4 py-3 rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] text-xs text-[var(--accent-coral)] font-bold items-center justify-center">
        <svg className="w-4 h-4 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
        </svg>
        <span>{t('common:status.error')}: {error}</span>
      </div>
    );
  }

  return (
    <TabPanel>
      {actionMsg && (
        <div className={`flex gap-2 px-4 py-3 rounded-[var(--radius-md)] border text-xs font-bold items-start animate-fade-in-up ${
          actionMsg.kind === 'ok' 
            ? 'bg-[rgba(52,211,153,0.08)] border-[rgba(52,211,153,0.15)] text-[var(--accent-emerald)]' 
            : 'bg-[rgba(244,63,94,0.08)] border-[rgba(244,63,94,0.15)] text-[var(--accent-coral)]'
        }`}>
          {actionMsg.kind === 'ok' ? (
            <svg className="w-4 h-4 shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
            </svg>
          ) : (
            <svg className="w-4 h-4 shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
            </svg>
          )}
          <span className="break-words">{actionMsg.text}</span>
        </div>
      )}

      {/* Users Section */}
      <section>
        <h3 className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest mb-3">
          {t('chat:settings.title.registered_users', { count: users.length })}
        </h3>
        <div className="space-y-2">
          {users.map((u) => {
            const isSelf = currentUser?.id === u.id;
            return (
              <div key={u.id} className="flex items-center justify-between p-3.5 rounded-[var(--radius-md)] bg-[var(--bg-elevated)]/20 border border-[var(--border-subtle)] gap-4 hover:border-[var(--border-default)] transition-colors">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 truncate">
                    <span className="text-sm font-bold text-[var(--text-primary)]">
                      {u.display_name || u.username}
                    </span>
                    {isSelf && (
                      <span className="px-1.5 py-0.5 rounded text-[8px] font-bold bg-[var(--accent-gold)]/10 text-[var(--accent-gold)] uppercase tracking-wider border border-[var(--accent-gold)]/15">
                        {t('chat:label.you')}
                      </span>
                    )}
                  </div>
                  <div className="flex items-center gap-2 mt-1 text-[10px] text-[var(--text-muted)] font-mono">
                    <span className="capitalize">{t(('common:role.' + u.role) as any, { defaultValue: u.role })}</span>
                    <span>·</span>
                    <div className="flex items-center gap-1">
                      <span className={`w-1 h-1 rounded-full ${u.status === 'active' ? 'bg-[var(--accent-emerald)]' : 'bg-[var(--accent-coral)]'}`} />
                      <span className="capitalize">{t(('common:status.' + u.status) as any, { defaultValue: u.status })}</span>
                    </div>
                  </div>
                </div>

                <div className="flex items-center gap-2 flex-shrink-0">
                  <button
                    type="button"
                    onClick={() => openResetModal(u)}
                    className="px-2.5 py-1.5 rounded-md text-[10px] font-bold bg-[var(--bg-elevated)] text-[var(--text-primary)] hover:text-[var(--accent-gold)] border border-[var(--border-subtle)] hover:border-[var(--border-default)] transition-all cursor-pointer active:scale-[0.98]"
                  >
                    {t('admin:users.action.reset_password', { defaultValue: '重置密码' })}
                  </button>
                  <button
                    type="button"
                    onClick={() => handleToggleUser(u)}
                    disabled={busyUserId !== null}
                    className={`px-3 py-1.5 rounded-md text-[10px] font-bold transition-all disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer ${
                      u.status === 'active' 
                        ? 'bg-[var(--accent-coral)]/10 text-[var(--accent-coral)] hover:bg-[var(--accent-coral)]/20 border border-[var(--accent-coral)]/15 active:scale-[0.98]' 
                        : 'bg-[var(--accent-emerald)]/10 text-[var(--accent-emerald)] hover:bg-[var(--accent-emerald)]/20 border border-[var(--accent-emerald)]/15 active:scale-[0.98]'
                    }`}
                  >
                    {busyUserId === u.id ? (
                      <svg className="w-3 h-3 animate-spin mx-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <circle className="opacity-25" cx={12} cy={12} r={10} stroke="currentColor" strokeWidth={4} />
                        <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
                      </svg>
                    ) : u.status === 'active' ? (
                      t('common:action.disable')
                    ) : (
                      t('common:action.enable')
                    )}
                  </button>
                </div>
              </div>
            );
          })}
          {users.length === 0 && (
            <p className="text-xs text-[var(--text-muted)] py-4 text-center border border-dashed border-[var(--border-subtle)] rounded-lg bg-[var(--bg-elevated)]/10">
              {t('chat:settings.text.no_users')}
            </p>
          )}
        </div>
      </section>

      {/* Invitations Section */}
      <section className="pt-2">
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest">
            {t('chat:settings.title.invite_codes', { count: invitations.length })}
          </h3>
          <button
            type="button"
            onClick={handleCreateInvitation}
            className="px-3 py-1.5 rounded-lg bg-[var(--accent-gold)] text-black text-[10px] font-bold hover:bg-[var(--accent-gold-bright)] transition-all cursor-pointer shadow-sm flex items-center gap-1 active:scale-[0.98]"
          >
            <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M12 4v16m8-8H4" />
            </svg>
            {t('chat:settings.action.generate_invite')}
          </button>
        </div>

        <div className="space-y-2">
          {invitations.map((inv) => {
            const used = !!inv.used_at;
            const expired = !used && (inv.is_expired ?? inv.expires_at * 1000 < Date.now());
            const state = used ? 'used' : expired ? 'expired' : 'active';
            const isCopied = copiedInviteId === inv.id;

            return (
              <div key={inv.id} className="flex items-center justify-between p-3.5 rounded-[var(--radius-md)] bg-[var(--bg-elevated)]/20 border border-[var(--border-subtle)] gap-4 hover:border-[var(--border-default)] transition-colors">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="text-xs font-mono font-bold text-[var(--text-primary)] select-all truncate bg-[var(--bg-elevated)] border border-[var(--border-subtle)] px-2 py-0.5 rounded">
                      {inv.code}
                    </span>
                    <button
                      type="button"
                      onClick={() => handleCopyInvite(inv.id, inv.code)}
                      title={t('chat:settings.action.copy_code')}
                      className="p-1 rounded hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text-primary)] transition-all cursor-pointer border border-transparent hover:border-[var(--border-subtle)]"
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
                  <div className="flex items-center gap-2 mt-1.5 text-[10px] text-[var(--text-muted)] font-mono">
                    <span className="capitalize">{t(('common:role.' + inv.role) as any, { defaultValue: inv.role })}</span>
                    <span>·</span>
                    <span className={`inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[8px] font-bold uppercase tracking-wide border ${
                      state === 'active'
                        ? 'bg-[var(--accent-gold)]/10 text-[var(--accent-gold)] border-[var(--accent-gold)]/15'
                        : state === 'used'
                        ? 'bg-[var(--accent-emerald)]/10 text-[var(--accent-emerald)] border-[var(--accent-emerald)]/15'
                        : 'bg-[var(--accent-coral)]/10 text-[var(--accent-coral)] border-[var(--accent-coral)]/15'
                    }`}>
                      {t(('chat:settings.invite_state.' + state) as any, { defaultValue: state })}
                    </span>
                    {!used && !expired && (
                      <>
                        <span>·</span>
                        <span>{t('chat:settings.text.expires', { date: new Date(inv.expires_at * 1000).toLocaleDateString() })}</span>
                      </>
                    )}
                  </div>
                </div>

                <button
                  type="button"
                  onClick={() => handleDeleteInvitation(inv.id)}
                  className="px-2.5 py-1.5 rounded-lg text-[10px] font-bold text-[var(--text-muted)] hover:text-[var(--accent-coral)] hover:bg-[var(--accent-coral)]/10 border border-transparent hover:border-[var(--accent-coral)]/15 transition-all flex-shrink-0 cursor-pointer active:scale-[0.98]"
                >
                  {t('common:action.delete')}
                </button>
              </div>
            );
          })}
          {invitations.length === 0 && (
            <p className="text-xs text-[var(--text-muted)] py-4 text-center border border-dashed border-[var(--border-subtle)] rounded-lg bg-[var(--bg-elevated)]/10">
              {t('chat:settings.text.no_invitations')}
            </p>
          )}
        </div>
      </section>

      {/* Reset Password Modal */}
      {resetModal.isOpen && resetModal.user && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-xs animate-fade-in">
          <div className="w-full max-w-sm bg-[var(--bg-surface)] border border-[var(--border-subtle)] rounded-xl shadow-2xl overflow-hidden p-5 space-y-4">
            <div>
              <h3 className="text-sm font-bold text-[var(--text-primary)]">
                {t('admin:users.reset_modal.title', { defaultValue: '重置用户密码' })}
              </h3>
              <p className="text-[11px] text-[var(--text-muted)] mt-1">
                {t('admin:users.reset_modal.subtitle', { username: resetModal.user.username, defaultValue: `管理员为账号 ${resetModal.user.username} 设置新密码` })}
              </p>
            </div>

            {resetModal.error && (
              <div className="p-2.5 rounded-lg bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.2)] text-[11px] text-[var(--accent-coral)] font-bold">
                {resetModal.error}
              </div>
            )}

            <form onSubmit={handleResetPasswordSubmit} className="space-y-3">
              <div>
                <label className="block text-[11px] font-bold text-[var(--text-primary)] mb-1">
                  {t('admin:users.reset_modal.new_password_label', { defaultValue: '新密码 *' })}
                </label>
                <input
                  type="password"
                  required
                  value={resetModal.newPassword}
                  onChange={(e) => setResetModal((prev) => ({ ...prev, newPassword: e.target.value }))}
                  placeholder={t('admin:users.reset_modal.password_placeholder', { defaultValue: '输入新密码 (至少 6 位)' })}
                  className="w-full px-3 py-1.5 text-xs bg-[var(--bg-elevated)] border border-[var(--border-subtle)] rounded-lg text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)]"
                />
              </div>

              <div>
                <label className="block text-[11px] font-bold text-[var(--text-primary)] mb-1">
                  {t('admin:users.reset_modal.confirm_password_label', { defaultValue: '确认新密码 *' })}
                </label>
                <input
                  type="password"
                  required
                  value={resetModal.confirmPassword}
                  onChange={(e) => setResetModal((prev) => ({ ...prev, confirmPassword: e.target.value }))}
                  placeholder={t('admin:users.reset_modal.confirm_placeholder', { defaultValue: '再次输入新密码' })}
                  className="w-full px-3 py-1.5 text-xs bg-[var(--bg-elevated)] border border-[var(--border-subtle)] rounded-lg text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)]"
                />
              </div>

              <div className="flex items-center justify-end gap-2 pt-2">
                <button
                  type="button"
                  onClick={closeResetModal}
                  disabled={resetModal.submitting}
                  className="px-3 py-1.5 rounded-lg text-xs font-bold border border-[var(--border-subtle)] text-[var(--text-muted)] hover:text-[var(--text-primary)] transition-colors cursor-pointer"
                >
                  {t('common:action.cancel')}
                </button>
                <button
                  type="submit"
                  disabled={resetModal.submitting}
                  className="px-3 py-1.5 rounded-lg text-xs font-bold bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-colors cursor-pointer disabled:opacity-50"
                >
                  {resetModal.submitting
                    ? t('admin:users.reset_modal.resetting', { defaultValue: '提交中...' })
                    : t('admin:users.action.reset_password', { defaultValue: '重置密码' })}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </TabPanel>
  );
}

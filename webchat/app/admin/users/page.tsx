'use client';

import { useState, useEffect, useCallback, useRef } from 'react';
import {
  adminListUsers,
  adminUpdateUserStatus,
  adminResetUserPassword,
  adminListInvitations,
  adminCreateInvitation,
  adminDeleteInvitation,
  type User,
  type Invitation,
} from '@/lib/api/auth';
import { LoadingState, ErrorState, EmptyState } from '@/components/admin/resource-states';
import { useAdminUI } from '@/context/admin-ui-context';
import { useTranslation } from 'react-i18next';

const DEFAULT_INVITE_TTL = 7 * 24 * 3600;

// Grid columns: user | role | status | last login | created | actions
const USER_GRID = 'grid-cols-[1.8fr_0.7fr_1fr_1.2fr_1.2fr_170px]';

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
  const { showToast } = useAdminUI();
  const [users, setUsers] = useState<User[]>([]);
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
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

  const copyTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const abortRef = useRef<AbortController | null>(null);

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
      if (copyTimer.current) clearTimeout(copyTimer.current);
    };
  }, [loadData]);

  const handleToggleUserStatus = async (user: User) => {
    const nextStatus = user.status === 'active' ? 'disabled' : 'active';
    setBusyUserId(user.id);
    try {
      await adminUpdateUserStatus(user.id, nextStatus);
      showToast(t('admin:users.reset_modal.success', { defaultValue: '操作成功' }), 'success');
      loadData();
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Update user status failed', 'error');
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

  const handleResetPasswordSubmit = async () => {
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
      showToast(
        `${t('admin:users.reset_modal.success', { defaultValue: '密码已重置' })} (${resetModal.user.username})`,
        'success'
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
      showToast(t('chat:settings.success.invite_generated', { defaultValue: '邀请码已生成' }), 'success');
      loadData();
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('chat:settings.error.create_failed', { defaultValue: '生成失败' }), 'error');
    }
  };

  const handleDeleteInvitation = async (id: string) => {
    try {
      await adminDeleteInvitation(id);
      showToast(t('chat:settings.success.invite_deleted', { defaultValue: '邀请码已删除' }), 'success');
      loadData();
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('chat:settings.error.delete_failed', { defaultValue: '删除失败' }), 'error');
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
    <div className="relative min-h-screen bg-[var(--bg-base)] px-6 py-8">
      {/* Background ambient gradient glow */}
      <div className="pointer-events-none fixed inset-0 z-0 bg-mesh opacity-30" />
      <div className="pointer-events-none fixed inset-0 z-0 noise-overlay" />

      <div className="relative z-10 max-w-6xl mx-auto">
        {/* Header */}
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 mb-8">
          <div>
            <h1 className="text-2xl font-display font-bold tracking-tight text-[var(--text-primary)]">
              {t('admin:users.title', { defaultValue: '用户管理' })}
            </h1>
            <p className="text-xs text-[var(--text-muted)] mt-1">
              {t('admin:users.subtitle', { defaultValue: '管理用户账号、启用状态与邀请码。' })}
            </p>
          </div>

          <div className="flex items-center gap-2 shrink-0">
            <button
              type="button"
              onClick={loadData}
              disabled={loading}
              title={t('common:action.refresh', { defaultValue: '刷新' })}
              className="inline-flex items-center justify-center w-9 h-9 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] transition-all active:scale-95 disabled:opacity-40 shadow-[var(--shadow-sm)]"
            >
              <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                strokeWidth={2}
                stroke="currentColor"
                className={`h-3.5 w-3.5 ${loading ? 'animate-spin text-[var(--accent-gold)]' : ''}`}
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.992 0 3.181 3.183a8.25 8.25 0 0 0 13.803-3.7M4.031 9.865a8.25 8.25 0 0 1 13.803-3.7l3.181 3.182"
                />
              </svg>
            </button>

            <button
              type="button"
              onClick={handleCreateInvitation}
              className="inline-flex items-center gap-1.5 px-3.5 py-2 rounded-[var(--radius-sm)] bg-[var(--accent-gold)] text-black text-xs font-bold hover:bg-[var(--accent-gold-bright)] transition-all active:scale-95 shadow-[var(--shadow-sm)]"
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M12 4v16m8-8H4" />
              </svg>
              {t('admin:users.action.generate_invite', { defaultValue: '生成邀请码' })}
            </button>
          </div>
        </div>

        {/* Toolbar: search + count */}
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 mb-6 bg-[var(--bg-glass)] border border-[var(--border-subtle)] rounded-[var(--radius-md)] p-3 backdrop-blur-md shadow-[var(--shadow-sm)]">
          <div className="relative w-full sm:w-64">
            <svg
              xmlns="http://www.w3.org/2000/svg"
              fill="none"
              viewBox="0 0 24 24"
              strokeWidth={2}
              stroke="currentColor"
              className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-[var(--text-faint)]"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="m21 21-5.197-5.197m0 0A7.5 7.5 0 1 0 5.196 5.196a7.5 7.5 0 0 0 10.607 10.607Z"
              />
            </svg>
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder={t('admin:users.search_placeholder', { defaultValue: '搜索用户名、显示名或 ID...' })}
              className="w-full pl-8 pr-7 py-1.5 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-base)] text-xs text-[var(--text-primary)] placeholder:text-[var(--text-faint)] outline-none transition-all focus:border-[var(--accent-gold)]/40"
            />
            {searchQuery && (
              <button
                type="button"
                onClick={() => setSearchQuery('')}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-[var(--text-faint)] hover:text-[var(--text-primary)] transition-colors p-0.5"
              >
                <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" className="w-3.5 h-3.5">
                  <path d="M6.28 5.22a.75.75 0 0 0-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 1 0 1.06 1.06L10 11.06l3.72 3.72a.75.75 0 1 0 1.06-1.06L11.06 10l3.72-3.72a.75.75 0 0 0-1.06-1.06L10 8.94 6.28 5.22Z" />
                </svg>
              </button>
            )}
          </div>

          <span className="text-[11px] font-mono text-[var(--text-faint)] px-2 py-0.5 rounded-full bg-[var(--bg-hover)] self-start sm:self-auto">
            {t('admin:users.user_count', { count: filteredUsers.length, defaultValue: '{{count}} 个用户' })}
          </span>
        </div>

        {/* Loading / Error */}
        {loading && <LoadingState label={t('admin:users.loading', { defaultValue: '加载用户数据...' })} />}
        {error && <ErrorState message={error} onRetry={loadData} />}

        {/* Users grid */}
        {!loading && !error && (
          <div className="rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] overflow-hidden mb-8">
            {/* Grid Header */}
            <div className={`grid ${USER_GRID} gap-3 px-5 py-3 border-b border-[var(--border-subtle)] bg-[var(--bg-surface)] items-center`}>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-widest font-mono">
                {t('admin:users.table.user', { defaultValue: '用户' })}
              </span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-widest font-mono">
                {t('admin:users.table.role', { defaultValue: '角色' })}
              </span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-widest font-mono">
                {t('admin:users.table.status', { defaultValue: '状态' })}
              </span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-widest font-mono">
                {t('admin:users.table.last_login', { defaultValue: '最后登录' })}
              </span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-widest font-mono">
                {t('admin:users.table.created_at', { defaultValue: '创建时间' })}
              </span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-widest font-mono text-right">
                {t('admin:users.table.actions', { defaultValue: '操作' })}
              </span>
            </div>

            {/* Rows */}
            {filteredUsers.length === 0 ? (
              <EmptyState
                title={
                  searchQuery
                    ? t('admin:users.empty.no_match', { defaultValue: '没有找到符合条件的用户' })
                    : t('admin:users.empty.no_users', { defaultValue: '尚无用户注册' })
                }
              />
            ) : (
              <div className="divide-y divide-[var(--border-subtle)]">
                {filteredUsers.map((u) => (
                  <div
                    key={u.id}
                    className={`grid ${USER_GRID} gap-3 px-5 py-3.5 items-center transition-colors hover:bg-[var(--bg-hover)]/40 ${
                      busyUserId === u.id ? 'opacity-60' : ''
                    }`}
                  >
                    {/* User */}
                    <div className="min-w-0">
                      <div className="text-xs font-semibold text-[var(--text-primary)] truncate">
                        {u.display_name || u.username}
                      </div>
                      <div className="text-[10px] font-mono text-[var(--text-faint)] truncate">
                        @{u.username} · {u.id}
                      </div>
                    </div>

                    {/* Role */}
                    <div>
                      <span className="inline-flex px-2 py-0.5 rounded text-[10px] font-mono font-bold bg-[var(--bg-hover)] text-[var(--text-secondary)] border border-[var(--border-subtle)] capitalize">
                        {t(('common:role.' + u.role) as any, { defaultValue: u.role })}
                      </span>
                    </div>

                    {/* Status */}
                    <div>
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
                    </div>

                    {/* Last login */}
                    <span className="text-[11px] font-mono text-[var(--text-muted)] truncate" title={formatDate(u.last_login_at)}>
                      {formatDate(u.last_login_at)}
                    </span>

                    {/* Created */}
                    <span className="text-[11px] font-mono text-[var(--text-muted)] truncate" title={formatDate(u.created_at)}>
                      {formatDate(u.created_at)}
                    </span>

                    {/* Actions */}
                    <div className="flex items-center justify-end gap-1.5">
                      <button
                        type="button"
                        onClick={() => openResetModal(u)}
                        className="px-2.5 py-1 rounded-[var(--radius-xs)] text-[10px] font-bold text-[var(--text-muted)] bg-[var(--bg-elevated)] border border-[var(--border-subtle)] hover:border-[var(--border-default)] hover:text-[var(--accent-gold)] transition-colors active:scale-95"
                      >
                        {t('admin:users.action.reset_password', { defaultValue: '重置密码' })}
                      </button>
                      <button
                        type="button"
                        disabled={busyUserId === u.id}
                        onClick={() => handleToggleUserStatus(u)}
                        className={`px-2.5 py-1 rounded-[var(--radius-xs)] text-[10px] font-bold border transition-colors active:scale-95 disabled:opacity-40 ${
                          u.status === 'active'
                            ? 'text-[var(--accent-coral)] bg-[var(--accent-coral)]/10 border-[var(--accent-coral)]/20 hover:bg-[var(--accent-coral)]/20'
                            : 'text-[var(--accent-emerald)] bg-[var(--accent-emerald)]/10 border-[var(--accent-emerald)]/20 hover:bg-[var(--accent-emerald)]/20'
                        }`}
                      >
                        {u.status === 'active'
                          ? t('admin:users.action.disable', { defaultValue: '禁用' })
                          : t('admin:users.action.enable', { defaultValue: '启用' })}
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        {/* Invitations Section */}
        {!loading && !error && (
          <div>
            <div className="flex items-center gap-2 mb-3">
              <h2 className="text-[10px] font-mono font-bold uppercase tracking-widest text-[var(--text-faint)]">
                {t('chat:settings.title.invite_codes', { count: invitations.length, defaultValue: '邀请码' })}
              </h2>
              <span className="text-[10px] font-mono text-[var(--text-faint)] px-2 py-0.5 rounded-full bg-[var(--bg-hover)]">
                {invitations.length}
              </span>
            </div>

            {invitations.length === 0 ? (
              <div className="rounded-[var(--radius-md)] border border-dashed border-[var(--border-subtle)] bg-[var(--bg-surface)] py-10 text-center">
                <p className="text-xs text-[var(--text-faint)]">
                  {t('admin:users.empty.no_invitations', { defaultValue: '暂无邀请码，点击右上角「生成邀请码」创建。' })}
                </p>
              </div>
            ) : (
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
                {invitations.map((inv) => {
                  const used = !!inv.used_at;
                  const expired = !used && (inv.is_expired ?? inv.expires_at * 1000 < Date.now());
                  const state = used ? 'used' : expired ? 'expired' : 'active';
                  const isCopied = copiedInviteId === inv.id;

                  return (
                    <div
                      key={inv.id}
                      className="flex items-center justify-between p-3 rounded-[var(--radius-md)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] hover:border-[var(--border-default)] transition-colors gap-3"
                    >
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <span className="font-mono font-bold text-xs bg-[var(--bg-elevated)] border border-[var(--border-subtle)] px-2 py-0.5 rounded-[var(--radius-xs)] select-all truncate">
                            {inv.code}
                          </span>
                          <button
                            type="button"
                            onClick={() => handleCopyInviteCode(inv.id, inv.code)}
                            className="p-1 text-[var(--text-muted)] hover:text-[var(--text-primary)] transition-colors active:scale-95"
                            title={t('common:action.copy', { defaultValue: '复制' })}
                          >
                            {isCopied ? (
                              <svg className="w-3.5 h-3.5 text-[var(--accent-emerald)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
                              </svg>
                            ) : (
                              <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 5H6a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2v-1M8 5a2 2 0 002 2h2a2 2 0 002-2M8 5a2 2 0 012-2h2a2 2 0 012 2m0 0h2a2 2 0 012 2v3m2 4H10m0 0l3-3m-3 3l3 3" />
                              </svg>
                            )}
                          </button>
                        </div>
                        <div className="flex items-center gap-2 mt-1 text-[10px] font-mono text-[var(--text-faint)]">
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
                        className="p-1.5 text-xs text-[var(--text-muted)] hover:text-[var(--accent-coral)] transition-colors active:scale-95"
                        title={t('common:action.delete', { defaultValue: '删除' })}
                      >
                        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                        </svg>
                      </button>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        )}
      </div>

      {/* Reset Password Modal */}
      {resetModal.isOpen && resetModal.user && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-sm animate-[fadeInUp_0.2s_ease-out]"
          onClick={closeResetModal}
        >
          <div
            className="w-full max-w-md bg-[var(--bg-surface)] border border-[var(--border-subtle)] rounded-[var(--radius-md)] shadow-2xl overflow-hidden animate-[fadeInScale_0.15s_ease-out]"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="px-6 pt-6 pb-2">
              <h3 className="text-base font-display font-bold text-[var(--text-primary)]">
                {t('admin:users.reset_modal.title', { defaultValue: '重置密码' })}
              </h3>
              <p className="text-xs text-[var(--text-muted)] mt-1">
                {t('admin:users.reset_modal.subtitle', { username: resetModal.user.username, defaultValue: '为 @{{username}} 设置新密码' })}
              </p>
            </div>

            <form onSubmit={(e) => { e.preventDefault(); void handleResetPasswordSubmit(); }} className="px-6 pb-6 pt-3 space-y-4">
              {resetModal.error && (
                <div className="p-3 rounded-[var(--radius-sm)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.2)] text-xs text-[var(--accent-coral)] font-bold">
                  {resetModal.error}
                </div>
              )}

              <div>
                <label className="block text-[10px] font-bold uppercase tracking-wider text-[var(--text-faint)] mb-1.5">
                  {t('admin:users.reset_modal.new_password_label', { defaultValue: '新密码' })}
                </label>
                <input
                  type="password"
                  required
                  value={resetModal.newPassword}
                  onChange={(e) => setResetModal((prev) => ({ ...prev, newPassword: e.target.value }))}
                  placeholder={t('admin:users.reset_modal.password_placeholder', { defaultValue: '至少 6 位' })}
                  className="w-full px-3 py-2 text-xs bg-[var(--bg-elevated)] border border-[var(--border-subtle)] rounded-[var(--radius-sm)] text-[var(--text-primary)] placeholder:text-[var(--text-faint)] focus:outline-none focus:border-[var(--accent-gold)]/40 transition-colors"
                />
              </div>

              <div>
                <label className="block text-[10px] font-bold uppercase tracking-wider text-[var(--text-faint)] mb-1.5">
                  {t('admin:users.reset_modal.confirm_password_label', { defaultValue: '确认新密码' })}
                </label>
                <input
                  type="password"
                  required
                  value={resetModal.confirmPassword}
                  onChange={(e) => setResetModal((prev) => ({ ...prev, confirmPassword: e.target.value }))}
                  placeholder={t('admin:users.reset_modal.confirm_placeholder', { defaultValue: '再次输入新密码' })}
                  className="w-full px-3 py-2 text-xs bg-[var(--bg-elevated)] border border-[var(--border-subtle)] rounded-[var(--radius-sm)] text-[var(--text-primary)] placeholder:text-[var(--text-faint)] focus:outline-none focus:border-[var(--accent-gold)]/40 transition-colors"
                />
              </div>

              <div className="flex items-center justify-end gap-2 pt-1">
                <button
                  type="button"
                  onClick={closeResetModal}
                  disabled={resetModal.submitting}
                  className="px-4 py-2 rounded-[var(--radius-sm)] text-xs font-bold border border-[var(--border-subtle)] text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)] transition-colors active:scale-95 disabled:opacity-50"
                >
                  {t('common:action.cancel', { defaultValue: '取消' })}
                </button>
                <button
                  type="submit"
                  disabled={resetModal.submitting}
                  className="inline-flex items-center gap-1.5 px-4 py-2 rounded-[var(--radius-sm)] text-xs font-bold bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-colors active:scale-95 disabled:opacity-50"
                >
                  {resetModal.submitting && <span className="w-3 h-3 border border-current border-t-transparent rounded-full animate-spin" />}
                  {resetModal.submitting
                    ? t('admin:users.reset_modal.resetting', { defaultValue: '重置中…' })
                    : t('admin:users.action.reset_password', { defaultValue: '重置密码' })}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}

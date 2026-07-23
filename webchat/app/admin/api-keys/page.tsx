'use client';

import { useEffect, useMemo, useState } from 'react';
import { useAdminUI } from '@/context/admin-ui-context';
import {
  listAPIKeys,
  createAPIKey,
  deleteAPIKey,
} from '@/lib/api/admin-apikeys';
import type { APIKeyUser } from '@/lib/types/admin';
import { formatRelative as formatTime, formatDateTime } from '@/lib/utils/format-time';
import { useResource } from '@/hooks/use-resource';
import { LoadingState, ErrorState, EmptyState } from '@/components/admin/resource-states';
import { useTranslation } from 'react-i18next';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function maskKey(key: string): string {
  if (!key) return '****';
  if (key.length <= 12) return key.slice(0, 4) + '****' + key.slice(-2);
  return key.slice(0, 8) + '••••••••' + key.slice(-4);
}

// ---------------------------------------------------------------------------
// Page Component
// ---------------------------------------------------------------------------

export default function APIKeysPage() {
  const { t } = useTranslation();
  const { showToast, confirm } = useAdminUI();
  const { data, loading, error, reload } = useResource<APIKeyUser[]>(
    async () => (await listAPIKeys()) ?? [],
    [],
  );
  const keys = data ?? [];
  const [query, setQuery] = useState('');
  const [actionLoading, setActionLoading] = useState<string | null>(null);

  const filteredKeys = useMemo(() => {
    if (!query.trim()) return keys;
    const q = query.toLowerCase();
    return keys.filter(
      (k) =>
        k.user_id.toLowerCase().includes(q) ||
        (k.description && k.description.toLowerCase().includes(q)) ||
        k.api_key.toLowerCase().includes(q),
    );
  }, [keys, query]);

  // Dialog state
  const [showCreate, setShowCreate] = useState(false);
  const [createdKey, setCreatedKey] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  // Form state
  const [formUserId, setFormUserId] = useState('');
  const [formDesc, setFormDesc] = useState('');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  // ---------------------------------------------------------------------------
  // Actions
  // ---------------------------------------------------------------------------

  const handleCreate = async () => {
    if (!formUserId.trim()) {
      setFormError(t('admin:api_keys.validation.user_id_required', { defaultValue: 'User ID is required' }));
      return;
    }
    const trimmedId = formUserId.trim();
    if (keys.some((k) => k.user_id === trimmedId)) {
      setFormError(t('admin:api_keys.validation.key_exists', { defaultValue: 'This User ID already has an API key' }));
      return;
    }
    try {
      setIsSubmitting(true);
      setFormError(null);
      const result = await createAPIKey({
        user_id: trimmedId,
        description: formDesc.trim() || undefined,
      });
      setCreatedKey(result.api_key);
      await reload();
      setFormUserId('');
      setFormDesc('');
    } catch (err) {
      const status = (err as any).status;
      if (status === 409) {
        setFormError(t('admin:api_keys.validation.key_exists', { defaultValue: 'This User ID already has an API key' }));
      } else {
        const msg = err instanceof Error ? err.message : t('admin:api_keys.error.create_failed', { defaultValue: 'Failed to create API key' });
        setFormError(msg);
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleDelete = async (id: number) => {
    const matchedKey = keys.find((k) => k.id === id);
    const userId = matchedKey?.user_id || 'unknown';

    const ok = await confirm(
      t('admin:api_keys.confirm.delete_title', { defaultValue: 'Delete API Key' }),
      t('admin:api_keys.confirm.delete_body', { userId, defaultValue: `Are you sure you want to permanently delete the API key for user "${userId}"? This will immediately revoke all access for this key. This action is irreversible.` }),
      {
        confirmLabel: t('common:action.delete', { defaultValue: 'Delete' }),
        cancelLabel: t('common:action.cancel', { defaultValue: 'Cancel' }),
        destructive: true,
      }
    );
    if (!ok) return;

    try {
      setActionLoading(id.toString());
      await deleteAPIKey(id);
      await reload();
      showToast(t('admin:api_keys.toast.deleted', { defaultValue: 'API key successfully deleted' }), 'success');
    } catch (err) {
      showToast(
        err instanceof Error ? err.message : t('admin:api_keys.error.delete_failed', { defaultValue: 'Failed to delete API key' }),
        'error',
      );
    } finally {
      setActionLoading(null);
    }
  };

  const openCreate = () => {
    setFormUserId('');
    setFormDesc('');
    setFormError(null);
    setCreatedKey(null);
    setCopied(false);
    setShowCreate(true);
  };

  const closeDialog = () => {
    setShowCreate(false);
    setCreatedKey(null);
    setCopied(false);
    setFormError(null);
  };

  return (
    <div className="min-h-screen bg-[var(--bg-base)] px-6 py-8">
      <div className="mx-auto max-w-6xl space-y-8">
        
        {/* Header */}
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
          <div>
            <div className="flex items-center gap-3">
              <h1 className="font-display text-2xl font-bold text-[var(--text-primary)]">
                {t('admin:api_keys.title', { defaultValue: 'API Key Credentials' })}
              </h1>
              {!loading && !error && (
                <span className="rounded-full bg-[var(--accent-gold)]/10 text-[var(--accent-gold)] border border-[var(--accent-gold)]/20 px-2.5 py-0.5 font-mono text-xs font-bold">
                  {t('admin:api_keys.key_count', { count: keys.length, defaultValue: `${keys.length} keys` })}
                </span>
              )}
            </div>
            <p className="mt-1 text-xs text-[var(--text-muted)]">
              {t('admin:api_keys.subtitle', { defaultValue: 'Manage API authentication tokens for administrator and service client access' })}
            </p>
          </div>

          <div className="flex items-center gap-2.5">
            <button
              type="button"
              onClick={reload}
              disabled={loading}
              className="p-2 rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)] transition-all disabled:opacity-50"
              title={t('common:action.refresh', { defaultValue: 'Refresh' })}
            >
              <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                strokeWidth={1.5}
                stroke="currentColor"
                className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`}
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
              onClick={openCreate}
              className="inline-flex items-center gap-2 rounded-[var(--radius-md)] bg-[var(--accent-gold)] px-4 py-2 text-xs font-bold text-[var(--bg-base)] shadow-sm transition-all hover:bg-[var(--accent-gold)]/90 hover:shadow-[var(--shadow-glow)]"
            >
              <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                strokeWidth={2.5}
                stroke="currentColor"
                className="h-4 w-4"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M12 4.5v15m7.5-7.5h-15"
                />
              </svg>
              {t('admin:api_keys.action.create', { defaultValue: 'Create API Key' })}
            </button>
          </div>
        </div>

        {/* Security Summary Banner */}
        {!loading && !error && (
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div className="p-4 rounded-[var(--radius-md)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] flex items-center gap-3">
              <div className="w-10 h-10 rounded-full bg-[var(--accent-gold)]/10 text-[var(--accent-gold)] border border-[var(--accent-gold)]/20 flex items-center justify-center shrink-0">
                <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="w-5 h-5">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M15.75 5.25a3 3 0 0 1 3 3m3 0a6 6 0 0 1-7.029 5.912c-.563-.097-1.159.026-1.563.43L10.5 17.25H8.25v2.25H6v2.25H2.25v-2.818c0-.597.237-1.17.659-1.591l6.499-6.499c.404-.404.527-1 .43-1.563A6 6 0 1 1 21.75 8.25Z" />
                </svg>
              </div>
              <div>
                <span className="text-[10px] font-bold uppercase tracking-wider text-[var(--text-faint)]">Active API Tokens</span>
                <p className="text-sm font-bold text-[var(--text-primary)] mt-0.5">
                  {keys.length} Active Key{keys.length === 1 ? '' : 's'} Configured
                </p>
              </div>
            </div>

            <div className="p-4 rounded-[var(--radius-md)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] flex items-center gap-3">
              <div className="w-10 h-10 rounded-full bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 flex items-center justify-center shrink-0">
                <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="w-5 h-5">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M9 12.75 11.25 15 15 9.75m-3-7.036A11.959 11.959 0 0 1 3.598 6 11.99 11.99 0 0 0 3 9.75c0 5.592 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.57-.598-3.75h-.152c-3.196 0-6.1-1.249-8.25-3.286Z" />
                </svg>
              </div>
              <div>
                <span className="text-[10px] font-bold uppercase tracking-wider text-[var(--text-faint)]">Scope Protection</span>
                <p className="text-sm font-bold text-[var(--text-primary)] mt-0.5">
                  Bearer Token Scope Enforcement Active
                </p>
              </div>
            </div>
          </div>
        )}

        {/* Loading State */}
        {loading && <LoadingState label={t('admin:api_keys.loading', { defaultValue: 'Loading API keys...' })} />}

        {/* Error State */}
        {error && <ErrorState message={error} onRetry={reload} />}

        {/* Empty State */}
        {!loading && !error && keys.length === 0 && (
          <EmptyState
            title={t('admin:api_keys.empty.title', { defaultValue: 'No API keys configured yet.' })}
            action={
              <button
                type="button"
                onClick={openCreate}
                className="inline-flex items-center gap-2 rounded-[var(--radius-md)] bg-[var(--accent-gold)] px-4 py-2 text-xs font-bold text-[var(--bg-base)] transition-all hover:bg-[var(--accent-gold)]/90"
              >
                {t('admin:api_keys.action.create_first', { defaultValue: 'Create First API Key' })}
              </button>
            }
          />
        )}

        {/* Search Bar */}
        {!loading && !error && keys.length > 0 && (
          <div className="relative">
            <svg
              className="absolute left-3.5 top-1/2 -translate-y-1/2 text-[var(--text-faint)]"
              width="15"
              height="15"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <circle cx="11" cy="11" r="8" />
              <path d="m21 21-4.3-4.3" />
            </svg>
            <input
              type="text"
              placeholder={t('admin:api_keys.placeholder.search', { defaultValue: 'Search by user ID, description or key prefix...' })}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="w-full pl-10 pr-10 py-2.5 rounded-[var(--radius-md)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] text-xs text-[var(--text-primary)] placeholder:text-[var(--text-faint)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors shadow-sm"
            />
            {query && (
              <button
                type="button"
                onClick={() => setQuery('')}
                className="absolute right-3.5 top-1/2 -translate-y-1/2 text-[var(--text-faint)] hover:text-[var(--text-primary)] p-1"
              >
                <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
                  <line x1="1" y1="1" x2="13" y2="13" />
                  <line x1="13" y1="1" x2="1" y2="13" />
                </svg>
              </button>
            )}
          </div>
        )}

        {/* Search no results */}
        {!loading && !error && keys.length > 0 && filteredKeys.length === 0 && (
          <div className="flex flex-col items-center justify-center py-16 text-center bg-[var(--bg-surface)] border border-[var(--border-subtle)] rounded-[var(--radius-lg)]">
            <p className="text-xs text-[var(--text-muted)] mb-2">
              {t('admin:api_keys.search_no_results', { query, defaultValue: `No API keys matching "${query}"` })}
            </p>
            <button
              type="button"
              onClick={() => setQuery('')}
              className="text-xs font-bold text-[var(--accent-gold)] hover:underline"
            >
              {t('admin:api_keys.action.clear_search', { defaultValue: 'Clear search' })}
            </button>
          </div>
        )}

        {/* Table */}
        {!loading && !error && filteredKeys.length > 0 && (
          <div className="overflow-hidden rounded-[var(--radius-lg)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] shadow-sm">
            {/* Table Header */}
            <div className="grid grid-cols-[1fr_160px_1fr_130px_100px] gap-4 border-b border-[var(--border-subtle)] bg-[var(--bg-hover)] px-5 py-3 text-[10px] font-bold uppercase tracking-wider text-[var(--text-faint)]">
              <span>{t('admin:api_keys.table.api_key', { defaultValue: 'API Key Token' })}</span>
              <span>{t('admin:api_keys.table.user_id', { defaultValue: 'User Identity' })}</span>
              <span>{t('admin:api_keys.table.description', { defaultValue: 'Description' })}</span>
              <span>{t('admin:api_keys.table.created', { defaultValue: 'Created' })}</span>
              <span className="text-right">{t('admin:api_keys.table.actions', { defaultValue: 'Actions' })}</span>
            </div>

            {/* Table Rows */}
            {filteredKeys.map((k) => {
              const displayKey = k.api_key.includes('****') ? k.api_key : maskKey(k.api_key);
              return (
                <div
                  key={k.id || k.api_key}
                  className="grid grid-cols-[1fr_160px_1fr_130px_100px] gap-4 border-b border-[var(--border-subtle)] px-5 py-3.5 last:border-b-0 items-center transition-colors hover:bg-[var(--bg-hover)]"
                >
                  {/* API Key Token */}
                  <div className="flex items-center gap-2.5 min-w-0">
                    <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="w-4 h-4 text-[var(--accent-gold)] shrink-0">
                      <path strokeLinecap="round" strokeLinejoin="round" d="M15.75 5.25a3 3 0 0 1 3 3m3 0a6 6 0 0 1-7.029 5.912c-.563-.097-1.159.026-1.563.43L10.5 17.25H8.25v2.25H6v2.25H2.25v-2.818c0-.597.237-1.17.659-1.591l6.499-6.499c.404-.404.527-1 .43-1.563A6 6 0 1 1 21.75 8.25Z" />
                    </svg>
                    <code className="truncate font-mono text-xs text-[var(--text-primary)] font-bold bg-[var(--bg-base)] px-2 py-0.5 rounded border border-[var(--border-subtle)] select-all" title={displayKey}>
                      {displayKey}
                    </code>
                  </div>

                  {/* User Identity Avatar + ID */}
                  <div className="flex items-center gap-2 min-w-0">
                    <div className="w-6 h-6 rounded-full bg-[var(--accent-gold)]/10 text-[var(--accent-gold)] border border-[var(--accent-gold)]/20 flex items-center justify-center text-[10px] font-bold shrink-0">
                      {k.user_id.charAt(0).toUpperCase()}
                    </div>
                    <span className="truncate text-xs font-bold text-[var(--text-primary)] font-mono">
                      {k.user_id}
                    </span>
                  </div>

                  {/* Description */}
                  <div className="min-w-0">
                    {k.description ? (
                      <span className="text-xs text-[var(--text-primary)] truncate block" title={k.description}>
                        {k.description}
                      </span>
                    ) : (
                      <span className="text-xs text-[var(--text-faint)] italic">
                        —
                      </span>
                    )}
                  </div>

                  {/* Created Relative Time */}
                  <span
                    className="text-xs text-[var(--text-muted)] font-mono"
                    title={k.created_at ? formatDateTime(k.created_at) : ''}
                  >
                    {formatTime(k.created_at)}
                  </span>

                  {/* Actions */}
                  <div className="flex items-center justify-end">
                    <button
                      type="button"
                      onClick={() => handleDelete(k.id!)}
                      disabled={actionLoading === k.id?.toString()}
                      className="p-1.5 rounded-[var(--radius-sm)] bg-rose-500/10 text-rose-400 hover:bg-rose-500/20 border border-rose-500/20 transition-all disabled:opacity-40"
                      title={t('common:action.delete', { defaultValue: 'Delete' })}
                    >
                      {actionLoading === k.id?.toString() ? (
                        <div className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-current border-t-transparent" />
                      ) : (
                        <svg
                          xmlns="http://www.w3.org/2000/svg"
                          fill="none"
                          viewBox="0 0 24 24"
                          strokeWidth={1.5}
                          stroke="currentColor"
                          className="h-3.5 w-3.5"
                        >
                          <path
                            strokeLinecap="round"
                            strokeLinejoin="round"
                            d="m14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0"
                          />
                        </svg>
                      )}
                    </button>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* ====== Create Dialog Popup (弹窗 UI UX 优化) ====== */}
      {showCreate && (
        <DialogOverlay onClose={closeDialog} preventOutsideClose={!!createdKey}>
          <div className="w-full space-y-6">
            
            {/* Modal Header */}
            <div className="flex items-start justify-between border-b border-[var(--border-subtle)] pb-4">
              <div className="flex items-center gap-3">
                <div className="w-9 h-9 rounded-full bg-[var(--accent-gold)]/10 text-[var(--accent-gold)] border border-[var(--accent-gold)]/20 flex items-center justify-center shrink-0">
                  <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="w-5 h-5">
                    <path strokeLinecap="round" strokeLinejoin="round" d="M15.75 5.25a3 3 0 0 1 3 3m3 0a6 6 0 0 1-7.029 5.912c-.563-.097-1.159.026-1.563.43L10.5 17.25H8.25v2.25H6v2.25H2.25v-2.818c0-.597.237-1.17.659-1.591l6.499-6.499c.404-.404.527-1 .43-1.563A6 6 0 1 1 21.75 8.25Z" />
                  </svg>
                </div>
                <div>
                  <h2 className="font-display text-base font-bold text-[var(--text-primary)]">
                    {t('admin:api_keys.dialog.create_title', { defaultValue: 'Generate New API Key' })}
                  </h2>
                  <p className="text-xs text-[var(--text-muted)] mt-0.5">
                    {t('admin:api_keys.dialog.create_subtitle', { defaultValue: 'Issue a unique access token for a user or service identity' })}
                  </p>
                </div>
              </div>

              {!createdKey && (
                <button
                  type="button"
                  onClick={closeDialog}
                  className="p-1 rounded-[var(--radius-sm)] text-[var(--text-faint)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)] transition-all"
                >
                  <svg width="18" height="18" viewBox="0 0 18 18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
                    <line x1="2" y1="2" x2="16" y2="16" />
                    <line x1="16" y1="2" x2="2" y2="16" />
                  </svg>
                </button>
              )}
            </div>

            {/* Modal Body: One-Time Key Reveal Screen */}
            {createdKey ? (
              <div className="space-y-5 animate-fade-in-up">
                <div className="rounded-[var(--radius-md)] border border-amber-500/30 bg-amber-500/10 p-4">
                  <div className="flex gap-3">
                    <svg
                      xmlns="http://www.w3.org/2000/svg"
                      fill="none"
                      viewBox="0 0 24 24"
                      strokeWidth={2}
                      stroke="var(--accent-gold)"
                      className="h-5 w-5 shrink-0 mt-0.5 animate-pulse"
                    >
                      <path
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        d="M12 9v3.75m0-10.036A11.959 11.959 0 0 1 3.598 6 11.99 11.99 0 0 0 3 9.75c0 5.592 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.57-.598-3.75h-.152c-3.196 0-6.1-1.249-8.25-3.286Zm0 13.036h.008v.008H12v-.008Z"
                      />
                    </svg>
                    <div>
                      <h4 className="font-display text-xs font-bold uppercase tracking-wider text-[var(--accent-gold)]">
                        {t('admin:api_keys.dialog.warning_title', { defaultValue: 'Critical Security Notice' })}
                      </h4>
                      <p className="mt-1 text-xs text-[var(--text-muted)] leading-relaxed" dangerouslySetInnerHTML={{
                        __html: t('admin:api_keys.dialog.warning_body', {
                          defaultValue: 'For security reasons, this API key will be shown <strong class="text-[var(--text-primary)] font-bold">ONLY ONCE</strong>. You will not be able to retrieve, view, or copy it again after closing this dialog.'
                        })
                      }} />
                    </div>
                  </div>
                </div>

                <div className="rounded-[var(--radius-md)] border border-[var(--border-default)] bg-[var(--bg-elevated)] p-4 shadow-inner space-y-2">
                  <span className="text-xs font-semibold text-[var(--text-secondary)] block">
                    Your Secret API Key Token
                  </span>
                  <div className="flex items-center gap-3">
                    <code className="flex-1 break-all font-mono text-sm text-[var(--accent-gold)] font-bold select-all bg-[var(--bg-surface)] p-2.5 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] leading-normal">
                      {createdKey}
                    </code>
                    <button
                      type="button"
                      onClick={() => {
                        navigator.clipboard.writeText(createdKey);
                        setCopied(true);
                        setTimeout(() => setCopied(false), 2000);
                      }}
                      className={`shrink-0 rounded-[var(--radius-md)] px-3.5 py-2 text-xs font-bold transition-all ${
                        copied
                          ? 'bg-emerald-500 text-white shadow-[0_0_12px_rgba(16,185,129,0.3)]'
                          : 'bg-[var(--accent-gold)] text-[var(--text-contrast)] hover:bg-[var(--accent-gold-bright)]'
                      }`}
                    >
                      {copied ? t('common:action.copied', { defaultValue: '✓ Copied!' }) : t('common:action.copy', { defaultValue: 'Copy Key' })}
                    </button>
                  </div>
                </div>

                <button
                  type="button"
                  onClick={closeDialog}
                  className="w-full rounded-[var(--radius-md)] bg-[var(--accent-gold)] text-[var(--text-contrast)] py-2.5 text-xs font-bold transition-all hover:bg-[var(--accent-gold-bright)] shadow-sm"
                >
                  {t('admin:api_keys.dialog.confirm_saved', { defaultValue: 'I Have Safely Saved This Key' })}
                </button>
              </div>
            ) : (
              /* Modal Body: Input Form */
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  handleCreate();
                }}
                className="space-y-5"
              >
                <Field
                  label={t('admin:api_keys.table.user_id', { defaultValue: 'User Identity / ID' })}
                  value={formUserId}
                  onChange={setFormUserId}
                  placeholder={t('admin:api_keys.placeholder.user_id', { defaultValue: 'e.g. alice or service-bot' })}
                  hint={t('admin:api_keys.dialog.user_id_hint', { defaultValue: 'Unique identity identifier for this API token owner' })}
                  required
                />

                <Field
                  label={t('admin:api_keys.table.description', { defaultValue: 'Description' })}
                  value={formDesc}
                  onChange={setFormDesc}
                  placeholder={t('admin:api_keys.placeholder.description', { defaultValue: 'Optional description for this key' })}
                  hint={t('admin:api_keys.dialog.desc_hint', { defaultValue: 'e.g. Production automation service client' })}
                />

                {formError && (
                  <div className="p-3.5 rounded-[var(--radius-md)] bg-rose-500/10 border border-rose-500/20 text-xs font-medium text-rose-400">
                    {formError}
                  </div>
                )}

                <div className="flex items-center justify-end gap-3 pt-4 border-t border-[var(--border-subtle)]">
                  <button
                    type="button"
                    onClick={closeDialog}
                    className="rounded-[var(--radius-md)] border border-[var(--border-default)] bg-transparent px-4 py-2.5 text-xs font-medium text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] transition-all"
                  >
                    {t('common:action.cancel', { defaultValue: 'Cancel' })}
                  </button>
                  
                  <button
                    type="submit"
                    disabled={isSubmitting}
                    className="inline-flex items-center justify-center gap-2 rounded-[var(--radius-md)] bg-[var(--accent-gold)] px-5 py-2.5 text-xs font-bold text-[var(--text-contrast)] transition-all hover:bg-[var(--accent-gold-bright)] disabled:opacity-50 shadow-sm"
                  >
                    {isSubmitting ? (
                      <div className="h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
                    ) : (
                      t('common:action.create', { defaultValue: 'Generate Key' })
                    )}
                  </button>
                </div>
              </form>
            )}
          </div>
        </DialogOverlay>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Shared UI components
// ---------------------------------------------------------------------------

function DialogOverlay({
  children,
  onClose,
  preventOutsideClose = false,
}: {
  children: React.ReactNode;
  onClose: () => void;
  preventOutsideClose?: boolean;
}) {
  useEffect(() => {
    if (preventOutsideClose) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [onClose, preventOutsideClose]);

  return (
    <div
      role="dialog"
      aria-modal="true"
      className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/70 backdrop-blur-sm transition-all duration-300 animate-fade-in"
    >
      <div
        className="relative w-full max-w-lg border border-[var(--border-default)] bg-[var(--bg-surface)] p-6 rounded-[var(--radius-lg)] shadow-[var(--shadow-lg)] transition-all duration-300 transform scale-100"
        onClick={(e) => e.stopPropagation()}
      >
        {children}
      </div>
      <div className="absolute inset-0 -z-10" onClick={() => {
        if (!preventOutsideClose) onClose();
      }} />
    </div>
  );
}

interface FieldProps {
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  hint?: string;
  required?: boolean;
}

function Field({
  label,
  value,
  onChange,
  placeholder,
  hint,
  required,
}: FieldProps) {
  return (
    <label className="block space-y-1.5">
      <span className="block text-xs font-semibold text-[var(--text-secondary)]">
        {label}
        {required && <span className="text-rose-400 ml-1">*</span>}
      </span>
      <input
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="w-full rounded-[var(--radius-md)] border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3.5 py-2.5 text-sm text-[var(--text-primary)] font-medium outline-none transition-all placeholder:text-[var(--text-faint)] focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)]"
      />
      {hint && (
        <span className="text-xs text-[var(--text-muted)] block">
          {hint}
        </span>
      )}
    </label>
  );
}

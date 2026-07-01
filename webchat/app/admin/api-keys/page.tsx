'use client';

import { useEffect, useState } from 'react';
import { useAdminUI } from '@/context/admin-ui-context';
import {
  listAPIKeys,
  createAPIKey,
  deleteAPIKey,
} from '@/lib/api/admin-apikeys';
import type { APIKeyUser } from '@/lib/types/admin';
import { formatRelative as formatTime } from '@/lib/utils/format-time';
import { useResource } from '@/hooks/use-resource';
import { LoadingState, ErrorState, EmptyState } from '@/components/admin/resource-states';
import { useTranslation } from 'react-i18next';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function maskKey(key: string): string {
  if (key.length <= 12) return '****';
  return key.slice(0, 8) + '****' + key.slice(-4);
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
  const [actionLoading, setActionLoading] = useState<string | null>(null);

  // Dialog state
  const [showCreate, setShowCreate] = useState(false);
  const [createdKey, setCreatedKey] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  // Form state
  const [formUserId, setFormUserId] = useState('');
  const [formDesc, setFormDesc] = useState('');
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
      setFormError(null);
      const result = await createAPIKey({
        user_id: trimmedId,
        description: formDesc.trim() || undefined,
      });
      setCreatedKey(result.api_key);
      // Refetch to refresh the list with the masked key.
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

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

  return (
    <div className="min-h-screen bg-[var(--bg-base)] px-6 py-8">
      <div className="mx-auto max-w-6xl">
        {/* Header */}
        <div className="mb-8 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <h1 className="font-display text-xl font-bold text-[var(--text-primary)]">
              {t('admin:api_keys.title', { defaultValue: 'API Keys' })}
            </h1>
            {!loading && !error && (
              <span className="rounded-full bg-[var(--bg-hover)] px-2 py-0.5 font-mono text-[11px] text-[var(--text-faint)]">
                {t('admin:api_keys.key_count', { count: keys.length, defaultValue: `${keys.length} keys` })}
              </span>
            )}
          </div>
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={openCreate}
              className="inline-flex items-center gap-1.5 rounded-[var(--radius-sm)] bg-[var(--accent-gold)] px-3 py-1.5 text-[11px] font-bold uppercase tracking-wider text-[var(--bg-base)] transition-colors hover:bg-[var(--accent-gold)]/90"
            >
              <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                strokeWidth={2}
                stroke="currentColor"
                className="h-3.5 w-3.5"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M12 4.5v15m7.5-7.5h-15"
                />
              </svg>
              {t('admin:api_keys.action.create', { defaultValue: 'Create Key' })}
            </button>
            <button
              type="button"
              onClick={reload}
              disabled={loading}
              className="inline-flex items-center gap-1.5 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] px-3 py-1.5 text-[11px] font-bold uppercase tracking-wider text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] disabled:opacity-40"
            >
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
                  d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.992 0 3.181 3.183a8.25 8.25 0 0 0 13.803-3.7M4.031 9.865a8.25 8.25 0 0 1 13.803-3.7l3.181 3.182"
                />
              </svg>
              {t('common:action.refresh', { defaultValue: 'Refresh' })}
            </button>
          </div>
        </div>

        {/* Loading */}
        {loading && <LoadingState label={t('admin:api_keys.loading', { defaultValue: 'Loading API keys...' })} />}

        {/* Error */}
        {error && <ErrorState message={error} onRetry={reload} />}

        {/* Empty state */}
        {!loading && !error && keys.length === 0 && (
          <EmptyState
            title={t('admin:api_keys.empty.title', { defaultValue: 'No API keys configured yet.' })}
            action={
              <button
                type="button"
                onClick={openCreate}
                className="inline-flex items-center gap-1.5 rounded-[var(--radius-sm)] bg-[var(--accent-gold)] px-4 py-2 text-xs font-bold uppercase tracking-wider text-[var(--bg-base)] transition-colors hover:bg-[var(--accent-gold)]/90"
              >
                {t('admin:api_keys.action.create_first', { defaultValue: 'Create First Key' })}
              </button>
            }
          />
        )}

        {/* Table */}
        {!loading && !error && keys.length > 0 && (
          <div className="overflow-hidden rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-surface)]">
            {/* Table header */}
            <div className="grid grid-cols-[1fr_140px_1fr_110px_120px] gap-2 border-b border-[var(--border-subtle)] bg-[var(--bg-elevated)] px-4 py-3">
              <span className="text-[10px] font-bold uppercase tracking-wider text-[var(--text-faint)]">
                {t('admin:api_keys.table.api_key', { defaultValue: 'API Key' })}
              </span>
              <span className="text-[10px] font-bold uppercase tracking-wider text-[var(--text-faint)]">
                {t('admin:api_keys.table.user_id', { defaultValue: 'User ID' })}
              </span>
              <span className="text-[10px] font-bold uppercase tracking-wider text-[var(--text-faint)]">
                {t('admin:api_keys.table.description', { defaultValue: 'Description' })}
              </span>
              <span className="text-[10px] font-bold uppercase tracking-wider text-[var(--text-faint)]">
                {t('admin:api_keys.table.created', { defaultValue: 'Created' })}
              </span>
              <span className="text-right text-[10px] font-bold uppercase tracking-wider text-[var(--text-faint)]">
                {t('admin:api_keys.table.actions', { defaultValue: 'Actions' })}
              </span>
            </div>

            {/* Table rows */}
            {keys.map((k) => (
              <div
                key={k.id || k.api_key}
                className="grid grid-cols-[1fr_140px_1fr_110px_120px] gap-2 border-b border-[var(--border-subtle)] px-4 py-2.5 last:border-b-0 items-center transition-colors hover:bg-[var(--bg-hover)]"
              >
                {/* API Key — display masked value only */}
                <span
                  className="truncate font-mono text-xs text-[var(--text-muted)]"
                >
                  {k.api_key.includes('****') ? k.api_key : maskKey(k.api_key)}
                </span>

                {/* User ID */}
                <span className="truncate text-xs font-medium text-[var(--text-primary)]">
                  {k.user_id}
                </span>

                {/* Description */}
                <span className="truncate text-xs text-[var(--text-muted)]">
                  {k.description || '—'}
                </span>

                {/* Created */}
                <span
                  className="text-xs text-[var(--text-muted)]"
                  title={k.created_at}
                >
                  {formatTime(k.created_at)}
                </span>

                {/* Actions */}
                <div className="flex items-center justify-end gap-1.5">
                  <button
                    type="button"
                    onClick={() => handleDelete(k.id!)}
                    disabled={actionLoading === k.id?.toString()}
                    className="inline-flex items-center gap-1.5 rounded-[var(--radius-sm)] bg-[rgba(244,63,94,0.08)] px-2.5 py-1 text-[10px] font-bold uppercase tracking-wider text-[var(--accent-coral)] transition-colors hover:bg-[rgba(244,63,94,0.15)] disabled:opacity-40"
                    title={t('common:action.delete', { defaultValue: 'Delete' })}
                  >
                    {actionLoading === k.id?.toString() ? (
                      <div className="h-3 w-3 animate-spin rounded-full border border-current border-t-transparent" />
                    ) : (
                      <>
                        <svg
                          xmlns="http://www.w3.org/2000/svg"
                          fill="none"
                          viewBox="0 0 24 24"
                          strokeWidth={1.5}
                          stroke="currentColor"
                          className="h-3 w-3"
                        >
                          <path
                            strokeLinecap="round"
                            strokeLinejoin="round"
                            d="m14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0"
                          />
                        </svg>
                        {t('common:action.delete', { defaultValue: 'Delete' })}
                      </>
                    )}
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* ====== Create Dialog ====== */}
      {showCreate && (
        <DialogOverlay onClose={closeDialog} preventOutsideClose={!!createdKey}>
          <div className="w-full max-w-md">
            <h2 className="font-display text-lg font-bold text-[var(--text-primary)]">
              {t('admin:api_keys.dialog.create_title', { defaultValue: 'Create API Key' })}
            </h2>

            {createdKey ? (
              <div className="space-y-4">
                <div className="rounded-[var(--radius-sm)] border border-amber-500/20 bg-amber-500/10 p-3.5">
                  <div className="flex gap-2.5">
                    <svg
                      xmlns="http://www.w3.org/2000/svg"
                      fill="none"
                      viewBox="0 0 24 24"
                      strokeWidth={2}
                      stroke="var(--accent-gold)"
                      className="h-5 w-5 shrink-0 mt-0.5"
                    >
                      <path
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        d="M12 9v3.75m0-10.036A11.959 11.959 0 0 1 3.598 6 11.99 11.99 0 0 0 3 9.75c0 5.592 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.57-.598-3.75h-.152c-3.196 0-6.1-1.249-8.25-3.286Zm0 13.036h.008v.008H12v-.008Z"
                      />
                    </svg>
                    <div>
                      <h4 className="font-display text-xs font-bold uppercase tracking-wider text-[var(--accent-gold)]">
                        {t('admin:api_keys.dialog.warning_title', { defaultValue: 'Critical Security Warning' })}
                      </h4>
                      <p className="mt-1 text-xs text-[var(--text-muted)] leading-relaxed" dangerouslySetInnerHTML={{
                        __html: t('admin:api_keys.dialog.warning_body', {
                          defaultValue: 'For security reasons, this API key will be shown <strong class="text-[var(--text-primary)]">ONLY ONCE</strong>. You will not be able to retrieve, view, or copy it again after closing this dialog.'
                        })
                      }} />
                    </div>
                  </div>
                </div>

                <div className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-elevated)] p-3">
                  <div className="flex items-center gap-3">
                    <code className="flex-1 break-all font-mono text-xs text-[var(--accent-gold)] select-all selection:bg-[var(--accent-gold)]/25">
                      {createdKey}
                    </code>
                    <button
                      type="button"
                      onClick={() => {
                        navigator.clipboard.writeText(createdKey);
                        setCopied(true);
                        setTimeout(() => setCopied(false), 2000);
                      }}
                      className="shrink-0 rounded-[var(--radius-sm)] bg-[var(--accent-gold)]/10 px-3 py-1.5 text-[10px] font-bold uppercase text-[var(--accent-gold)] transition-colors hover:bg-[var(--accent-gold)]/20"
                    >
                      {copied ? t('common:action.copied', { defaultValue: 'Copied!' }) : t('common:action.copy', { defaultValue: 'Copy' })}
                    </button>
                  </div>
                </div>

                <button
                  type="button"
                  onClick={closeDialog}
                  className="mt-2 w-full rounded-[var(--radius-sm)] bg-[var(--bg-hover)] border border-[var(--border-subtle)] py-2 text-xs font-bold text-[var(--text-primary)] transition-colors hover:bg-[var(--bg-hover-more)] uppercase tracking-wider"
                >
                  {t('admin:api_keys.dialog.confirm_saved', { defaultValue: 'I have copied & saved this key' })}
                </button>
              </div>
            ) : (
              <>
                <form
                  onSubmit={(e) => {
                    e.preventDefault();
                    handleCreate();
                  }}
                  className="mt-4 space-y-4"
                >
                  <Field
                    label={t('admin:api_keys.table.user_id', { defaultValue: 'User ID' })}
                    value={formUserId}
                    onChange={setFormUserId}
                    placeholder={t('admin:api_keys.placeholder.user_id', { defaultValue: 'e.g. alice' })}
                    required
                  />
                  <Field
                    label={t('admin:api_keys.table.description', { defaultValue: 'Description' })}
                    value={formDesc}
                    onChange={setFormDesc}
                    placeholder={t('admin:api_keys.placeholder.description', { defaultValue: 'Optional description' })}
                  />
                  {formError && (
                    <p className="text-xs text-[var(--accent-coral)]">
                      {formError}
                    </p>
                  )}
                  <div className="flex gap-3 pt-2">
                    <button
                      type="submit"
                      className="flex-1 rounded-[var(--radius-sm)] bg-[var(--accent-gold)] px-4 py-2 text-xs font-bold uppercase tracking-wider text-[var(--bg-base)] transition-colors hover:bg-[var(--accent-gold)]/90"
                    >
                      {t('common:action.create', { defaultValue: 'Create' })}
                    </button>
                    <button
                      type="button"
                      onClick={closeDialog}
                      className="flex-1 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] px-4 py-2 text-xs font-medium text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-hover)]"
                    >
                      {t('common:action.cancel', { defaultValue: 'Cancel' })}
                    </button>
                  </div>
                </form>
              </>
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
      className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-md transition-all duration-300"
    >
      <div
        className="relative w-full max-w-md border border-[var(--border-default)] bg-[var(--bg-glass)] backdrop-blur-xl p-6 rounded-[var(--radius-lg)] shadow-[var(--shadow-lg)] transition-all duration-300 transform scale-100 hover:border-[var(--accent-gold)]/20"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="absolute -inset-px -z-10 rounded-[var(--radius-lg)] opacity-10 blur-sm pointer-events-none transition-all bg-[var(--accent-gold)]" />
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
  required?: boolean;
}

function Field({
  label,
  value,
  onChange,
  placeholder,
  required,
}: FieldProps) {
  return (
    <label className="block">
      <span className="mb-1 block text-[10px] font-bold uppercase tracking-wider text-[var(--text-faint)]">
        {label}
        {required && ' *'}
      </span>
      <input
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="w-full rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-elevated)] px-3 py-2 text-sm text-[var(--text-primary)] outline-none transition-colors placeholder:text-[var(--text-faint)] focus:border-[var(--accent-gold)]/40"
      />
    </label>
  );
}

'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { useTranslation } from 'react-i18next';

interface DeleteButtonProps {
  // Resource name shown in the confirmation prompt (e.g. bot/workspace name).
  resourceName: string;
  // Trigger button label (e.g. "Delete Workspace"). Defaults to "Delete".
  buttonLabel?: string;
  // Where to navigate after a successful delete. Omit to stay on the page.
  redirectHref?: string;
  // Performs the delete (e.g. `() => deleteBot(name)`). Thrown errors are
  // surfaced inline; on success the button navigates to redirectHref.
  onDelete: () => Promise<void>;
}

// Two-step delete control with inline error handling. Consolidates the
// near-identical DeleteButton previously duplicated in bots/detail and
// workspaces/detail (PR-1b).
export function DeleteButton({
  resourceName,
  buttonLabel,
  redirectHref,
  onDelete,
}: DeleteButtonProps) {
  const { t } = useTranslation();
  const router = useRouter();
  const [confirming, setConfirming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const label = buttonLabel || t('common:action.delete', { defaultValue: 'Delete' });

  const handleDelete = async () => {
    setDeleting(true);
    setError(null);
    try {
      await onDelete();
      if (redirectHref) router.push(redirectHref);
    } catch (err) {
      setError(err instanceof Error ? err.message : t('admin:delete_button.error_failed', { defaultValue: 'Failed to delete' }));
      setDeleting(false);
    }
  };

  if (confirming) {
    return (
      <div className="mt-6 pt-6 border-t border-[var(--border-subtle)]">
        <div className="rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.06)] border border-[rgba(244,63,94,0.12)] p-4">
          <p className="text-sm text-[var(--accent-coral)] font-medium mb-3">
            {t('admin:delete_button.confirm_message', { name: resourceName, defaultValue: `Delete "${resourceName}"? This action cannot be undone.` })}
          </p>
          {error && <p className="text-xs text-[var(--accent-coral)] mb-3">{error}</p>}
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={handleDelete}
              disabled={deleting}
              className="px-3 py-1.5 rounded-[var(--radius-sm)] text-xs font-bold bg-[var(--accent-coral)] text-white hover:opacity-90 transition-opacity disabled:opacity-50"
            >
              {deleting ? t('common:action.deleting', { defaultValue: 'Deleting...' }) : t('admin:delete_button.yes_delete', { defaultValue: 'Yes, Delete' })}
            </button>
            <button
              type="button"
              onClick={() => { setConfirming(false); setError(null); }}
              className="px-3 py-1.5 rounded-[var(--radius-sm)] text-xs font-semibold text-[var(--text-faint)] hover:text-[var(--text-secondary)] transition-colors"
            >
              {t('common:action.cancel', { defaultValue: 'Cancel' })}
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="mt-6 pt-6 border-t border-[var(--border-subtle)]">
      <button
        type="button"
        onClick={() => setConfirming(true)}
        className="px-3 py-1.5 rounded-[var(--radius-sm)] text-xs font-semibold text-[var(--accent-coral)] border border-[rgba(244,63,94,0.2)] hover:bg-[rgba(244,63,94,0.06)] transition-colors"
      >
        {label}
      </button>
    </div>
  );
}

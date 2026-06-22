'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';

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
  buttonLabel = 'Delete',
  redirectHref,
  onDelete,
}: DeleteButtonProps) {
  const router = useRouter();
  const [confirming, setConfirming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleDelete = async () => {
    setDeleting(true);
    setError(null);
    try {
      await onDelete();
      if (redirectHref) router.push(redirectHref);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete');
      setDeleting(false);
    }
  };

  if (confirming) {
    return (
      <div className="mt-6 pt-6 border-t border-[var(--border-subtle)]">
        <div className="rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.06)] border border-[rgba(244,63,94,0.12)] p-4">
          <p className="text-sm text-[var(--accent-coral)] font-medium mb-3">
            Delete &ldquo;{resourceName}&rdquo;? This action cannot be undone.
          </p>
          {error && <p className="text-xs text-[var(--accent-coral)] mb-3">{error}</p>}
          <div className="flex items-center gap-2">
            <button
              onClick={handleDelete}
              disabled={deleting}
              className="px-3 py-1.5 rounded-[var(--radius-sm)] text-xs font-bold bg-[var(--accent-coral)] text-white hover:opacity-90 transition-opacity disabled:opacity-50"
            >
              {deleting ? 'Deleting...' : 'Yes, Delete'}
            </button>
            <button
              onClick={() => { setConfirming(false); setError(null); }}
              className="px-3 py-1.5 rounded-[var(--radius-sm)] text-xs font-semibold text-[var(--text-faint)] hover:text-[var(--text-secondary)] transition-colors"
            >
              Cancel
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="mt-6 pt-6 border-t border-[var(--border-subtle)]">
      <button
        onClick={() => setConfirming(true)}
        className="px-3 py-1.5 rounded-[var(--radius-sm)] text-xs font-semibold text-[var(--accent-coral)] border border-[rgba(244,63,94,0.2)] hover:bg-[rgba(244,63,94,0.06)] transition-colors"
      >
        {buttonLabel}
      </button>
    </div>
  );
}

import type { ReactNode } from 'react';

// ---------------------------------------------------------------------------
// Shared resource-state components for admin list pages.
// Replaces the per-page hand-rolled loading/error/empty templates.
// Text/icons are passed via props; nothing is hardcoded by default except the
// "Loading..." fallback label.
// ---------------------------------------------------------------------------

export function LoadingState({ label }: { label?: string }) {
  return (
    <div className="flex items-center justify-center py-24">
      <div className="flex flex-col items-center gap-3">
        <div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
        <span className="text-xs text-[var(--text-faint)]">{label ?? 'Loading...'}</span>
      </div>
    </div>
  );
}

export function ErrorState({
  message,
  onRetry,
}: {
  message: string;
  onRetry?: () => void;
}) {
  return (
    <div className="rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] p-4">
      <div className="flex items-center justify-between">
        <p className="text-sm text-[var(--accent-coral)]">{message}</p>
        {onRetry && (
          <button
            onClick={onRetry}
            className="text-xs text-[var(--accent-coral)] hover:underline"
          >
            Retry
          </button>
        )}
      </div>
    </div>
  );
}

export function EmptyState({
  title,
  description,
  action,
}: {
  title: string;
  description?: string;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-col items-center justify-center py-24 text-center">
      <p className="text-sm font-medium text-[var(--text-secondary)] mb-1">{title}</p>
      {description && <p className="text-xs text-[var(--text-faint)] mb-5">{description}</p>}
      {action}
    </div>
  );
}

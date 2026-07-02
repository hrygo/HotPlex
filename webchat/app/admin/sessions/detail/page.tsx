'use client';

import { useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import Link from 'next/link';
import { listSessions, terminateSession } from '@/lib/api/admin-sessions';
import { SessionStatusBadge } from '@/components/admin/session-status-badge';
import { useAdminUI } from '@/context/admin-ui-context';
import type { AdminSessionInfo } from '@/lib/types/admin';
import { formatDateTime } from '@/lib/utils/format-time';
import { useTranslation } from 'react-i18next';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

interface InfoRowProps {
  label: string;
  value: string;
  mono?: boolean;
}

function InfoRow({ label, value, mono }: InfoRowProps) {
  return (
    <div className="px-4 py-3 rounded-[var(--radius-md)] bg-[var(--bg-surface)] border border-[var(--border-subtle)]">
      <p className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1">
        {label}
      </p>
      <p className={`text-sm text-[var(--text-primary)] ${mono ? 'font-mono' : ''} break-all`}>
        {value || '—'}
      </p>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Page Component
// ---------------------------------------------------------------------------

export default function SessionDetailPage() {
  const { t } = useTranslation();
  const { showToast, confirm } = useAdminUI();
  const searchParams = useSearchParams();
  const id = searchParams.get('id') ?? '';

  const [session, setSession] = useState<AdminSessionInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [terminating, setTerminating] = useState(false);
  const [notFound, setNotFound] = useState(false);

  const loadSession = useCallback(async () => {
    if (!id) {
      setNotFound(true);
      setLoading(false);
      return;
    }
    try {
      setLoading(true);
      setError(null);
      setNotFound(false);
      const data = await listSessions(100, 0);
      const found = data.sessions.find((s) => s.id === id);
      if (!found) {
        setNotFound(true);
      } else {
        setSession(found);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t('admin:sessions.detail.error_load', { defaultValue: 'Failed to load session' }));
    } finally {
      setLoading(false);
    }
  }, [id, t]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- mount-time fetch
    loadSession();
  }, [loadSession]);

  const handleTerminate = async () => {
    if (!session || session.state === 'terminated') return;
    const confirmed = await confirm(
      t('admin:sessions.confirm.terminate_title', { defaultValue: 'Terminate Session?' }),
      t('admin:sessions.confirm.terminate_body', { id: session.id, defaultValue: `Are you sure you want to terminate session "${session.id}"? The running worker process will be stopped immediately.` }),
      { confirmLabel: t('admin:sessions.action.terminate', { defaultValue: 'Terminate' }), destructive: true }
    );
    if (!confirmed) return;
    try {
      setTerminating(true);
      await terminateSession(session.id);
      setSession((prev) => (prev ? { ...prev, state: 'terminated' } : prev));
      showToast(t('admin:sessions.toast.terminated', { id: session.id, defaultValue: `Session "${session.id}" successfully terminated.` }), 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('admin:sessions.toast.terminate_failed', { defaultValue: 'Failed to terminate session' }), 'error');
    } finally {
      setTerminating(false);
    }
  };

  // ---------------------------------------------------------------------------
  // Shared layout wrapper for all states
  // ---------------------------------------------------------------------------

  const wrapper = (children: React.ReactNode) => (
    <div className="max-w-5xl mx-auto px-6 py-8">
      <Link
        href="/admin/sessions"
        className="inline-flex items-center gap-1.5 text-xs text-[var(--text-faint)] hover:text-[var(--text-primary)] transition-colors mb-6"
      >
        <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-3.5 w-3.5">
          <path strokeLinecap="round" strokeLinejoin="round" d="M10.5 19.5 3 12m0 0 7.5-7.5M3 12h18" />
        </svg>
        {t('admin:sessions.action.back_to_sessions', { defaultValue: 'Back to Sessions' })}
      </Link>
      {children}
    </div>
  );

  if (!id) {
    return wrapper(
      <div className="flex items-center justify-center min-h-[60vh]">
        <p className="text-sm text-[var(--text-faint)]">{t('admin:sessions.detail.no_id', { defaultValue: 'No session ID specified' })}</p>
      </div>,
    );
  }

  if (loading) {
    return wrapper(
      <div className="flex items-center justify-center min-h-[60vh]">
        <div className="flex flex-col items-center gap-3">
          <div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
          <span className="text-xs text-[var(--text-faint)]">{t('admin:sessions.detail.loading', { defaultValue: 'Loading session...' })}</span>
        </div>
      </div>,
    );
  }

  if (error) {
    return wrapper(
      <div className="rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] p-4">
        <div className="flex items-center justify-between">
          <p className="text-sm text-[var(--text-coral)]">{error}</p>
          <button
            type="button"
            onClick={loadSession}
            className="text-xs font-medium text-[var(--accent-coral)] underline underline-offset-2 hover:text-[var(--accent-coral)]/80 transition-colors"
          >
            {t('common:action.retry', { defaultValue: 'Retry' })}
          </button>
        </div>
      </div>,
    );
  }

  if (notFound || !session) {
    return wrapper(
      <div className="flex flex-col items-center justify-center min-h-[60vh] text-center">
        <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-10 w-10 text-[var(--text-faint)] mb-4">
          <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v3.75m9-.75a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9 3.75h.008v.008H12v-.008Z" />
        </svg>
        <p className="text-sm text-[var(--text-muted)]">{t('admin:sessions.detail.not_found', { defaultValue: 'Session not found' })}</p>
        <p className="text-xs text-[var(--text-faint)] mt-1 font-mono">{id}</p>
      </div>,
    );
  }

  return wrapper(
    <>
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center gap-3">
          <h1 className="text-xl font-display font-bold text-[var(--text-primary)] font-mono">
            {session.id}
          </h1>
          <SessionStatusBadge state={session.state} />
        </div>
        {session.state !== 'terminated' && (
          <button
            type="button"
            onClick={handleTerminate}
            disabled={terminating}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-[var(--radius-sm)] text-[11px] font-bold uppercase tracking-wider text-[var(--accent-amber)] bg-[rgba(245,158,11,0.1)] hover:bg-[rgba(245,158,11,0.2)] transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {terminating ? (
              <div className="w-3 h-3 border border-current border-t-transparent rounded-full animate-spin" />
            ) : (
              <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-3.5 w-3.5">
                <path strokeLinecap="round" strokeLinejoin="round" d="M5.636 5.636a9 9 0 1 0 12.728 0M12 3v9" />
              </svg>
            )}
            {t('admin:sessions.action.terminate', { defaultValue: 'Terminate' })}
          </button>
        )}
      </div>

      {/* Info cards */}
      <div className="grid grid-cols-2 gap-3">
        <InfoRow label={t('admin:sessions.drawer.state', { defaultValue: 'State' })} value={session.state} />
        <InfoRow label={t('admin:sessions.drawer.engine_type', { defaultValue: 'Worker Type' })} value={session.worker_type ?? ''} mono />
        <InfoRow label={t('admin:sessions.drawer.user_id', { defaultValue: 'User ID' })} value={session.user_id ?? ''} mono />
        <InfoRow label={t('admin:sessions.drawer.turns', { defaultValue: 'Turns' })} value={session.turn_count != null ? String(session.turn_count) : ''} />
        <InfoRow label={t('admin:sessions.drawer.work_dir', { defaultValue: 'Work Dir' })} value={session.work_dir ?? ''} mono />
        {session.title && <InfoRow label={t('admin:sessions.detail.title_label', { defaultValue: 'Title' })} value={session.title} />}
        <InfoRow label={t('admin:sessions.drawer.started_at', { defaultValue: 'Created At' })} value={formatDateTime(session.created_at)} />
        <InfoRow label={t('admin:sessions.drawer.last_active', { defaultValue: 'Last Active' })} value={formatDateTime(session.updated_at)} />
      </div>
    </>,
  );
}

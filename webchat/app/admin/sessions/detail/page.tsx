'use client';

import { useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import Link from 'next/link';
import { listSessions, terminateSession } from '@/lib/api/admin-sessions';
import { SessionStatusBadge } from '@/components/admin/session-status-badge';
import type { AdminSessionInfo } from '@/lib/types/admin';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function InfoRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="px-4 py-3 rounded-xl bg-[var(--bg-surface)] border border-[var(--border-subtle)]">
      <p className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1">
        {label}
      </p>
      <p className={`text-sm text-[var(--text-primary)] ${mono ? 'font-mono' : ''} break-all`}>
        {value || '—'}
      </p>
    </div>
  );
}

function formatDateTime(iso?: string): string {
  if (!iso) return '—';
  return new Date(iso).toLocaleString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

// ---------------------------------------------------------------------------
// Page Component
// ---------------------------------------------------------------------------

export default function SessionDetailPage() {
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
      setError(err instanceof Error ? err.message : 'Failed to load session');
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    loadSession();
  }, [loadSession]);

  const handleTerminate = async () => {
    if (!session || session.state === 'terminated') return;
    if (!window.confirm('Terminate this session? The worker will be stopped.')) return;
    try {
      setTerminating(true);
      await terminateSession(session.id);
      setSession((prev) => (prev ? { ...prev, state: 'terminated' } : prev));
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to terminate session');
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
        Back to Sessions
      </Link>
      {children}
    </div>
  );

  if (!id) {
    return wrapper(
      <div className="flex items-center justify-center min-h-[60vh]">
        <p className="text-sm text-[var(--text-faint)]">No session ID specified</p>
      </div>,
    );
  }

  if (loading) {
    return wrapper(
      <div className="flex items-center justify-center min-h-[60vh]">
        <div className="flex flex-col items-center gap-3">
          <div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
          <span className="text-xs text-[var(--text-faint)]">Loading session...</span>
        </div>
      </div>,
    );
  }

  if (error) {
    return wrapper(
      <div className="rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] p-4">
        <div className="flex items-center justify-between">
          <p className="text-sm text-[var(--accent-coral)]">{error}</p>
          <button
            onClick={loadSession}
            className="text-xs font-medium text-[var(--accent-coral)] underline underline-offset-2 hover:text-[var(--accent-coral)]/80 transition-colors"
          >
            Retry
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
        <p className="text-sm text-[var(--text-muted)]">Session not found</p>
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
            Terminate
          </button>
        )}
      </div>

      {/* Info cards */}
      <div className="grid grid-cols-2 gap-3">
        <InfoRow label="State" value={session.state} />
        <InfoRow label="Worker Type" value={session.worker_type ?? ''} mono />
        <InfoRow label="User ID" value={session.user_id ?? ''} mono />
        <InfoRow label="Turns" value={session.turn_count != null ? String(session.turn_count) : ''} />
        <InfoRow label="Work Dir" value={session.work_dir ?? ''} mono />
        {session.title && <InfoRow label="Title" value={session.title} />}
        <InfoRow label="Created At" value={formatDateTime(session.created_at)} />
        <InfoRow label="Last Active" value={formatDateTime(session.updated_at)} />
      </div>
    </>,
  );
}

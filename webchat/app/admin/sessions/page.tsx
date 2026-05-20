'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import { listSessions, terminateSession, deleteSession } from '@/lib/api/admin-sessions';
import { SessionStatusBadge } from '@/components/admin/session-status-badge';
import type { AdminSessionInfo } from '@/lib/types/admin';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type SessionState = AdminSessionInfo['state'];
type FilterOption = 'all' | SessionState;
type SortOption = 'last_active' | 'created';

function truncateId(id: string): string {
  if (id.length <= 12) return id;
  return `${id.slice(0, 8)}...${id.slice(-4)}`;
}

function formatTime(iso?: string): string {
  if (!iso) return '--';
  const date = new Date(iso);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMin = Math.floor(diffMs / 60000);
  const diffHour = Math.floor(diffMs / 3600000);
  const diffDay = Math.floor(diffMs / 86400000);

  if (diffMin < 1) return 'now';
  if (diffMin < 60) return `${diffMin}m`;
  if (diffHour < 24) return `${diffHour}h`;
  if (diffDay < 7) return `${diffDay}d`;
  return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}

// ---------------------------------------------------------------------------
// Page Component
// ---------------------------------------------------------------------------

export default function SessionsPage() {
  const [sessions, setSessions] = useState<AdminSessionInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState<FilterOption>('all');
  const [sort, setSort] = useState<SortOption>('last_active');
  const [query, setQuery] = useState('');
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [confirmId, setConfirmId] = useState<string | null>(null);

  const loadSessions = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      setConfirmId(null);
      const data = await listSessions(100, 0);
      setSessions(data.sessions);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load sessions');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadSessions();
  }, [loadSessions]);

  // ---------------------------------------------------------------------------
  // Derived data
  // ---------------------------------------------------------------------------

  const filtered = useMemo(() => {
    let result = sessions;
    if (filter !== 'all') {
      result = result.filter((s) => s.state === filter);
    }
    if (query.trim()) {
      const q = query.toLowerCase();
      result = result.filter(
        (s) =>
          s.id.toLowerCase().includes(q) ||
          s.user_id?.toLowerCase().includes(q) ||
          s.worker_type?.toLowerCase().includes(q),
      );
    }
    return result;
  }, [sessions, filter, query]);

  const sorted = useMemo(
    () =>
      [...filtered].sort((a, b) => {
        if (sort === 'created') {
          return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
        }
        return new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime();
      }),
    [filtered, sort],
  );

  const activeCount = sessions.filter(
    (s) => s.state === 'active' || s.state === 'working',
  ).length;

  // ---------------------------------------------------------------------------
  // Actions
  // ---------------------------------------------------------------------------

  const handleTerminate = async (id: string) => {
    if (!window.confirm('Terminate this session? The worker will be stopped.')) return;
    try {
      setActionLoading(id);
      await terminateSession(id);
      setSessions((prev) =>
        prev.map((s) => (s.id === id ? { ...s, state: 'terminated' } : s)),
      );
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to terminate session');
    } finally {
      setActionLoading(null);
    }
  };

  const handleDelete = async (id: string) => {
    try {
      setActionLoading(id);
      setConfirmId(null);
      await deleteSession(id);
      setSessions((prev) => prev.filter((s) => s.id !== id));
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to delete session');
    } finally {
      setActionLoading(null);
    }
  };

  // ---------------------------------------------------------------------------
  // Grid column template — header + rows must match
  // ---------------------------------------------------------------------------

  const gridCols =
    'grid-cols-[minmax(160px,2fr)_120px_100px_100px_72px_72px_80px]';

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

  return (
    <div className="min-h-screen bg-[var(--bg-base)] px-6 py-8">
      <div className="max-w-6xl mx-auto">
        {/* Header */}
        <div className="flex items-center justify-between mb-6">
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-display font-bold text-[var(--text-primary)]">
              Sessions
            </h1>
            {!loading && !error && (
              <span className="text-[11px] font-mono text-[var(--text-faint)] px-2 py-0.5 rounded-full bg-[var(--bg-hover)]">
                {activeCount} active / {sessions.length} total
              </span>
            )}
          </div>

          {/* Controls */}
          <div className="flex items-center gap-3">
            {/* Search */}
            <div className="relative">
              <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                strokeWidth={1.5}
                stroke="currentColor"
                className="absolute left-2 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-[var(--text-faint)]"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="m21 21-5.197-5.197m0 0A7.5 7.5 0 1 0 5.196 5.196a7.5 7.5 0 0 0 10.607 10.607Z"
                />
              </svg>
              <input
                type="text"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Search ID, user, worker..."
                className="w-48 pl-7 pr-2.5 py-1.5 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] text-xs text-[var(--text-primary)] placeholder:text-[var(--text-faint)] outline-none transition-colors focus:border-[var(--accent-gold)]/40"
              />
            </div>

            {/* Status filter */}
            <select
              value={filter}
              onChange={(e) => setFilter(e.target.value as FilterOption)}
              className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-2.5 py-1.5 text-xs text-[var(--text-primary)] outline-none transition-colors focus:border-[var(--accent-gold)]/40"
            >
              <option value="all">All Status</option>
              <option value="active">Active</option>
              <option value="working">Working</option>
              <option value="idle">Idle</option>
              <option value="terminated">Terminated</option>
            </select>

            {/* Sort */}
            <select
              value={sort}
              onChange={(e) => setSort(e.target.value as SortOption)}
              className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-2.5 py-1.5 text-xs text-[var(--text-primary)] outline-none transition-colors focus:border-[var(--accent-gold)]/40"
            >
              <option value="last_active">Last Active</option>
              <option value="created">Created</option>
            </select>

            {/* Refresh */}
            <button
              onClick={loadSessions}
              disabled={loading}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] text-[11px] font-bold uppercase tracking-wider text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] transition-colors disabled:opacity-40"
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
              Refresh
            </button>
          </div>
        </div>

        {/* Loading */}
        {loading && (
          <div className="flex items-center justify-center py-24">
            <div className="flex flex-col items-center gap-3">
              <div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
              <span className="text-xs text-[var(--text-faint)]">Loading sessions...</span>
            </div>
          </div>
        )}

        {/* Error */}
        {error && (
          <div className="rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] p-4">
            <div className="flex items-center justify-between">
              <p className="text-sm text-[var(--accent-coral)]">{error}</p>
              <button
                onClick={loadSessions}
                className="text-xs font-medium text-[var(--accent-coral)] underline underline-offset-2 hover:text-[var(--accent-coral)]/80 transition-colors"
              >
                Retry
              </button>
            </div>
          </div>
        )}

        {/* Empty state */}
        {!loading && !error && sorted.length === 0 && (
          <div className="flex flex-col items-center justify-center py-24 text-center">
            <svg
              xmlns="http://www.w3.org/2000/svg"
              fill="none"
              viewBox="0 0 24 24"
              strokeWidth={1.5}
              stroke="currentColor"
              className="h-10 w-10 text-[var(--text-faint)] mb-4"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M20.25 8.511c.884.284 1.5 1.128 1.5 2.097v4.286c0 1.136-.847 2.1-1.98 2.193-.34.027-.68.052-1.02.072v3.091l-3-3c-1.354 0-2.694-.055-4.02-.163a2.115 2.115 0 0 1-.825-.242m9.345-8.334a2.126 2.126 0 0 0-.476-.095 48.64 48.64 0 0 0-8.048 0c-1.131.094-1.976 1.057-1.976 2.192v4.286c0 .837.46 1.58 1.155 1.951m9.345-8.334V6.637c0-1.621-1.152-3.026-2.76-3.235A48.455 48.455 0 0 0 11.25 3c-2.115 0-4.198.137-6.24.402-1.608.209-2.76 1.614-2.76 3.235v6.226c0 1.621 1.152 3.026 2.76 3.235.577.075 1.157.14 1.74.194V21l4.155-4.155"
              />
            </svg>
            <p className="text-sm text-[var(--text-muted)]">
              {filter !== 'all' || query.trim()
                ? 'No matching sessions found.'
                : 'No sessions yet.'}
            </p>
          </div>
        )}

        {/* Table */}
        {!loading && !error && sorted.length > 0 && (
          <div className="rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] overflow-hidden">
            {/* Header */}
            <div
              className={`grid ${gridCols} gap-2 px-4 py-2.5 border-b border-[var(--border-subtle)] bg-[var(--bg-elevated)]`}
            >
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">
                ID
              </span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">
                Worker
              </span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">
                User
              </span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">
                Status
              </span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">
                Created
              </span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">
                Active
              </span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider text-right">
                &nbsp;
              </span>
            </div>

            {/* Rows */}
            {sorted.map((session) => (
              <Link
                key={session.id}
                href={`/admin/sessions/detail?id=${encodeURIComponent(session.id)}`}
                className={`grid ${gridCols} gap-2 px-4 py-2.5 border-b border-[var(--border-subtle)] last:border-b-0 hover:bg-[var(--bg-hover)] transition-colors items-center`}
              >
                {/* ID */}
                <span
                  className="text-xs font-mono text-[var(--accent-gold)] truncate"
                  title={session.id}
                >
                  {truncateId(session.id)}
                </span>

                {/* Worker */}
                <span className="text-xs text-[var(--text-muted)] truncate" title={session.worker_type}>
                  {session.worker_type || '--'}
                </span>

                {/* User */}
                <span className="text-xs text-[var(--text-muted)] truncate" title={session.user_id}>
                  {session.user_id ? truncateId(session.user_id) : '--'}
                </span>

                {/* Status */}
                <span onClick={(e) => e.preventDefault()}>
                  <SessionStatusBadge state={session.state} />
                </span>

                {/* Created */}
                <span className="text-xs text-[var(--text-muted)]" title={session.created_at}>
                  {formatTime(session.created_at)}
                </span>

                {/* Last active */}
                <span className="text-xs text-[var(--text-muted)]" title={session.updated_at}>
                  {formatTime(session.updated_at)}
                </span>

                {/* Actions */}
                <span
                  className="flex items-center justify-end gap-1"
                  onClick={(e) => e.preventDefault()}
                >
                  {confirmId === session.id ? (
                    <div className="flex items-center gap-1 animate-[fadeInScale_0.12s_ease-out]">
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          handleDelete(session.id);
                        }}
                        disabled={actionLoading === session.id}
                        className="px-2 py-1 rounded-[var(--radius-sm)] text-[10px] font-bold text-[var(--accent-coral)] bg-[rgba(244,63,94,0.1)] hover:bg-[rgba(244,63,94,0.2)] transition-colors disabled:opacity-40"
                      >
                        {actionLoading === session.id ? (
                          <span className="inline-block w-3 h-3 border border-current border-t-transparent rounded-full animate-spin" />
                        ) : (
                          'Confirm'
                        )}
                      </button>
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          setConfirmId(null);
                        }}
                        disabled={actionLoading === session.id}
                        className="px-2 py-1 rounded-[var(--radius-sm)] text-[10px] font-bold text-[var(--text-faint)] hover:text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] transition-colors disabled:opacity-40"
                      >
                        Cancel
                      </button>
                    </div>
                  ) : (
                    <>
                      {session.state !== 'terminated' && (
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            handleTerminate(session.id);
                          }}
                          disabled={actionLoading === session.id}
                          className="p-1.5 rounded-[var(--radius-sm)] text-[var(--accent-amber)] bg-[rgba(245,158,11,0.1)] hover:bg-[rgba(245,158,11,0.2)] transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
                          title="Terminate session"
                        >
                          {actionLoading === session.id ? (
                            <div className="w-3.5 h-3.5 border border-current border-t-transparent rounded-full animate-spin" />
                          ) : (
                            <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-3.5 w-3.5">
                              <path strokeLinecap="round" strokeLinejoin="round" d="M5.636 5.636a9 9 0 1 0 12.728 0M12 3v9" />
                            </svg>
                          )}
                        </button>
                      )}
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          setConfirmId(session.id);
                        }}
                        disabled={actionLoading === session.id}
                        className="p-1.5 rounded-[var(--radius-sm)] text-[var(--accent-coral)] bg-[rgba(244,63,94,0.08)] hover:bg-[rgba(244,63,94,0.15)] transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
                        title="Delete session"
                      >
                        <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-3.5 w-3.5">
                          <path strokeLinecap="round" strokeLinejoin="round" d="m14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0" />
                        </svg>
                      </button>
                    </>
                  )}
                </span>
              </Link>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

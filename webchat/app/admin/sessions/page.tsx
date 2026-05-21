'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import { listSessions, terminateSession, deleteSession } from '@/lib/api/admin-sessions';
import { SessionStatusBadge } from '@/components/admin/session-status-badge';
import { useAdminUI } from '@/context/admin-ui-context';
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

export default function SessionsPage() {
  const { showToast, confirm } = useAdminUI();
  const [sessions, setSessions] = useState<AdminSessionInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState<FilterOption>('all');
  const [sort, setSort] = useState<SortOption>('last_active');
  const [query, setQuery] = useState('');
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [confirmId, setConfirmId] = useState<string | null>(null);
  const [drawerSession, setDrawerSession] = useState<AdminSessionInfo | null>(null);
  const [copyIdFeedback, setCopyIdFeedback] = useState<string | null>(null);

  const loadSessions = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      setConfirmId(null);
      const data = await listSessions(100, 0);
      setSessions(data.sessions);

      // If drawer is open, update its session details in case of state changes
      if (drawerSession) {
        const updated = data.sessions.find((s) => s.id === drawerSession.id);
        if (updated) {
          setDrawerSession(updated);
        } else {
          setDrawerSession(null); // Session was deleted
        }
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load sessions');
    } finally {
      setLoading(false);
    }
  }, [drawerSession]);

  useEffect(() => {
    loadSessions();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // ---------------------------------------------------------------------------
  // Derived Stats
  // ---------------------------------------------------------------------------

  const stats = useMemo(() => {
    const total = sessions.length;
    const active = sessions.filter((s) => s.state === 'active' || s.state === 'working').length;
    const idle = sessions.filter((s) => s.state === 'idle').length;
    const terminated = sessions.filter((s) => s.state === 'terminated').length;
    return { total, active, idle, terminated };
  }, [sessions]);

  // ---------------------------------------------------------------------------
  // Filtering & Sorting
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
          s.worker_type?.toLowerCase().includes(q) ||
          s.title?.toLowerCase().includes(q)
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
    [filtered, sort]
  );

  // ---------------------------------------------------------------------------
  // Actions
  // ---------------------------------------------------------------------------

  const handleCopyId = async (e: React.MouseEvent, id: string) => {
    e.stopPropagation();
    e.preventDefault();
    try {
      await navigator.clipboard.writeText(id);
      setCopyIdFeedback(id);
      showToast('Copied Session ID to clipboard', 'success');
      setTimeout(() => setCopyIdFeedback(null), 2000);
    } catch {
      showToast('Failed to copy ID', 'error');
    }
  };

  const handleTerminate = async (id: string, fromDrawer = false) => {
    const confirmed = await confirm(
      'Terminate Session?',
      `Are you sure you want to terminate session "${truncateId(id)}"? The running worker process will be stopped immediately.`,
      { confirmLabel: 'Terminate', destructive: true }
    );
    if (!confirmed) return;
    try {
      setActionLoading(id);
      await terminateSession(id);

      setSessions((prev) =>
        prev.map((s) => (s.id === id ? { ...s, state: 'terminated' } : s))
      );

      if (fromDrawer && drawerSession && drawerSession.id === id) {
        setDrawerSession((prev) => (prev ? { ...prev, state: 'terminated' } : null));
      }

      showToast(`Session "${truncateId(id)}" successfully terminated.`, 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to terminate session', 'error');
    } finally {
      setActionLoading(null);
    }
  };

  const handleDelete = async (id: string, fromDrawer = false) => {
    const confirmed = await confirm(
      'Delete Session?',
      `Are you sure you want to permanently delete session "${truncateId(id)}"? All database traces will be deleted. This action is irreversible.`,
      { confirmLabel: 'Delete', destructive: true }
    );
    if (!confirmed) return;

    try {
      setActionLoading(id);
      setConfirmId(null);
      await deleteSession(id);
      setSessions((prev) => prev.filter((s) => s.id !== id));

      if (fromDrawer || (drawerSession && drawerSession.id === id)) {
        setDrawerSession(null);
      }

      showToast(`Session "${truncateId(id)}" successfully deleted.`, 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to delete session', 'error');
    } finally {
      setActionLoading(null);
    }
  };

  // ---------------------------------------------------------------------------
  // Grid layout structure
  // ---------------------------------------------------------------------------
  const gridCols = 'grid-cols-[1.5fr_1fr_1fr_120px_100px_100px_100px]';

  // Keyboard navigation for drawer (close on Esc)
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setDrawerSession(null);
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, []);

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
              Sessions
            </h1>
            <p className="text-xs text-[var(--text-muted)] mt-1">
              Monitor, audit, and manage real-time active MOSS/Claude Code workers and sessions.
            </p>
          </div>

          <button
            onClick={loadSessions}
            disabled={loading}
            className="self-start md:self-auto inline-flex items-center gap-1.5 px-3.5 py-1.5 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[10px] font-bold uppercase tracking-wider text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] transition-all active:scale-95 disabled:opacity-40 shadow-sm"
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
            Refresh List
          </button>
        </div>

        {/* Stats Grid */}
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
          {/* Card 1: Total */}
          <div className="relative overflow-hidden rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-glass)] backdrop-blur-md p-4 transition-all hover:border-[var(--border-bright)] shadow-md">
            <div className="absolute top-0 right-0 p-3 opacity-10">
              <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="w-12 h-12 text-[var(--text-primary)]">
                <path strokeLinecap="round" strokeLinejoin="round" d="M9 17.25v1.007a3 3 0 0 1-.879 2.122L7.5 21h9l-.621-.621A3 3 0 0 1 15 18.257V17.25m6-12V15a2.25 2.25 0 0 1-2.25 2.25H5.25A2.25 2.25 0 0 1 3 15V5.25m18 0A2.25 2.25 0 0 0 18.75 3H5.25A2.25 2.25 0 0 0 3 5.25m18 0V12a2.25 2.25 0 0 1-2.25 2.25H5.25A2.25 2.25 0 0 1 3 12V5.25" />
              </svg>
            </div>
            <p className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">
              Total Sessions
            </p>
            <div className="mt-2 flex items-baseline gap-2">
              <span className="text-2xl font-display font-extrabold text-[var(--text-primary)]">
                {stats.total}
              </span>
              <span className="text-[10px] text-[var(--text-muted)] font-mono">sessions</span>
            </div>
            <p className="text-[10px] text-[var(--text-faint)] mt-1.5">Lifetime execution tracks</p>
          </div>

          {/* Card 2: Active */}
          <div className="relative overflow-hidden rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-glass)] backdrop-blur-md p-4 transition-all hover:border-[var(--border-bright)] shadow-md">
            <div className="absolute top-0 right-0 p-3 opacity-15">
              <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="w-12 h-12 text-[var(--accent-emerald)]">
                <path strokeLinecap="round" strokeLinejoin="round" d="M3.75 13.5l10.5-11.25L12 10.5h8.25L9.75 21.75 12 13.5H3.75z" />
              </svg>
            </div>
            <div className="flex items-center gap-1.5">
              <span className="relative flex h-2 w-2">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-[var(--accent-emerald)] opacity-75"></span>
                <span className="relative inline-flex rounded-full h-2 w-2 bg-[var(--accent-emerald)]"></span>
              </span>
              <p className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">
                Active Engines
              </p>
            </div>
            <div className="mt-2 flex items-baseline gap-2">
              <span className="text-2xl font-display font-extrabold text-[var(--accent-emerald)]">
                {stats.active}
              </span>
              <span className="text-[10px] text-[var(--text-muted)] font-mono">running</span>
            </div>
            <p className="text-[10px] text-[var(--text-faint)] mt-1.5">Consuming worker resources</p>
          </div>

          {/* Card 3: Idle */}
          <div className="relative overflow-hidden rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-glass)] backdrop-blur-md p-4 transition-all hover:border-[var(--border-bright)] shadow-md">
            <div className="absolute top-0 right-0 p-3 opacity-15">
              <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="w-12 h-12 text-[var(--accent-amber)]">
                <path strokeLinecap="round" strokeLinejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 1 1-18 0 9 9 0 0 1 18 0z" />
              </svg>
            </div>
            <p className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">
              Idle Workers
            </p>
            <div className="mt-2 flex items-baseline gap-2">
              <span className="text-2xl font-display font-extrabold text-[var(--accent-amber)]">
                {stats.idle}
              </span>
              <span className="text-[10px] text-[var(--text-muted)] font-mono">waiting</span>
            </div>
            <p className="text-[10px] text-[var(--text-faint)] mt-1.5">Suspended, waiting for input</p>
          </div>

          {/* Card 4: Terminated */}
          <div className="relative overflow-hidden rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-glass)] backdrop-blur-md p-4 transition-all hover:border-[var(--border-bright)] shadow-md">
            <div className="absolute top-0 right-0 p-3 opacity-10">
              <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="w-12 h-12 text-[var(--text-muted)]">
                <path strokeLinecap="round" strokeLinejoin="round" d="M5.636 5.636a9 9 0 1 0 12.728 0M12 3v9" />
              </svg>
            </div>
            <p className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">
              Terminated
            </p>
            <div className="mt-2 flex items-baseline gap-2">
              <span className="text-2xl font-display font-extrabold text-[var(--text-muted)]">
                {stats.terminated}
              </span>
              <span className="text-[10px] text-[var(--text-muted)] font-mono">completed</span>
            </div>
            <p className="text-[10px] text-[var(--text-faint)] mt-1.5">Safely released & exited</p>
          </div>
        </div>

        {/* Toolbar Controls */}
        <div className="flex flex-col lg:flex-row items-stretch lg:items-center justify-between gap-4 mb-6 bg-[var(--bg-glass)] border border-[var(--border-subtle)] rounded-[var(--radius-md)] p-3 backdrop-blur-md shadow-sm">
          {/* Segmented Tabs Status Filter */}
          <div className="flex flex-wrap items-center gap-1 bg-[var(--bg-base)] border border-[var(--border-subtle)] p-1 rounded-[var(--radius-sm)]">
            <button
              onClick={() => setFilter('all')}
              className={`px-3 py-1 text-[11px] font-semibold rounded-[var(--radius-xs)] transition-all ${
                filter === 'all'
                  ? 'bg-[var(--bg-elevated)] text-[var(--accent-gold)] shadow-sm'
                  : 'text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)]'
              }`}
            >
              All <span className="opacity-60 ml-0.5">({stats.total})</span>
            </button>
            <button
              onClick={() => setFilter('active')}
              className={`px-3 py-1 text-[11px] font-semibold rounded-[var(--radius-xs)] transition-all ${
                filter === 'active'
                  ? 'bg-[var(--bg-elevated)] text-[var(--accent-emerald)] shadow-sm'
                  : 'text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)]'
              }`}
            >
              Active <span className="opacity-60 ml-0.5">({sessions.filter((s) => s.state === 'active').length})</span>
            </button>
            <button
              onClick={() => setFilter('working')}
              className={`px-3 py-1 text-[11px] font-semibold rounded-[var(--radius-xs)] transition-all ${
                filter === 'working'
                  ? 'bg-[var(--bg-elevated)] text-[var(--accent-emerald)] shadow-sm'
                  : 'text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)]'
              }`}
            >
              Working <span className="opacity-60 ml-0.5">({sessions.filter((s) => s.state === 'working').length})</span>
            </button>
            <button
              onClick={() => setFilter('idle')}
              className={`px-3 py-1 text-[11px] font-semibold rounded-[var(--radius-xs)] transition-all ${
                filter === 'idle'
                  ? 'bg-[var(--bg-elevated)] text-[var(--accent-amber)] shadow-sm'
                  : 'text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)]'
              }`}
            >
              Idle <span className="opacity-60 ml-0.5">({stats.idle})</span>
            </button>
            <button
              onClick={() => setFilter('terminated')}
              className={`px-3 py-1 text-[11px] font-semibold rounded-[var(--radius-xs)] transition-all ${
                filter === 'terminated'
                  ? 'bg-[var(--bg-elevated)] text-[var(--text-muted)] shadow-sm'
                  : 'text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)]'
              }`}
            >
              Terminated <span className="opacity-60 ml-0.5">({stats.terminated})</span>
            </button>
          </div>

          <div className="flex flex-col sm:flex-row items-center gap-3 shrink-0">
            {/* Search Input */}
            <div className="relative w-full sm:w-56">
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
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Search session details..."
                className="w-full pl-8 pr-7 py-1.5 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-base)] text-xs text-[var(--text-primary)] placeholder:text-[var(--text-faint)] outline-none transition-all focus:border-[var(--accent-gold)]/40"
              />
              {query && (
                <button
                  onClick={() => setQuery('')}
                  className="absolute right-2 top-1/2 -translate-y-1/2 text-[var(--text-faint)] hover:text-[var(--text-primary)] transition-colors p-0.5"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" className="w-3.5 h-3.5">
                    <path d="M6.28 5.22a.75.75 0 0 0-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 1 0 1.06 1.06L10 11.06l3.72 3.72a.75.75 0 1 0 1.06-1.06L11.06 10l3.72-3.72a.75.75 0 0 0-1.06-1.06L10 8.94 6.28 5.22Z" />
                  </svg>
                </button>
              )}
            </div>

            {/* Sort Dropdown */}
            <div className="relative w-full sm:w-auto shrink-0">
              <select
                value={sort}
                onChange={(e) => setSort(e.target.value as SortOption)}
                className="w-full sm:w-auto rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-base)] pl-3 pr-8 py-1.5 text-xs text-[var(--text-primary)] outline-none transition-all focus:border-[var(--accent-gold)]/40 appearance-none"
              >
                <option value="last_active">Last Active</option>
                <option value="created">Created Date</option>
              </select>
              <div className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-[var(--text-faint)]">
                <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={2} stroke="currentColor" className="w-3 h-3">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M19.5 8.25l-7.5 7.5-7.5-7.5" />
                </svg>
              </div>
            </div>
          </div>
        </div>

        {/* Loading Spinner */}
        {loading && sessions.length === 0 && (
          <div className="flex items-center justify-center py-32 bg-[var(--bg-glass)] border border-[var(--border-subtle)] rounded-[var(--radius-md)] backdrop-blur-md shadow-md">
            <div className="flex flex-col items-center gap-3">
              <div className="w-8 h-8 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
              <span className="text-xs font-medium text-[var(--text-muted)] animate-pulse">Loading execution registry...</span>
            </div>
          </div>
        )}

        {/* Error Callout */}
        {error && (
          <div className="rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] p-4 shadow-sm mb-6">
            <div className="flex items-center justify-between">
              <div className="flex gap-2">
                <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={2} stroke="var(--accent-coral)" className="w-4 h-4 mt-0.5">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v3.75m9-.75a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9 3.75h.008v.008H12v-.008Z" />
                </svg>
                <p className="text-xs font-semibold text-[var(--accent-coral)]">{error}</p>
              </div>
              <button
                onClick={loadSessions}
                className="text-xs font-bold text-[var(--accent-coral)] underline underline-offset-4 hover:text-[var(--accent-coral)]/80 transition-colors"
              >
                Retry Fetch
              </button>
            </div>
          </div>
        )}

        {/* Empty State */}
        {!loading && !error && sorted.length === 0 && (
          <div className="flex flex-col items-center justify-center py-24 text-center bg-[var(--bg-glass)] border border-[var(--border-subtle)] rounded-[var(--radius-md)] backdrop-blur-md shadow-md">
            <svg
              xmlns="http://www.w3.org/2000/svg"
              fill="none"
              viewBox="0 0 24 24"
              strokeWidth={1.2}
              stroke="currentColor"
              className="h-12 w-12 text-[var(--text-faint)] mb-4 animate-float"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M20.25 8.511c.884.284 1.5 1.128 1.5 2.097v4.286c0 1.136-.847 2.1-1.98 2.193-.34.027-.68.052-1.02.072v3.091l-3-3c-1.354 0-2.694-.055-4.02-.163a2.115 2.115 0 0 1-.825-.242m9.345-8.334a2.126 2.126 0 0 0-.476-.095 48.64 48.64 0 0 0-8.048 0c-1.131.094-1.976 1.057-1.976 2.192v4.286c0 .837.46 1.58 1.155 1.951m9.345-8.334V6.637c0-1.621-1.152-3.026-2.76-3.235A48.455 48.455 0 0 0 11.25 3c-2.115 0-4.198.137-6.24.402-1.608.209-2.76 1.614-2.76 3.235v6.226c0 1.621 1.152 3.026 2.76 3.235.577.075 1.157.14 1.74.194V21l4.155-4.155"
              />
            </svg>
            <p className="text-sm font-semibold text-[var(--text-muted)]">
              {filter !== 'all' || query.trim()
                ? 'No matching execution channels found'
                : 'No session registry exists'}
            </p>
            <p className="text-xs text-[var(--text-faint)] mt-1.5 max-w-xs leading-relaxed">
              Try adjusting your search criteria, selecting another filter tab, or refreshing.
            </p>
            {(filter !== 'all' || query.trim()) && (
              <button
                onClick={() => {
                  setFilter('all');
                  setQuery('');
                }}
                className="mt-4 px-3 py-1.5 rounded-[var(--radius-xs)] border border-[var(--border-subtle)] text-[10px] font-bold uppercase tracking-wider text-[var(--text-primary)] hover:bg-[var(--bg-hover)] transition-all"
              >
                Reset Filter Parameters
              </button>
            )}
          </div>
        )}

        {/* Interactive Grid Table */}
        {!loading && !error && sorted.length > 0 && (
          <div className="rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-glass)] backdrop-blur-md shadow-lg overflow-hidden animate-[fadeInScale_0.15s_ease-out]">
            {/* Grid Header */}
            <div
              className={`grid ${gridCols} gap-3 px-5 py-3.5 border-b border-[var(--border-subtle)] bg-[var(--bg-surface)] items-center`}
            >
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-widest font-mono">
                ID / Title
              </span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-widest font-mono">
                Engine type
              </span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-widest font-mono">
                Executing user
              </span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-widest font-mono">
                Status
              </span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-widest font-mono">
                Started
              </span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-widest font-mono">
                Active time
              </span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-widest font-mono text-right">
                Actions
              </span>
            </div>

            {/* Grid Rows */}
            <div className="divide-y divide-[var(--border-subtle)]">
              {sorted.map((session) => {
                const isSelected = drawerSession?.id === session.id;
                return (
                  <div
                    key={session.id}
                    onClick={() => setDrawerSession(session)}
                    className={`grid ${gridCols} gap-3 px-5 py-3.5 transition-all items-center cursor-pointer select-none hover:bg-[var(--bg-hover)] ${
                      isSelected
                        ? 'bg-[var(--bg-active)] border-l-2 border-l-[var(--accent-gold)] pl-[18px]'
                        : 'border-l-2 border-l-transparent'
                    }`}
                  >
                    {/* ID / Title */}
                    <div className="flex flex-col min-w-0 pr-2">
                      <div className="flex items-center gap-1.5 min-w-0">
                        <span
                          className="text-xs font-semibold text-[var(--text-primary)] truncate"
                          title={session.title || 'Untitled Session'}
                        >
                          {session.title || 'Untitled Session'}
                        </span>
                      </div>
                      <div className="flex items-center gap-1 mt-1 group">
                        <span className="text-[10px] font-mono text-[var(--accent-gold)] shrink-0 select-all">
                          {truncateId(session.id)}
                        </span>
                        <button
                          onClick={(e) => handleCopyId(e, session.id)}
                          className="text-[var(--text-faint)] hover:text-[var(--accent-gold)] p-0.5 rounded transition-all opacity-0 group-hover:opacity-100 focus:opacity-100"
                          title="Copy session ID"
                        >
                          {copyIdFeedback === session.id ? (
                            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="var(--accent-emerald)" className="w-3 h-3">
                              <path fillRule="evenodd" d="M16.704 4.153a.75.75 0 0 1 .143 1.052l-8 10.5a.75.75 0 0 1-1.127.075l-4.5-4.5a.75.75 0 0 1 1.06-1.06l3.894 3.893 7.48-9.817a.75.75 0 0 1 1.05-.143Z" clipRule="evenodd" />
                            </svg>
                          ) : (
                            <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={2} stroke="currentColor" className="w-3 h-3">
                              <path strokeLinecap="round" strokeLinejoin="round" d="M8.25 7.5V6.108c0-1.135.845-2.098 1.976-2.192.373-.03.748-.057 1.123-.08M15.75 18H18a2.25 2.25 0 0 0 2.25-2.25V6.108c0-1.135-.845-2.098-1.976-2.192a48.424 48.424 0 0 0-1.123-.08M15.75 18.75v-1.875a3.375 3.375 0 0 0-3.375-3.375h-1.5a1.125 1.125 0 0 1-1.125-1.125v-1.5A3.375 3.375 0 0 0 6.375 7.5H5.25m11.9-3.664A2.251 2.251 0 0 0 15 2.25h-1.5a2.251 2.251 0 0 0-2.15 1.586m5.8 0c.065.21.1.433.1.664v.75h-6V4.5c0-.231.035-.454.1-.664M6.75 7.5H4.875c-.621 0-1.125.504-1.125 1.125v12c0 .621.504 1.125 1.125 1.125h9.75c.621 0 1.125-.504 1.125-1.125V16.5a9 9 0 0 0-9-9Z" />
                            </svg>
                          )}
                        </button>
                      </div>
                    </div>

                    {/* Worker */}
                    <div className="flex items-center gap-1.5 truncate">
                      <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="w-3.5 h-3.5 text-[var(--text-faint)]">
                        <path strokeLinecap="round" strokeLinejoin="round" d="M9 17.25v1.007a3 3 0 0 1-.879 2.122L7.5 21h9l-.621-.621A3 3 0 0 1 15 18.257V17.25m6-12V15a2.25 2.25 0 0 1-2.25 2.25H5.25A2.25 2.25 0 0 1 3 15V5.25m18 0A2.25 2.25 0 0 0 18.75 3H5.25A2.25 2.25 0 0 0 3 5.25m18 0V12a2.25 2.25 0 0 1-2.25 2.25H5.25A2.25 2.25 0 0 1 3 12V5.25" />
                      </svg>
                      <span className="text-xs font-mono text-[var(--text-muted)] truncate" title={session.worker_type}>
                        {session.worker_type || '--'}
                      </span>
                    </div>

                    {/* User */}
                    <div className="flex items-center gap-1.5 truncate">
                      <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="w-3.5 h-3.5 text-[var(--text-faint)]">
                        <path strokeLinecap="round" strokeLinejoin="round" d="M15.75 6a3.75 3.75 0 1 1-7.5 0 3.75 3.75 0 0 1 7.5 0ZM4.501 20.118a7.5 7.5 0 0 1 14.998 0A17.933 17.933 0 0 1 12 21.75c-2.676 0-5.216-.584-7.499-1.632Z" />
                      </svg>
                      <span className="text-xs text-[var(--text-muted)] truncate font-medium" title={session.user_id}>
                        {session.user_id ? truncateId(session.user_id) : '--'}
                      </span>
                    </div>

                    {/* Status */}
                    <div onClick={(e) => e.stopPropagation()}>
                      <SessionStatusBadge state={session.state} />
                    </div>

                    {/* Created */}
                    <span className="text-xs text-[var(--text-muted)]" title={formatDateTime(session.created_at)}>
                      {formatTime(session.created_at)}
                    </span>

                    {/* Last active */}
                    <span className="text-xs text-[var(--text-muted)]" title={formatDateTime(session.updated_at)}>
                      {formatTime(session.updated_at)}
                    </span>

                    {/* Actions */}
                    <div
                      className="flex items-center justify-end gap-1.5"
                      onClick={(e) => e.stopPropagation()}
                    >
                      {confirmId === session.id ? (
                        <div className="flex items-center gap-1 animate-[fadeInScale_0.12s_ease-out]">
                          <button
                            onClick={() => handleDelete(session.id)}
                            disabled={actionLoading === session.id}
                            className="px-2.5 py-1 rounded-[var(--radius-xs)] text-[9px] font-extrabold uppercase tracking-wide text-[var(--accent-coral)] bg-[rgba(244,63,94,0.12)] hover:bg-[rgba(244,63,94,0.22)] transition-colors disabled:opacity-40"
                          >
                            {actionLoading === session.id ? (
                              <span className="inline-block w-2.5 h-2.5 border border-current border-t-transparent rounded-full animate-spin" />
                            ) : (
                              'Delete'
                            )}
                          </button>
                          <button
                            onClick={() => setConfirmId(null)}
                            disabled={actionLoading === session.id}
                            className="px-2 py-1 rounded-[var(--radius-xs)] text-[9px] font-bold text-[var(--text-faint)] hover:text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] transition-colors disabled:opacity-40"
                          >
                            Cancel
                          </button>
                        </div>
                      ) : (
                        <>
                          {session.state !== 'terminated' && (
                            <button
                              onClick={() => handleTerminate(session.id)}
                              disabled={actionLoading === session.id}
                              className="p-2 rounded-[var(--radius-sm)] text-[var(--accent-amber)] bg-[rgba(245,158,11,0.08)] border border-transparent hover:border-[rgba(245,158,11,0.2)] hover:bg-[rgba(245,158,11,0.15)] transition-all disabled:opacity-40 disabled:cursor-not-allowed active:scale-95"
                              title="Terminate active execution worker"
                            >
                              {actionLoading === session.id ? (
                                <div className="w-3.5 h-3.5 border border-current border-t-transparent rounded-full animate-spin" />
                              ) : (
                                <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={2} stroke="currentColor" className="h-3.5 w-3.5">
                                  <path strokeLinecap="round" strokeLinejoin="round" d="M5.636 5.636a9 9 0 1 0 12.728 0M12 3v9" />
                                </svg>
                              )}
                            </button>
                          )}
                          <button
                            onClick={() => setConfirmId(session.id)}
                            disabled={actionLoading === session.id}
                            className="p-2 rounded-[var(--radius-sm)] text-[var(--accent-coral)] bg-[rgba(244,63,94,0.06)] border border-transparent hover:border-[rgba(244,63,94,0.18)] hover:bg-[rgba(244,63,94,0.12)] transition-all disabled:opacity-40 disabled:cursor-not-allowed active:scale-95"
                            title="Delete session database entry"
                          >
                            <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.8} stroke="currentColor" className="h-3.5 w-3.5">
                              <path strokeLinecap="round" strokeLinejoin="round" d="m14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0" />
                            </svg>
                          </button>
                        </>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        )}
      </div>

      {/* ====== DETAIL SLIDE-OUT DRAWER ====== */}
      {drawerSession && (
        <div
          className="fixed inset-0 z-50 overflow-hidden"
          role="dialog"
          aria-modal="true"
        >
          {/* Backdrop blur overlay */}
          <div
            className="absolute inset-0 bg-black/50 backdrop-blur-sm transition-opacity duration-300 animate-[fadeInUp_0.2s_ease-out]"
            onClick={() => setDrawerSession(null)}
          />

          <div className="absolute inset-y-0 right-0 max-w-full flex">
            <div className="relative w-screen max-w-md bg-[var(--bg-surface)] border-l border-[var(--border-subtle)] shadow-2xl flex flex-col justify-between transform transition-transform duration-300 translate-x-0 animate-[slideIn_0.22s_cubic-bezier(0.2,0.8,0.2,1)]">
              
              {/* Header */}
              <div className="px-6 pt-7 pb-4 border-b border-[var(--border-subtle)] flex items-center justify-between">
                <div>
                  <div className="flex items-center gap-2">
                    <h2 className="text-sm font-bold text-[var(--text-faint)] uppercase tracking-wider font-mono">
                      Session Inspector
                    </h2>
                    <span className="w-1.5 h-1.5 rounded-full bg-[var(--accent-gold)]" />
                  </div>
                  <h1 className="text-md font-display font-bold text-[var(--text-primary)] mt-1 truncate max-w-[280px]">
                    {drawerSession.title || 'Untitled Session'}
                  </h1>
                </div>
                <button
                  onClick={() => setDrawerSession(null)}
                  className="p-1.5 rounded-full text-[var(--text-faint)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)] transition-all"
                  title="Close inspector"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={2} stroke="currentColor" className="w-5 h-5">
                    <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              </div>

              {/* Body Metadata details */}
              <div className="flex-1 overflow-y-auto px-6 py-6 space-y-5">
                
                {/* ID with interactive Copy button */}
                <div className="px-4 py-3 rounded-[var(--radius-md)] bg-[var(--bg-glass)] border border-[var(--border-subtle)]">
                  <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider block mb-1">
                    Full Session ID
                  </span>
                  <div className="flex items-center justify-between gap-3">
                    <code className="text-xs font-mono text-[var(--accent-gold)] break-all select-all font-semibold">
                      {drawerSession.id}
                    </code>
                    <button
                      onClick={(e) => handleCopyId(e, drawerSession.id)}
                      className="shrink-0 flex items-center gap-1 px-2.5 py-1 rounded-[var(--radius-xs)] bg-[var(--accent-gold)]/10 border border-[var(--accent-gold)]/20 text-[9px] font-bold uppercase text-[var(--accent-gold)] hover:bg-[var(--accent-gold)]/20 transition-all"
                    >
                      {copyIdFeedback === drawerSession.id ? 'Copied' : 'Copy'}
                    </button>
                  </div>
                </div>

                {/* Info key-value block grid */}
                <div className="grid grid-cols-2 gap-3.5">
                  <div className="px-4 py-3 rounded-[var(--radius-md)] bg-[var(--bg-glass)] border border-[var(--border-subtle)]">
                    <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider block mb-1">
                      Execution State
                    </span>
                    <SessionStatusBadge state={drawerSession.state} />
                  </div>
                  <div className="px-4 py-3 rounded-[var(--radius-md)] bg-[var(--bg-glass)] border border-[var(--border-subtle)]">
                    <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider block mb-1">
                      Total turns
                    </span>
                    <span className="text-xs text-[var(--text-primary)] font-bold">
                      {drawerSession.turn_count ?? 0} <span className="text-[9px] font-normal text-[var(--text-muted)]">turns completed</span>
                    </span>
                  </div>
                </div>

                <div className="px-4 py-3 rounded-[var(--radius-md)] bg-[var(--bg-glass)] border border-[var(--border-subtle)] space-y-3.5">
                  <div>
                    <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider block mb-1">
                      Worker engine type
                    </span>
                    <span className="text-xs font-mono font-medium text-[var(--text-primary)]">
                      {drawerSession.worker_type || '—'}
                    </span>
                  </div>
                  <div>
                    <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider block mb-1">
                      Executing User ID
                    </span>
                    <span className="text-xs font-mono font-medium text-[var(--text-primary)]">
                      {drawerSession.user_id || '—'}
                    </span>
                  </div>
                  <div>
                    <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider block mb-1">
                      Working directory
                    </span>
                    <span className="text-xs font-mono text-[var(--text-muted)] break-all leading-normal" title={drawerSession.work_dir}>
                      {drawerSession.work_dir || '—'}
                    </span>
                  </div>
                </div>

                <div className="px-4 py-3 rounded-[var(--radius-md)] bg-[var(--bg-glass)] border border-[var(--border-subtle)] space-y-3.5">
                  <div>
                    <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider block mb-1">
                      Started At
                    </span>
                    <span className="text-xs text-[var(--text-secondary)] font-medium">
                      {formatDateTime(drawerSession.created_at)}
                    </span>
                  </div>
                  <div>
                    <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider block mb-1">
                      Last Active At
                    </span>
                    <span className="text-xs text-[var(--text-secondary)] font-medium">
                      {formatDateTime(drawerSession.updated_at)}
                    </span>
                  </div>
                </div>
              </div>

              {/* Bottom Actions */}
              <div className="p-6 border-t border-[var(--border-subtle)] bg-[var(--bg-surface)] space-y-3">
                <div className="flex gap-3">
                  {drawerSession.state !== 'terminated' && (
                    <button
                      onClick={() => handleTerminate(drawerSession.id, true)}
                      disabled={actionLoading === drawerSession.id}
                      className="flex-1 inline-flex items-center justify-center gap-1.5 px-4 py-2.5 rounded-[var(--radius-sm)] text-[11px] font-bold uppercase tracking-wider text-[var(--accent-amber)] bg-[rgba(245,158,11,0.08)] border border-[rgba(245,158,11,0.18)] hover:bg-[rgba(245,158,11,0.15)] transition-all active:scale-95 disabled:opacity-40"
                    >
                      {actionLoading === drawerSession.id ? (
                        <div className="w-3.5 h-3.5 border border-current border-t-transparent rounded-full animate-spin" />
                      ) : (
                        <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={2} stroke="currentColor" className="h-3.5 w-3.5">
                          <path strokeLinecap="round" strokeLinejoin="round" d="M5.636 5.636a9 9 0 1 0 12.728 0M12 3v9" />
                        </svg>
                      )}
                      Terminate Engine
                    </button>
                  )}
                  <button
                    onClick={() => handleDelete(drawerSession.id, true)}
                    disabled={actionLoading === drawerSession.id}
                    className="flex-1 inline-flex items-center justify-center gap-1.5 px-4 py-2.5 rounded-[var(--radius-sm)] text-[11px] font-bold uppercase tracking-wider text-[var(--accent-coral)] bg-[rgba(244,63,94,0.06)] border border-[rgba(244,63,94,0.18)] hover:bg-[rgba(244,63,94,0.12)] transition-all active:scale-95 disabled:opacity-40"
                  >
                    Delete Entry
                  </button>
                </div>

                <Link
                  href={`/admin/sessions/detail?id=${encodeURIComponent(drawerSession.id)}`}
                  className="w-full inline-flex items-center justify-center gap-1.5 px-4 py-2.5 rounded-[var(--radius-sm)] text-[11px] font-bold uppercase tracking-wider text-[var(--text-muted)] border border-[var(--border-subtle)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] transition-all text-center"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={2} stroke="currentColor" className="w-3.5 h-3.5">
                    <path strokeLinecap="round" strokeLinejoin="round" d="M13.5 6H5.25A2.25 2.25 0 0 0 3 8.25v10.5A2.25 2.25 0 0 0 5.25 21h10.5A2.25 2.25 0 0 0 18 18.75V10.5m-10.5 6L21 3m0 0h-5.25M21 3v5.25" />
                  </svg>
                  Open Full Detail View
                </Link>
              </div>

            </div>
          </div>
        </div>
      )}
    </div>
  );
}

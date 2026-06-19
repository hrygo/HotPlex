'use client';

import { useEffect, useState, useMemo, useCallback } from 'react';
import Link from 'next/link';
import { listWorkspaces } from '@/lib/api/workspaces';
import type { Workspace } from '@/lib/api/workspaces';

function formatDate(ms: number): string {
  if (!ms) return '—';
  try {
    return new Date(ms * 1000).toLocaleDateString(undefined, {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
    });
  } catch {
    return '—';
  }
}

export default function WorkspacesPage() {
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState('');

  const load = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await listWorkspaces();
      setWorkspaces(data.workspaces ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load workspaces');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const filtered = useMemo(() => {
    if (!query.trim()) return workspaces;
    const q = query.toLowerCase();
    return workspaces.filter(
      (w) =>
        w.name.toLowerCase().includes(q) ||
        (w.work_dir ?? '').toLowerCase().includes(q) ||
        w.id.toLowerCase().includes(q),
    );
  }, [workspaces, query]);

  return (
    <div className="min-h-screen bg-[var(--bg-base)] p-6">
      <div className="max-w-5xl mx-auto">
        {/* Header */}
        <div className="flex items-center justify-between mb-6">
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-display font-bold text-[var(--text-primary)]">Workspaces</h1>
            {!loading && !error && (
              <span className="text-[11px] font-mono text-[var(--text-faint)] px-2 py-0.5 rounded-full bg-[var(--bg-hover)]">
                {workspaces.length}
              </span>
            )}
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={load}
              disabled={loading}
              className="inline-flex items-center justify-center w-8 h-8 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] text-[var(--text-faint)] hover:text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] transition-colors disabled:opacity-50"
              title="Refresh"
            >
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
                className={loading ? 'animate-spin' : ''}
              >
                <path d="M21 2v6h-6" />
                <path d="M3 12a9 9 0 0 1 15-6.7L21 8" />
                <path d="M3 22v-6h6" />
                <path d="M21 12a9 9 0 0 1-15 6.7L3 16" />
              </svg>
            </button>
            <Link
              href="/admin/workspaces/new"
              className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-[var(--radius-sm)] text-[11px] font-bold uppercase tracking-wider bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-colors"
            >
              + New Workspace
            </Link>
          </div>
        </div>

        {/* Search */}
        {!loading && !error && workspaces.length > 0 && (
          <div className="relative mb-5">
            <svg
              className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--text-faint)]"
              width="14"
              height="14"
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
              placeholder="Search by name, work dir, or ID..."
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="w-full pl-9 pr-4 py-2 rounded-[var(--radius-sm)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] text-sm text-[var(--text-primary)] placeholder:text-[var(--text-faint)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors"
            />
            {query && (
              <button
                onClick={() => setQuery('')}
                className="absolute right-3 top-1/2 -translate-y-1/2 text-[var(--text-faint)] hover:text-[var(--text-secondary)]"
              >
                <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
                  <line x1="1" y1="1" x2="13" y2="13" />
                  <line x1="13" y1="1" x2="1" y2="13" />
                </svg>
              </button>
            )}
          </div>
        )}

        {/* Loading */}
        {loading && (
          <div className="flex items-center justify-center py-24">
            <div className="flex flex-col items-center gap-3">
              <div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
              <span className="text-xs text-[var(--text-faint)]">Loading workspaces...</span>
            </div>
          </div>
        )}

        {/* Error */}
        {error && (
          <div className="rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] p-4">
            <div className="flex items-center justify-between">
              <p className="text-sm text-[var(--accent-coral)]">{error}</p>
              <button onClick={load} className="text-xs text-[var(--accent-coral)] hover:underline">
                Retry
              </button>
            </div>
          </div>
        )}

        {/* Empty state */}
        {!loading && !error && workspaces.length === 0 && (
          <div className="flex flex-col items-center justify-center py-24 text-center">
            <div className="w-16 h-16 mb-4 rounded-2xl bg-[var(--bg-hover)] flex items-center justify-center">
              <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="var(--text-faint)" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                <path d="M3 7.5A2.5 2.5 0 0 1 5.5 5h13A2.5 2.5 0 0 1 21 7.5v9A2.5 2.5 0 0 1 18.5 19h-13A2.5 2.5 0 0 1 3 16.5v-9Z" />
                <path d="M3 9h18" />
                <path d="M8 13h4" />
              </svg>
            </div>
            <p className="text-sm font-medium text-[var(--text-secondary)] mb-1">No workspaces yet</p>
            <p className="text-xs text-[var(--text-faint)] mb-5">Create your first workspace to scope a chat environment.</p>
            <Link
              href="/admin/workspaces/new"
              className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-[var(--radius-sm)] text-[11px] font-bold uppercase tracking-wider bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-colors"
            >
              + New Workspace
            </Link>
          </div>
        )}

        {/* Search no results */}
        {!loading && !error && workspaces.length > 0 && filtered.length === 0 && (
          <div className="flex flex-col items-center justify-center py-16 text-center">
            <p className="text-sm text-[var(--text-muted)] mb-1">
              No workspaces matching &ldquo;{query}&rdquo;
            </p>
            <button onClick={() => setQuery('')} className="text-xs text-[var(--accent-gold)] hover:underline">
              Clear search
            </button>
          </div>
        )}

        {/* Workspace grid */}
        {!loading && !error && filtered.length > 0 && (
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            {filtered.map((w) => (
              <Link
                key={w.id}
                href={`/admin/workspaces/detail?id=${encodeURIComponent(w.id)}`}
                className="group block rounded-[var(--radius-md)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] p-5 hover:border-[var(--accent-gold)]/40 hover:bg-[var(--bg-hover)] transition-all"
              >
                <div className="flex items-start justify-between mb-3">
                  <div className="min-w-0">
                    <h2 className="text-sm font-semibold text-[var(--text-primary)] group-hover:text-[var(--accent-gold)] transition-colors truncate">
                      {w.name}
                    </h2>
                    <p className="text-[10px] font-mono text-[var(--text-faint)] mt-0.5 truncate">{w.id}</p>
                  </div>
                  {w.worker_preference && (
                    <span className="shrink-0 px-2 py-0.5 rounded-full text-[10px] font-mono font-semibold bg-[var(--bg-elevated)] text-[var(--text-secondary)] border border-[var(--border-subtle)]">
                      {w.worker_preference}
                    </span>
                  )}
                </div>

                <div className="flex items-center gap-2 text-[11px] text-[var(--text-faint)]">
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M3 7.5A2.5 2.5 0 0 1 5.5 5h13A2.5 2.5 0 0 1 21 7.5v9A2.5 2.5 0 0 1 18.5 19h-13A2.5 2.5 0 0 1 3 16.5v-9Z" />
                  </svg>
                  <span className="font-mono truncate">{w.work_dir || '—'}</span>
                </div>

                <div className="mt-3 pt-3 border-t border-[var(--border-subtle)] flex items-center justify-between text-[10px] text-[var(--text-faint)]">
                  <span>Created {formatDate(w.created_at)}</span>
                  <span className="opacity-0 group-hover:opacity-100 text-[var(--accent-gold)] transition-opacity">Open →</span>
                </div>
              </Link>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

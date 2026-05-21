'use client';

import { useCallback, useEffect, useState } from 'react';
import Link from 'next/link';
import { listCronJobs, updateCronJob, deleteCronJob, triggerCronJob } from '@/lib/api/admin-cron';
import { useAdminUI } from '@/context/admin-ui-context';
import type { CronJob } from '@/lib/types/admin';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type FilterOption = 'all' | 'enabled' | 'disabled';

function formatTime(iso?: string): string {
  if (!iso) return '--';
  const date = new Date(iso);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffSec = Math.floor(diffMs / 1000);
  const diffMin = Math.floor(diffMs / 60000);
  const diffHour = Math.floor(diffMs / 3600000);
  const diffDay = Math.floor(diffMs / 86400000);

  if (diffSec < 0) {
    // Future time
    const futureMs = -diffMs;
    const futureMin = Math.floor(futureMs / 60000);
    const futureHour = Math.floor(futureMs / 3600000);
    if (futureMin < 60) return `in ${futureMin}m`;
    if (futureHour < 24) return `in ${futureHour}h`;
    return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
  }
  if (diffSec < 60) return 'Just now';
  if (diffMin < 60) return `${diffMin}m ago`;
  if (diffHour < 24) return `${diffHour}h ago`;
  if (diffDay < 7) return `${diffDay}d ago`;
  return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}

// ---------------------------------------------------------------------------
// Page Component
// ---------------------------------------------------------------------------

export default function CronPage() {
  const { showToast, confirm } = useAdminUI();
  const [jobs, setJobs] = useState<CronJob[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState<FilterOption>('all');
  const [actionLoading, setActionLoading] = useState<string | null>(null);

  const loadJobs = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await listCronJobs();
      setJobs(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load cron jobs');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadJobs();
  }, [loadJobs]);

  // ---------------------------------------------------------------------------
  // Derived data
  // ---------------------------------------------------------------------------

  const filtered = jobs.filter((j) => {
    if (filter === 'all') return true;
    if (filter === 'enabled') return j.enabled;
    return !j.enabled;
  });

  const enabledCount = jobs.filter((j) => j.enabled).length;

  // ---------------------------------------------------------------------------
  // Actions
  // ---------------------------------------------------------------------------

  const handleToggle = async (job: CronJob) => {
    const next = !job.enabled;
    const label = next ? 'enable' : 'disable';
    const confirmed = await confirm(
      `${next ? 'Enable' : 'Disable'} Cron Job?`,
      `Are you sure you want to ${label} cron job "${job.name}"?`
    );
    if (!confirmed) return;
    try {
      setActionLoading(job.id);
      await updateCronJob(job.id, { enabled: next });
      setJobs((prev) =>
        prev.map((j) => (j.id === job.id ? { ...j, enabled: next } : j)),
      );
      showToast(`Cron job "${job.name}" ${next ? 'enabled' : 'disabled'} successfully.`, 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : `Failed to ${label} cron job`, 'error');
    } finally {
      setActionLoading(null);
    }
  };

  const handleTrigger = async (id: string, name: string) => {
    const confirmed = await confirm(
      'Trigger Cron Job?',
      `Manually execute cron job "${name}" right now?`
    );
    if (!confirmed) return;
    try {
      setActionLoading(id);
      await triggerCronJob(id);
      showToast(`Cron job "${name}" manually triggered.`, 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to trigger cron job', 'error');
    } finally {
      setActionLoading(null);
    }
  };

  const handleDelete = async (id: string, name: string) => {
    const confirmed = await confirm(
      'Delete Cron Job?',
      `Are you sure you want to permanently delete cron job "${name}"? This action is irreversible.`,
      { destructive: true }
    );
    if (!confirmed) return;
    try {
      setActionLoading(id);
      await deleteCronJob(id);
      setJobs((prev) => prev.filter((j) => j.id !== id));
      showToast(`Cron job "${name}" successfully deleted.`, 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to delete cron job', 'error');
    } finally {
      setActionLoading(null);
    }
  };

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

  return (
    <div className="min-h-screen bg-[var(--bg-base)] px-6 py-8">
      <div className="max-w-6xl mx-auto">
        {/* Header */}
        <div className="flex items-center justify-between mb-8">
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-display font-bold text-[var(--text-primary)]">
              Cron Jobs
            </h1>
            {!loading && !error && (
              <span className="text-[11px] font-mono text-[var(--text-faint)] px-2 py-0.5 rounded-full bg-[var(--bg-hover)]">
                {enabledCount} enabled / {jobs.length} total
              </span>
            )}
          </div>

          {/* Controls */}
          <div className="flex items-center gap-3">
            {/* Filter */}
            <select
              value={filter}
              onChange={(e) => setFilter(e.target.value as FilterOption)}
              className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-2.5 py-1.5 text-xs text-[var(--text-primary)] outline-none transition-colors focus:border-[var(--accent-gold)]/40"
            >
              <option value="all">All Jobs</option>
              <option value="enabled">Enabled</option>
              <option value="disabled">Disabled</option>
            </select>

            {/* Refresh */}
            <button
              onClick={loadJobs}
              disabled={loading}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] text-[11px] font-bold uppercase tracking-wider text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] transition-colors disabled:opacity-40"
            >
              <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-3.5 w-3.5">
                <path strokeLinecap="round" strokeLinejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.992 0 3.181 3.183a8.25 8.25 0 0 0 13.803-3.7M4.031 9.865a8.25 8.25 0 0 1 13.803-3.7l3.181 3.182" />
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
              <span className="text-xs text-[var(--text-faint)]">Loading cron jobs...</span>
            </div>
          </div>
        )}

        {/* Error */}
        {error && (
          <div className="rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] p-4">
            <div className="flex items-center justify-between">
              <p className="text-sm text-[var(--accent-coral)]">{error}</p>
              <button
                onClick={loadJobs}
                className="text-xs font-medium text-[var(--accent-coral)] underline underline-offset-2 hover:text-[var(--accent-coral)]/80 transition-colors"
              >
                Retry
              </button>
            </div>
          </div>
        )}

        {/* Empty state */}
        {!loading && !error && filtered.length === 0 && (
          <div className="flex flex-col items-center justify-center py-24 text-center">
            <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-10 w-10 text-[var(--text-faint)] mb-4">
              <path strokeLinecap="round" strokeLinejoin="round" d="M6.75 3v2.25M17.25 3v2.25M3 18.75V7.5a2.25 2.25 0 0 1 2.25-2.25h13.5A2.25 2.25 0 0 1 21 7.5v11.25m-18 0A2.25 2.25 0 0 0 5.25 21h13.5A2.25 2.25 0 0 0 21 18.75m-18 0v-7.5A2.25 2.25 0 0 1 5.25 9h13.5A2.25 2.25 0 0 1 21 11.25v7.5m-9-6h.008v.008H12v-.008ZM12 15h.008v.008H12V15Zm0 2.25h.008v.008H12v-.008ZM9.75 15h.008v.008H9.75V15Zm0 2.25h.008v.008H9.75v-.008ZM7.5 15h.008v.008H7.5V15Zm0 2.25h.008v.008H7.5v-.008Zm6.75-4.5h.008v.008h-.008v-.008Zm0 2.25h.008v.008h-.008V15Zm0 2.25h.008v.008h-.008v-.008Zm2.25-4.5h.008v.008H16.5v-.008Zm0 2.25h.008v.008H16.5V15Z" />
            </svg>
            <p className="text-sm text-[var(--text-muted)]">
              {filter !== 'all' ? `No ${filter} cron jobs found.` : 'No cron jobs configured yet.'}
            </p>
          </div>
        )}

        {/* Table */}
        {!loading && !error && filtered.length > 0 && (
          <div className="rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] overflow-hidden">
            {/* Table header */}
            <div className="grid grid-cols-[1fr_160px_80px_100px_100px_90px_180px] gap-2 px-4 py-3 border-b border-[var(--border-subtle)] bg-[var(--bg-elevated)]">
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">Name</span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">Schedule</span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">Enabled</span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">Last Run</span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">Next Run</span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">Runs</span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider text-right">Actions</span>
            </div>

            {/* Table rows */}
            {filtered.map((job) => (
              <div
                key={job.id}
                className={`grid grid-cols-[1fr_160px_80px_100px_100px_90px_180px] gap-2 px-4 py-2.5 border-b border-[var(--border-subtle)] last:border-b-0 hover:bg-[var(--bg-hover)] transition-colors items-center ${!job.enabled ? 'opacity-60' : ''}`}
              >
                {/* Name */}
                <div className="flex flex-col gap-0.5">
                  <Link
                    href={`/admin/cron/detail?id=${encodeURIComponent(job.id)}`}
                    className="text-xs font-medium text-[var(--accent-gold)] hover:text-[var(--accent-gold-bright)] truncate transition-colors"
                  >
                    {job.name}
                  </Link>
                  {job.message && (
                    <span className="text-[10px] text-[var(--text-faint)] truncate" title={job.message}>
                      {job.message}
                    </span>
                  )}
                </div>

                {/* Schedule */}
                <span className="text-xs font-mono text-[var(--text-muted)] truncate" title={job.schedule}>
                  {job.schedule}
                </span>

                {/* Enabled toggle */}
                <button
                  onClick={() => handleToggle(job)}
                  disabled={actionLoading === job.id}
                  className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${
                    job.enabled
                      ? 'bg-[var(--accent-emerald)]'
                      : 'bg-[var(--text-faint)]/30'
                  }`}
                  title={job.enabled ? 'Disable' : 'Enable'}
                >
                  <span
                    className={`inline-block h-3.5 w-3.5 rounded-full bg-white shadow-sm transition-transform ${
                      job.enabled ? 'translate-x-4' : 'translate-x-0.5'
                    }`}
                  />
                </button>

                {/* Last run */}
                <span className="text-xs text-[var(--text-muted)]" title={job.last_run_at}>
                  {formatTime(job.last_run_at)}
                </span>

                {/* Next run */}
                <span className="text-xs text-[var(--text-muted)]" title={job.next_run_at}>
                  {job.enabled ? formatTime(job.next_run_at) : '--'}
                </span>

                {/* Runs count / max */}
                <span className="text-xs text-[var(--text-muted)]">
                  {job.runs_count ?? 0}
                  {job.max_runs != null ? <span className="text-[var(--text-faint)]"> / {job.max_runs}</span> : null}
                </span>

                {/* Actions */}
                <div className="flex items-center justify-end gap-1.5">
                  {/* Trigger */}
                  <button
                    onClick={() => handleTrigger(job.id, job.name)}
                    disabled={actionLoading === job.id || !job.enabled}
                    className="inline-flex items-center gap-1 px-2 py-1 rounded-[var(--radius-sm)] text-[10px] font-bold uppercase tracking-wider text-[var(--accent-gold)] bg-[var(--accent-gold)]/10 hover:bg-[var(--accent-gold)]/20 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
                    title="Trigger manually"
                  >
                    {actionLoading === job.id ? (
                      <div className="w-3 h-3 border border-current border-t-transparent rounded-full animate-spin" />
                    ) : (
                      <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-3 w-3">
                        <path strokeLinecap="round" strokeLinejoin="round" d="M5.636 5.636a9 9 0 1 0 12.728 0M12 3v9" />
                      </svg>
                    )}
                    Run
                  </button>

                  {/* Delete */}
                  <button
                    onClick={() => handleDelete(job.id, job.name)}
                    disabled={actionLoading === job.id}
                    className="inline-flex items-center gap-1 px-2 py-1 rounded-[var(--radius-sm)] text-[10px] font-bold uppercase tracking-wider text-[var(--accent-coral)] bg-[rgba(244,63,94,0.08)] hover:bg-[rgba(244,63,94,0.15)] transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
                    title="Delete job"
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-3 w-3">
                      <path strokeLinecap="round" strokeLinejoin="round" d="m14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0" />
                    </svg>
                    Delete
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

'use client';

import { useState } from 'react';
import Link from 'next/link';
import { listCronJobs, updateCronJob, deleteCronJob, triggerCronJob } from '@/lib/api/admin-cron';
import { useAdminUI } from '@/context/admin-ui-context';
import { formatDuration } from '@/lib/utils/format-duration';
import { formatRelative as formatTime } from '@/lib/utils/format-time';
import type { CronJob } from '@/lib/types/admin';
import { useResource } from '@/hooks/use-resource';
import { LoadingState, ErrorState, EmptyState } from '@/components/admin/resource-states';
import { useTranslation } from 'react-i18next';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type FilterOption = 'all' | 'enabled' | 'disabled';

// ---------------------------------------------------------------------------
// Page Component
// ---------------------------------------------------------------------------

export default function CronPage() {
  const { t } = useTranslation();
  const { showToast, confirm } = useAdminUI();
  const { data: jobs, loading, error, reload } = useResource<CronJob[]>(
    () => listCronJobs(),
    [],
  );
  const jobList = jobs ?? [];
  const [filter, setFilter] = useState<FilterOption>('all');
  const [actionLoading, setActionLoading] = useState<string | null>(null);

  function formatSchedule(s: CronJob['schedule']): string {
    if (!s) return '—';
    switch (s.kind) {
      case 'cron': return s.expr ?? '—';
      case 'every': return s.every_ms ? t('admin:cron.schedule.every', { duration: formatDuration(s.every_ms), defaultValue: `every ${formatDuration(s.every_ms)}` }) : '—';
      case 'at': return s.at ?? '—';
      default: return s.kind ?? '—';
    }
  }

  // ---------------------------------------------------------------------------
  // Derived data
  // ---------------------------------------------------------------------------

  const filtered = jobList.filter((j) => {
    if (filter === 'all') return true;
    if (filter === 'enabled') return j.enabled;
    return !j.enabled;
  });

  const enabledCount = jobList.filter((j) => j.enabled).length;

  // ---------------------------------------------------------------------------
  // Actions
  // ---------------------------------------------------------------------------

  const handleToggle = async (job: CronJob) => {
    const next = !job.enabled;
    const label = next ? t('admin:cron.action.enable_verb', { defaultValue: 'enable' }) : t('admin:cron.action.disable_verb', { defaultValue: 'disable' });
    const confirmed = await confirm(
      next ? t('admin:cron.confirm.enable_title', { defaultValue: 'Enable Cron Job?' }) : t('admin:cron.confirm.disable_title', { defaultValue: 'Disable Cron Job?' }),
      next 
        ? t('admin:cron.confirm.enable_body', { name: job.name, defaultValue: `Are you sure you want to enable cron job "${job.name}"?` })
        : t('admin:cron.confirm.disable_body', { name: job.name, defaultValue: `Are you sure you want to disable cron job "${job.name}"?` })
    );
    if (!confirmed) return;
    try {
      setActionLoading(job.id);
      await updateCronJob(job.id, { enabled: next });
      await reload();
      showToast(
        next 
          ? t('admin:cron.toast.enabled', { name: job.name, defaultValue: `Cron job "${job.name}" enabled successfully.` })
          : t('admin:cron.toast.disabled', { name: job.name, defaultValue: `Cron job "${job.name}" disabled successfully.` }),
        'success'
      );
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('admin:cron.error.toggle_failed', { label, defaultValue: `Failed to ${label} cron job` }), 'error');
    } finally {
      setActionLoading(null);
    }
  };

  const handleTrigger = async (id: string, name: string) => {
    const confirmed = await confirm(
      t('admin:cron.confirm.trigger_title', { defaultValue: 'Trigger Cron Job?' }),
      t('admin:cron.confirm.trigger_body', { name, defaultValue: `Manually execute cron job "${name}" right now?` })
    );
    if (!confirmed) return;
    try {
      setActionLoading(id);
      await triggerCronJob(id);
      showToast(t('admin:cron.toast.triggered', { name, defaultValue: `Cron job "${name}" manually triggered.` }), 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('admin:cron.error.trigger_failed', { defaultValue: 'Failed to trigger cron job' }), 'error');
    } finally {
      setActionLoading(null);
    }
  };

  const handleDelete = async (id: string, name: string) => {
    const confirmed = await confirm(
      t('admin:cron.confirm.delete_title', { defaultValue: 'Delete Cron Job?' }),
      t('admin:cron.confirm.delete_body', { name, defaultValue: `Are you sure you want to permanently delete cron job "${name}"? This action is irreversible.` }),
      { destructive: true }
    );
    if (!confirmed) return;
    try {
      setActionLoading(id);
      await deleteCronJob(id);
      await reload();
      showToast(t('admin:cron.toast.deleted', { name, defaultValue: `Cron job "${name}" successfully deleted.` }), 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('admin:cron.error.delete_failed', { defaultValue: 'Failed to delete cron job' }), 'error');
    } finally {
      setActionLoading(null);
    }
  };

  return (
    <div className="min-h-screen bg-[var(--bg-base)] px-6 py-8">
      <div className="max-w-6xl mx-auto">
        {/* Header */}
        <div className="flex items-center justify-between mb-8">
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-display font-bold text-[var(--text-primary)]">
              {t('admin:cron.title', { defaultValue: 'Cron Jobs' })}
            </h1>
            {!loading && !error && (
              <span className="text-[11px] font-mono text-[var(--text-faint)] px-2 py-0.5 rounded-full bg-[var(--bg-hover)]">
                {t('admin:cron.job_count', { enabled: enabledCount, total: jobList.length, defaultValue: `${enabledCount} enabled / ${jobList.length} total` })}
              </span>
            )}
          </div>

          {/* Controls */}
          <div className="flex items-center gap-3">
            {/* Filter */}
            <select
              value={filter}
              onChange={(e) => setFilter(e.target.value as FilterOption)}
              className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-2.5 py-1.5 text-xs text-[var(--text-primary)] outline-none transition-colors focus:border-[var(--accent-gold)]/40 cursor-pointer"
            >
              <option value="all">{t('admin:cron.filter.all', { defaultValue: 'All Jobs' })}</option>
              <option value="enabled">{t('admin:cron.filter.enabled', { defaultValue: 'Enabled' })}</option>
              <option value="disabled">{t('admin:cron.filter.disabled', { defaultValue: 'Disabled' })}</option>
            </select>

            {/* Refresh */}
            <button
              type="button"
              onClick={reload}
              disabled={loading}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] text-[11px] font-bold uppercase tracking-wider text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] transition-colors disabled:opacity-40"
            >
              <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-3.5 w-3.5">
                <path strokeLinecap="round" strokeLinejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.992 0 3.181 3.183a8.25 8.25 0 0 0 13.803-3.7M4.031 9.865a8.25 8.25 0 0 1 13.803-3.7l3.181 3.182" />
              </svg>
              {t('common:action.refresh', { defaultValue: 'Refresh' })}
            </button>
          </div>
        </div>

        {/* Loading */}
        {loading && <LoadingState label={t('admin:cron.loading', { defaultValue: 'Loading cron jobs...' })} />}

        {/* Error */}
        {error && <ErrorState message={error} onRetry={reload} />}

        {/* Empty state */}
        {!loading && !error && filtered.length === 0 && (
          <EmptyState
            title={filter !== 'all' 
              ? t('admin:cron.empty.no_match', { filter, defaultValue: `No ${filter} cron jobs found.` }) 
              : t('admin:cron.empty.no_jobs', { defaultValue: 'No cron jobs configured yet.' })}
          />
        )}

        {/* Table */}
        {!loading && !error && filtered.length > 0 && (
          <div className="rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] overflow-hidden">
            {/* Table header */}
            <div className="grid grid-cols-[minmax(0,1fr)_160px_80px_100px_100px_90px_180px] gap-2 px-4 py-3 border-b border-[var(--border-subtle)] bg-[var(--bg-elevated)]">
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">{t('admin:cron.table.name', { defaultValue: 'Name' })}</span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">{t('admin:cron.table.schedule', { defaultValue: 'Schedule' })}</span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">{t('admin:cron.table.enabled', { defaultValue: 'Enabled' })}</span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">{t('admin:cron.table.last_run', { defaultValue: 'Last Run' })}</span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">{t('admin:cron.table.next_run', { defaultValue: 'Next Run' })}</span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">{t('admin:cron.table.runs', { defaultValue: 'Runs' })}</span>
              <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider text-right">{t('admin:cron.table.actions', { defaultValue: 'Actions' })}</span>
            </div>

            {/* Table rows */}
            {filtered.map((job) => (
              <div
                key={job.id}
                className={`grid grid-cols-[minmax(0,1fr)_160px_80px_100px_100px_90px_180px] gap-2 px-4 py-2.5 border-b border-[var(--border-subtle)] last:border-b-0 hover:bg-[var(--bg-hover)] transition-colors items-center ${!job.enabled ? 'opacity-60' : ''}`}
              >
                {/* Name */}
                <div className="flex flex-col gap-0.5 min-w-0 overflow-hidden">
                  <Link
                    href={`/admin/cron/detail?id=${encodeURIComponent(job.id)}`}
                    className="text-xs font-medium text-[var(--accent-gold)] hover:text-[var(--accent-gold-bright)] truncate transition-colors"
                  >
                    {job.name}
                  </Link>
                  {job.payload?.message && (
                    <span className="text-[10px] text-[var(--text-faint)] truncate" title={job.payload.message}>
                      {job.payload.message}
                    </span>
                  )}
                </div>

                {/* Schedule */}
                <span className="text-xs font-mono text-[var(--text-muted)] truncate" title={formatSchedule(job.schedule)}>
                  {formatSchedule(job.schedule)}
                </span>

                {/* Enabled toggle */}
                <button
                  type="button"
                  onClick={() => handleToggle(job)}
                  disabled={actionLoading === job.id}
                  className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${
                    job.enabled
                      ? 'bg-[var(--accent-emerald)]'
                      : 'bg-[var(--text-faint)]/30'
                  }`}
                  title={job.enabled ? t('admin:cron.action.disable_verb', { defaultValue: 'Disable' }) : t('admin:cron.action.enable_verb', { defaultValue: 'Enable' })}
                >
                  <span
                    className={`inline-block h-3.5 w-3.5 rounded-full bg-white shadow-[var(--shadow-sm)] transition-transform ${
                      job.enabled ? 'translate-x-4' : 'translate-x-0.5'
                    }`}
                  />
                </button>

                {/* Last run */}
                <span className="text-xs text-[var(--text-muted)]" title={job.state?.last_run_at_ms ? new Date(job.state.last_run_at_ms).toISOString() : undefined}>
                  {formatTime(job.state?.last_run_at_ms)}
                </span>

                {/* Next run */}
                <span className="text-xs text-[var(--text-muted)]" title={job.state?.next_run_at_ms ? new Date(job.state.next_run_at_ms).toISOString() : undefined}>
                  {job.enabled ? formatTime(job.state?.next_run_at_ms) : '--'}
                </span>

                {/* Runs count / max */}
                <span className="text-xs text-[var(--text-muted)]">
                  {job.state?.run_count ?? 0}
                  {job.max_runs != null ? <span className="text-[var(--text-faint)]"> / {job.max_runs}</span> : null}
                </span>

                {/* Actions */}
                <div className="flex items-center justify-end gap-1.5">
                  {/* Trigger */}
                  <button
                    type="button"
                    onClick={() => handleTrigger(job.id, job.name)}
                    disabled={actionLoading === job.id || !job.enabled}
                    className="inline-flex items-center gap-1 px-2 py-1 rounded-[var(--radius-sm)] text-[10px] font-bold uppercase tracking-wider text-[var(--accent-gold)] bg-[var(--accent-gold)]/10 hover:bg-[var(--accent-gold)]/20 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
                    title={t('admin:cron.action.trigger', { defaultValue: 'Trigger manually' })}
                  >
                    {actionLoading === job.id ? (
                      <div className="w-3 h-3 border border-current border-t-transparent rounded-full animate-spin" />
                    ) : (
                      <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-3 w-3">
                        <path strokeLinecap="round" strokeLinejoin="round" d="M5.636 5.636a9 9 0 1 0 12.728 0M12 3v9" />
                      </svg>
                    )}
                    {t('admin:cron.action.run', { defaultValue: 'Run' })}
                  </button>

                  {/* Delete */}
                  <button
                    type="button"
                    onClick={() => handleDelete(job.id, job.name)}
                    disabled={actionLoading === job.id}
                    className="inline-flex items-center gap-1 px-2 py-1 rounded-[var(--radius-sm)] text-[10px] font-bold uppercase tracking-wider text-[var(--accent-coral)] bg-[rgba(244,63,94,0.08)] hover:bg-[rgba(244,63,94,0.15)] transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
                    title={t('common:action.delete', { defaultValue: 'Delete job' })}
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-3 w-3">
                      <path strokeLinecap="round" strokeLinejoin="round" d="m14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0" />
                    </svg>
                    {t('common:action.delete', { defaultValue: 'Delete' })}
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

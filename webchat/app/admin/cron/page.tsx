'use client';

import { useState, useMemo } from 'react';
import Link from 'next/link';
import { listCronJobs, updateCronJob, deleteCronJob, triggerCronJob } from '@/lib/api/admin-cron';
import { useAdminUI } from '@/context/admin-ui-context';
import { formatDuration } from '@/lib/utils/format-duration';
import { formatRelative as formatTime } from '@/lib/utils/format-time';
import type { CronJob } from '@/lib/types/admin';
import { useResource } from '@/hooks/use-resource';
import { LoadingState, ErrorState, EmptyState } from '@/components/admin/resource-states';
import { CreateCronModal } from '@/components/admin/create-cron-modal';
import { useTranslation } from 'react-i18next';

type FilterOption = 'all' | 'enabled' | 'disabled';

export default function CronPage() {
  const { t } = useTranslation();
  const { showToast, confirm } = useAdminUI();
  const { data: jobs, loading, error, reload } = useResource<CronJob[]>(
    () => listCronJobs(),
    [],
  );
  const jobList = jobs ?? [];

  const [query, setQuery] = useState('');
  const [filter, setFilter] = useState<FilterOption>('all');
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [isCreateOpen, setIsCreateOpen] = useState(false);

  function formatSchedule(s: CronJob['schedule']): string {
    if (!s) return '—';
    switch (s.kind) {
      case 'cron': return s.expr ?? '—';
      case 'every': return s.every_ms ? t('admin:cron.schedule.every', { duration: formatDuration(s.every_ms), defaultValue: `every ${formatDuration(s.every_ms)}` }) : '—';
      case 'at': return s.at ?? '—';
      default: return s.kind ?? '—';
    }
  }

  const filtered = useMemo(() => {
    return jobList.filter((j) => {
      if (filter === 'enabled' && !j.enabled) return false;
      if (filter === 'disabled' && j.enabled) return false;

      if (!query.trim()) return true;
      const q = query.toLowerCase();
      return (
        j.name.toLowerCase().includes(q) ||
        (j.id && j.id.toLowerCase().includes(q)) ||
        (j.payload?.message && j.payload.message.toLowerCase().includes(q)) ||
        (j.bot_id && j.bot_id.toLowerCase().includes(q))
      );
    });
  }, [jobList, filter, query]);

  const enabledCount = jobList.filter((j) => j.enabled).length;
  const disabledCount = jobList.length - enabledCount;

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
      await reload();
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
      <div className="max-w-6xl mx-auto space-y-6">
        
        {/* Header */}
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
          <div>
            <div className="flex items-center gap-3">
              <h1 className="text-2xl font-display font-bold text-[var(--text-primary)]">
                {t('admin:cron.title', { defaultValue: 'Cron Scheduled Jobs' })}
              </h1>
              {!loading && !error && (
                <div className="flex items-center gap-2">
                  <span className="text-xs font-mono font-bold px-2.5 py-0.5 rounded-full bg-[var(--bg-hover)] text-[var(--text-secondary)] border border-[var(--border-subtle)]">
                    {jobList.length} Total
                  </span>
                  {enabledCount > 0 && (
                    <span className="text-xs font-mono font-bold px-2.5 py-0.5 rounded-full bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                      {enabledCount} active
                    </span>
                  )}
                  {disabledCount > 0 && (
                    <span className="text-xs font-mono font-bold px-2.5 py-0.5 rounded-full bg-amber-500/10 text-amber-400 border border-amber-500/20">
                      {disabledCount} disabled
                    </span>
                  )}
                </div>
              )}
            </div>
            <p className="mt-1 text-xs text-[var(--text-muted)]">
              {t('admin:cron.subtitle', { defaultValue: 'Schedule recurring agent tasks, automated reminders, and cron triggers' })}
            </p>
          </div>

          <div className="flex items-center gap-2.5">
            <button
              type="button"
              onClick={reload}
              disabled={loading}
              className="p-2 rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)] transition-all disabled:opacity-50"
              title={t('common:action.refresh', { defaultValue: 'Refresh' })}
            >
              <svg
                width="16"
                height="16"
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

            <button
              type="button"
              onClick={() => setIsCreateOpen(true)}
              className="inline-flex items-center gap-2 px-4 py-2 rounded-[var(--radius-md)] text-xs font-bold bg-[var(--accent-gold)] text-[var(--bg-base)] transition-all hover:bg-[var(--accent-gold)]/90 shadow-sm"
            >
              <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={2.5} stroke="currentColor" className="w-4 h-4">
                <path strokeLinecap="round" strokeLinejoin="round" d="M12 4.5v15m7.5-7.5h-15" />
              </svg>
              {t('admin:cron.action.create_job', { defaultValue: '+ Create Cron Job' })}
            </button>
          </div>
        </div>

        {/* Controls Bar: Search & Filter */}
        {!loading && !error && jobList.length > 0 && (
          <div className="flex flex-col sm:flex-row items-center justify-between gap-3">
            <div className="relative flex-1 w-full">
              <svg
                className="absolute left-3.5 top-1/2 -translate-y-1/2 text-[var(--text-faint)]"
                width="15"
                height="15"
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
                placeholder={t('admin:cron.placeholder.search', { defaultValue: 'Search cron jobs by name, ID, or prompt message...' })}
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                className="w-full pl-10 pr-10 py-2.5 rounded-[var(--radius-md)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] text-xs text-[var(--text-primary)] placeholder:text-[var(--text-faint)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors shadow-sm"
              />
              {query && (
                <button
                  type="button"
                  onClick={() => setQuery('')}
                  className="absolute right-3.5 top-1/2 -translate-y-1/2 text-[var(--text-faint)] hover:text-[var(--text-primary)] p-1"
                >
                  <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
                    <line x1="1" y1="1" x2="13" y2="13" />
                    <line x1="13" y1="1" x2="1" y2="13" />
                  </svg>
                </button>
              )}
            </div>

            <div className="flex items-center gap-2 shrink-0 w-full sm:w-auto">
              <select
                value={filter}
                onChange={(e) => setFilter(e.target.value as FilterOption)}
                className="w-full sm:w-auto rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-3 py-2.5 text-xs font-medium text-[var(--text-primary)] outline-none focus:border-[var(--accent-gold)] transition-colors cursor-pointer shadow-sm"
              >
                <option value="all">{t('admin:cron.filter.all', { defaultValue: 'All Jobs' })}</option>
                <option value="enabled">{t('admin:cron.filter.enabled', { defaultValue: 'Enabled Jobs Only' })}</option>
                <option value="disabled">{t('admin:cron.filter.disabled', { defaultValue: 'Disabled Jobs Only' })}</option>
              </select>
            </div>
          </div>
        )}

        {/* Loading State */}
        {loading && <LoadingState label={t('admin:cron.loading', { defaultValue: 'Loading cron jobs...' })} />}

        {/* Error State */}
        {error && <ErrorState message={error} onRetry={reload} />}

        {/* Empty State */}
        {!loading && !error && jobList.length === 0 && (
          <EmptyState
            title={t('admin:cron.empty.no_jobs', { defaultValue: 'No scheduled cron jobs configured yet' })}
            description={t('admin:cron.empty.no_jobs_desc', { defaultValue: 'Create a scheduled cron job to run automated tasks or AI workflows.' })}
            action={
              <button
                type="button"
                onClick={() => setIsCreateOpen(true)}
                className="inline-flex items-center gap-2 px-4 py-2 rounded-[var(--radius-md)] text-xs font-bold bg-[var(--accent-gold)] text-[var(--bg-base)] transition-all hover:bg-[var(--accent-gold)]/90"
              >
                {t('admin:cron.action.create_job', { defaultValue: '+ Create Cron Job' })}
              </button>
            }
          />
        )}

        {/* Search No Results */}
        {!loading && !error && jobList.length > 0 && filtered.length === 0 && (
          <div className="flex flex-col items-center justify-center py-16 text-center bg-[var(--bg-surface)] border border-[var(--border-subtle)] rounded-[var(--radius-lg)]">
            <p className="text-xs text-[var(--text-muted)] mb-2">
              {t('admin:cron.empty.no_match', { filter, defaultValue: `No cron jobs matching criteria.` })}
            </p>
            <button
              type="button"
              onClick={() => { setQuery(''); setFilter('all'); }}
              className="text-xs font-bold text-[var(--accent-gold)] hover:underline"
            >
              {t('admin:cron.action.clear_search', { defaultValue: 'Clear search filters' })}
            </button>
          </div>
        )}

        {/* Jobs Table */}
        {!loading && !error && filtered.length > 0 && (
          <div className="rounded-[var(--radius-lg)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] overflow-hidden shadow-sm">
            {/* Table Header */}
            <div className="grid grid-cols-[minmax(0,1.5fr)_140px_80px_110px_110px_80px_200px] gap-3 px-5 py-3 border-b border-[var(--border-subtle)] bg-[var(--bg-elevated)] text-xs font-semibold text-[var(--text-secondary)]">
              <span>{t('admin:cron.table.name', { defaultValue: 'Job Name & Task' })}</span>
              <span>{t('admin:cron.table.schedule', { defaultValue: 'Schedule' })}</span>
              <span>{t('admin:cron.table.enabled', { defaultValue: 'Status' })}</span>
              <span>{t('admin:cron.table.last_run', { defaultValue: 'Last Run' })}</span>
              <span>{t('admin:cron.table.next_run', { defaultValue: 'Next Run' })}</span>
              <span>{t('admin:cron.table.runs', { defaultValue: 'Run Count' })}</span>
              <span className="text-right">{t('admin:cron.table.actions', { defaultValue: 'Actions' })}</span>
            </div>

            {/* Table Rows */}
            {filtered.map((job) => {
              const isRecentlyRun = actionLoading === job.id || Boolean(job.state?.last_run_at_ms && Date.now() - job.state.last_run_at_ms < 15000);
              return (
                <div
                  key={job.id}
                  className={`grid grid-cols-[minmax(0,1.5fr)_140px_80px_110px_110px_80px_200px] gap-3 px-5 py-3.5 border-b border-[var(--border-subtle)] last:border-b-0 hover:bg-[var(--bg-hover)] transition-colors items-center ${!job.enabled ? 'opacity-65' : ''}`}
                >
                  {/* Name & Payload Message */}
                  <div className="flex flex-col gap-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <Link
                        href={`/admin/cron/detail?id=${encodeURIComponent(job.id)}`}
                        className="text-xs font-bold text-[var(--accent-gold)] hover:text-[var(--accent-gold-bright)] truncate transition-colors"
                      >
                        {job.name}
                      </Link>
                      {isRecentlyRun && (
                        <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-bold bg-amber-500/15 text-amber-400 border border-amber-500/30 animate-pulse">
                          🔄 Executing
                        </span>
                      )}
                    </div>
                    {job.payload?.message && (
                      <span className="text-[11px] text-[var(--text-muted)] truncate" title={job.payload.message}>
                        {job.payload.message}
                      </span>
                    )}
                  </div>

                  {/* Schedule Badge */}
                  <div>
                    <span className="inline-flex items-center px-2.5 py-1 rounded-[var(--radius-sm)] text-xs font-mono font-medium bg-[var(--bg-elevated)] text-[var(--text-primary)] border border-[var(--border-subtle)] truncate max-w-full" title={formatSchedule(job.schedule)}>
                      {formatSchedule(job.schedule)}
                    </span>
                  </div>

                  {/* Enabled Toggle Switch */}
                  <div>
                    <button
                      type="button"
                      onClick={() => handleToggle(job)}
                      disabled={actionLoading === job.id}
                      className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${
                        job.enabled
                          ? 'bg-emerald-500'
                          : 'bg-[var(--text-faint)]/40'
                      }`}
                      title={job.enabled ? t('admin:cron.action.disable_verb', { defaultValue: 'Disable' }) : t('admin:cron.action.enable_verb', { defaultValue: 'Enable' })}
                    >
                      <span
                        className={`inline-block h-3.5 w-3.5 rounded-full bg-white shadow-sm transition-transform ${
                          job.enabled ? 'translate-x-4' : 'translate-x-0.5'
                        }`}
                      />
                    </button>
                  </div>

                  {/* Last Run */}
                  <span className="text-xs text-[var(--text-muted)] font-mono" title={job.state?.last_run_at_ms ? new Date(job.state.last_run_at_ms).toISOString() : undefined}>
                    {formatTime(job.state?.last_run_at_ms)}
                  </span>

                  {/* Next Run */}
                  <span className="text-xs text-[var(--text-muted)] font-mono" title={job.state?.next_run_at_ms ? new Date(job.state.next_run_at_ms).toISOString() : undefined}>
                    {job.enabled ? formatTime(job.state?.next_run_at_ms) : '—'}
                  </span>

                  {/* Runs count */}
                  <span className="text-xs font-mono text-[var(--text-primary)]">
                    {job.state?.run_count ?? 0}
                    {job.max_runs != null ? <span className="text-[var(--text-faint)]"> / {job.max_runs}</span> : null}
                  </span>

                  {/* Actions */}
                  <div className="flex items-center justify-end gap-1.5">
                    <Link
                      href={`/admin/cron/detail?id=${encodeURIComponent(job.id)}`}
                      className="inline-flex items-center gap-1 px-2 py-1 rounded-[var(--radius-sm)] text-xs font-bold text-[var(--text-secondary)] bg-[var(--bg-elevated)] hover:text-[var(--accent-gold)] hover:bg-[var(--bg-hover)] border border-[var(--border-subtle)] transition-colors"
                      title={t('admin:cron.action.edit_detail', { defaultValue: 'Edit & View History' })}
                    >
                      ⚙️ Edit
                    </Link>

                    <button
                      type="button"
                      onClick={() => handleTrigger(job.id, job.name)}
                      disabled={actionLoading === job.id || !job.enabled}
                      className="inline-flex items-center gap-1 px-2.5 py-1 rounded-[var(--radius-sm)] text-xs font-bold text-[var(--accent-gold)] bg-[var(--accent-gold)]/10 hover:bg-[var(--accent-gold)]/20 transition-colors disabled:opacity-40"
                      title={t('admin:cron.action.trigger', { defaultValue: 'Trigger manually' })}
                    >
                      {actionLoading === job.id ? (
                        <div className="w-3 h-3 border border-current border-t-transparent rounded-full animate-spin" />
                      ) : (
                        '▶ Run'
                      )}
                    </button>

                    <button
                      type="button"
                      onClick={() => handleDelete(job.id, job.name)}
                      disabled={actionLoading === job.id}
                      className="inline-flex items-center gap-1 px-2 py-1 rounded-[var(--radius-sm)] text-xs font-bold text-rose-400 bg-rose-500/10 hover:bg-rose-500/20 transition-colors disabled:opacity-40"
                      title={t('common:action.delete', { defaultValue: 'Delete job' })}
                    >
                      ✕
                    </button>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Creation Modal */}
      <CreateCronModal
        isOpen={isCreateOpen}
        onClose={() => setIsCreateOpen(false)}
        onSuccess={() => reload()}
      />
    </div>
  );
}

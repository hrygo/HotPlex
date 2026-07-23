'use client';

import { useCallback, useEffect, useState } from 'react';
import { useSearchParams, useRouter } from 'next/navigation';
import Link from 'next/link';
import { listCronJobs, updateCronJob, deleteCronJob, triggerCronJob, getCronRunHistory } from '@/lib/api/admin-cron';
import { useAdminUI } from '@/context/admin-ui-context';
import { formatDuration } from '@/lib/utils/format-duration';
import { formatDateTime, formatRelative } from '@/lib/utils/format-time';
import type { CronJob, CronSchedule, CronRunHistoryItem } from '@/lib/types/admin';
import { useTranslation } from 'react-i18next';

function formatScheduleStr(s?: CronJob['schedule']): string {
  if (!s) return '';
  switch (s.kind) {
    case 'cron': return `cron:${s.expr ?? ''}`;
    case 'every': return `every:${formatDuration(s.every_ms ?? 0)}`;
    case 'at': return `at:${s.at ?? ''}`;
    default: return '';
  }
}

function parseDurationToMs(dur: string): number {
  let ms = 0;
  const re = /(\d+)(d|h|m|s)/g;
  let m;
  while ((m = re.exec(dur)) !== null) {
    const n = parseInt(m[1], 10);
    switch (m[2]) {
      case 'd': ms += n * 86400000; break;
      case 'h': ms += n * 3600000; break;
      case 'm': ms += n * 60000; break;
      case 's': ms += n * 1000; break;
    }
  }
  return ms;
}

function parseScheduleStr(s: string): CronSchedule | null {
  const t = s.trim();
  if (t.startsWith('cron:')) return { kind: 'cron', expr: t.slice(5).trim() };
  if (t.startsWith('every:')) {
    const ms = parseDurationToMs(t.slice(6));
    return ms > 0 ? { kind: 'every', every_ms: ms } : null;
  }
  if (t.startsWith('at:')) return { kind: 'at', at: t.slice(3).trim() };
  return null;
}

function InfoRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="px-4 py-3 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-subtle)]">
      <p className="text-xs font-semibold text-[var(--text-secondary)] mb-1">
        {label}
      </p>
      <p className={`text-sm text-[var(--text-primary)] ${mono ? 'font-mono' : ''} break-all font-medium`}>
        {value || '—'}
      </p>
    </div>
  );
}

export default function CronDetailPage() {
  const { t } = useTranslation();
  const { showToast, confirm } = useAdminUI();
  const searchParams = useSearchParams();
  const router = useRouter();
  const id = searchParams.get('id') ?? '';

  const [job, setJob] = useState<CronJob | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [notFound, setNotFound] = useState(false);

  // Execution History state
  const [history, setHistory] = useState<CronRunHistoryItem[]>([]);
  const [loadingHistory, setLoadingHistory] = useState(false);

  // Editable fields
  const [schedule, setSchedule] = useState('');
  const [message, setMessage] = useState('');
  const [maxRuns, setMaxRuns] = useState<string>('');
  const [enabled, setEnabled] = useState(true);

  // Action states
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [triggering, setTriggering] = useState(false);
  const [hasChanges, setHasChanges] = useState(false);

  const loadJob = useCallback(async () => {
    if (!id) {
      setNotFound(true);
      setLoading(false);
      return;
    }
    try {
      setLoading(true);
      setError(null);
      setNotFound(false);
      const data = await listCronJobs();
      const found = data.find((j) => j.id === id);
      if (!found) {
        setNotFound(true);
      } else {
        setJob(found);
        setSchedule(formatScheduleStr(found.schedule));
        setMessage(found.payload?.message ?? '');
        setMaxRuns(found.max_runs != null ? String(found.max_runs) : '');
        setEnabled(found.enabled);
        setHasChanges(false);

        // Fetch execution history
        setLoadingHistory(true);
        getCronRunHistory(found.id)
          .then((runs) => setHistory(Array.isArray(runs) ? runs : []))
          .catch(() => setHistory([]))
          .finally(() => setLoadingHistory(false));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t('admin:cron.detail.error_load', { defaultValue: 'Failed to load cron job' }));
    } finally {
      setLoading(false);
    }
  }, [id, t]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- mount-time fetch
    loadJob();
  }, [loadJob]);

  // Track changes
  useEffect(() => {
    if (!job) return;
    const changed =
      schedule !== formatScheduleStr(job.schedule) ||
      message !== (job.payload?.message ?? '') ||
      maxRuns !== (job.max_runs != null ? String(job.max_runs) : '') ||
      enabled !== job.enabled;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- derive dirty flag from form vs loaded job
    setHasChanges(changed);
  }, [schedule, message, maxRuns, enabled, job]);

  const handleSave = async () => {
    if (!job || !hasChanges) return;
    try {
      setSaving(true);
      const updates: Record<string, unknown> = {};
      if (schedule !== formatScheduleStr(job.schedule)) {
        const parsed = parseScheduleStr(schedule);
        if (!parsed) {
          showToast(t('admin:cron.detail.invalid_schedule', { defaultValue: 'Invalid schedule format. Use cron:EXPR, every:DURATION, or at:TIMESTAMP.' }), 'error');
          return;
        }
        updates.schedule = parsed;
      }
      if (message !== (job.payload?.message ?? '')) updates.payload = { ...job.payload, message };
      if (maxRuns !== (job.max_runs != null ? String(job.max_runs) : '')) {
        updates.max_runs = maxRuns ? Number(maxRuns) : undefined;
      }
      if (enabled !== job.enabled) updates.enabled = enabled;
      await updateCronJob(job.id, updates);
      setJob((prev) => {
        if (!prev) return prev;
        const updated = { ...prev };
        if (updates.schedule) updated.schedule = updates.schedule as CronSchedule;
        if (updates.payload) updated.payload = { ...prev.payload, ...(updates.payload as Record<string, unknown>) };
        if (updates.max_runs !== undefined) updated.max_runs = updates.max_runs as number;
        if (updates.enabled !== undefined) updated.enabled = updates.enabled as boolean;
        return updated;
      });
      setHasChanges(false);
      showToast(t('admin:cron.toast.updated', { name: job.name, defaultValue: `Cron job "${job.name}" configuration updated.` }), 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('admin:cron.error.update_failed', { defaultValue: 'Failed to update cron job' }), 'error');
    } finally {
      setSaving(false);
    }
  };

  const handleToggle = async () => {
    if (!job) return;
    const next = !enabled;
    const confirmed = await confirm(
      next ? t('admin:cron.confirm.enable_title', { defaultValue: 'Enable Cron Job?' }) : t('admin:cron.confirm.disable_title', { defaultValue: 'Disable Cron Job?' }),
      next
        ? t('admin:cron.confirm.enable_body', { name: job.name, defaultValue: `Are you sure you want to enable cron job "${job.name}"?` })
        : t('admin:cron.confirm.disable_body', { name: job.name, defaultValue: `Are you sure you want to disable cron job "${job.name}"?` })
    );
    if (!confirmed) return;
    try {
      setSaving(true);
      await updateCronJob(job.id, { enabled: next });
      setJob((prev) => (prev ? { ...prev, enabled: next } : prev));
      setEnabled(next);
      setHasChanges(false);
      showToast(
        next
          ? t('admin:cron.toast.enabled', { name: job.name, defaultValue: `Cron job "${job.name}" enabled successfully.` })
          : t('admin:cron.toast.disabled', { name: job.name, defaultValue: `Cron job "${job.name}" disabled successfully.` }),
        'success'
      );
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('admin:cron.error.toggle_failed', { defaultValue: 'Failed to toggle cron job' }), 'error');
    } finally {
      setSaving(false);
    }
  };

  const handleTrigger = async () => {
    if (!job) return;
    const confirmed = await confirm(
      t('admin:cron.confirm.trigger_title', { defaultValue: 'Trigger Cron Job?' }),
      t('admin:cron.confirm.trigger_body', { name: job.name, defaultValue: `Manually execute cron job "${job.name}" right now?` })
    );
    if (!confirmed) return;
    try {
      setTriggering(true);
      await triggerCronJob(job.id);
      showToast(t('admin:cron.toast.triggered', { name: job.name, defaultValue: `Cron job "${job.name}" manually triggered.` }), 'success');
      loadJob();
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('admin:cron.error.trigger_failed', { defaultValue: 'Failed to trigger cron job' }), 'error');
    } finally {
      setTriggering(false);
    }
  };

  const handleDelete = async () => {
    if (!job) return;
    const confirmed = await confirm(
      t('admin:cron.confirm.delete_title', { defaultValue: 'Delete Cron Job?' }),
      t('admin:cron.confirm.delete_body', { name: job.name, defaultValue: `Are you sure you want to permanently delete cron job "${job.name}"? This cannot be undone.` }),
      { destructive: true }
    );
    if (!confirmed) return;
    try {
      setDeleting(true);
      await deleteCronJob(job.id);
      router.push('/admin/cron');
      showToast(t('admin:cron.toast.deleted', { name: job.name, defaultValue: `Cron job "${job.name}" successfully deleted.` }), 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('admin:cron.error.delete_failed', { defaultValue: 'Failed to delete cron job' }), 'error');
      setDeleting(false);
    }
  };

  const backLink = (
    <Link
      href="/admin/cron"
      className="inline-flex items-center gap-1.5 text-xs font-semibold text-[var(--text-muted)] hover:text-[var(--text-primary)] transition-colors mb-6"
    >
      <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={2} stroke="currentColor" className="h-4 w-4">
        <path strokeLinecap="round" strokeLinejoin="round" d="M10.5 19.5 3 12m0 0 7.5-7.5M3 12h18" />
      </svg>
      {t('admin:cron.action.back_to_cron', { defaultValue: 'Back to Cron Jobs' })}
    </Link>
  );

  if (!id) {
    return (
      <div className="max-w-5xl mx-auto px-6 py-8">
        {backLink}
        <div className="flex items-center justify-center min-h-[60vh]">
          <p className="text-sm text-[var(--text-faint)]">{t('admin:cron.detail.no_id', { defaultValue: 'No cron job ID specified' })}</p>
        </div>
      </div>
    );
  }

  if (loading) {
    return (
      <div className="max-w-5xl mx-auto px-6 py-8">
        {backLink}
        <div className="flex items-center justify-center min-h-[60vh]">
          <div className="flex flex-col items-center gap-3">
            <div className="w-7 h-7 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
            <span className="text-xs text-[var(--text-faint)]">{t('admin:cron.detail.loading', { defaultValue: 'Loading cron job details...' })}</span>
          </div>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="max-w-5xl mx-auto px-6 py-8">
        {backLink}
        <div className="rounded-[var(--radius-md)] bg-rose-500/10 border border-rose-500/20 p-4">
          <div className="flex items-center justify-between">
            <p className="text-sm text-rose-400 font-medium">{error}</p>
            <button
              type="button"
              onClick={loadJob}
              className="text-xs font-bold text-rose-400 underline underline-offset-2 hover:text-rose-300 transition-colors"
            >
              {t('common:action.retry', { defaultValue: 'Retry' })}
            </button>
          </div>
        </div>
      </div>
    );
  }

  if (notFound || !job) {
    return (
      <div className="max-w-5xl mx-auto px-6 py-8">
        {backLink}
        <div className="flex flex-col items-center justify-center min-h-[60vh] text-center">
          <p className="text-sm font-medium text-[var(--text-muted)]">{t('admin:cron.detail.not_found', { defaultValue: 'Cron job not found' })}</p>
          <p className="text-xs text-[var(--text-faint)] mt-1 font-mono">{id}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-5xl mx-auto px-6 py-8 space-y-6">
      {/* Back link */}
      {backLink}

      {/* Header Banner */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 p-6 rounded-[var(--radius-lg)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] shadow-sm">
        <div className="flex items-center gap-3.5">
          <div className="w-12 h-12 rounded-[var(--radius-md)] bg-[var(--accent-gold)]/10 text-[var(--accent-gold)] border border-[var(--accent-gold)]/20 flex items-center justify-center text-xl shrink-0">
            ⏰
          </div>
          <div>
            <div className="flex items-center gap-3">
              <h1 className="text-2xl font-display font-bold text-[var(--text-primary)]">{job.name}</h1>
              <button
                type="button"
                onClick={handleToggle}
                disabled={saving}
                className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${
                  enabled ? 'bg-emerald-500' : 'bg-[var(--text-faint)]/40'
                }`}
                title={enabled ? 'Disable' : 'Enable'}
              >
                <span
                  className={`inline-block h-3.5 w-3.5 rounded-full bg-white shadow-sm transition-transform ${
                    enabled ? 'translate-x-4' : 'translate-x-0.5'
                  }`}
                />
              </button>
            </div>
            <p className="text-xs font-mono text-[var(--text-muted)] mt-1">ID: {job.id}</p>
          </div>
        </div>

        <div className="flex items-center gap-3 shrink-0">
          <button
            type="button"
            onClick={handleTrigger}
            disabled={triggering || !job.enabled}
            className="inline-flex items-center gap-2 px-4 py-2.5 rounded-[var(--radius-md)] text-xs font-bold bg-[var(--accent-gold)] text-[var(--text-contrast)] hover:bg-[var(--accent-gold-bright)] transition-all disabled:opacity-40 shadow-sm"
          >
            {triggering ? (
              <div className="w-3.5 h-3.5 border-2 border-current border-t-transparent rounded-full animate-spin" />
            ) : (
              '▶ Run Now'
            )}
          </button>

          <button
            type="button"
            onClick={handleDelete}
            disabled={deleting}
            className="inline-flex items-center gap-1.5 px-4 py-2.5 rounded-[var(--radius-md)] text-xs font-bold text-rose-400 bg-rose-500/10 border border-rose-500/20 hover:bg-rose-500/20 transition-all disabled:opacity-40"
          >
            {deleting ? (
              <div className="w-3.5 h-3.5 border-2 border-current border-t-transparent rounded-full animate-spin" />
            ) : (
              'Delete'
            )}
          </button>
        </div>
      </div>

      {/* Editable Fields Card */}
      <div className="rounded-[var(--radius-lg)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-6 shadow-sm space-y-5">
        <h2 className="text-xs font-bold text-[var(--accent-gold)] uppercase tracking-wider border-b border-[var(--border-subtle)] pb-2">
          {t('admin:cron.detail.configuration', { defaultValue: 'Cron Job Configuration' })}
        </h2>

        <div className="space-y-4">
          {/* Schedule */}
          <div>
            <label className="block text-xs font-semibold text-[var(--text-secondary)] mb-1.5">
              {t('admin:cron.detail.field_schedule', { defaultValue: 'Schedule Specification (cron:EXPR, every:DURATION, at:TIMESTAMP)' })}
            </label>
            <input
              type="text"
              value={schedule}
              onChange={(e) => setSchedule(e.target.value)}
              className="w-full rounded-[var(--radius-md)] border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3.5 py-2.5 text-sm font-mono text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] transition-colors"
              placeholder="cron:0 9 * * 1-5"
            />
          </div>

          {/* Message */}
          <div>
            <label className="block text-xs font-semibold text-[var(--text-secondary)] mb-1.5">
              {t('admin:cron.detail.field_message', { defaultValue: 'Task Prompt / Message Payload' })}
            </label>
            <textarea
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              rows={4}
              className="w-full rounded-[var(--radius-md)] border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3.5 py-2.5 text-xs text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] transition-colors resize-y"
              placeholder={t('admin:cron.detail.placeholder_message', { defaultValue: 'Task prompt message...' })}
            />
          </div>

          {/* Max Runs */}
          <div>
            <label className="block text-xs font-semibold text-[var(--text-secondary)] mb-1.5">
              {t('admin:cron.detail.field_max_runs', { defaultValue: 'Max Runs (Execution Limit)' })}
            </label>
            <input
              type="number"
              value={maxRuns}
              onChange={(e) => setMaxRuns(e.target.value)}
              min={0}
              className="w-48 rounded-[var(--radius-md)] border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3.5 py-2.5 text-xs text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] transition-colors"
              placeholder={t('admin:cron.detail.placeholder_unlimited', { defaultValue: 'Unlimited' })}
            />
          </div>

          {/* Save Action */}
          {hasChanges && (
            <div className="flex items-center gap-3 pt-3 border-t border-[var(--border-subtle)]">
              <button
                type="button"
                onClick={handleSave}
                disabled={saving}
                className="inline-flex items-center justify-center gap-2 rounded-[var(--radius-md)] bg-[var(--accent-gold)] px-5 py-2.5 text-xs font-bold text-[var(--text-contrast)] transition-all hover:bg-[var(--accent-gold-bright)] disabled:opacity-50 shadow-sm"
              >
                {saving ? (
                  <div className="h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
                ) : (
                  t('admin:cron.detail.save_changes', { defaultValue: 'Save Changes' })
                )}
              </button>

              <button
                type="button"
                onClick={() => {
                  setSchedule(formatScheduleStr(job.schedule));
                  setMessage(job.payload?.message ?? '');
                  setMaxRuns(job.max_runs != null ? String(job.max_runs) : '');
                  setEnabled(job.enabled);
                }}
                className="rounded-[var(--radius-md)] border border-[var(--border-default)] bg-transparent px-4 py-2.5 text-xs font-medium text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] transition-all"
              >
                {t('admin:cron.detail.discard', { defaultValue: 'Discard' })}
              </button>
            </div>
          )}
        </div>
      </div>

      {/* Read-only Details Grid */}
      <div className="space-y-3">
        <h2 className="text-xs font-bold text-[var(--accent-gold)] uppercase tracking-wider">
          {t('admin:cron.detail.details_heading', { defaultValue: 'Job Metadata' })}
        </h2>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
          <InfoRow label="Job ID" value={job.id} mono />
          <InfoRow label={t('admin:cron.detail.owner_id', { defaultValue: 'Owner ID' })} value={job.owner_id ?? ''} mono />
          <InfoRow label={t('admin:cron.detail.bot_id', { defaultValue: 'Bound Bot ID' })} value={job.bot_id ?? ''} mono />
          <InfoRow label={t('admin:cron.detail.expires_at', { defaultValue: 'Expiration Time' })} value={formatDateTime(job.expires_at)} />
          <InfoRow
            label={t('admin:cron.detail.run_count', { defaultValue: 'Run Count / Limit' })}
            value={job.state?.run_count != null ? `${job.state.run_count}${job.max_runs != null ? ` / ${job.max_runs}` : ''}` : '0'}
          />
          <InfoRow label={t('admin:cron.detail.last_run', { defaultValue: 'Last Execution' })} value={formatDateTime(job.state?.last_run_at_ms)} />
        </div>
      </div>

      {/* Run Execution History Section */}
      <div className="rounded-[var(--radius-lg)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-6 shadow-sm space-y-4">
        <div className="flex items-center justify-between border-b border-[var(--border-subtle)] pb-3">
          <div>
            <h2 className="text-xs font-bold text-[var(--accent-gold)] uppercase tracking-wider">
              {t('admin:cron.detail.history_title', { defaultValue: 'Run Execution History Logs' })}
            </h2>
            <p className="text-xs text-[var(--text-muted)] mt-0.5">
              {t('admin:cron.detail.history_subtitle', { defaultValue: 'Execution turn statistics, durations, and error logs' })}
            </p>
          </div>
          <button
            type="button"
            onClick={() => loadJob()}
            disabled={loadingHistory}
            className="text-xs text-[var(--text-muted)] hover:text-[var(--text-primary)] flex items-center gap-1.5"
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" className={loadingHistory ? 'animate-spin' : ''}>
              <path d="M21 2v6h-6" />
              <path d="M3 12a9 9 0 0 1 15-6.7L21 8" />
              <path d="M3 22v-6h6" />
              <path d="M21 12a9 9 0 0 1-15 6.7L3 16" />
            </svg>
            Refresh Logs
          </button>
        </div>

        {loadingHistory ? (
          <div className="py-8 text-center text-xs text-[var(--text-muted)]">
            <div className="w-5 h-5 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin mx-auto mb-2" />
            Loading execution logs...
          </div>
        ) : history.length === 0 ? (
          <div className="py-8 text-center text-xs text-[var(--text-muted)] italic bg-[var(--bg-elevated)] rounded-[var(--radius-md)] border border-[var(--border-subtle)]">
            No execution history records found for this cron job.
          </div>
        ) : (
          <div className="rounded-[var(--radius-md)] border border-[var(--border-subtle)] overflow-hidden">
            <div className="grid grid-cols-[80px_100px_120px_100px_minmax(0,1fr)_160px] gap-3 px-4 py-2.5 bg-[var(--bg-elevated)] text-xs font-semibold text-[var(--text-secondary)] border-b border-[var(--border-subtle)]">
              <span>Turn #</span>
              <span>Status</span>
              <span>Duration</span>
              <span>Tokens</span>
              <span>Error / Details</span>
              <span>Timestamp</span>
            </div>
            {history.map((item, idx) => (
              <div
                key={item.id || idx}
                className="grid grid-cols-[80px_100px_120px_100px_minmax(0,1fr)_160px] gap-3 px-4 py-3 text-xs border-b border-[var(--border-subtle)] last:border-b-0 items-center hover:bg-[var(--bg-hover)] transition-colors"
              >
                <span className="font-mono font-bold text-[var(--text-primary)]">#{item.turn_index ?? item.generation ?? idx + 1}</span>
                <div>
                  <span className={`inline-flex items-center px-2 py-0.5 rounded text-[10px] font-bold uppercase font-mono ${
                    item.status === 'success' || item.status === 'done' || !item.error
                      ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
                      : 'bg-rose-500/10 text-rose-400 border border-rose-500/20'
                  }`}>
                    {item.status || (item.error ? 'failed' : 'success')}
                  </span>
                </div>
                <span className="font-mono text-[var(--text-muted)]">
                  {item.duration_ms ? `${item.duration_ms}ms` : '—'}
                </span>
                <span className="font-mono text-[var(--text-muted)]">
                  {item.tokens_used ? `${item.tokens_used}` : '—'}
                </span>
                <div className="min-w-0">
                  {item.error ? (
                    <span className="text-rose-400 font-mono text-[11px] truncate block" title={item.error}>
                      {item.error}
                    </span>
                  ) : (
                    <span className="text-[var(--text-faint)] italic">—</span>
                  )}
                </div>
                <span className="font-mono text-[var(--text-muted)] text-[11px]">
                  {item.created_at ? formatRelative(item.created_at) : '—'}
                </span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

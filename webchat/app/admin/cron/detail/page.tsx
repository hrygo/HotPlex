'use client';

import { useCallback, useEffect, useState } from 'react';
import { useSearchParams, useRouter } from 'next/navigation';
import Link from 'next/link';
import { listCronJobs, updateCronJob, deleteCronJob, triggerCronJob } from '@/lib/api/admin-cron';
import { useAdminUI } from '@/context/admin-ui-context';
import { formatDuration } from '@/lib/utils/format-duration';
import { formatDateTime } from '@/lib/utils/format-time';
import type { CronJob, CronSchedule } from '@/lib/types/admin';
import { useTranslation } from 'react-i18next';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

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

  // ---------------------------------------------------------------------------
  // Render — shared back link
  // ---------------------------------------------------------------------------

  const backLink = (
    <Link
      href="/admin/cron"
      className="inline-flex items-center gap-1.5 text-xs text-[var(--text-faint)] hover:text-[var(--text-primary)] transition-colors mb-6"
    >
      <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-3.5 w-3.5">
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
            <div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
            <span className="text-xs text-[var(--text-faint)]">{t('admin:cron.detail.loading', { defaultValue: 'Loading cron job...' })}</span>
          </div>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="max-w-5xl mx-auto px-6 py-8">
        {backLink}
        <div className="rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] p-4">
          <div className="flex items-center justify-between">
            <p className="text-sm text-[var(--accent-coral)]">{error}</p>
            <button
              type="button"
              onClick={loadJob}
              className="text-xs font-medium text-[var(--accent-coral)] underline underline-offset-2 hover:text-[var(--accent-coral)]/80 transition-colors"
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
          <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-10 w-10 text-[var(--text-faint)] mb-4">
            <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v3.75m9-.75a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9 3.75h.008v.008H12v-.008Z" />
          </svg>
          <p className="text-sm text-[var(--text-muted)]">{t('admin:cron.detail.not_found', { defaultValue: 'Cron job not found' })}</p>
          <p className="text-xs text-[var(--text-faint)] mt-1 font-mono">{id}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-5xl mx-auto px-6 py-8">
      {/* Back link */}
      {backLink}

      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center gap-3">
          <h1 className="text-xl font-display font-bold text-[var(--text-primary)]">
            {job.name}
          </h1>
          <button
            type="button"
            onClick={handleToggle}
            disabled={saving}
            className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${
              enabled
                ? 'bg-[var(--accent-emerald)]'
                : 'bg-[var(--text-faint)]/30'
            }`}
            title={enabled ? t('admin:cron.action.disable_verb', { defaultValue: 'Disable' }) : t('admin:cron.action.enable_verb', { defaultValue: 'Enable' })}
          >
            <span
              className={`inline-block h-3.5 w-3.5 rounded-full bg-white shadow-[var(--shadow-sm)] transition-transform ${
                enabled ? 'translate-x-4' : 'translate-x-0.5'
              }`}
            />
          </button>
        </div>
        <div className="flex items-center gap-2">
          {/* Trigger */}
          <button
            type="button"
            onClick={handleTrigger}
            disabled={triggering || !job.enabled}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-[var(--radius-sm)] text-[11px] font-bold uppercase tracking-wider text-[var(--accent-gold)] bg-[var(--accent-gold)]/10 hover:bg-[var(--accent-gold)]/20 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {triggering ? (
              <div className="w-3 h-3 border border-current border-t-transparent rounded-full animate-spin" />
            ) : (
              <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-3.5 w-3.5">
                <path strokeLinecap="round" strokeLinejoin="round" d="M5.636 5.636a9 9 0 1 0 12.728 0M12 3v9" />
              </svg>
            )}
            {t('admin:cron.action.trigger', { defaultValue: 'Trigger' })}
          </button>
          {/* Delete */}
          <button
            type="button"
            onClick={handleDelete}
            disabled={deleting}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-[var(--radius-sm)] text-[11px] font-bold uppercase tracking-wider text-[var(--accent-coral)] bg-[rgba(244,63,94,0.08)] hover:bg-[rgba(244,63,94,0.15)] transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {deleting ? (
              <div className="w-3 h-3 border border-current border-t-transparent rounded-full animate-spin" />
            ) : (
              <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-3.5 w-3.5">
                <path strokeLinecap="round" strokeLinejoin="round" d="m14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0" />
              </svg>
            )}
            {t('common:action.delete', { defaultValue: 'Delete' })}
          </button>
        </div>
      </div>

      {/* Editable fields */}
      <div className="rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-5 mb-4">
        <h2 className="text-xs font-bold text-[var(--text-faint)] uppercase tracking-wider mb-4">{t('admin:cron.detail.configuration', { defaultValue: 'Configuration' })}</h2>
        <div className="space-y-4">
          {/* Schedule */}
          <div>
            <label className="block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5">
              {t('admin:cron.detail.field_schedule', { defaultValue: 'Schedule' })}
            </label>
            <input
              type="text"
              value={schedule}
              onChange={(e) => setSchedule(e.target.value)}
              className="w-full rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-base)] px-3 py-2 text-sm font-mono text-[var(--text-primary)] outline-none transition-colors focus:border-[var(--accent-gold)]/40"
              placeholder="cron:0 9 * * 1-5"
            />
          </div>

          {/* Message */}
          <div>
            <label className="block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5">
              {t('admin:cron.detail.field_message', { defaultValue: 'Message' })}
            </label>
            <textarea
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              rows={4}
              className="w-full rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-base)] px-3 py-2 text-sm text-[var(--text-primary)] outline-none transition-colors focus:border-[var(--accent-gold)]/40 resize-y"
              placeholder={t('admin:cron.detail.placeholder_message', { defaultValue: 'Task message...' })}
            />
          </div>

          {/* Max Runs */}
          <div>
            <label className="block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5">
              {t('admin:cron.detail.field_max_runs', { defaultValue: 'Max Runs' })}
            </label>
            <input
              type="number"
              value={maxRuns}
              onChange={(e) => setMaxRuns(e.target.value)}
              min={0}
              className="w-40 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-base)] px-3 py-2 text-sm text-[var(--text-primary)] outline-none transition-colors focus:border-[var(--accent-gold)]/40"
              placeholder={t('admin:cron.detail.placeholder_unlimited', { defaultValue: 'Unlimited' })}
            />
          </div>

          {/* Save */}
          {hasChanges && (
            <div className="flex items-center gap-3 pt-2">
              <button
                type="button"
                onClick={handleSave}
                disabled={saving}
                className="inline-flex items-center gap-1.5 px-4 py-2 rounded-[var(--radius-sm)] text-xs font-bold uppercase tracking-wider bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
              >
                {saving ? (
                  <div className="w-3 h-3 border-2 border-current border-t-transparent rounded-full animate-spin" />
                ) : null}
                {t('admin:cron.detail.save_changes', { defaultValue: 'Save Changes' })}
              </button>
              <button
                type="button"
                onClick={() => {
                  setSchedule(formatScheduleStr(job.schedule));
                  setMessage(job.payload?.message ?? '');
                  setMaxRuns(job.max_runs != null ? String(job.max_runs) : '');
                  setEnabled(job.enabled);
                }}
                className="text-xs text-[var(--text-faint)] hover:text-[var(--text-primary)] transition-colors"
              >
                {t('admin:cron.detail.discard', { defaultValue: 'Discard' })}
              </button>
            </div>
          )}
        </div>
      </div>

      {/* Read-only info cards */}
      <h2 className="text-xs font-bold text-[var(--text-faint)] uppercase tracking-wider mb-3">{t('admin:cron.detail.details_heading', { defaultValue: 'Details' })}</h2>
      <div className="grid grid-cols-2 gap-3">
        <InfoRow label="ID" value={job.id} mono />
        <InfoRow label={t('admin:cron.detail.owner_id', { defaultValue: 'Owner ID' })} value={job.owner_id ?? ''} mono />
        <InfoRow label={t('admin:cron.detail.bot_id', { defaultValue: 'Bot ID' })} value={job.bot_id ?? ''} mono />
        <InfoRow label={t('admin:cron.detail.expires_at', { defaultValue: 'Expires At' })} value={formatDateTime(job.expires_at)} />
        <InfoRow
          label={t('admin:cron.detail.run_count', { defaultValue: 'Run Count' })}
          value={job.state?.run_count != null ? `${job.state.run_count}${job.max_runs != null ? ` / ${job.max_runs}` : ''}` : ''}
        />
        <InfoRow label={t('admin:cron.detail.last_run', { defaultValue: 'Last Run' })} value={formatDateTime(job.state?.last_run_at_ms)} />
        <InfoRow label={t('admin:cron.detail.next_run', { defaultValue: 'Next Run' })} value={job.enabled ? formatDateTime(job.state?.next_run_at_ms) : '—'} />
      </div>
    </div>
  );
}

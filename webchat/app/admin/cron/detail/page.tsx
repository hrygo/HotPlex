'use client';

import { useCallback, useEffect, useState } from 'react';
import { useSearchParams, useRouter } from 'next/navigation';
import Link from 'next/link';
import { listCronJobs, updateCronJob, deleteCronJob, triggerCronJob, getCronRunHistory } from '@/lib/api/admin-cron';
import { listBots } from '@/lib/api/admin-bots';
import { useAdminUI } from '@/context/admin-ui-context';
import { formatDuration } from '@/lib/utils/format-duration';
import { formatDateTime, formatRelative } from '@/lib/utils/format-time';
import type { CronJob, CronSchedule, CronRunHistoryItem, BotConfigEntry } from '@/lib/types/admin';
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

function InfoCard({ label, value, subValue, mono }: { label: string; value: string; subValue?: string; mono?: boolean }) {
  return (
    <div className="p-4 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-subtle)] space-y-1">
      <p className="text-xs font-semibold text-[var(--text-secondary)]">{label}</p>
      <p className={`text-sm font-bold text-[var(--text-primary)] ${mono ? 'font-mono' : ''} break-all`}>
        {value || '—'}
      </p>
      {subValue && <p className="text-[11px] text-[var(--text-muted)] font-mono">{subValue}</p>}
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

  // Bot list for selection
  const [bots, setBots] = useState<BotConfigEntry[]>([]);

  // Execution History state
  const [history, setHistory] = useState<CronRunHistoryItem[]>([]);
  const [loadingHistory, setLoadingHistory] = useState(false);

  // Full Editable fields state
  const [name, setName] = useState('');
  const [schedule, setSchedule] = useState('');
  const [message, setMessage] = useState('');
  const [botId, setBotId] = useState('system');
  const [maxRuns, setMaxRuns] = useState<string>('');
  const [expiresAt, setExpiresAt] = useState<string>('');
  const [enabled, setEnabled] = useState(true);

  // Action states
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [triggering, setTriggering] = useState(false);
  const [hasChanges, setHasChanges] = useState(false);

  // Load configured bots for dropdown
  useEffect(() => {
    listBots()
      .then((data) => setBots(Array.isArray(data) ? data : []))
      .catch(() => setBots([]));
  }, []);

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
        setName(found.name ?? '');
        setSchedule(formatScheduleStr(found.schedule));
        setMessage(found.payload?.message ?? '');
        setBotId(found.bot_id || 'system');
        setMaxRuns(found.max_runs != null ? String(found.max_runs) : '');
        setExpiresAt(found.expires_at ? new Date(found.expires_at).toISOString().slice(0, 16) : '');
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

  // Track full dirty state across editable fields
  useEffect(() => {
    if (!job) return;
    const initialExpires = job.expires_at ? new Date(job.expires_at).toISOString().slice(0, 16) : '';
    const changed =
      name !== (job.name ?? '') ||
      schedule !== formatScheduleStr(job.schedule) ||
      message !== (job.payload?.message ?? '') ||
      botId !== (job.bot_id || 'system') ||
      maxRuns !== (job.max_runs != null ? String(job.max_runs) : '') ||
      expiresAt !== initialExpires ||
      enabled !== job.enabled;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- derive dirty flag
    setHasChanges(changed);
  }, [name, schedule, message, botId, maxRuns, expiresAt, enabled, job]);

  const handleSave = async () => {
    if (!job || !hasChanges) return;
    try {
      setSaving(true);
      const updates: Record<string, unknown> = {};

      if (name.trim() && name.trim() !== job.name) {
        updates.name = name.trim();
      }

      if (schedule !== formatScheduleStr(job.schedule)) {
        const parsed = parseScheduleStr(schedule);
        if (!parsed) {
          showToast(t('admin:cron.detail.invalid_schedule', { defaultValue: 'Invalid schedule format. Use cron:EXPR, every:DURATION, or at:TIMESTAMP.' }), 'error');
          return;
        }
        updates.schedule = parsed;
      }

      if (message !== (job.payload?.message ?? '')) {
        updates.payload = { ...job.payload, message: message.trim() };
      }

      if (botId !== (job.bot_id || 'system')) {
        updates.bot_id = botId.trim() || 'system';
      }

      if (maxRuns !== (job.max_runs != null ? String(job.max_runs) : '')) {
        updates.max_runs = maxRuns ? Number(maxRuns) : undefined;
      }

      const initialExpires = job.expires_at ? new Date(job.expires_at).toISOString().slice(0, 16) : '';
      if (expiresAt !== initialExpires) {
        updates.expires_at = expiresAt ? new Date(expiresAt).toISOString() : undefined;
      }

      if (enabled !== job.enabled) {
        updates.enabled = enabled;
      }

      await updateCronJob(job.id, updates);

      // Apply changes locally to job state
      setJob((prev) => {
        if (!prev) return prev;
        const updated = { ...prev };
        if (updates.name) updated.name = updates.name as string;
        if (updates.schedule) updated.schedule = updates.schedule as CronSchedule;
        if (updates.payload) updated.payload = { ...prev.payload, ...(updates.payload as Record<string, unknown>) };
        if (updates.bot_id) updated.bot_id = updates.bot_id as string;
        if (updates.max_runs !== undefined) updated.max_runs = updates.max_runs as number;
        if (updates.expires_at !== undefined) updated.expires_at = updates.expires_at as string;
        if (updates.enabled !== undefined) updated.enabled = updates.enabled as boolean;
        return updated;
      });

      setHasChanges(false);
      showToast(t('admin:cron.toast.updated', { name: job.name, defaultValue: `Cron job updated successfully.` }), 'success');
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
      setTimeout(() => loadJob(), 1000);
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
      className="inline-flex items-center gap-1.5 text-xs font-semibold text-[var(--text-secondary)] hover:text-[var(--accent-gold)] transition-colors"
    >
      <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
        <path d="M10 12L4 8L10 4" />
      </svg>
      {t('admin:cron.detail.back', { defaultValue: 'Back to Cron Jobs' })}
    </Link>
  );

  if (loading) {
    return (
      <div className="max-w-5xl mx-auto px-6 py-8 space-y-6">
        {backLink}
        <div className="flex flex-col items-center justify-center min-h-[40vh] space-y-3">
          <div className="w-8 h-8 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
          <p className="text-xs text-[var(--text-muted)] font-medium">
            {t('admin:cron.detail.loading', { defaultValue: 'Loading job details & history...' })}
          </p>
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
    <div className="max-w-5xl mx-auto px-6 py-8 space-y-6 animate-fade-in">
      {/* Back link */}
      {backLink}

      {/* Header Banner */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 p-6 rounded-[var(--radius-lg)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] shadow-sm">
        <div className="flex items-center gap-3.5">
          <div className="w-12 h-12 rounded-[var(--radius-md)] bg-[var(--accent-gold)]/10 text-[var(--accent-gold)] border border-[var(--accent-gold)]/20 flex items-center justify-center text-xl shrink-0 font-bold">
            ⏰
          </div>
          <div>
            <div className="flex items-center gap-3">
              <h1 className="text-2xl font-display font-bold text-[var(--text-primary)]">{job.name}</h1>
              <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-bold ${
                enabled ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20' : 'bg-zinc-500/10 text-zinc-400 border border-zinc-500/20'
              }`}>
                {enabled ? t('admin:cron.status.enabled', { defaultValue: 'Active' }) : t('admin:cron.status.disabled', { defaultValue: 'Disabled' })}
              </span>
            </div>
            <p className="text-xs font-mono text-[var(--text-muted)] mt-1">ID: {job.id}</p>
          </div>
        </div>

        <div className="flex items-center gap-3 shrink-0">
          <button
            type="button"
            onClick={handleTrigger}
            disabled={triggering || !enabled}
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

      {/* Summary KPI Metadata Grid */}
      <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-4 gap-4">
        <InfoCard
          label={t('admin:cron.table.last_run', { defaultValue: 'Last Executed' })}
          value={job.state?.last_run_at_ms ? formatDateTime(job.state.last_run_at_ms) : 'Never'}
          subValue={job.state?.last_run_at_ms ? formatRelative(job.state.last_run_at_ms) : undefined}
        />
        <InfoCard
          label={t('admin:cron.table.next_run', { defaultValue: 'Next Scheduled Run' })}
          value={enabled && job.state?.next_run_at_ms ? formatDateTime(job.state.next_run_at_ms) : '—'}
          subValue={enabled && job.state?.next_run_at_ms ? formatRelative(job.state.next_run_at_ms) : undefined}
        />
        <InfoCard
          label={t('admin:cron.detail.field_runs', { defaultValue: 'Total Runs / Limit' })}
          value={`${job.state?.run_count ?? 0} ${job.max_runs != null ? `/ ${job.max_runs}` : '(Unlimited)'}`}
          mono
        />
        <InfoCard
          label={t('admin:cron.detail.field_session', { defaultValue: 'Bound Session ID' })}
          value={job.id}
          mono
        />
      </div>

      {/* Full Editable Configuration Form Card */}
      <div className="rounded-[var(--radius-lg)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-6 shadow-sm space-y-5">
        <div className="flex items-center justify-between border-b border-[var(--border-subtle)] pb-3">
          <h2 className="text-xs font-bold text-[var(--accent-gold)] uppercase tracking-wider">
            ⚙️ {t('admin:cron.detail.configuration', { defaultValue: 'Edit Cron Job Configuration' })}
          </h2>
          {hasChanges && (
            <span className="text-[11px] font-bold text-[var(--accent-gold)] bg-[var(--accent-gold)]/10 px-2 py-0.5 rounded border border-[var(--accent-gold)]/20 animate-pulse">
              Unsaved Changes
            </span>
          )}
        </div>

        <div className="space-y-4">
          {/* Job Name */}
          <div>
            <label className="block text-xs font-semibold text-[var(--text-secondary)] mb-1.5">
              {t('admin:cron.modal.label_name', { defaultValue: 'Job Name' })}
            </label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full rounded-[var(--radius-md)] border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3.5 py-2.5 text-sm font-medium text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] transition-colors"
            />
          </div>

          {/* Schedule Specification */}
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

          {/* Prompt / Payload Message */}
          <div>
            <label className="block text-xs font-semibold text-[var(--text-secondary)] mb-1.5">
              {t('admin:cron.detail.field_message', { defaultValue: 'Task Prompt / Payload Message' })}
            </label>
            <textarea
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              rows={4}
              className="w-full rounded-[var(--radius-md)] border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3.5 py-2.5 text-xs text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] transition-colors resize-y"
              placeholder={t('admin:cron.detail.placeholder_message', { defaultValue: 'Task prompt message...' })}
            />
          </div>

          {/* Target Bot Selector */}
          <div>
            <label className="block text-xs font-semibold text-[var(--text-secondary)] mb-1.5">
              {t('admin:cron.modal.label_bot_id', { defaultValue: 'Target Bot Engine' })}
            </label>
            <select
              value={botId}
              onChange={(e) => setBotId(e.target.value)}
              className="w-full rounded-[var(--radius-md)] border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3.5 py-2.5 text-xs font-medium text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] transition-colors cursor-pointer"
            >
              <option value="system">🤖 Default System Worker (系统默认)</option>
              {bots.map((b) => (
                <option key={b.bot_id || b.name} value={b.bot_id || b.name}>
                  {b.name} ({b.platform} — ID: {b.bot_id || b.name})
                </option>
              ))}
            </select>
          </div>

          {/* Max Runs & Expires At */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            <div>
              <label className="block text-xs font-semibold text-[var(--text-secondary)] mb-1.5">
                {t('admin:cron.modal.label_max_runs', { defaultValue: 'Max Runs (Execution Limit)' })}
              </label>
              <input
                type="number"
                value={maxRuns}
                onChange={(e) => setMaxRuns(e.target.value)}
                min={1}
                placeholder="Default 1000"
                className="w-full rounded-[var(--radius-md)] border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3.5 py-2.5 text-xs text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] transition-colors"
              />
            </div>

            <div>
              <label className="block text-xs font-semibold text-[var(--text-secondary)] mb-1.5">
                {t('admin:cron.modal.label_expires_at', { defaultValue: 'Expiration Time' })}
              </label>
              <input
                type="datetime-local"
                value={expiresAt}
                onChange={(e) => setExpiresAt(e.target.value)}
                className="w-full rounded-[var(--radius-md)] border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3.5 py-2 text-xs text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] transition-colors"
              />
            </div>
          </div>

          {/* Enabled Switch */}
          <div className="flex items-center gap-3 pt-2">
            <input
              id="cron_detail_enabled"
              type="checkbox"
              checked={enabled}
              onChange={(e) => setEnabled(e.target.checked)}
              className="h-4 w-4 rounded border-[var(--border-default)] bg-[var(--bg-surface)] accent-[var(--accent-gold)] cursor-pointer"
            />
            <label htmlFor="cron_detail_enabled" className="text-xs font-semibold text-[var(--text-primary)] cursor-pointer">
              {t('admin:cron.modal.label_enabled', { defaultValue: 'Enable scheduled job immediately' })}
            </label>
          </div>

          {/* Save & Discard Action Toolbar */}
          {hasChanges && (
            <div className="flex items-center gap-3 pt-4 border-t border-[var(--border-subtle)]">
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
                  setName(job.name ?? '');
                  setSchedule(formatScheduleStr(job.schedule));
                  setMessage(job.payload?.message ?? '');
                  setBotId(job.bot_id || 'system');
                  setMaxRuns(job.max_runs != null ? String(job.max_runs) : '');
                  setExpiresAt(job.expires_at ? new Date(job.expires_at).toISOString().slice(0, 16) : '');
                  setEnabled(job.enabled);
                }}
                disabled={saving}
                className="rounded-[var(--radius-md)] border border-[var(--border-default)] bg-transparent px-4 py-2.5 text-xs font-medium text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] transition-all"
              >
                {t('common:action.discard', { defaultValue: 'Discard' })}
              </button>
            </div>
          )}
        </div>
      </div>

      {/* Execution Run History Logs Section */}
      <div className="rounded-[var(--radius-lg)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-6 shadow-sm space-y-4">
        <div className="flex items-center justify-between border-b border-[var(--border-subtle)] pb-3">
          <div className="flex items-center gap-2">
            <h2 className="text-xs font-bold text-[var(--accent-gold)] uppercase tracking-wider">
              📊 {t('admin:cron.detail.history_title', { defaultValue: 'Execution Run History & Trace Logs' })}
            </h2>
            <span className="px-2 py-0.5 rounded-full text-[10px] font-mono font-bold bg-[var(--bg-elevated)] text-[var(--text-secondary)]">
              {history.length}
            </span>
          </div>

          <button
            type="button"
            onClick={() => {
              setLoadingHistory(true);
              getCronRunHistory(job.id)
                .then((runs) => setHistory(Array.isArray(runs) ? runs : []))
                .catch(() => setHistory([]))
                .finally(() => setLoadingHistory(false));
            }}
            disabled={loadingHistory}
            className="text-xs font-semibold text-[var(--text-muted)] hover:text-[var(--accent-gold)] transition-colors inline-flex items-center gap-1"
          >
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className={loadingHistory ? 'animate-spin' : ''}>
              <path d="M21 2v6h-6M3 12a9 9 0 0 1 15-6.7L21 8M3 22v-6h6M21 12a9 9 0 0 1-15 6.7L3 16" />
            </svg>
            Refresh Logs
          </button>
        </div>

        {loadingHistory ? (
          <div className="py-8 flex items-center justify-center text-xs text-[var(--text-muted)]">
            <div className="w-4 h-4 border-2 border-current border-t-transparent rounded-full animate-spin mr-2" />
            Loading execution logs...
          </div>
        ) : history.length === 0 ? (
          <div className="py-10 text-center text-xs text-[var(--text-muted)] border border-dashed border-[var(--border-subtle)] rounded-[var(--radius-md)]">
            No execution history records found yet for this scheduled job.
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs border-collapse">
              <thead>
                <tr className="border-b border-[var(--border-subtle)] text-[var(--text-secondary)] font-semibold bg-[var(--bg-elevated)]">
                  <th className="py-2.5 px-3">Turn #</th>
                  <th className="py-2.5 px-3">Status</th>
                  <th className="py-2.5 px-3">Duration</th>
                  <th className="py-2.5 px-3">Tokens</th>
                  <th className="py-2.5 px-3">Executed At</th>
                  <th className="py-2.5 px-3">Error Log</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--border-subtle)] font-mono">
                {history.map((run, i) => (
                  <tr key={run.id || i} className="hover:bg-[var(--bg-hover)] transition-colors">
                    <td className="py-2.5 px-3 text-[var(--text-primary)] font-bold">#{run.turn_index ?? i + 1}</td>
                    <td className="py-2.5 px-3">
                      <span className={`inline-flex items-center px-2 py-0.5 rounded text-[10px] font-bold uppercase ${
                        run.status === 'success' || !run.error
                          ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
                          : 'bg-rose-500/10 text-rose-400 border border-rose-500/20'
                      }`}>
                        {run.status || (run.error ? 'failed' : 'success')}
                      </span>
                    </td>
                    <td className="py-2.5 px-3 text-[var(--text-secondary)]">
                      {run.duration_ms ? formatDuration(run.duration_ms) : '—'}
                    </td>
                    <td className="py-2.5 px-3 text-[var(--text-secondary)]">
                      {run.tokens_used ? run.tokens_used.toLocaleString() : '—'}
                    </td>
                    <td className="py-2.5 px-3 text-[var(--text-muted)] text-[11px]">
                      {run.created_at ? formatDateTime(run.created_at) : '—'}
                    </td>
                    <td className="py-2.5 px-3 text-rose-400 font-mono text-[11px] truncate max-w-xs" title={run.error}>
                      {run.error || '—'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}

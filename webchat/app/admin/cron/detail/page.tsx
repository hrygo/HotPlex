'use client';

import { useCallback, useEffect, useState } from 'react';
import { useSearchParams, useRouter } from 'next/navigation';
import Link from 'next/link';
import { listCronJobs, updateCronJob, deleteCronJob, triggerCronJob } from '@/lib/api/admin-cron';
import type { CronJob } from '@/lib/types/admin';

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

export default function CronDetailPage() {
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
        setSchedule(found.schedule);
        setMessage(found.message);
        setMaxRuns(found.max_runs != null ? String(found.max_runs) : '');
        setEnabled(found.enabled);
        setHasChanges(false);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load cron job');
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    loadJob();
  }, [loadJob]);

  // Track changes
  useEffect(() => {
    if (!job) return;
    const changed =
      schedule !== job.schedule ||
      message !== job.message ||
      maxRuns !== (job.max_runs != null ? String(job.max_runs) : '') ||
      enabled !== job.enabled;
    setHasChanges(changed);
  }, [schedule, message, maxRuns, enabled, job]);

  const handleSave = async () => {
    if (!job || !hasChanges) return;
    try {
      setSaving(true);
      const updates: Partial<CronJob> = {};
      if (schedule !== job.schedule) updates.schedule = schedule;
      if (message !== job.message) updates.message = message;
      if (maxRuns !== (job.max_runs != null ? String(job.max_runs) : '')) {
        updates.max_runs = maxRuns ? Number(maxRuns) : undefined;
      }
      if (enabled !== job.enabled) updates.enabled = enabled;
      await updateCronJob(job.id, updates);
      setJob((prev) => (prev ? { ...prev, ...updates } : prev));
      setHasChanges(false);
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to update cron job');
    } finally {
      setSaving(false);
    }
  };

  const handleToggle = async () => {
    if (!job) return;
    const next = !enabled;
    const label = next ? 'enable' : 'disable';
    if (!window.confirm(`${next ? 'Enable' : 'Disable'} cron job "${job.name}"?`)) return;
    try {
      setSaving(true);
      await updateCronJob(job.id, { enabled: next });
      setJob((prev) => (prev ? { ...prev, enabled: next } : prev));
      setEnabled(next);
      setHasChanges(false);
    } catch (err) {
      alert(err instanceof Error ? err.message : `Failed to ${label} cron job`);
    } finally {
      setSaving(false);
    }
  };

  const handleTrigger = async () => {
    if (!job) return;
    if (!window.confirm(`Manually trigger cron job "${job.name}"?`)) return;
    try {
      setTriggering(true);
      await triggerCronJob(job.id);
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to trigger cron job');
    } finally {
      setTriggering(false);
    }
  };

  const handleDelete = async () => {
    if (!job) return;
    if (!window.confirm(`Delete cron job "${job.name}" permanently? This cannot be undone.`)) return;
    try {
      setDeleting(true);
      await deleteCronJob(job.id);
      router.push('/admin/cron');
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to delete cron job');
      setDeleting(false);
    }
  };

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

  if (!id) {
    return (
      <div className="max-w-5xl mx-auto px-6 py-8">
        <Link
          href="/admin/cron"
          className="inline-flex items-center gap-1.5 text-xs text-[var(--text-faint)] hover:text-[var(--text-primary)] transition-colors mb-6"
        >
          <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-3.5 w-3.5">
            <path strokeLinecap="round" strokeLinejoin="round" d="M10.5 19.5 3 12m0 0 7.5-7.5M3 12h18" />
          </svg>
          Back to Cron Jobs
        </Link>
        <div className="flex items-center justify-center min-h-[60vh]">
          <p className="text-sm text-[var(--text-faint)]">No cron job ID specified</p>
        </div>
      </div>
    );
  }

  if (loading) {
    return (
      <div className="max-w-5xl mx-auto px-6 py-8">
        <Link
          href="/admin/cron"
          className="inline-flex items-center gap-1.5 text-xs text-[var(--text-faint)] hover:text-[var(--text-primary)] transition-colors mb-6"
        >
          <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-3.5 w-3.5">
            <path strokeLinecap="round" strokeLinejoin="round" d="M10.5 19.5 3 12m0 0 7.5-7.5M3 12h18" />
          </svg>
          Back to Cron Jobs
        </Link>
        <div className="flex items-center justify-center min-h-[60vh]">
          <div className="flex flex-col items-center gap-3">
            <div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
            <span className="text-xs text-[var(--text-faint)]">Loading cron job...</span>
          </div>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="max-w-5xl mx-auto px-6 py-8">
        <Link
          href="/admin/cron"
          className="inline-flex items-center gap-1.5 text-xs text-[var(--text-faint)] hover:text-[var(--text-primary)] transition-colors mb-6"
        >
          <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-3.5 w-3.5">
            <path strokeLinecap="round" strokeLinejoin="round" d="M10.5 19.5 3 12m0 0 7.5-7.5M3 12h18" />
          </svg>
          Back to Cron Jobs
        </Link>
        <div className="rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] p-4">
          <div className="flex items-center justify-between">
            <p className="text-sm text-[var(--accent-coral)]">{error}</p>
            <button
              onClick={loadJob}
              className="text-xs font-medium text-[var(--accent-coral)] underline underline-offset-2 hover:text-[var(--accent-coral)]/80 transition-colors"
            >
              Retry
            </button>
          </div>
        </div>
      </div>
    );
  }

  if (notFound || !job) {
    return (
      <div className="max-w-5xl mx-auto px-6 py-8">
        <Link
          href="/admin/cron"
          className="inline-flex items-center gap-1.5 text-xs text-[var(--text-faint)] hover:text-[var(--text-primary)] transition-colors mb-6"
        >
          <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-3.5 w-3.5">
            <path strokeLinecap="round" strokeLinejoin="round" d="M10.5 19.5 3 12m0 0 7.5-7.5M3 12h18" />
          </svg>
          Back to Cron Jobs
        </Link>
        <div className="flex flex-col items-center justify-center min-h-[60vh] text-center">
          <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-10 w-10 text-[var(--text-faint)] mb-4">
            <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v3.75m9-.75a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9 3.75h.008v.008H12v-.008Z" />
          </svg>
          <p className="text-sm text-[var(--text-muted)]">Cron job not found</p>
          <p className="text-xs text-[var(--text-faint)] mt-1 font-mono">{id}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-5xl mx-auto px-6 py-8">
      {/* Back link */}
      <Link
        href="/admin/cron"
        className="inline-flex items-center gap-1.5 text-xs text-[var(--text-faint)] hover:text-[var(--text-primary)] transition-colors mb-6"
      >
        <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor" className="h-3.5 w-3.5">
          <path strokeLinecap="round" strokeLinejoin="round" d="M10.5 19.5 3 12m0 0 7.5-7.5M3 12h18" />
        </svg>
        Back to Cron Jobs
      </Link>

      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div className="flex items-center gap-3">
          <h1 className="text-xl font-display font-bold text-[var(--text-primary)]">
            {job.name}
          </h1>
          <button
            onClick={handleToggle}
            disabled={saving}
            className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${
              enabled
                ? 'bg-[var(--accent-emerald)]'
                : 'bg-[var(--text-faint)]/30'
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
        <div className="flex items-center gap-2">
          {/* Trigger */}
          <button
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
            Trigger
          </button>
          {/* Delete */}
          <button
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
            Delete
          </button>
        </div>
      </div>

      {/* Editable fields */}
      <div className="rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-5 mb-4">
        <h2 className="text-xs font-bold text-[var(--text-faint)] uppercase tracking-wider mb-4">Configuration</h2>
        <div className="space-y-4">
          {/* Schedule */}
          <div>
            <label className="block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5">
              Schedule
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
              Message
            </label>
            <textarea
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              rows={4}
              className="w-full rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-base)] px-3 py-2 text-sm text-[var(--text-primary)] outline-none transition-colors focus:border-[var(--accent-gold)]/40 resize-y"
              placeholder="Task message..."
            />
          </div>

          {/* Max Runs */}
          <div>
            <label className="block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5">
              Max Runs
            </label>
            <input
              type="number"
              value={maxRuns}
              onChange={(e) => setMaxRuns(e.target.value)}
              min={0}
              className="w-40 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-base)] px-3 py-2 text-sm text-[var(--text-primary)] outline-none transition-colors focus:border-[var(--accent-gold)]/40"
              placeholder="Unlimited"
            />
          </div>

          {/* Save */}
          {hasChanges && (
            <div className="flex items-center gap-3 pt-2">
              <button
                onClick={handleSave}
                disabled={saving}
                className="inline-flex items-center gap-1.5 px-4 py-2 rounded-[var(--radius-sm)] text-xs font-bold uppercase tracking-wider bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
              >
                {saving ? (
                  <div className="w-3 h-3 border-2 border-current border-t-transparent rounded-full animate-spin" />
                ) : null}
                Save Changes
              </button>
              <button
                onClick={() => {
                  setSchedule(job.schedule);
                  setMessage(job.message);
                  setMaxRuns(job.max_runs != null ? String(job.max_runs) : '');
                  setEnabled(job.enabled);
                }}
                className="text-xs text-[var(--text-faint)] hover:text-[var(--text-primary)] transition-colors"
              >
                Discard
              </button>
            </div>
          )}
        </div>
      </div>

      {/* Read-only info cards */}
      <h2 className="text-xs font-bold text-[var(--text-faint)] uppercase tracking-wider mb-3">Details</h2>
      <div className="grid grid-cols-2 gap-3">
        <InfoRow label="ID" value={job.id} mono />
        <InfoRow label="Owner ID" value={job.owner_id ?? ''} mono />
        <InfoRow label="Bot ID" value={job.bot_id ?? ''} mono />
        <InfoRow label="Expires At" value={formatDateTime(job.expires_at)} />
        <InfoRow
          label="Run Count"
          value={job.runs_count != null ? `${job.runs_count}${job.max_runs != null ? ` / ${job.max_runs}` : ''}` : ''}
        />
        <InfoRow label="Last Run" value={formatDateTime(job.last_run_at)} />
        <InfoRow label="Next Run" value={job.enabled ? formatDateTime(job.next_run_at) : '—'} />
      </div>
    </div>
  );
}

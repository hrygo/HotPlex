'use client';

import { useState, useEffect } from 'react';
import { createCronJob } from '@/lib/api/admin-cron';
import { listBots } from '@/lib/api/admin-bots';
import type { BotConfigEntry } from '@/lib/types/admin';
import { useTranslation } from 'react-i18next';

interface CreateCronModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

type ScheduleKind = 'cron' | 'every' | 'at';

const CRON_PRESETS = [
  { label: 'Every 5 Min', value: '*/5 * * * *' },
  { label: 'Hourly', value: '0 * * * *' },
  { label: 'Daily @ 9:00 AM', value: '0 9 * * *' },
  { label: 'Mon-Fri @ 9:00 AM', value: '0 9 * * 1-5' },
];

const EVERY_PRESETS = [
  { label: '5 Minutes', value: '5m' },
  { label: '15 Minutes', value: '15m' },
  { label: '30 Minutes', value: '30m' },
  { label: '1 Hour', value: '1h' },
  { label: '24 Hours', value: '24h' },
];

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

export function CreateCronModal({ isOpen, onClose, onSuccess }: CreateCronModalProps) {
  const { t } = useTranslation();
  const [name, setName] = useState('');
  const [scheduleKind, setScheduleKind] = useState<ScheduleKind>('cron');
  const [scheduleVal, setScheduleVal] = useState('0 9 * * 1-5');
  const [message, setMessage] = useState('');
  const [maxRuns, setMaxRuns] = useState('');
  
  // Bot selection state
  const [bots, setBots] = useState<BotConfigEntry[]>([]);
  const [botSelection, setBotSelection] = useState<string>('system');
  const [customBotId, setCustomBotId] = useState('');

  const [enabled, setEnabled] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (isOpen) {
      listBots()
        .then((data) => setBots(Array.isArray(data) ? data : []))
        .catch(() => setBots([]));
    }
  }, [isOpen]);

  if (!isOpen) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);

    if (!name.trim()) {
      setError(t('admin:cron.modal.error_name_required', { defaultValue: 'Job name is required.' }));
      return;
    }
    if (!message.trim()) {
      setError(t('admin:cron.modal.error_message_required', { defaultValue: 'Task message/prompt is required.' }));
      return;
    }

    let scheduleObj: Record<string, unknown> = { kind: scheduleKind };
    if (scheduleKind === 'cron') {
      if (!scheduleVal.trim()) {
        setError(t('admin:cron.modal.error_schedule_required', { defaultValue: 'Cron expression is required.' }));
        return;
      }
      scheduleObj.expr = scheduleVal.trim();
    } else if (scheduleKind === 'every') {
      const ms = parseDurationToMs(scheduleVal.trim());
      if (ms <= 0) {
        setError(t('admin:cron.modal.error_duration_invalid', { defaultValue: 'Invalid interval duration (e.g. 30m, 1h, 24h).' }));
        return;
      }
      scheduleObj.every_ms = ms;
    } else if (scheduleKind === 'at') {
      if (!scheduleVal.trim()) {
        setError(t('admin:cron.modal.error_at_required', { defaultValue: 'Target timestamp ISO format is required.' }));
        return;
      }
      scheduleObj.at = scheduleVal.trim();
    }

    const payload: Record<string, unknown> = {
      kind: 'agent_task',
      message: message.trim(),
    };

    // Determine target bot_id
    let effectiveBotId = 'system';
    if (botSelection === 'custom') {
      effectiveBotId = customBotId.trim() || 'system';
    } else if (botSelection) {
      effectiveBotId = botSelection;
    }

    const body: Record<string, unknown> = {
      name: name.trim(),
      owner_id: 'admin',
      bot_id: effectiveBotId,
      schedule: scheduleObj,
      payload,
      enabled,
    };

    if (maxRuns.trim()) {
      const parsedMax = parseInt(maxRuns.trim(), 10);
      if (!isNaN(parsedMax) && parsedMax > 0) {
        body.max_runs = parsedMax;
      }
    }

    try {
      setSubmitting(true);
      await createCronJob(body);
      onSuccess();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : t('admin:cron.modal.error_create_failed', { defaultValue: 'Failed to create cron job' }));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-sm animate-fade-in">
      <div className="w-full max-w-lg rounded-[var(--radius-lg)] bg-[var(--bg-surface)] border border-[var(--border-default)] shadow-2xl p-6 space-y-5 animate-scale-up">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-[var(--border-subtle)] pb-3">
          <div className="flex items-center gap-2.5">
            <div className="w-8 h-8 rounded-[var(--radius-md)] bg-[var(--accent-gold)]/10 text-[var(--accent-gold)] border border-[var(--accent-gold)]/20 flex items-center justify-center text-sm font-bold">
              ⏰
            </div>
            <div>
              <h2 className="text-lg font-display font-bold text-[var(--text-primary)]">
                {t('admin:cron.modal.title', { defaultValue: 'Create Cron Scheduled Job' })}
              </h2>
              <p className="text-xs text-[var(--text-muted)]">
                {t('admin:cron.modal.subtitle', { defaultValue: 'Schedule automated agent tasks or message triggers' })}
              </p>
            </div>
          </div>

          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-[var(--radius-sm)] text-[var(--text-faint)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)] transition-all"
          >
            <svg width="18" height="18" viewBox="0 0 18 18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
              <line x1="2" y1="2" x2="16" y2="16" />
              <line x1="16" y1="2" x2="2" y2="16" />
            </svg>
          </button>
        </div>

        {/* Form Error Alert */}
        {error && (
          <div className="p-3 rounded-[var(--radius-md)] bg-rose-500/10 border border-rose-500/20 text-xs font-medium text-rose-400">
            {error}
          </div>
        )}

        {/* Body Form */}
        <form onSubmit={handleSubmit} className="space-y-4">
          {/* Job Name */}
          <div>
            <label className="block text-xs font-semibold text-[var(--text-secondary)] mb-1.5">
              {t('admin:cron.modal.label_name', { defaultValue: 'Job Name *' })}
            </label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. daily-standup-summary"
              className="w-full rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-default)] px-3.5 py-2.5 text-sm text-[var(--text-primary)] font-medium placeholder:text-[var(--text-faint)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors"
              required
            />
          </div>

          {/* Schedule Kind & Value */}
          <div className="space-y-2">
            <label className="block text-xs font-semibold text-[var(--text-secondary)]">
              {t('admin:cron.modal.label_schedule', { defaultValue: 'Schedule Trigger *' })}
            </label>
            <div className="grid grid-cols-3 gap-2 mb-2">
              {(['cron', 'every', 'at'] as ScheduleKind[]).map((kind) => (
                <button
                  key={kind}
                  type="button"
                  onClick={() => {
                    setScheduleKind(kind);
                    if (kind === 'cron') setScheduleVal('0 9 * * 1-5');
                    else if (kind === 'every') setScheduleVal('1h');
                    else setScheduleVal(new Date(Date.now() + 3600000).toISOString());
                  }}
                  className={`py-2 text-xs font-bold rounded-[var(--radius-md)] border uppercase tracking-wider transition-all ${
                    scheduleKind === kind
                      ? 'bg-[var(--accent-gold)] text-[var(--text-contrast)] border-[var(--accent-gold)]'
                      : 'bg-[var(--bg-elevated)] text-[var(--text-muted)] border-[var(--border-subtle)] hover:bg-[var(--bg-hover)]'
                  }`}
                >
                  {kind}
                </button>
              ))}
            </div>

            <input
              type="text"
              value={scheduleVal}
              onChange={(e) => setScheduleVal(e.target.value)}
              placeholder={scheduleKind === 'cron' ? '0 9 * * 1-5' : scheduleKind === 'every' ? '1h or 30m' : '2026-07-25T10:00:00Z'}
              className="w-full rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-default)] px-3.5 py-2.5 text-sm font-mono text-[var(--text-primary)] placeholder:text-[var(--text-faint)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors"
            />

            {/* Quick Presets */}
            {scheduleKind === 'cron' && (
              <div className="flex flex-wrap gap-1.5 pt-1">
                {CRON_PRESETS.map((p) => (
                  <button
                    key={p.value}
                    type="button"
                    onClick={() => setScheduleVal(p.value)}
                    className="px-2.5 py-1 rounded-[var(--radius-sm)] text-[11px] font-mono bg-[var(--bg-hover)] text-[var(--text-secondary)] border border-[var(--border-subtle)] hover:border-[var(--accent-gold)]/40 hover:text-[var(--accent-gold)] transition-all"
                  >
                    {p.label} ({p.value})
                  </button>
                ))}
              </div>
            )}
            {scheduleKind === 'every' && (
              <div className="flex flex-wrap gap-1.5 pt-1">
                {EVERY_PRESETS.map((p) => (
                  <button
                    key={p.value}
                    type="button"
                    onClick={() => setScheduleVal(p.value)}
                    className="px-2.5 py-1 rounded-[var(--radius-sm)] text-[11px] font-mono bg-[var(--bg-hover)] text-[var(--text-secondary)] border border-[var(--border-subtle)] hover:border-[var(--accent-gold)]/40 hover:text-[var(--accent-gold)] transition-all"
                  >
                    {p.label}
                  </button>
                ))}
              </div>
            )}
          </div>

          {/* Payload Message */}
          <div>
            <label className="block text-xs font-semibold text-[var(--text-secondary)] mb-1.5">
              {t('admin:cron.modal.label_message', { defaultValue: 'Task Message / Agent Prompt *' })}
            </label>
            <textarea
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              rows={3}
              placeholder="e.g. Generate a summary of daily dev pull requests and post to slack"
              className="w-full rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-default)] px-3.5 py-2.5 text-xs text-[var(--text-primary)] placeholder:text-[var(--text-faint)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors resize-y"
              required
            />
          </div>

          {/* Target Bot Selector & Max Runs */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            <div>
              <label className="block text-xs font-semibold text-[var(--text-secondary)] mb-1.5">
                {t('admin:cron.modal.label_bot_id', { defaultValue: 'Target Bot / Worker Engine' })}
              </label>
              <select
                value={botSelection}
                onChange={(e) => setBotSelection(e.target.value)}
                className="w-full rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-default)] px-3.5 py-2.5 text-xs font-medium text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] transition-colors cursor-pointer"
              >
                <option value="system">🤖 Default System Worker (系统默认)</option>
                {bots.map((b) => (
                  <option key={b.bot_id || b.name} value={b.bot_id || b.name}>
                    {b.name} ({b.platform} — ID: {b.bot_id || b.name})
                  </option>
                ))}
                <option value="custom">✏️ Custom Bot ID...</option>
              </select>

              {botSelection === 'custom' && (
                <input
                  type="text"
                  value={customBotId}
                  onChange={(e) => setCustomBotId(e.target.value)}
                  placeholder="Enter custom Bot ID"
                  className="w-full mt-2 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-default)] px-3.5 py-2 text-xs font-mono text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] transition-colors"
                />
              )}
            </div>

            <div>
              <label className="block text-xs font-semibold text-[var(--text-secondary)] mb-1.5">
                {t('admin:cron.modal.label_max_runs', { defaultValue: 'Max Runs (Execution Limit)' })}
              </label>
              <input
                type="number"
                value={maxRuns}
                onChange={(e) => setMaxRuns(e.target.value)}
                placeholder="Unlimited (Default 10000)"
                min={1}
                className="w-full rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-default)] px-3.5 py-2.5 text-xs text-[var(--text-primary)] placeholder:text-[var(--text-faint)] focus:outline-none focus:border-[var(--accent-gold)] transition-colors"
              />
            </div>
          </div>

          {/* Enabled Switch */}
          <div className="flex items-center gap-3 pt-1">
            <input
              id="cron_modal_enabled"
              type="checkbox"
              checked={enabled}
              onChange={(e) => setEnabled(e.target.checked)}
              className="h-4 w-4 rounded border-[var(--border-default)] bg-[var(--bg-surface)] accent-[var(--accent-gold)] cursor-pointer"
            />
            <label htmlFor="cron_modal_enabled" className="text-xs font-semibold text-[var(--text-primary)] cursor-pointer">
              {t('admin:cron.modal.label_enabled', { defaultValue: 'Enable scheduled job immediately upon creation' })}
            </label>
          </div>

          {/* Form Actions */}
          <div className="flex items-center justify-end gap-3 pt-4 border-t border-[var(--border-subtle)]">
            <button
              type="button"
              onClick={onClose}
              className="rounded-[var(--radius-md)] border border-[var(--border-default)] bg-transparent px-4 py-2.5 text-xs font-medium text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] transition-all"
            >
              {t('common:action.cancel', { defaultValue: 'Cancel' })}
            </button>
            <button
              type="submit"
              disabled={submitting}
              className="inline-flex items-center justify-center gap-2 rounded-[var(--radius-md)] bg-[var(--accent-gold)] px-5 py-2.5 text-xs font-bold text-[var(--text-contrast)] transition-all hover:bg-[var(--accent-gold-bright)] disabled:opacity-50 shadow-sm"
            >
              {submitting ? (
                <div className="h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
              ) : (
                t('common:action.create', { defaultValue: 'Create Cron Job' })
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

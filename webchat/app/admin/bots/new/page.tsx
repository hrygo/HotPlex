'use client';

import React, { useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { createBot } from '@/lib/api/admin-bots';

type Platform = 'feishu' | 'slack';
type WorkerType = 'claude_code' | 'open_code_server';
type Policy = 'open' | 'allowlist' | 'disabled';

interface FormState {
  platform: Platform | '';
  name: string;
  app_id: string;
  app_secret: string;
  bot_token: string;
  app_token: string;
  worker_type: WorkerType;
  work_dir: string;
  dm_policy: Policy;
  group_policy: Policy;
  require_mention: boolean;
  stt_provider: string;
  tts_provider: string;
  tts_voice: string;
}

interface FieldError {
  field: string;
  message: string;
}

const INITIAL: FormState = {
  platform: '',
  name: '',
  app_id: '',
  app_secret: '',
  bot_token: '',
  app_token: '',
  worker_type: 'claude_code',
  work_dir: '',
  dm_policy: 'open',
  group_policy: 'open',
  require_mention: false,
  stt_provider: '',
  tts_provider: '',
  tts_voice: '',
};

const NAME_RE = /^[a-zA-Z0-9-]+$/;

const inputClass =
  'w-full rounded-[var(--radius-sm)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] px-3 py-2 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-faint)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors font-mono';

const selectClass =
  'w-full rounded-[var(--radius-sm)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] px-3 py-2 text-sm text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors appearance-none';

const labelClass =
  'block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5';

export default function NewBotPage() {
  const router = useRouter();
  const [form, setForm] = useState<FormState>(INITIAL);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<FieldError[]>([]);
  const [success, setSuccess] = useState(false);
  const [touched, setTouched] = useState<Set<string>>(new Set());

  function set<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((prev) => ({ ...prev, [key]: value }));
    setTouched((prev) => new Set(prev).add(key));
    // Clear field error on edit
    setFieldErrors((prev) => prev.filter((e) => e.field !== key));
  }

  function getFieldError(field: string): string | undefined {
    if (!touched.has(field)) return undefined;
    return fieldErrors.find((e) => e.field === field)?.message;
  }

  function validate(): FieldError[] {
    const errors: FieldError[] = [];

    if (!form.platform) {
      errors.push({ field: 'platform', message: 'Platform is required.' });
    }
    if (!form.name.trim()) {
      errors.push({ field: 'name', message: 'Bot name is required.' });
    } else if (!NAME_RE.test(form.name.trim())) {
      errors.push({ field: 'name', message: 'Only letters, numbers, and hyphens.' });
    }
    if (form.platform === 'feishu') {
      if (!form.app_id.trim()) errors.push({ field: 'app_id', message: 'App ID is required.' });
      if (!form.app_secret.trim()) errors.push({ field: 'app_secret', message: 'App Secret is required.' });
    }
    if (form.platform === 'slack') {
      if (!form.bot_token.trim()) errors.push({ field: 'bot_token', message: 'Bot Token is required.' });
      if (!form.app_token.trim()) errors.push({ field: 'app_token', message: 'App Token is required.' });
    }

    return errors;
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);

    // Touch all fields to show errors
    setTouched(new Set(['platform', 'name', 'app_id', 'app_secret', 'bot_token', 'app_token']));

    const errors = validate();
    setFieldErrors(errors);
    if (errors.length > 0) return;

    setSubmitting(true);

    const body: Record<string, unknown> = {
      platform: form.platform,
      name: form.name.trim(),
    };

    if (form.platform === 'feishu') {
      body.app_id = form.app_id.trim();
      body.app_secret = form.app_secret.trim();
    } else {
      body.bot_token = form.bot_token.trim();
      body.app_token = form.app_token.trim();
    }

    body.worker_type = form.worker_type;
    if (form.work_dir.trim()) body.work_dir = form.work_dir.trim();
    body.dm_policy = form.dm_policy;
    body.group_policy = form.group_policy;
    body.require_mention = form.require_mention;
    if (form.stt_provider) body.stt = { provider: form.stt_provider };
    if (form.tts_provider || form.tts_voice) body.tts = { provider: form.tts_provider, voice: form.tts_voice };

    createBot(body)
      .then(() => {
        setSuccess(true);
        router.push('/admin/bots');
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : 'Failed to create bot');
      })
      .finally(() => {
        setSubmitting(false);
      });
  }

  if (success) {
    return (
      <div className="min-h-screen bg-[var(--bg-base)] p-6">
        <div className="max-w-2xl mx-auto px-6 py-8">
          <div className="rounded-[var(--radius-md)] bg-[rgba(16,185,129,0.08)] border border-[rgba(16,185,129,0.15)] p-4">
            <p className="text-sm text-[var(--accent-emerald)]">
              Bot created. Restart gateway to apply.
            </p>
          </div>
          <Link
            href="/admin/bots"
            className="inline-flex items-center gap-1.5 mt-4 px-3 py-1.5 rounded-[var(--radius-sm)] text-[11px] font-bold uppercase tracking-wider bg-[var(--bg-elevated)] text-[var(--text-secondary)] hover:text-[var(--text-primary)] transition-colors"
          >
            Back to Bots
          </Link>
        </div>
      </div>
    );
  }

  function fieldBorder(field: string): string {
    const err = getFieldError(field);
    if (err) return 'border-[var(--accent-coral)]';
    return '';
  }

  return (
    <div className="min-h-screen bg-[var(--bg-base)] p-6">
      <div className="max-w-2xl mx-auto px-6 py-8">
        {/* Breadcrumb */}
        <div className="flex items-center gap-2 mb-6 text-xs text-[var(--text-faint)]">
          <Link
            href="/admin/bots"
            className="hover:text-[var(--text-secondary)] transition-colors flex items-center gap-1"
          >
            <svg width="12" height="12" viewBox="0 0 16 16" fill="none">
              <path d="M10 3L5 8l5 5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
            Bots
          </Link>
          <span className="text-[var(--border-subtle)]">/</span>
          <span className="text-[var(--text-secondary)]">New Bot</span>
        </div>

        <h1 className="text-xl font-display font-bold text-[var(--text-primary)] mb-8">Create Bot</h1>

        {/* Global error */}
        {error && (
          <div className="rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] p-4 mb-6">
            <p className="text-sm text-[var(--accent-coral)]">{error}</p>
          </div>
        )}

        <form onSubmit={handleSubmit} className="space-y-8">
          {/* Section 1: Basic Info */}
          <section className="space-y-4 pb-8 border-b border-[var(--border-subtle)]">
            <h2 className="text-xs font-semibold text-[var(--text-faint)] uppercase tracking-wider">
              Basic Info
            </h2>

            {/* Platform */}
            <div>
              <label htmlFor="platform" className={labelClass}>Platform *</label>
              <select
                id="platform"
                value={form.platform}
                onChange={(e) => set('platform', e.target.value as Platform | '')}
                className={`${selectClass} ${fieldBorder('platform')}`}
              >
                <option value="">Select platform...</option>
                <option value="feishu">Feishu</option>
                <option value="slack">Slack</option>
              </select>
              {getFieldError('platform') && (
                <p className="mt-1 text-[11px] text-[var(--accent-coral)]">{getFieldError('platform')}</p>
              )}
            </div>

            {/* Name */}
            <div>
              <label htmlFor="name" className={labelClass}>Bot Name *</label>
              <input
                id="name"
                type="text"
                placeholder="my-bot"
                value={form.name}
                onChange={(e) => set('name', e.target.value)}
                className={`${inputClass} ${fieldBorder('name')}`}
              />
              {getFieldError('name') ? (
                <p className="mt-1 text-[11px] text-[var(--accent-coral)]">{getFieldError('name')}</p>
              ) : (
                <p className="mt-1 text-[11px] text-[var(--text-faint)]">
                  Letters, numbers, and hyphens only.
                </p>
              )}
            </div>

            {/* Feishu credentials */}
            {form.platform === 'feishu' && (
              <div className="grid grid-cols-1 gap-4">
                <div>
                  <label htmlFor="app_id" className={labelClass}>App ID *</label>
                  <input
                    id="app_id"
                    type="text"
                    placeholder="cli_a1b2c3d4"
                    value={form.app_id}
                    onChange={(e) => set('app_id', e.target.value)}
                    className={`${inputClass} ${fieldBorder('app_id')}`}
                  />
                  {getFieldError('app_id') && (
                    <p className="mt-1 text-[11px] text-[var(--accent-coral)]">{getFieldError('app_id')}</p>
                  )}
                </div>
                <div>
                  <label htmlFor="app_secret" className={labelClass}>App Secret *</label>
                  <input
                    id="app_secret"
                    type="password"
                    placeholder="Secret value"
                    value={form.app_secret}
                    onChange={(e) => set('app_secret', e.target.value)}
                    className={`${inputClass} ${fieldBorder('app_secret')}`}
                  />
                  {getFieldError('app_secret') && (
                    <p className="mt-1 text-[11px] text-[var(--accent-coral)]">{getFieldError('app_secret')}</p>
                  )}
                </div>
              </div>
            )}

            {/* Slack credentials */}
            {form.platform === 'slack' && (
              <div className="grid grid-cols-1 gap-4">
                <div>
                  <label htmlFor="bot_token" className={labelClass}>Bot Token *</label>
                  <input
                    id="bot_token"
                    type="password"
                    placeholder="xoxb-..."
                    value={form.bot_token}
                    onChange={(e) => set('bot_token', e.target.value)}
                    className={`${inputClass} ${fieldBorder('bot_token')}`}
                  />
                  {getFieldError('bot_token') && (
                    <p className="mt-1 text-[11px] text-[var(--accent-coral)]">{getFieldError('bot_token')}</p>
                  )}
                </div>
                <div>
                  <label htmlFor="app_token" className={labelClass}>App Token *</label>
                  <input
                    id="app_token"
                    type="password"
                    placeholder="xapp-..."
                    value={form.app_token}
                    onChange={(e) => set('app_token', e.target.value)}
                    className={`${inputClass} ${fieldBorder('app_token')}`}
                  />
                  {getFieldError('app_token') && (
                    <p className="mt-1 text-[11px] text-[var(--accent-coral)]">{getFieldError('app_token')}</p>
                  )}
                </div>
              </div>
            )}
          </section>

          {/* Section 2: Worker Config */}
          <section className="space-y-4 pb-8 border-b border-[var(--border-subtle)]">
            <h2 className="text-xs font-semibold text-[var(--text-faint)] uppercase tracking-wider">
              Worker Config
            </h2>

            <div>
              <label htmlFor="worker_type" className={labelClass}>Worker Type</label>
              <select
                id="worker_type"
                value={form.worker_type}
                onChange={(e) => set('worker_type', e.target.value as WorkerType)}
                className={selectClass}
              >
                <option value="claude_code">claude_code</option>
                <option value="open_code_server">open_code_server</option>
              </select>
            </div>

            <div>
              <label htmlFor="work_dir" className={labelClass}>Work Dir</label>
              <input
                id="work_dir"
                type="text"
                placeholder="/home/user/workspace"
                value={form.work_dir}
                onChange={(e) => set('work_dir', e.target.value)}
                className={inputClass}
              />
            </div>

          </section>

          {/* Section 3: Access Control */}
          <section className="space-y-4 pb-8 border-b border-[var(--border-subtle)]">
            <h2 className="text-xs font-semibold text-[var(--text-faint)] uppercase tracking-wider">
              Access Control
            </h2>

            <div>
              <label htmlFor="dm_policy" className={labelClass}>DM Policy</label>
              <select
                id="dm_policy"
                value={form.dm_policy}
                onChange={(e) => set('dm_policy', e.target.value as Policy)}
                className={selectClass}
              >
                <option value="open">Open</option>
                <option value="allowlist">Allowlist</option>
                <option value="disabled">Disabled</option>
              </select>
            </div>

            <div>
              <label htmlFor="group_policy" className={labelClass}>Group Policy</label>
              <select
                id="group_policy"
                value={form.group_policy}
                onChange={(e) => set('group_policy', e.target.value as Policy)}
                className={selectClass}
              >
                <option value="open">Open</option>
                <option value="allowlist">Allowlist</option>
                <option value="disabled">Disabled</option>
              </select>
            </div>

            <div className="flex items-center gap-3">
              <input
                id="require_mention"
                type="checkbox"
                checked={form.require_mention}
                onChange={(e) => set('require_mention', e.target.checked)}
                className="h-4 w-4 rounded border-[var(--border-subtle)] bg-[var(--bg-surface)] accent-[var(--accent-gold)]"
              />
              <label htmlFor="require_mention" className="text-sm text-[var(--text-secondary)]">
                Require mention in group messages
              </label>
            </div>
          </section>

          {/* Section 4: Voice (STT/TTS) */}
          <section className="space-y-4 pb-8 border-b border-[var(--border-subtle)]">
            <h2 className="text-xs font-semibold text-[var(--text-faint)] uppercase tracking-wider">
              Voice (STT/TTS)
            </h2>

            <div className="grid grid-cols-3 gap-4">
              <div>
                <label htmlFor="stt_provider" className={labelClass}>STT Provider</label>
                <select
                  id="stt_provider"
                  value={form.stt_provider}
                  onChange={(e) => set('stt_provider', e.target.value)}
                  className={selectClass}
                >
                  <option value="">Default</option>
                  <option value="local">Local</option>
                  <option value="feishu">Feishu</option>
                  <option value="feishu+local">Feishu + Local</option>
                </select>
              </div>
              <div>
                <label htmlFor="tts_provider" className={labelClass}>TTS Provider</label>
                <select
                  id="tts_provider"
                  value={form.tts_provider}
                  onChange={(e) => set('tts_provider', e.target.value)}
                  className={selectClass}
                >
                  <option value="">Default</option>
                  <option value="edge">Edge</option>
                  <option value="edge+moss">Edge + MOSS</option>
                </select>
              </div>
              <div>
                <label htmlFor="tts_voice" className={labelClass}>TTS Voice</label>
                <input
                  id="tts_voice"
                  type="text"
                  placeholder="zh-CN-XiaoxiaoNeural"
                  value={form.tts_voice}
                  onChange={(e) => set('tts_voice', e.target.value)}
                  className={inputClass}
                />
              </div>
            </div>
          </section>

          {/* Actions */}
          <div className="flex items-center justify-end gap-3 pt-2">
            <Link
              href="/admin/bots"
              className="px-4 py-2 rounded-[var(--radius-sm)] text-xs font-semibold text-[var(--text-faint)] hover:text-[var(--text-secondary)] transition-colors"
            >
              Cancel
            </Link>
            <button
              type="submit"
              disabled={submitting}
              className="inline-flex items-center gap-1.5 px-4 py-2 rounded-[var(--radius-sm)] text-xs font-bold uppercase tracking-wider bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {submitting && (
                <div className="w-3 h-3 border-2 border-black border-t-transparent rounded-full animate-spin" />
              )}
              {submitting ? 'Creating...' : 'Create Bot'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

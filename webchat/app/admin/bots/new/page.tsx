'use client';

import { useState, useEffect } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { createBot } from '@/lib/api/admin-bots';
import { getWorkers, WorkerInstallationStatus } from '@/lib/api/sessions';
import { useTranslation } from 'react-i18next';

type Platform = 'feishu' | 'slack';
type WorkerType = 'claude_code' | 'opencode_server' | 'codex_cli' | 'acp';
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
  'w-full rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-default)] px-3.5 py-2.5 text-sm text-[var(--text-primary)] font-mono placeholder:text-[var(--text-faint)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors';

const selectClass =
  'w-full rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-default)] px-3.5 py-2.5 text-sm text-[var(--text-primary)] font-medium focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors appearance-none';

const labelClass =
  'block text-xs font-semibold text-[var(--text-secondary)] mb-1.5';

export default function NewBotPage() {
  const { t } = useTranslation();
  const router = useRouter();
  const [form, setForm] = useState<FormState>(INITIAL);
  const [workers, setWorkers] = useState<WorkerInstallationStatus[]>([]);
  const [showSecret, setShowSecret] = useState(false);

  useEffect(() => {
    let active = true;
    getWorkers()
      .then((data) => {
        if (active) setWorkers(data);
      })
      .catch((err) => console.error("Failed to fetch workers", err));
    return () => {
      active = false;
    };
  }, []);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<FieldError[]>([]);
  const [touched, setTouched] = useState<Set<string>>(new Set());

  function set<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((prev) => ({ ...prev, [key]: value }));
    setTouched((prev) => new Set(prev).add(key));
    setFieldErrors((prev) => prev.filter((e) => e.field !== key));
  }

  function getFieldError(field: string): string | undefined {
    if (!touched.has(field)) return undefined;
    return fieldErrors.find((e) => e.field === field)?.message;
  }

  function validate(): FieldError[] {
    const errors: FieldError[] = [];

    if (!form.platform) {
      errors.push({ field: 'platform', message: t('admin:bots.validation.platform_required', { defaultValue: 'Please select a messaging platform.' }) });
    }
    if (!form.name.trim()) {
      errors.push({ field: 'name', message: t('admin:bots.validation.name_required', { defaultValue: 'Bot name is required.' }) });
    } else if (!NAME_RE.test(form.name.trim())) {
      errors.push({ field: 'name', message: t('admin:bots.validation.name_invalid', { defaultValue: 'Only letters, numbers, and hyphens are allowed.' }) });
    }
    if (form.platform === 'feishu') {
      if (!form.app_id.trim()) errors.push({ field: 'app_id', message: t('admin:bots.validation.app_id_required', { defaultValue: 'Feishu App ID is required.' }) });
      if (!form.app_secret.trim()) errors.push({ field: 'app_secret', message: t('admin:bots.validation.app_secret_required', { defaultValue: 'Feishu App Secret is required.' }) });
    }
    if (form.platform === 'slack') {
      if (!form.bot_token.trim()) errors.push({ field: 'bot_token', message: t('admin:bots.validation.bot_token_required', { defaultValue: 'Slack Bot Token (xoxb-) is required.' }) });
      if (!form.app_token.trim()) errors.push({ field: 'app_token', message: t('admin:bots.validation.app_token_required', { defaultValue: 'Slack App Token (xapp-) is required.' }) });
    }

    return errors;
  }

  function handleSubmit() {
    setError(null);

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
        router.push('/admin/bots');
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : t('admin:bots.error.create_failed', { defaultValue: 'Failed to create bot' }));
      })
      .finally(() => {
        setSubmitting(false);
      });
  }

  function fieldBorder(field: string): string {
    const err = getFieldError(field);
    if (err) return 'border-rose-500 focus:border-rose-500';
    return '';
  }

  return (
    <div className="min-h-screen bg-[var(--bg-base)] px-6 py-8">
      <div className="max-w-3xl mx-auto space-y-6">
        {/* Breadcrumb */}
        <div className="flex items-center gap-2 text-xs font-medium text-[var(--text-muted)]">
          <Link
            href="/admin/bots"
            className="hover:text-[var(--text-primary)] transition-colors flex items-center gap-1"
          >
            <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
              <path d="M10 3L5 8l5 5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
            {t('admin:bots.title', { defaultValue: 'Bot Management' })}
          </Link>
          <span className="text-[var(--border-subtle)]">/</span>
          <span className="text-[var(--text-primary)] font-bold">{t('admin:bots.action.new_bot', { defaultValue: 'New Bot' })}</span>
        </div>

        {/* Page Header */}
        <div>
          <h1 className="text-2xl font-display font-bold text-[var(--text-primary)]">
            {t('admin:bots.create_title', { defaultValue: 'Connect New Messaging Bot' })}
          </h1>
          <p className="text-xs text-[var(--text-muted)] mt-1">
            Configure App credentials for Feishu or Slack to dispatch messages to local Worker engines.
          </p>
        </div>

        {/* Global Error Alert */}
        {error && (
          <div className="rounded-[var(--radius-md)] bg-rose-500/10 border border-rose-500/20 p-4">
            <p className="text-xs font-medium text-rose-400">{error}</p>
          </div>
        )}

        <form onSubmit={(e) => { e.preventDefault(); handleSubmit(); }} className="space-y-6">
          
          {/* Section 1: Platform & Identity */}
          <section className="bg-[var(--bg-surface)] border border-[var(--border-subtle)] rounded-[var(--radius-lg)] p-6 shadow-sm space-y-5">
            <h2 className="text-xs font-bold text-[var(--accent-gold)] uppercase tracking-wider border-b border-[var(--border-subtle)] pb-2">
              1. {t('admin:bots.sections.basic_info', { defaultValue: 'Platform & Identity' })}
            </h2>

            {/* Platform Card Selector */}
            <div>
              <label className={labelClass}>
                {t('admin:bots.labels.platform', { defaultValue: 'Messaging Platform *' })}
              </label>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 mt-2">
                {/* Feishu Card */}
                <button
                  type="button"
                  onClick={() => set('platform', 'feishu')}
                  className={`p-4 rounded-[var(--radius-md)] border text-left transition-all flex items-start gap-3.5 ${
                    form.platform === 'feishu'
                      ? 'bg-[#3370FF]/10 border-[#3370FF] ring-1 ring-[#3370FF]'
                      : 'bg-[var(--bg-elevated)] border-[var(--border-subtle)] hover:border-[var(--border-default)]'
                  }`}
                >
                  <div className="w-10 h-10 rounded-[var(--radius-md)] bg-[#3370FF]/15 text-[#3370FF] flex items-center justify-center text-xl shrink-0">
                    🚀
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center justify-between">
                      <span className="font-bold text-sm text-[var(--text-primary)]">飞书 (Feishu)</span>
                      {form.platform === 'feishu' && (
                        <span className="text-[10px] font-bold px-2 py-0.5 rounded bg-[#3370FF] text-white">✓ Selected</span>
                      )}
                    </div>
                    <p className="text-xs text-[var(--text-muted)] mt-1">Feishu Long-Connection WebSocket Bot with App ID & Secret.</p>
                  </div>
                </button>

                {/* Slack Card */}
                <button
                  type="button"
                  onClick={() => set('platform', 'slack')}
                  className={`p-4 rounded-[var(--radius-md)] border text-left transition-all flex items-start gap-3.5 ${
                    form.platform === 'slack'
                      ? 'bg-[#E01E5A]/10 border-[#E01E5A] ring-1 ring-[#E01E5A]'
                      : 'bg-[var(--bg-elevated)] border-[var(--border-subtle)] hover:border-[var(--border-default)]'
                  }`}
                >
                  <div className="w-10 h-10 rounded-[var(--radius-md)] bg-[#E01E5A]/15 text-[#E01E5A] flex items-center justify-center text-xl shrink-0">
                    💬
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center justify-between">
                      <span className="font-bold text-sm text-[var(--text-primary)]">Slack</span>
                      {form.platform === 'slack' && (
                        <span className="text-[10px] font-bold px-2 py-0.5 rounded bg-[#E01E5A] text-white">✓ Selected</span>
                      )}
                    </div>
                    <p className="text-xs text-[var(--text-muted)] mt-1">Slack Socket Mode Bot with Bot Token & App Token.</p>
                  </div>
                </button>
              </div>
              {getFieldError('platform') && (
                <p className="mt-1.5 text-xs text-rose-400 font-medium">{getFieldError('platform')}</p>
              )}
            </div>

            {/* Bot Name */}
            <div>
              <label htmlFor="name" className={labelClass}>
                {t('admin:bots.labels.bot_name', { defaultValue: 'Bot Name / Instance Alias *' })}
              </label>
              <input
                id="name"
                type="text"
                placeholder="e.g. dev-assistant or customer-bot"
                value={form.name}
                onChange={(e) => set('name', e.target.value)}
                className={`${inputClass} ${fieldBorder('name')}`}
              />
              {getFieldError('name') ? (
                <p className="mt-1.5 text-xs text-rose-400 font-medium">{getFieldError('name')}</p>
              ) : (
                <p className="mt-1 text-[11px] text-[var(--text-muted)]">
                  {t('admin:bots.hints.bot_name', { defaultValue: 'Letters, numbers, and hyphens only.' })}
                </p>
              )}
            </div>
          </section>

          {/* Section 2: Platform Credentials */}
          {form.platform && (
            <section className="bg-[var(--bg-surface)] border border-[var(--border-subtle)] rounded-[var(--radius-lg)] p-6 shadow-sm space-y-5 animate-fade-in-up">
              <div className="flex items-center justify-between border-b border-[var(--border-subtle)] pb-2">
                <h2 className="text-xs font-bold text-[var(--accent-gold)] uppercase tracking-wider">
                  2. {form.platform === 'feishu' ? 'Feishu App Credentials' : 'Slack App Tokens'}
                </h2>
                <button
                  type="button"
                  onClick={() => setShowSecret((v) => !v)}
                  className="text-xs text-[var(--text-muted)] hover:text-[var(--text-primary)]"
                >
                  {showSecret ? '🙈 Hide Tokens' : '👁 Show Tokens'}
                </button>
              </div>

              {/* Feishu Credentials */}
              {form.platform === 'feishu' && (
                <div className="grid grid-cols-1 gap-4">
                  <div>
                    <label htmlFor="app_id" className={labelClass}>{t('admin:bots.labels.app_id', { defaultValue: 'App ID (cli_...)' })} *</label>
                    <input
                      id="app_id"
                      type="text"
                      placeholder="cli_a1b2c3d4e5f6"
                      value={form.app_id}
                      onChange={(e) => set('app_id', e.target.value)}
                      className={`${inputClass} ${fieldBorder('app_id')}`}
                    />
                    {getFieldError('app_id') && (
                      <p className="mt-1.5 text-xs text-rose-400 font-medium">{getFieldError('app_id')}</p>
                    )}
                  </div>

                  <div>
                    <label htmlFor="app_secret" className={labelClass}>{t('admin:bots.labels.app_secret', { defaultValue: 'App Secret' })} *</label>
                    <input
                      id="app_secret"
                      type={showSecret ? 'text' : 'password'}
                      placeholder="App secret key"
                      value={form.app_secret}
                      onChange={(e) => set('app_secret', e.target.value)}
                      className={`${inputClass} ${fieldBorder('app_secret')}`}
                    />
                    {getFieldError('app_secret') && (
                      <p className="mt-1.5 text-xs text-rose-400 font-medium">{getFieldError('app_secret')}</p>
                    )}
                  </div>
                </div>
              )}

              {/* Slack Credentials */}
              {form.platform === 'slack' && (
                <div className="grid grid-cols-1 gap-4">
                  <div>
                    <label htmlFor="bot_token" className={labelClass}>{t('admin:bots.labels.bot_token', { defaultValue: 'Bot User OAuth Token (xoxb-...)' })} *</label>
                    <input
                      id="bot_token"
                      type={showSecret ? 'text' : 'password'}
                      placeholder="xoxb-1234567890-..."
                      value={form.bot_token}
                      onChange={(e) => set('bot_token', e.target.value)}
                      className={`${inputClass} ${fieldBorder('bot_token')}`}
                    />
                    {getFieldError('bot_token') && (
                      <p className="mt-1.5 text-xs text-rose-400 font-medium">{getFieldError('bot_token')}</p>
                    )}
                  </div>

                  <div>
                    <label htmlFor="app_token" className={labelClass}>{t('admin:bots.labels.app_token', { defaultValue: 'App-Level Token (xapp-...)' })} *</label>
                    <input
                      id="app_token"
                      type={showSecret ? 'text' : 'password'}
                      placeholder="xapp-1-A1234567890-..."
                      value={form.app_token}
                      onChange={(e) => set('app_token', e.target.value)}
                      className={`${inputClass} ${fieldBorder('app_token')}`}
                    />
                    {getFieldError('app_token') && (
                      <p className="mt-1.5 text-xs text-rose-400 font-medium">{getFieldError('app_token')}</p>
                    )}
                  </div>
                </div>
              )}
            </section>
          )}

          {/* Section 3: Worker Engine & Workspace */}
          <section className="bg-[var(--bg-surface)] border border-[var(--border-subtle)] rounded-[var(--radius-lg)] p-6 shadow-sm space-y-5">
            <h2 className="text-xs font-bold text-[var(--accent-gold)] uppercase tracking-wider border-b border-[var(--border-subtle)] pb-2">
              3. {t('admin:bots.sections.worker_config', { defaultValue: 'Worker Engine & Workspace' })}
            </h2>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <label htmlFor="worker_type" className={labelClass}>{t('admin:bots.labels.worker_type', { defaultValue: 'Worker Engine Type' })}</label>
                <select
                  id="worker_type"
                  value={form.worker_type}
                  onChange={(e) => set('worker_type', e.target.value as WorkerType)}
                  className={selectClass}
                >
                  {workers.map((w) => (
                    <option key={w.type} value={w.type}>
                      {w.type}{!w.installed ? t('admin:bots.hints.not_installed', { defaultValue: ' (Not Installed)' }) : ''}
                    </option>
                  ))}
                  {workers.length === 0 && (
                    <>
                      <option value="claude_code">claude_code</option>
                      <option value="opencode_server">opencode_server</option>
                      <option value="codex_cli">codex_cli</option>
                      <option value="acp">acp (ACP Agent)</option>
                    </>
                  )}
                </select>
              </div>

              <div>
                <label htmlFor="work_dir" className={labelClass}>{t('admin:bots.labels.work_dir', { defaultValue: 'Working Directory (work_dir)' })}</label>
                <input
                  id="work_dir"
                  type="text"
                  placeholder="/home/user/workspace"
                  value={form.work_dir}
                  onChange={(e) => set('work_dir', e.target.value)}
                  className={inputClass}
                />
              </div>
            </div>
          </section>

          {/* Section 4: Access Control & Policies */}
          <section className="bg-[var(--bg-surface)] border border-[var(--border-subtle)] rounded-[var(--radius-lg)] p-6 shadow-sm space-y-5">
            <h2 className="text-xs font-bold text-[var(--accent-gold)] uppercase tracking-wider border-b border-[var(--border-subtle)] pb-2">
              4. {t('admin:bots.sections.access_control', { defaultValue: 'Access Policies & Defaults' })}
            </h2>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <label htmlFor="dm_policy" className={labelClass}>{t('admin:bots.labels.dm_policy', { defaultValue: 'Direct Message (DM) Policy' })}</label>
                <select
                  id="dm_policy"
                  value={form.dm_policy}
                  onChange={(e) => set('dm_policy', e.target.value as Policy)}
                  className={selectClass}
                >
                  <option value="open">{t('admin:bots.policies.open', { defaultValue: 'Open (Allow All)' })}</option>
                  <option value="allowlist">{t('admin:bots.policies.allowlist', { defaultValue: 'Allowlist (Restricted)' })}</option>
                  <option value="disabled">{t('admin:bots.policies.disabled', { defaultValue: 'Disabled (Blocked)' })}</option>
                </select>
              </div>

              <div>
                <label htmlFor="group_policy" className={labelClass}>{t('admin:bots.labels.group_policy', { defaultValue: 'Group Channel Policy' })}</label>
                <select
                  id="group_policy"
                  value={form.group_policy}
                  onChange={(e) => set('group_policy', e.target.value as Policy)}
                  className={selectClass}
                >
                  <option value="open">{t('admin:bots.policies.open', { defaultValue: 'Open (Allow All)' })}</option>
                  <option value="allowlist">{t('admin:bots.policies.allowlist', { defaultValue: 'Allowlist (Restricted)' })}</option>
                  <option value="disabled">{t('admin:bots.policies.disabled', { defaultValue: 'Disabled (Blocked)' })}</option>
                </select>
              </div>
            </div>

            <div className="flex items-center gap-3 p-3 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-subtle)]">
              <input
                id="require_mention"
                type="checkbox"
                checked={form.require_mention}
                onChange={(e) => set('require_mention', e.target.checked)}
                className="h-4 w-4 rounded border-[var(--border-default)] bg-[var(--bg-surface)] accent-[var(--accent-gold)] cursor-pointer"
              />
              <label htmlFor="require_mention" className="text-xs font-semibold text-[var(--text-primary)] cursor-pointer">
                {t('admin:bots.labels.require_mention', { defaultValue: 'Require explicit @mention in group messages' })}
              </label>
            </div>
          </section>

          {/* Form Actions */}
          <div className="flex items-center justify-end gap-3 pt-4">
            <Link
              href="/admin/bots"
              className="rounded-[var(--radius-md)] border border-[var(--border-default)] bg-transparent px-4 py-2.5 text-xs font-medium text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] transition-all"
            >
              {t('common:action.cancel', { defaultValue: 'Cancel' })}
            </Link>
            <button
              type="submit"
              disabled={submitting}
              className="inline-flex items-center gap-2 rounded-[var(--radius-md)] bg-[var(--accent-gold)] px-6 py-2.5 text-xs font-bold text-[var(--text-contrast)] transition-all hover:bg-[var(--accent-gold-bright)] disabled:opacity-50 shadow-sm"
            >
              {submitting && (
                <div className="w-3.5 h-3.5 border-2 border-current border-t-transparent rounded-full animate-spin" />
              )}
              {submitting ? t('common:action.creating', { defaultValue: 'Creating Bot...' }) : t('admin:bots.action.create', { defaultValue: 'Create Bot Instance' })}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

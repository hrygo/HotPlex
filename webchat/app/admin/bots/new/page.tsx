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
  'w-full rounded-[var(--radius-sm)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] px-3 py-2 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-faint)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors font-mono';

const selectClass =
  'w-full rounded-[var(--radius-sm)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] px-3 py-2 text-sm text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors appearance-none';

const labelClass =
  'block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5';

export default function NewBotPage() {
  const { t } = useTranslation();
  const router = useRouter();
  const [form, setForm] = useState<FormState>(INITIAL);
  const [workers, setWorkers] = useState<WorkerInstallationStatus[]>([]);

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
      errors.push({ field: 'platform', message: t('admin:bots.validation.platform_required', { defaultValue: 'Platform is required.' }) });
    }
    if (!form.name.trim()) {
      errors.push({ field: 'name', message: t('admin:bots.validation.name_required', { defaultValue: 'Bot name is required.' }) });
    } else if (!NAME_RE.test(form.name.trim())) {
      errors.push({ field: 'name', message: t('admin:bots.validation.name_invalid', { defaultValue: 'Only letters, numbers, and hyphens.' }) });
    }
    if (form.platform === 'feishu') {
      if (!form.app_id.trim()) errors.push({ field: 'app_id', message: t('admin:bots.validation.app_id_required', { defaultValue: 'App ID is required.' }) });
      if (!form.app_secret.trim()) errors.push({ field: 'app_secret', message: t('admin:bots.validation.app_secret_required', { defaultValue: 'App Secret is required.' }) });
    }
    if (form.platform === 'slack') {
      if (!form.bot_token.trim()) errors.push({ field: 'bot_token', message: t('admin:bots.validation.bot_token_required', { defaultValue: 'Bot Token is required.' }) });
      if (!form.app_token.trim()) errors.push({ field: 'app_token', message: t('admin:bots.validation.app_token_required', { defaultValue: 'App Token is required.' }) });
    }

    return errors;
  }

  function handleSubmit() {
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
            {t('admin:bots.title', { defaultValue: 'Bots' })}
          </Link>
          <span className="text-[var(--border-subtle)]">/</span>
          <span className="text-[var(--text-secondary)]">{t('admin:bots.action.new_bot', { defaultValue: 'New Bot' })}</span>
        </div>

        <h1 className="text-xl font-display font-bold text-[var(--text-primary)] mb-8">{t('admin:bots.create_title', { defaultValue: 'Create Bot' })}</h1>

        {/* Global error */}
        {error && (
          <div className="rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] p-4 mb-6">
            <p className="text-sm text-[var(--accent-coral)]">{error}</p>
          </div>
        )}

        <form onSubmit={(e) => { e.preventDefault(); handleSubmit(); }} className="space-y-8">
          {/* Section 1: Basic Info */}
          <section className="space-y-4 pb-8 border-b border-[var(--border-subtle)]">
            <h2 className="text-xs font-semibold text-[var(--text-faint)] uppercase tracking-wider">
              {t('admin:bots.sections.basic_info', { defaultValue: 'Basic Info' })}
            </h2>

            {/* Platform */}
            <div>
              <label htmlFor="platform" className={labelClass}>{t('admin:bots.labels.platform', { defaultValue: 'Platform *' })}</label>
              <select
                id="platform"
                value={form.platform}
                onChange={(e) => set('platform', e.target.value as Platform | '')}
                className={`${selectClass} ${fieldBorder('platform')}`}
              >
                <option value="">{t('admin:bots.placeholder.select_platform', { defaultValue: 'Select platform...' })}</option>
                <option value="feishu">Feishu</option>
                <option value="slack">Slack</option>
              </select>
              {getFieldError('platform') && (
                <p className="mt-1 text-[11px] text-[var(--accent-coral)]">{getFieldError('platform')}</p>
              )}
            </div>

            {/* Name */}
            <div>
              <label htmlFor="name" className={labelClass}>{t('admin:bots.labels.bot_name', { defaultValue: 'Bot Name *' })}</label>
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
                  {t('admin:bots.hints.bot_name', { defaultValue: 'Letters, numbers, and hyphens only.' })}
                </p>
              )}
            </div>

            {/* Feishu credentials */}
            {form.platform === 'feishu' && (
              <div className="grid grid-cols-1 gap-4">
                <div>
                  <label htmlFor="app_id" className={labelClass}>{t('admin:bots.labels.app_id', { defaultValue: 'App ID *' })}</label>
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
                  <label htmlFor="app_secret" className={labelClass}>{t('admin:bots.labels.app_secret', { defaultValue: 'App Secret *' })}</label>
                  <input
                    id="app_secret"
                    type="password"
                    placeholder={t('admin:bots.placeholder.secret_value', { defaultValue: 'Secret value' })}
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
                  <label htmlFor="bot_token" className={labelClass}>{t('admin:bots.labels.bot_token', { defaultValue: 'Bot Token *' })}</label>
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
                  <label htmlFor="app_token" className={labelClass}>{t('admin:bots.labels.app_token', { defaultValue: 'App Token *' })}</label>
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
              {t('admin:bots.sections.worker_config', { defaultValue: 'Worker Config' })}
            </h2>

            <div>
              <label htmlFor="worker_type" className={labelClass}>{t('admin:bots.labels.worker_type', { defaultValue: 'Worker Type' })}</label>
              <select
                id="worker_type"
                value={form.worker_type}
                onChange={(e) => set('worker_type', e.target.value as WorkerType)}
                className={selectClass}
              >
                {workers.map((w) => (
                  <option key={w.type} value={w.type}>
                    {w.type}{!w.installed ? t('admin:bots.hints.not_installed', { defaultValue: ' (Not Installed)' }) : ""}
                  </option>
                ))}
                {workers.length === 0 && (
                  <>
                    <option value="claude_code">claude_code</option>
                    <option value="opencode_server">opencode_server</option>
                    <option value="codex_cli">codex_cli</option>
                    <option value="acp">acp (ACP)</option>
                  </>
                )}
              </select>
            </div>

            <div>
              <label htmlFor="work_dir" className={labelClass}>{t('admin:bots.labels.work_dir', { defaultValue: 'Work Dir' })}</label>
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
              {t('admin:bots.sections.access_control', { defaultValue: 'Access Control' })}
            </h2>

            <div>
              <label htmlFor="dm_policy" className={labelClass}>{t('admin:bots.labels.dm_policy', { defaultValue: 'DM Policy' })}</label>
              <select
                id="dm_policy"
                value={form.dm_policy}
                onChange={(e) => set('dm_policy', e.target.value as Policy)}
                className={selectClass}
              >
                <option value="open">{t('admin:bots.policies.open', { defaultValue: 'Open' })}</option>
                <option value="allowlist">{t('admin:bots.policies.allowlist', { defaultValue: 'Allowlist' })}</option>
                <option value="disabled">{t('admin:bots.policies.disabled', { defaultValue: 'Disabled' })}</option>
              </select>
            </div>

            <div>
              <label htmlFor="group_policy" className={labelClass}>{t('admin:bots.labels.group_policy', { defaultValue: 'Group Policy' })}</label>
              <select
                id="group_policy"
                value={form.group_policy}
                onChange={(e) => set('group_policy', e.target.value as Policy)}
                className={selectClass}
              >
                <option value="open">{t('admin:bots.policies.open', { defaultValue: 'Open' })}</option>
                <option value="allowlist">{t('admin:bots.policies.allowlist', { defaultValue: 'Allowlist' })}</option>
                <option value="disabled">{t('admin:bots.policies.disabled', { defaultValue: 'Disabled' })}</option>
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
                {t('admin:bots.labels.require_mention', { defaultValue: 'Require mention in group messages' })}
              </label>
            </div>
          </section>

          {/* Section 4: Voice (STT/TTS) */}
          <section className="space-y-4 pb-8 border-b border-[var(--border-subtle)]">
            <h2 className="text-xs font-semibold text-[var(--text-faint)] uppercase tracking-wider">
              {t('admin:bots.sections.voice', { defaultValue: 'Voice (STT/TTS)' })}
            </h2>

            <div className="grid grid-cols-3 gap-4">
              <div>
                <label htmlFor="stt_provider" className={labelClass}>{t('admin:bots.labels.stt_provider', { defaultValue: 'STT Provider' })}</label>
                <select
                  id="stt_provider"
                  value={form.stt_provider}
                  onChange={(e) => set('stt_provider', e.target.value)}
                  className={selectClass}
                >
                  <option value="">{t('admin:bots.options.default', { defaultValue: 'Default' })}</option>
                  <option value="local">{t('admin:bots.options.local', { defaultValue: 'Local' })}</option>
                  <option value="feishu">Feishu</option>
                  <option value="feishu+local">Feishu + Local</option>
                </select>
              </div>
              <div>
                <label htmlFor="tts_provider" className={labelClass}>{t('admin:bots.labels.tts_provider', { defaultValue: 'TTS Provider' })}</label>
                <select
                  id="tts_provider"
                  value={form.tts_provider}
                  onChange={(e) => set('tts_provider', e.target.value)}
                  className={selectClass}
                >
                  <option value="">{t('admin:bots.options.default', { defaultValue: 'Default' })}</option>
                  <option value="edge">Edge</option>
                  <option value="edge+moss">Edge + MOSS</option>
                </select>
              </div>
              <div>
                <label htmlFor="tts_voice" className={labelClass}>{t('admin:bots.labels.tts_voice', { defaultValue: 'TTS Voice' })}</label>
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
              {t('common:action.cancel', { defaultValue: 'Cancel' })}
            </Link>
            <button
              type="submit"
              disabled={submitting}
              className="inline-flex items-center gap-1.5 px-4 py-2 rounded-[var(--radius-sm)] text-xs font-bold uppercase tracking-wider bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {submitting && (
                <div className="w-3 h-3 border-2 border-black border-t-transparent rounded-full animate-spin" />
              )}
              {submitting ? t('common:action.creating', { defaultValue: 'Creating...' }) : t('admin:bots.action.create', { defaultValue: 'Create Bot' })}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

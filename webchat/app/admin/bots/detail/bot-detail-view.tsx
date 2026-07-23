'use client';

import { useState, useEffect } from 'react';
import Link from 'next/link';
import { useSearchParams } from 'next/navigation';
import { getBot, deleteBot, updateBot } from '@/lib/api/admin-bots';
import { getWorkers, WorkerInstallationStatus } from '@/lib/api/sessions';
import { BotConfigEditor } from '@/components/admin/bot-config-editor';
import { DeleteButton } from '@/components/admin/delete-button';
import { SystemPromptPreview } from '@/components/admin/system-prompt-preview';
import { StatusBadge } from '@/components/admin/status-badge';
import type { BotConfigEntry } from '@/lib/types/admin';
import { useTranslation } from 'react-i18next';

type Policy = 'open' | 'allowlist' | 'disabled';
type WorkerType = 'claude_code' | 'opencode_server' | 'codex_cli' | 'acp';

const selectClass =
  'w-full rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-default)] px-3.5 py-2.5 text-sm text-[var(--text-primary)] font-medium focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors appearance-none';

const inputClass =
  'w-full rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-default)] px-3.5 py-2.5 text-sm text-[var(--text-primary)] font-mono placeholder:text-[var(--text-faint)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors';

const labelClass =
  'block text-xs font-semibold text-[var(--text-secondary)] mb-1.5';

// ---------------------------------------------------------------------------
// TagInput — editable list of strings
// ---------------------------------------------------------------------------

function TagInput({
  label,
  value,
  onChange,
  placeholder,
}: {
  label: string;
  value: string[];
  onChange: (v: string[]) => void;
  placeholder?: string;
}) {
  const { t } = useTranslation();
  const [input, setInput] = useState('');

  const add = () => {
    const trimmed = input.trim();
    if (trimmed && !value.includes(trimmed)) {
      onChange([...value, trimmed]);
    }
    setInput('');
  };

  const remove = (idx: number) => {
    onChange(value.filter((_, i) => i !== idx));
  };

  return (
    <div className="space-y-2">
      <label className={labelClass}>
        {label}
      </label>
      <div className="flex flex-wrap gap-2 min-h-[32px]">
        {value.map((tag, i) => (
          <span
            key={i}
            className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-[var(--radius-sm)] text-xs font-mono bg-[var(--bg-hover)] text-[var(--text-primary)] border border-[var(--border-subtle)] font-medium"
          >
            {tag}
            <button
              type="button"
              onClick={() => remove(i)}
              className="text-[var(--text-faint)] hover:text-rose-400 transition-colors p-0.5"
            >
              <svg width="12" height="12" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
                <line x1="1" y1="1" x2="9" y2="9" />
                <line x1="9" y1="1" x2="1" y2="9" />
              </svg>
            </button>
          </span>
        ))}
      </div>
      <div className="flex gap-2">
        <input
          type="text"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); add(); } }}
          placeholder={placeholder || t('admin:bots.detail.add_item_placeholder', { defaultValue: 'Add item...' })}
          className={`${inputClass} flex-1`}
        />
        <button
          type="button"
          onClick={add}
          disabled={!input.trim()}
          className="px-4 py-2.5 rounded-[var(--radius-md)] text-xs font-bold border border-[var(--border-default)] bg-[var(--bg-elevated)] text-[var(--text-primary)] hover:bg-[var(--bg-hover)] transition-colors disabled:opacity-40"
        >
          {t('common:action.add', { defaultValue: 'Add' })}
        </button>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// OverviewEditor
// ---------------------------------------------------------------------------

function OverviewEditor({
  bot,
  botName,
}: {
  bot: BotConfigEntry;
  botName: string;
}) {
  const { t } = useTranslation();
  const cfg = bot.config;
  const [workerType, setWorkerType] = useState<WorkerType>((cfg?.worker_type as WorkerType) || 'claude_code');
  const [workDir, setWorkDir] = useState(cfg?.work_dir ?? '');
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
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

  const dirty =
    workerType !== ((cfg?.worker_type as WorkerType) || 'claude_code') ||
    workDir !== (cfg?.work_dir ?? '');

  const handleSave = async () => {
    setSaving(true);
    setMessage(null);
    try {
      await updateBot(botName, {
        worker_type: workerType,
        work_dir: workDir || undefined,
      });
      setMessage({ type: 'success', text: t('admin:bots.detail.overview_updated', { defaultValue: 'Updated. Restart gateway to apply changes.' }) });
      setTimeout(() => setMessage(null), 3500);
    } catch (err) {
      setMessage({ type: 'error', text: err instanceof Error ? err.message : t('admin:bots.detail.update_failed', { defaultValue: 'Failed to update bot overview' }) });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-6 bg-[var(--bg-surface)] border border-[var(--border-subtle)] rounded-[var(--radius-lg)] p-6 shadow-sm">
      <div className="space-y-5">
        {/* Bot ID — read-only */}
        <div>
          <label className={labelClass}>
            {t('admin:bots.labels.bot_id', { defaultValue: 'Bot ID / App ID' })}
          </label>
          <div className="px-4 py-2.5 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-subtle)] font-mono text-xs font-bold text-[var(--text-primary)] break-all select-all">
            {bot.bot_id}
          </div>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div>
            <label className={labelClass}>
              {t('admin:bots.labels.worker_type', { defaultValue: 'Worker Engine Type' })}
            </label>
            <select value={workerType} onChange={(e) => setWorkerType(e.target.value as WorkerType)} className={selectClass}>
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
            <label className={labelClass}>
              {t('admin:bots.labels.work_dir', { defaultValue: 'Working Directory (work_dir)' })}
            </label>
            <input
              type="text"
              value={workDir}
              onChange={(e) => setWorkDir(e.target.value)}
              placeholder="/home/user/workspace"
              className={inputClass}
            />
          </div>
        </div>

        <div>
          <label className={labelClass}>
            {t('admin:bots.labels.connected_at', { defaultValue: 'Connected Timestamp' })}
          </label>
          <div className="px-4 py-2.5 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-xs font-mono text-[var(--text-primary)]">
            {bot.connected_at || 'Offline / Standby'}
          </div>
        </div>
      </div>

      {message && (
        <div
          className={`px-4 py-3 rounded-[var(--radius-md)] text-xs font-medium ${
            message.type === 'success'
              ? 'bg-emerald-500/10 border border-emerald-500/20 text-emerald-400'
              : 'bg-rose-500/10 border border-rose-500/20 text-rose-400'
          }`}
        >
          {message.text}
        </div>
      )}

      <div className="flex items-center justify-end pt-4 border-t border-[var(--border-subtle)]">
        <button
          type="button"
          onClick={handleSave}
          disabled={saving || !dirty}
          className="px-5 py-2.5 rounded-[var(--radius-md)] text-xs font-bold bg-[var(--accent-gold)] text-[var(--text-contrast)] hover:bg-[var(--accent-gold-bright)] transition-all disabled:opacity-40 disabled:cursor-not-allowed shadow-sm"
        >
          {saving ? t('common:action.saving', { defaultValue: 'Saving...' }) : t('common:action.save_changes', { defaultValue: 'Save Overview Changes' })}
        </button>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// AccessEditor — full access control + STT/TTS editing
// ---------------------------------------------------------------------------

function AccessEditor({
  initial,
  botName,
}: {
  initial: BotConfigEntry['config'];
  botName: string;
}) {
  const { t } = useTranslation();
  const [dmPolicy, setDmPolicy] = useState<Policy>((initial?.dm_policy as Policy) || 'open');
  const [groupPolicy, setGroupPolicy] = useState<Policy>((initial?.group_policy as Policy) || 'open');
  const [requireMention, setRequireMention] = useState(initial?.require_mention ?? false);
  const [allowFrom, setAllowFrom] = useState<string[]>(initial?.allow_from ?? []);
  const [allowDMFrom, setAllowDMFrom] = useState<string[]>(initial?.allow_dm_from ?? []);
  const [allowGroupFrom, setAllowGroupFrom] = useState<string[]>(initial?.allow_group_from ?? []);
  const [sttProvider, setSttProvider] = useState(initial?.stt?.provider ?? '');
  const [ttsProvider, setTtsProvider] = useState(initial?.tts?.provider ?? '');
  const [ttsVoice, setTtsVoice] = useState(initial?.tts?.voice ?? '');
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

  const dirty =
    dmPolicy !== ((initial?.dm_policy as Policy) || 'open') ||
    groupPolicy !== ((initial?.group_policy as Policy) || 'open') ||
    requireMention !== (initial?.require_mention ?? false) ||
    JSON.stringify(allowFrom) !== JSON.stringify(initial?.allow_from ?? []) ||
    JSON.stringify(allowDMFrom) !== JSON.stringify(initial?.allow_dm_from ?? []) ||
    JSON.stringify(allowGroupFrom) !== JSON.stringify(initial?.allow_group_from ?? []) ||
    sttProvider !== (initial?.stt?.provider ?? '') ||
    ttsProvider !== (initial?.tts?.provider ?? '') ||
    ttsVoice !== (initial?.tts?.voice ?? '');

  const handleSave = async () => {
    setSaving(true);
    setMessage(null);
    try {
      const updates: Record<string, unknown> = {
        dm_policy: dmPolicy,
        group_policy: groupPolicy,
        require_mention: requireMention,
        allow_from: allowFrom,
        allow_dm_from: allowDMFrom,
        allow_group_from: allowGroupFrom,
      };
      if (sttProvider) updates.stt = { provider: sttProvider };
      if (ttsProvider || ttsVoice) updates.tts = { provider: ttsProvider, voice: ttsVoice };

      await updateBot(botName, updates);
      setMessage({ type: 'success', text: t('admin:bots.detail.access_updated', { defaultValue: 'Access control updated. Restart gateway to apply.' }) });
      setTimeout(() => setMessage(null), 3500);
    } catch (err) {
      setMessage({ type: 'error', text: err instanceof Error ? err.message : t('admin:bots.detail.update_failed', { defaultValue: 'Failed to update access control' }) });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-6 bg-[var(--bg-surface)] border border-[var(--border-subtle)] rounded-[var(--radius-lg)] p-6 shadow-sm">
      {/* Access Control Section */}
      <section className="space-y-4">
        <h3 className="text-xs font-bold text-[var(--accent-gold)] uppercase tracking-wider border-b border-[var(--border-subtle)] pb-2">
          {t('admin:bots.sections.access_control', { defaultValue: 'Access Control Policies' })}
        </h3>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div>
            <label className={labelClass}>
              {t('admin:bots.labels.dm_policy', { defaultValue: 'Direct Message (DM) Policy' })}
            </label>
            <select value={dmPolicy} onChange={(e) => setDmPolicy(e.target.value as Policy)} className={selectClass}>
              <option value="open">{t('admin:bots.policies.open', { defaultValue: 'Open (Allow All)' })}</option>
              <option value="allowlist">{t('admin:bots.policies.allowlist', { defaultValue: 'Allowlist (Restricted)' })}</option>
              <option value="disabled">{t('admin:bots.policies.disabled', { defaultValue: 'Disabled (Blocked)' })}</option>
            </select>
          </div>

          <div>
            <label className={labelClass}>
              {t('admin:bots.labels.group_policy', { defaultValue: 'Group Channel Policy' })}
            </label>
            <select value={groupPolicy} onChange={(e) => setGroupPolicy(e.target.value as Policy)} className={selectClass}>
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
            checked={requireMention}
            onChange={(e) => setRequireMention(e.target.checked)}
            className="h-4 w-4 rounded border-[var(--border-default)] bg-[var(--bg-surface)] accent-[var(--accent-gold)] cursor-pointer"
          />
          <label htmlFor="require_mention" className="text-xs font-semibold text-[var(--text-primary)] cursor-pointer">
            {t('admin:bots.labels.require_mention', { defaultValue: 'Require explicit @mention in group channel messages' })}
          </label>
        </div>

        <TagInput
          label={t('admin:bots.labels.allow_from', { defaultValue: 'Global Allowlist (Users or Channels)' })}
          value={allowFrom}
          onChange={setAllowFrom}
          placeholder={t('admin:bots.placeholder.allow_from', { defaultValue: 'User ID or channel ID...' })}
        />
        <TagInput
          label={t('admin:bots.labels.allow_dm_from', { defaultValue: 'DM Allowlist (User IDs)' })}
          value={allowDMFrom}
          onChange={setAllowDMFrom}
          placeholder={t('admin:bots.placeholder.allow_dm_from', { defaultValue: 'User ID...' })}
        />
        <TagInput
          label={t('admin:bots.labels.allow_group_from', { defaultValue: 'Group Allowlist (Channel/Group IDs)' })}
          value={allowGroupFrom}
          onChange={setAllowGroupFrom}
          placeholder={t('admin:bots.placeholder.allow_group_from', { defaultValue: 'Channel or group ID...' })}
        />
      </section>

      {/* Voice Configuration */}
      <section className="space-y-4 pt-4 border-t border-[var(--border-subtle)]">
        <h3 className="text-xs font-bold text-[var(--accent-gold)] uppercase tracking-wider border-b border-[var(--border-subtle)] pb-2">
          {t('admin:bots.sections.voice', { defaultValue: 'Voice Synthesis & Recognition (STT/TTS)' })}
        </h3>

        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <div>
            <label className={labelClass}>
              {t('admin:bots.labels.stt_provider', { defaultValue: 'STT Provider' })}
            </label>
            <select value={sttProvider} onChange={(e) => setSttProvider(e.target.value)} className={selectClass}>
              <option value="">{t('admin:bots.options.default', { defaultValue: 'Default' })}</option>
              <option value="local">{t('admin:bots.options.local', { defaultValue: 'Local' })}</option>
              <option value="feishu">Feishu</option>
              <option value="feishu+local">Feishu + Local</option>
            </select>
          </div>
          <div>
            <label className={labelClass}>
              {t('admin:bots.labels.tts_provider', { defaultValue: 'TTS Provider' })}
            </label>
            <select value={ttsProvider} onChange={(e) => setTtsProvider(e.target.value)} className={selectClass}>
              <option value="">{t('admin:bots.options.default', { defaultValue: 'Default' })}</option>
              <option value="edge">Edge TTS</option>
              <option value="edge+moss">Edge + MOSS</option>
            </select>
          </div>
          <div>
            <label className={labelClass}>
              {t('admin:bots.labels.tts_voice', { defaultValue: 'TTS Voice Identifier' })}
            </label>
            <input
              type="text"
              value={ttsVoice}
              onChange={(e) => setTtsVoice(e.target.value)}
              placeholder="e.g. zh-CN-XiaoxiaoNeural"
              className={inputClass}
            />
          </div>
        </div>
      </section>

      {message && (
        <div
          className={`px-4 py-3 rounded-[var(--radius-md)] text-xs font-medium ${
            message.type === 'success'
              ? 'bg-emerald-500/10 border border-emerald-500/20 text-emerald-400'
              : 'bg-rose-500/10 border border-rose-500/20 text-rose-400'
          }`}
        >
          {message.text}
        </div>
      )}

      <div className="flex items-center justify-end pt-4 border-t border-[var(--border-subtle)]">
        <button
          type="button"
          onClick={handleSave}
          disabled={saving || !dirty}
          className="px-5 py-2.5 rounded-[var(--radius-md)] text-xs font-bold bg-[var(--accent-gold)] text-[var(--text-contrast)] hover:bg-[var(--accent-gold-bright)] transition-all disabled:opacity-40 disabled:cursor-not-allowed shadow-sm"
        >
          {saving ? t('common:action.saving', { defaultValue: 'Saving...' }) : t('common:action.save_changes', { defaultValue: 'Save Access Policies' })}
        </button>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main BotDetailView
// ---------------------------------------------------------------------------

type TabKey = 'overview' | 'config' | 'access';

export function BotDetailView() {
  const { t } = useTranslation();
  const searchParams = useSearchParams();
  const name = searchParams.get('name') ?? '';

  const [bot, setBot] = useState<BotConfigEntry | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<TabKey>('overview');

  const TABS: { key: TabKey; label: string }[] = [
    { key: 'overview', label: t('admin:bots.detail.tabs.overview', { defaultValue: 'Overview & Runtime' }) },
    { key: 'config', label: t('admin:bots.detail.tabs.config', { defaultValue: 'Agent Prompt & Directives' }) },
    { key: 'access', label: t('admin:bots.detail.tabs.access', { defaultValue: 'Access & Security' }) },
  ];

  useEffect(() => {
    if (!name) return;
    let cancelled = false;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- loading state for async fetch
    setLoading(true);
    getBot(name)
      .then((data: BotConfigEntry) => {
        if (!cancelled) setBot(data);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(String(err));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => { cancelled = true; };
  }, [name]);

  if (!name) {
    return (
      <div className="flex items-center justify-center min-h-[60vh]">
        <p className="text-sm text-[var(--text-faint)]">{t('admin:bots.detail.no_name', { defaultValue: 'No bot name specified' })}</p>
      </div>
    );
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-[60vh]">
        <div className="flex flex-col items-center gap-3">
          <div className="w-7 h-7 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
          <span className="text-xs font-medium text-[var(--text-faint)]">{t('admin:bots.detail.loading', { defaultValue: 'Loading bot details...' })}</span>
        </div>
      </div>
    );
  }

  if (error || !bot) {
    return (
      <div className="flex flex-col items-center justify-center min-h-[60vh] gap-4">
        <div className="rounded-[var(--radius-md)] bg-rose-500/10 border border-rose-500/20 p-4">
          <p className="text-sm font-medium text-rose-400">{error || t('admin:bots.detail.not_found', { defaultValue: 'Bot not found' })}</p>
        </div>
        <Link
          href="/admin/bots"
          className="text-xs font-bold text-[var(--accent-gold)] hover:underline"
        >
          {t('admin:bots.detail.back_to_bots', { defaultValue: '← Back to Bots' })}
        </Link>
      </div>
    );
  }

  return (
    <div className="max-w-5xl mx-auto px-6 py-8 space-y-6">
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
        <span className="text-[var(--text-primary)] font-bold">{name}</span>
      </div>

      {/* Header Banner */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 p-6 rounded-[var(--radius-lg)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] shadow-sm">
        <div className="flex items-center gap-3.5">
          <div className="w-12 h-12 rounded-[var(--radius-md)] bg-[var(--accent-gold)]/10 text-[var(--accent-gold)] border border-[var(--accent-gold)]/20 flex items-center justify-center text-xl shrink-0">
            🤖
          </div>
          <div>
            <div className="flex items-center gap-3">
              <h1 className="text-2xl font-display font-bold text-[var(--text-primary)]">{name}</h1>
              <span className="px-2.5 py-0.5 rounded-full text-xs font-mono font-bold uppercase bg-[var(--bg-elevated)] text-[var(--text-secondary)] border border-[var(--border-subtle)]">
                {bot.platform}
              </span>
              <StatusBadge status={bot.status} />
            </div>
            <p className="text-xs text-[var(--text-muted)] font-mono mt-1">
              Bot ID: {bot.bot_id}
            </p>
          </div>
        </div>

        <div className="flex items-center gap-3 shrink-0">
          <SystemPromptPreview botName={name} />
        </div>
      </div>

      {/* Navigation Tabs */}
      <div className="flex gap-2 border-b border-[var(--border-subtle)]">
        {TABS.map((tab) => (
          <button
            key={tab.key}
            type="button"
            onClick={() => setActiveTab(tab.key)}
            className={`px-5 py-3 text-xs font-bold transition-all border-b-2 -mb-px ${
              activeTab === tab.key
                ? 'border-[var(--accent-gold)] text-[var(--accent-gold)] font-bold'
                : 'border-transparent text-[var(--text-muted)] hover:text-[var(--text-primary)]'
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* Tab Content */}
      {activeTab === 'overview' && (
        <div className="space-y-6">
          <OverviewEditor bot={bot} botName={name} />
          <div className="p-6 rounded-[var(--radius-lg)] bg-[var(--bg-surface)] border border-rose-500/20 shadow-sm">
            <h3 className="text-xs font-bold text-rose-400 uppercase tracking-wider mb-2">Danger Zone</h3>
            <p className="text-xs text-[var(--text-muted)] mb-4">Permanently remove this bot configuration from gateway persistence.</p>
            <DeleteButton resourceName={name} buttonLabel={t('admin:bots.action.delete', { defaultValue: 'Delete Bot Instance' })} redirectHref="/admin/bots" onDelete={() => deleteBot(name)} />
          </div>
        </div>
      )}

      {activeTab === 'config' && <BotConfigEditor botName={name} />}

      {activeTab === 'access' && <AccessEditor initial={bot.config} botName={name} />}
    </div>
  );
}

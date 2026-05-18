'use client';

import { useState, useEffect } from 'react';
import { useSearchParams } from 'next/navigation';
import { getBot } from '@/lib/api/admin-bots';
import { BotConfigEditor } from '@/components/admin/bot-config-editor';
import { SystemPromptPreview } from '@/components/admin/system-prompt-preview';
import type { BotConfigEntry } from '@/lib/types/admin';

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

type TabKey = 'overview' | 'config' | 'access';

const TABS: { key: TabKey; label: string }[] = [
  { key: 'overview', label: 'Overview' },
  { key: 'config', label: 'Config' },
  { key: 'access', label: 'Access' },
];

export function BotDetailView() {
  const searchParams = useSearchParams();
  const name = searchParams.get('name') ?? '';

  const [bot, setBot] = useState<BotConfigEntry | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<TabKey>('overview');

  useEffect(() => {
    if (!name) return;
    let cancelled = false;
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
        <p className="text-sm text-[var(--text-faint)]">No bot name specified</p>
      </div>
    );
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-[60vh]">
        <div className="w-5 h-5 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
      </div>
    );
  }

  if (error || !bot) {
    return (
      <div className="flex items-center justify-center min-h-[60vh]">
        <p className="text-sm text-[var(--accent-coral)]">{error || 'Bot not found'}</p>
      </div>
    );
  }

  const cfg = bot.config;

  return (
    <div className="max-w-5xl mx-auto px-6 py-8">
      <div className="flex items-center gap-3 mb-6">
        <h1 className="text-xl font-display font-bold text-[var(--text-primary)]">{name}</h1>
        <span className="px-2 py-0.5 rounded-full text-[10px] font-mono font-semibold bg-[var(--bg-elevated)] text-[var(--text-secondary)] border border-[var(--border-subtle)] uppercase">
          {bot.platform}
        </span>
        <span
          className={`px-2 py-0.5 rounded-full text-[10px] font-bold uppercase ${
            bot.status === 'connected'
              ? 'bg-[var(--accent-emerald-glow)] text-[var(--accent-emerald)]'
              : 'bg-[rgba(244,63,94,0.1)] text-[var(--accent-coral)]'
          }`}
        >
          {bot.status}
        </span>
        <SystemPromptPreview botName={name} />
      </div>

      <div className="flex gap-1 mb-6 border-b border-[var(--border-subtle)]">
        {TABS.map((tab) => (
          <button
            key={tab.key}
            onClick={() => setActiveTab(tab.key)}
            className={`px-4 py-2.5 text-xs font-semibold transition-colors border-b-2 -mb-px ${
              activeTab === tab.key
                ? 'border-[var(--accent-gold)] text-[var(--accent-gold)]'
                : 'border-transparent text-[var(--text-faint)] hover:text-[var(--text-secondary)]'
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {activeTab === 'overview' && (
        <div className="grid grid-cols-2 gap-3">
          <InfoRow label="Bot ID" value={bot.bot_id} mono />
          <InfoRow label="Worker Type" value={cfg?.worker_type ?? ''} mono />
          <InfoRow label="Work Dir" value={cfg?.work_dir ?? ''} mono />
          <InfoRow label="Connected At" value={bot.connected_at ?? ''} />
        </div>
      )}

      {activeTab === 'config' && <BotConfigEditor botName={name} />}

      {activeTab === 'access' && (
        <div className="grid grid-cols-2 gap-3">
          <InfoRow label="DM Policy" value={cfg?.dm_policy ?? ''} />
          <InfoRow label="Group Policy" value={cfg?.group_policy ?? ''} />
          <InfoRow label="Require Mention" value={cfg?.require_mention ? 'Yes' : 'No'} />
          <InfoRow label="Allow From" value={cfg?.allow_from?.join(', ') ?? ''} />
          <InfoRow label="Allow DM From" value={cfg?.allow_dm_from?.join(', ') ?? ''} />
          <InfoRow label="Allow Group From" value={cfg?.allow_group_from?.join(', ') ?? ''} />
        </div>
      )}
    </div>
  );
}

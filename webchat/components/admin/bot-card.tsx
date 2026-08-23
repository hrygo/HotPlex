'use client';

import Link from 'next/link';
import type { BotConfigEntry } from '@/lib/types/admin';
import { StatusBadge } from './status-badge';
import { formatRelative } from '@/lib/utils/format-time';
import { useTranslation } from 'react-i18next';

interface BotCardProps {
  bot: BotConfigEntry;
}

const PLATFORM_STYLES: Record<string, { color: string; label: string; icon: string }> = {
  slack: { color: 'bg-[#E01E5A]/10 text-[#E01E5A] border-[#E01E5A]/20', label: 'Slack', icon: '💬' },
  feishu: { color: 'bg-[#3370FF]/10 text-[#3370FF] border-[#3370FF]/20', label: '飞书 (Feishu)', icon: '🚀' },
};

const DEFAULT_PLATFORM_STYLE = { color: 'bg-[var(--bg-hover)] text-[var(--text-muted)] border-[var(--border-subtle)]', label: '', icon: '🤖' };

const SOURCE_ICONS: Record<string, string> = {
  global: 'G',
  platform: 'P',
  bot: 'B',
};

export function BotCard({ bot }: BotCardProps) {
  const { t } = useTranslation();
  const platform = PLATFORM_STYLES[bot.platform] ?? DEFAULT_PLATFORM_STYLE;

  const getSourceLabel = (key: string) => {
    switch (key) {
      case 'agents': return t('admin:bots.source_labels.agents', { defaultValue: 'Rules' });
      case 'tools':
      case 'skills': return t('admin:bots.source_labels.tools', { defaultValue: 'Tools' });
      case 'user': return t('admin:bots.source_labels.user', { defaultValue: 'User' });
      case 'memory': return t('admin:bots.source_labels.memory', { defaultValue: 'Memory' });
      default: return key;
    }
  };

  const dmPolicy = bot.config?.dm_policy || 'open';
  const groupPolicy = bot.config?.group_policy || 'open';

  return (
    <Link
      href={`/admin/bots/detail?name=${encodeURIComponent(bot.name)}`}
      className="group block rounded-[var(--radius-lg)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] p-5 transition-all duration-300 hover:border-[var(--accent-gold)]/40 hover:bg-[var(--bg-hover)] shadow-sm hover:shadow-[var(--shadow-md)] flex flex-col justify-between space-y-4"
    >
      <div>
        {/* Header: Platform Icon + Bot Name + Platform Tag + Status Badge */}
        <div className="flex items-center justify-between gap-3 mb-3">
          <div className="flex items-center gap-3 min-w-0">
            <div className="w-9 h-9 rounded-[var(--radius-md)] bg-[var(--bg-hover)] border border-[var(--border-subtle)] flex items-center justify-center text-lg shrink-0 group-hover:scale-105 transition-transform">
              {platform.icon}
            </div>
            <div className="min-w-0">
              <h3 className="text-base font-display font-bold text-[var(--text-primary)] group-hover:text-[var(--accent-gold)] transition-colors truncate">
                {bot.name}
              </h3>
              <div className="flex items-center gap-2 mt-0.5">
                <span className={`inline-flex items-center px-2 py-0.5 rounded text-[10px] font-bold font-mono border ${platform.color}`}>
                  {platform.label || bot.platform}
                </span>
                {bot.bot_id && (
                  <span className="text-[10px] font-mono text-[var(--text-faint)] truncate max-w-[140px]" title={bot.bot_id}>
                    ID: {bot.bot_id}
                  </span>
                )}
              </div>
            </div>
          </div>

          <div className="shrink-0">
            <StatusBadge status={bot.status} />
          </div>
        </div>

        {/* Worker Engine & Access Policy info */}
        <div className="grid grid-cols-2 gap-2 text-xs py-2 px-3 rounded-[var(--radius-md)] bg-[var(--bg-base)] border border-[var(--border-subtle)]">
          <div className="flex items-center gap-1.5 min-w-0">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="text-[var(--accent-gold)] shrink-0">
              <rect x="2" y="6" width="20" height="12" rx="2" />
              <path d="M6 12h.01M10 12h.01" />
            </svg>
            <span className="font-mono font-bold text-[var(--text-primary)] truncate">
              {bot.config?.worker_type || 'claude_code'}
            </span>
          </div>

          <div className="flex items-center justify-end gap-1.5 text-[11px] text-[var(--text-muted)]">
            <span className="font-medium">DM: <span className="text-[var(--text-primary)] capitalize">{dmPolicy}</span></span>
            <span>•</span>
            <span className="font-medium">Group: <span className="text-[var(--text-primary)] capitalize">{groupPolicy}</span></span>
          </div>
        </div>
      </div>

      {/* Footer: Connected time + Agent Config Badges */}
      <div className="pt-3 border-t border-[var(--border-subtle)] flex items-center justify-between gap-2 text-xs">
        {bot.connected_at ? (
          <span className="text-[11px] text-[var(--text-faint)] font-mono flex items-center gap-1">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <circle cx="12" cy="12" r="10" />
              <path d="M12 6v6l4 2" />
            </svg>
            {formatRelative(bot.connected_at)}
          </span>
        ) : (
          <span className="text-[11px] text-[var(--text-faint)] italic">Offline</span>
        )}

        {bot.agent_configs && (
          <div className="flex flex-wrap gap-1 justify-end">
            {Object.entries(bot.agent_configs).map(([key, meta]) => {
              if (!meta?.source) return null;
              const label = getSourceLabel(key);
              return (
                <span
                  key={key}
                  className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-mono bg-[var(--bg-hover)] text-[var(--text-secondary)] border border-[var(--border-subtle)]"
                  title={`${label}: ${meta.source} (${meta.size}B)`}
                >
                  {label}
                  <span className="text-[9px] font-bold text-[var(--accent-gold)]">{SOURCE_ICONS[meta.source] || meta.source[0]}</span>
                </span>
              );
            })}
          </div>
        )}
      </div>
    </Link>
  );
}

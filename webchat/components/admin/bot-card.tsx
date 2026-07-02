'use client';

import Link from 'next/link';
import type { BotConfigEntry } from '@/lib/types/admin';
import { StatusBadge } from './status-badge';
import { formatRelative } from '@/lib/utils/format-time';
import { useTranslation } from 'react-i18next';

interface BotCardProps {
  bot: BotConfigEntry;
}

const PLATFORM_STYLES: Record<string, { color: string; label: string }> = {
  slack: { color: 'bg-[#E01E5A]/15 text-[#E01E5A]', label: 'Slack' },
  feishu: { color: 'bg-[#3370FF]/15 text-[#3370FF]', label: 'Feishu' },
};

const DEFAULT_PLATFORM_STYLE = { color: 'bg-[var(--bg-hover)] text-[var(--text-muted)]', label: '' };

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
      case 'skills': return t('admin:bots.source_labels.skills', { defaultValue: 'Skills' });
      case 'user': return t('admin:bots.source_labels.user', { defaultValue: 'User' });
      case 'memory': return t('admin:bots.source_labels.memory', { defaultValue: 'Memory' });
      default: return key;
    }
  };

  return (
    <Link
      href={`/admin/bots/detail?name=${encodeURIComponent(bot.name)}`}
      className="group block rounded-[var(--radius-md)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] p-4 transition-all hover:border-[var(--border-bright)] hover:bg-[var(--bg-elevated)]"
    >
      {/* Header: name + platform + status */}
      <div className="flex items-center gap-2 mb-2.5">
        <h3 className="text-sm font-display font-bold text-[var(--text-primary)] truncate">
          {bot.name}
        </h3>
        <span
          className={`inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wider ${platform.color}`}
        >
          {platform.label || bot.platform}
        </span>
        <div className="ml-auto">
          <StatusBadge status={bot.status} />
        </div>
      </div>

      {/* Worker info */}
      <div className="flex items-center gap-3 mb-2.5 text-[11px] text-[var(--text-faint)]">
        {bot.config?.worker_type && (
          <span className="flex items-center gap-1 font-mono">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <rect x="2" y="6" width="20" height="12" rx="2" />
              <path d="M6 12h.01M10 12h.01" />
            </svg>
            {bot.config.worker_type}
          </span>
        )}
        {bot.connected_at && (
          <span className="flex items-center gap-1">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <circle cx="12" cy="12" r="10" />
              <path d="M12 6v6l4 2" />
            </svg>
            {formatRelative(bot.connected_at)}
          </span>
        )}
      </div>

      {/* Agent config source indicators */}
      {bot.agent_configs && (
        <div className="flex flex-wrap gap-1.5 pt-2.5 border-t border-[var(--border-subtle)]">
          {Object.entries(bot.agent_configs).map(([key, meta]) => {
            if (!meta?.source) return null;
            const label = getSourceLabel(key);
            return (
              <span
                key={key}
                className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[9px] font-mono bg-[var(--bg-hover)] text-[var(--text-faint)]"
                title={`${label}: ${meta.source} (${meta.size}B)`}
              >
                {label}
                <span className="text-[8px] opacity-60">{SOURCE_ICONS[meta.source] || meta.source[0]}</span>
              </span>
            );
          })}
        </div>
      )}
    </Link>
  );
}

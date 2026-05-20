'use client';

import Link from 'next/link';
import type { BotConfigEntry } from '@/lib/types/admin';
import { StatusBadge } from './status-badge';

interface BotCardProps {
  bot: BotConfigEntry;
}

const PLATFORM_STYLES: Record<string, { color: string; label: string }> = {
  slack: { color: 'bg-[#E01E5A]/15 text-[#E01E5A]', label: 'Slack' },
  feishu: { color: 'bg-[#3370FF]/15 text-[#3370FF]', label: 'Feishu' },
};

const DEFAULT_PLATFORM_STYLE = { color: 'bg-[var(--bg-hover)] text-[var(--text-muted)]', label: '' };

const SOURCE_LABELS: Record<string, string> = {
  agents: 'Rules',
  skills: 'Skills',
  user: 'User',
  memory: 'Memory',
};

const SOURCE_ICONS: Record<string, string> = {
  global: 'G',
  platform: 'P',
  bot: 'B',
};

function formatConnectedTime(connectedAt?: string): string {
  if (!connectedAt) return '';
  const date = new Date(connectedAt);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMin = Math.floor(diffMs / 60000);
  const diffHour = Math.floor(diffMs / 3600000);
  const diffDay = Math.floor(diffMs / 86400000);

  if (diffMin < 1) return 'Just now';
  if (diffMin < 60) return `${diffMin}m ago`;
  if (diffHour < 24) return `${diffHour}h ago`;
  if (diffDay < 7) return `${diffDay}d ago`;
  return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}

export function BotCard({ bot }: BotCardProps) {
  const platform = PLATFORM_STYLES[bot.platform] ?? DEFAULT_PLATFORM_STYLE;

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
            {formatConnectedTime(bot.connected_at)}
          </span>
        )}
      </div>

      {/* Agent config source indicators */}
      {bot.agent_configs && (
        <div className="flex flex-wrap gap-1.5 pt-2.5 border-t border-[var(--border-subtle)]">
          {Object.entries(bot.agent_configs).map(([key, meta]) => {
            if (!meta?.source) return null;
            return (
              <span
                key={key}
                className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[9px] font-mono bg-[var(--bg-hover)] text-[var(--text-faint)]"
                title={`${SOURCE_LABELS[key] || key}: ${meta.source} (${meta.size}B)`}
              >
                {SOURCE_LABELS[key] || key}
                <span className="text-[8px] opacity-60">{SOURCE_ICONS[meta.source] || meta.source[0]}</span>
              </span>
            );
          })}
        </div>
      )}
    </Link>
  );
}

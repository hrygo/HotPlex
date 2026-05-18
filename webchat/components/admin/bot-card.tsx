'use client';

import Link from 'next/link';
import type { BotConfigEntry } from '@/lib/types/admin';
import { StatusBadge } from './status-badge';

interface BotCardProps {
  bot: BotConfigEntry;
}

const PLATFORM_COLORS: Record<string, string> = {
  slack: 'bg-[#E01E5A]/15 text-[#E01E5A]',
  feishu: 'bg-[#3370FF]/15 text-[#3370FF]',
};

const DEFAULT_PLATFORM_COLOR = 'bg-[var(--bg-hover)] text-[var(--text-muted)]';

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
  const platformColor = PLATFORM_COLORS[bot.platform] ?? DEFAULT_PLATFORM_COLOR;

  return (
    <Link
      href={`/admin/bots/detail?name=${encodeURIComponent(bot.name)}`}
      className="group block rounded-[var(--radius-md)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] p-4 transition-all hover:border-[var(--border-bright)] hover:bg-[var(--bg-elevated)]"
    >
      {/* Header: name + platform */}
      <div className="flex items-center gap-2 mb-3">
        <h3 className="text-sm font-display font-bold text-[var(--text-primary)] truncate">
          {bot.name}
        </h3>
        <span
          className={`inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wider ${platformColor}`}
        >
          {bot.platform}
        </span>
      </div>

      {/* Status + worker info */}
      <div className="flex items-center gap-3 mb-3">
        <StatusBadge status={bot.status} />
        {bot.config?.worker_type && (
          <span className="text-[11px] font-mono text-[var(--text-faint)]">
            {bot.config.worker_type}
          </span>
        )}
      </div>

      {/* Connected time */}
      {bot.connected_at && (
        <p className="text-[11px] text-[var(--text-muted)] mb-3">
          Connected {formatConnectedTime(bot.connected_at)}
        </p>
      )}

      {/* Agent config source badges */}
      {bot.agent_configs && (
        <div className="flex flex-wrap gap-1.5 pt-3 border-t border-[var(--border-subtle)]">
          {renderSourceBadge('soul', bot.agent_configs.soul)}
          {renderSourceBadge('agents', bot.agent_configs.agents)}
          {renderSourceBadge('skills', bot.agent_configs.skills)}
          {renderSourceBadge('user', bot.agent_configs.user)}
          {renderSourceBadge('memory', bot.agent_configs.memory)}
        </div>
      )}
    </Link>
  );
}

function renderSourceBadge(
  key: string,
  meta?: { source?: string; size?: number },
) {
  if (!meta?.source) return null;

  return (
    <span
      key={key}
      className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[9px] font-mono bg-[var(--bg-hover)] text-[var(--text-faint)]"
      title={`${key}: ${meta.source} (${meta.size}B)`}
    >
      {key}:{meta.source === 'global' ? 'g' : meta.source === 'platform' ? 'p' : 'b'}
    </span>
  );
}

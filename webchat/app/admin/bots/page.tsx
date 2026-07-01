'use client';

import { useMemo, useState } from 'react';
import Link from 'next/link';
import { listBots } from '@/lib/api/admin-bots';
import type { BotConfigEntry } from '@/lib/types/admin';
import { BotCard } from '@/components/admin/bot-card';
import { ChannelConfigEditor } from '@/components/admin/channel-config-editor';
import { useResource } from '@/hooks/use-resource';
import { LoadingState, ErrorState, EmptyState } from '@/components/admin/resource-states';
import { useTranslation } from 'react-i18next';

export default function BotsPage() {
  const { t } = useTranslation();
  const [query, setQuery] = useState('');
  const [showChannelDefaults, setShowChannelDefaults] = useState(false);

  const { data: bots, loading, error, reload } = useResource<BotConfigEntry[]>(
    () => listBots(),
    [],
  );
  const botList = useMemo(() => bots ?? [], [bots]);

  const filtered = useMemo(() => {
    if (!query.trim()) return botList;
    const q = query.toLowerCase();
    return botList.filter(
      (b) =>
        b.name.toLowerCase().includes(q) ||
        b.platform.toLowerCase().includes(q) ||
        b.bot_id.toLowerCase().includes(q),
    );
  }, [botList, query]);

  const onlineCount = botList.filter((b) => b.status === 'connected').length;

  return (
    <div className="min-h-screen bg-[var(--bg-base)] p-6">
      <div className="max-w-5xl mx-auto">
        {/* Header */}
        <div className="flex items-center justify-between mb-6">
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-display font-bold text-[var(--text-primary)]">
              {t('admin:bots.title', { defaultValue: 'Bots' })}
            </h1>
            {!loading && !error && (
              <div className="flex items-center gap-2">
                <span className="text-[11px] font-mono text-[var(--text-faint)] px-2 py-0.5 rounded-full bg-[var(--bg-hover)]">
                  {botList.length}
                </span>
                {onlineCount > 0 && (
                  <span className="text-[11px] font-mono text-[var(--accent-emerald)] px-2 py-0.5 rounded-full bg-[rgba(16,185,129,0.08)]">
                    {t('admin:bots.online_count', { count: onlineCount, defaultValue: `${onlineCount} online` })}
                  </span>
                )}
              </div>
            )}
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={reload}
              disabled={loading}
              className="inline-flex items-center justify-center w-8 h-8 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] text-[var(--text-faint)] hover:text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] transition-colors disabled:opacity-50"
              title="Refresh"
            >
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
                className={loading ? 'animate-spin' : ''}
              >
                <path d="M21 2v6h-6" />
                <path d="M3 12a9 9 0 0 1 15-6.7L21 8" />
                <path d="M3 22v-6h6" />
                <path d="M21 12a9 9 0 0 1-15 6.7L3 16" />
              </svg>
            </button>
            <Link
              href="/admin/bots/new"
              className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-[var(--radius-sm)] text-[11px] font-bold uppercase tracking-wider bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-colors"
            >
              {t('admin:bots.action.new_bot', { defaultValue: '+ New Bot' })}
            </Link>
          </div>
        </div>

        {/* Channel defaults — platform-level team configs for channels without
            a bot instance (webchat owns dir/webchat/ but is never a bot). #796 */}
        <div className="mb-6 rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] overflow-hidden">
          <button
            type="button"
            onClick={() => setShowChannelDefaults((v) => !v)}
            className="w-full flex items-center justify-between px-4 py-3 hover:bg-[var(--bg-hover)] transition-colors"
          >
            <div className="flex items-center gap-2 flex-wrap">
              <span className="text-sm font-semibold text-[var(--text-primary)]">
                {t('admin:bots.defaults.title', { defaultValue: 'Channel Defaults' })}
              </span>
              <span className="text-[11px] font-mono text-[var(--text-faint)] px-2 py-0.5 rounded-full bg-[var(--bg-hover)]">
                WebChat
              </span>
              <span className="text-[11px] text-[var(--text-faint)]">
                {t('admin:bots.defaults.subtitle', { defaultValue: 'Team defaults for channels without a bot instance' })}
              </span>
            </div>
            <svg
              className={`flex-shrink-0 text-[var(--text-faint)] transition-transform ${showChannelDefaults ? 'rotate-180' : ''}`}
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <polyline points="6 9 12 15 18 9" />
            </svg>
          </button>
          {showChannelDefaults && (
            <div className="px-4 pb-4 pt-3 border-t border-[var(--border-subtle)]">
              <ChannelConfigEditor platform="webchat" />
            </div>
          )}
        </div>

        {/* Search */}
        {!loading && !error && botList.length > 0 && (
          <div className="relative mb-5">
            <svg
              className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--text-faint)]"
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <circle cx="11" cy="11" r="8" />
              <path d="m21 21-4.3-4.3" />
            </svg>
            <input
              type="text"
              placeholder={t('admin:bots.placeholder.search', { defaultValue: 'Search by name, platform, or bot ID...' })}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="w-full pl-9 pr-4 py-2 rounded-[var(--radius-sm)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] text-sm text-[var(--text-primary)] placeholder:text-[var(--text-faint)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors"
            />
            {query && (
              <button
                type="button"
                onClick={() => setQuery('')}
                className="absolute right-3 top-1/2 -translate-y-1/2 text-[var(--text-faint)] hover:text-[var(--text-secondary)]"
              >
                <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
                  <line x1="1" y1="1" x2="13" y2="13" />
                  <line x1="13" y1="1" x2="1" y2="13" />
                </svg>
              </button>
            )}
          </div>
        )}

        {/* Loading */}
        {loading && <LoadingState label={t('admin:bots.loading', { defaultValue: 'Loading bots...' })} />}

        {/* Error */}
        {error && <ErrorState message={error} onRetry={reload} />}

        {/* Empty state */}
        {!loading && !error && botList.length === 0 && (
          <EmptyState
            title={t('admin:bots.empty.title', { defaultValue: 'No bots configured' })}
            description={t('admin:bots.empty.description', { defaultValue: 'Create your first bot to connect a messaging platform.' })}
            action={
              <Link
                href="/admin/bots/new"
                className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-[var(--radius-sm)] text-[11px] font-bold uppercase tracking-wider bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-colors"
              >
                {t('admin:bots.action.new_bot', { defaultValue: '+ New Bot' })}
              </Link>
            }
          />
        )}

        {/* Search no results */}
        {!loading && !error && botList.length > 0 && filtered.length === 0 && (
          <div className="flex flex-col items-center justify-center py-16 text-center">
            <p className="text-sm text-[var(--text-muted)] mb-1">
              {t('admin:bots.search_no_results', { query, defaultValue: `No bots matching "${query}"` })}
            </p>
            <button
              type="button"
              onClick={() => setQuery('')}
              className="text-xs text-[var(--accent-gold)] hover:underline"
            >
              {t('admin:bots.action.clear_search', { defaultValue: 'Clear search' })}
            </button>
          </div>
        )}

        {/* Bot grid */}
        {!loading && !error && filtered.length > 0 && (
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            {filtered.map((bot) => (
              <BotCard key={bot.bot_id} bot={bot} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

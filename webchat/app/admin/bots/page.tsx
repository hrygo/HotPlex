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
  const standbyCount = botList.length - onlineCount;

  return (
    <div className="min-h-screen bg-[var(--bg-base)] px-6 py-8">
      <div className="max-w-6xl mx-auto space-y-8">
        
        {/* Header */}
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
          <div>
            <div className="flex items-center gap-3">
              <h1 className="text-2xl font-display font-bold text-[var(--text-primary)]">
                {t('admin:bots.title', { defaultValue: 'Bot Management' })}
              </h1>
              {!loading && !error && (
                <div className="flex items-center gap-2">
                  <span className="text-xs font-mono font-bold px-2.5 py-0.5 rounded-full bg-[var(--bg-hover)] text-[var(--text-secondary)] border border-[var(--border-subtle)]">
                    {botList.length} Total
                  </span>
                  {onlineCount > 0 && (
                    <span className="text-xs font-mono font-bold px-2.5 py-0.5 rounded-full bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                      {t('admin:bots.online_count', { count: onlineCount, defaultValue: `${onlineCount} online` })}
                    </span>
                  )}
                  {standbyCount > 0 && (
                    <span className="text-xs font-mono font-bold px-2.5 py-0.5 rounded-full bg-amber-500/10 text-amber-400 border border-amber-500/20">
                      {standbyCount} standby
                    </span>
                  )}
                </div>
              )}
            </div>
            <p className="mt-1 text-xs text-[var(--text-muted)]">
              {t('admin:bots.subtitle', { defaultValue: 'Configure messaging platform bots, agent prompt rules, and channel defaults' })}
            </p>
          </div>

          <div className="flex items-center gap-2.5">
            <button
              type="button"
              onClick={reload}
              disabled={loading}
              className="p-2 rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)] transition-all disabled:opacity-50"
              title={t('common:action.refresh', { defaultValue: 'Refresh' })}
            >
              <svg
                width="16"
                height="16"
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
              className="inline-flex items-center gap-2 px-4 py-2 rounded-[var(--radius-md)] text-xs font-bold bg-[var(--accent-gold)] text-[var(--bg-base)] transition-all hover:bg-[var(--accent-gold)]/90 hover:shadow-[var(--shadow-glow)] shadow-sm"
            >
              <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={2.5} stroke="currentColor" className="w-4 h-4">
                <path strokeLinecap="round" strokeLinejoin="round" d="M12 4.5v15m7.5-7.5h-15" />
              </svg>
              {t('admin:bots.action.new_bot', { defaultValue: '+ New Bot' })}
            </Link>
          </div>
        </div>

        {/* Channel Defaults Section */}
        <div className="rounded-[var(--radius-lg)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] overflow-hidden shadow-sm">
          <button
            type="button"
            onClick={() => setShowChannelDefaults((v) => !v)}
            className="w-full flex items-center justify-between px-5 py-4 hover:bg-[var(--bg-hover)] transition-colors text-left"
          >
            <div className="flex items-center gap-3 flex-wrap">
              <div className="w-8 h-8 rounded-[var(--radius-md)] bg-[var(--accent-gold)]/10 text-[var(--accent-gold)] border border-[var(--accent-gold)]/20 flex items-center justify-center text-sm font-bold">
                ⚙️
              </div>
              <div>
                <div className="flex items-center gap-2">
                  <span className="text-sm font-bold text-[var(--text-primary)]">
                    {t('admin:bots.defaults.title', { defaultValue: 'Channel Team Defaults' })}
                  </span>
                  <span className="text-[10px] font-mono font-bold text-[var(--text-muted)] px-2 py-0.5 rounded bg-[var(--bg-hover)] border border-[var(--border-subtle)]">
                    WebChat
                  </span>
                </div>
                <p className="text-xs text-[var(--text-muted)] mt-0.5">
                  {t('admin:bots.defaults.subtitle', { defaultValue: 'Global fallback configuration for messaging channels without a dedicated bot instance' })}
                </p>
              </div>
            </div>

            <svg
              className={`flex-shrink-0 text-[var(--text-faint)] transition-transform duration-300 ${showChannelDefaults ? 'rotate-180' : ''}`}
              width="16"
              height="16"
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
            <div className="px-5 pb-5 pt-4 border-t border-[var(--border-subtle)] bg-[var(--bg-base)]">
              <ChannelConfigEditor platform="webchat" />
            </div>
          )}
        </div>

        {/* Search Bar */}
        {!loading && !error && botList.length > 0 && (
          <div className="relative">
            <svg
              className="absolute left-3.5 top-1/2 -translate-y-1/2 text-[var(--text-faint)]"
              width="15"
              height="15"
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
              placeholder={t('admin:bots.placeholder.search', { defaultValue: 'Search by bot name, platform (Feishu/Slack), or bot ID...' })}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="w-full pl-10 pr-10 py-2.5 rounded-[var(--radius-md)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] text-xs text-[var(--text-primary)] placeholder:text-[var(--text-faint)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors shadow-sm"
            />
            {query && (
              <button
                type="button"
                onClick={() => setQuery('')}
                className="absolute right-3.5 top-1/2 -translate-y-1/2 text-[var(--text-faint)] hover:text-[var(--text-primary)] p-1"
              >
                <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
                  <line x1="1" y1="1" x2="13" y2="13" />
                  <line x1="13" y1="1" x2="1" y2="13" />
                </svg>
              </button>
            )}
          </div>
        )}

        {/* Loading State */}
        {loading && <LoadingState label={t('admin:bots.loading', { defaultValue: 'Loading bots...' })} />}

        {/* Error State */}
        {error && <ErrorState message={error} onRetry={reload} />}

        {/* Empty State */}
        {!loading && !error && botList.length === 0 && (
          <EmptyState
            title={t('admin:bots.empty.title', { defaultValue: 'No messaging bots configured' })}
            description={t('admin:bots.empty.description', { defaultValue: 'Create your first bot to connect Slack or Feishu messaging platforms.' })}
            action={
              <Link
                href="/admin/bots/new"
                className="inline-flex items-center gap-2 px-4 py-2 rounded-[var(--radius-md)] text-xs font-bold bg-[var(--accent-gold)] text-[var(--bg-base)] transition-all hover:bg-[var(--accent-gold)]/90"
              >
                {t('admin:bots.action.new_bot', { defaultValue: '+ New Bot' })}
              </Link>
            }
          />
        )}

        {/* Search No Results */}
        {!loading && !error && botList.length > 0 && filtered.length === 0 && (
          <div className="flex flex-col items-center justify-center py-16 text-center bg-[var(--bg-surface)] border border-[var(--border-subtle)] rounded-[var(--radius-lg)]">
            <p className="text-xs text-[var(--text-muted)] mb-2">
              {t('admin:bots.search_no_results', { query, defaultValue: `No bots matching "${query}"` })}
            </p>
            <button
              type="button"
              onClick={() => setQuery('')}
              className="text-xs font-bold text-[var(--accent-gold)] hover:underline"
            >
              {t('admin:bots.action.clear_search', { defaultValue: 'Clear search' })}
            </button>
          </div>
        )}

        {/* Bot Grid */}
        {!loading && !error && filtered.length > 0 && (
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-5">
            {filtered.map((bot) => (
              <BotCard key={bot.bot_id} bot={bot} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

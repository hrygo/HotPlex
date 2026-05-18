'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { listBots } from '@/lib/api/admin-bots';
import type { BotConfigEntry } from '@/lib/types/admin';
import { BotCard } from '@/components/admin/bot-card';

export default function BotsPage() {
  const [bots, setBots] = useState<BotConfigEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        setLoading(true);
        setError(null);
        const data = await listBots();
        if (!cancelled) setBots(data);
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load bots');
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    load();
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <div className="min-h-screen bg-[var(--bg-base)] p-6">
      <div className="max-w-5xl mx-auto">
        {/* Header */}
        <div className="flex items-center justify-between mb-8">
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-display font-bold text-[var(--text-primary)]">Bots</h1>
            {!loading && !error && (
              <span className="text-[11px] font-mono text-[var(--text-faint)] px-2 py-0.5 rounded-full bg-[var(--bg-hover)]">
                {bots.length}
              </span>
            )}
          </div>
          <Link
            href="/admin/bots/new"
            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-[var(--radius-sm)] text-[11px] font-bold uppercase tracking-wider bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-colors"
          >
            + New Bot
          </Link>
        </div>

        {/* Loading */}
        {loading && (
          <div className="flex items-center justify-center py-24">
            <div className="flex flex-col items-center gap-3">
              <div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
              <span className="text-xs text-[var(--text-faint)]">Loading bots...</span>
            </div>
          </div>
        )}

        {/* Error */}
        {error && (
          <div className="rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] p-4">
            <p className="text-sm text-[var(--accent-coral)]">{error}</p>
          </div>
        )}

        {/* Empty state */}
        {!loading && !error && bots.length === 0 && (
          <div className="flex flex-col items-center justify-center py-24 text-center">
            <p className="text-sm text-[var(--text-muted)] mb-4">No bots configured yet.</p>
            <Link
              href="/admin/bots/new"
              className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-[var(--radius-sm)] text-[11px] font-bold uppercase tracking-wider bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-colors"
            >
              + New Bot
            </Link>
          </div>
        )}

        {/* Bot grid */}
        {!loading && !error && bots.length > 0 && (
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            {bots.map((bot) => (
              <BotCard key={bot.name} bot={bot} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

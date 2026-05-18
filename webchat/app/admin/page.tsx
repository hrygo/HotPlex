'use client';

import { useEffect, useState } from 'react';
import { listBots } from '@/lib/api/admin-bots';
import { listSessions } from '@/lib/api/admin-sessions';
import { listCronJobs } from '@/lib/api/admin-cron';
import { MetricCard } from '@/components/admin/metric-card';

interface DashboardMetrics {
  botsTotal: number;
  botsConnected: number;
  botsDisconnected: number;
  sessionsTotal: number;
  sessionsActive: number;
  cronTotal: number;
  cronEnabled: number;
  gatewayOnline: boolean;
}

function useDashboardMetrics() {
  const [metrics, setMetrics] = useState<DashboardMetrics>({
    botsTotal: 0,
    botsConnected: 0,
    botsDisconnected: 0,
    sessionsTotal: 0,
    sessionsActive: 0,
    cronTotal: 0,
    cronEnabled: 0,
    gatewayOnline: false,
  });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        setLoading(true);
        setError(null);

        // Fire all three requests concurrently. Individual failures are
        // tolerated -- partial data is better than no data.
        const [botsRes, sessionsRes, cronRes] = await Promise.allSettled([
          listBots(),
          listSessions(1, 0),
          listCronJobs(),
        ]);

        if (cancelled) return;

        const m: DashboardMetrics = {
          botsTotal: 0,
          botsConnected: 0,
          botsDisconnected: 0,
          sessionsTotal: 0,
          sessionsActive: 0,
          cronTotal: 0,
          cronEnabled: 0,
          gatewayOnline: false,
        };

        if (botsRes.status === 'fulfilled') {
          const bots = botsRes.value;
          m.botsTotal = bots.length;
          m.botsConnected = bots.filter((b) => b.status === 'connected').length;
          m.botsDisconnected = bots.filter((b) => b.status !== 'connected').length;
          m.gatewayOnline = true;
        }

        if (sessionsRes.status === 'fulfilled') {
          const sessions = sessionsRes.value.sessions;
          m.sessionsTotal = sessions.length;
          m.sessionsActive = sessions.filter(
            (s) => s.state === 'active' || s.state === 'working',
          ).length;
          m.gatewayOnline = true;
        }

        if (cronRes.status === 'fulfilled') {
          const jobs = cronRes.value;
          m.cronTotal = jobs.length;
          m.cronEnabled = jobs.filter((j) => j.enabled).length;
          m.gatewayOnline = true;
        }

        // If every request failed, the gateway is unreachable.
        const allFailed =
          botsRes.status === 'rejected' &&
          sessionsRes.status === 'rejected' &&
          cronRes.status === 'rejected';

        if (allFailed) {
          const firstErr = botsRes.reason;
          setError(
            firstErr instanceof Error ? firstErr.message : 'Gateway unreachable',
          );
        }

        setMetrics(m);
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Failed to load dashboard');
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    load();
    return () => {
      cancelled = true;
    };
  }, []);

  return { metrics, loading, error };
}

export default function DashboardPage() {
  const { metrics, loading, error } = useDashboardMetrics();

  return (
    <div className="min-h-screen bg-[var(--bg-base)] px-6 py-8">
      <div className="max-w-5xl mx-auto">
        {/* Header */}
        <div className="mb-8">
          <h1 className="text-xl font-display font-bold text-[var(--text-primary)]">
            Dashboard
          </h1>
          <p className="mt-1 text-sm text-[var(--text-muted)]">
            Gateway overview and system status
          </p>
        </div>

        {/* Loading */}
        {loading && (
          <div className="flex items-center justify-center py-24">
            <div className="flex flex-col items-center gap-3">
              <div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
              <span className="text-xs text-[var(--text-faint)]">
                Loading dashboard...
              </span>
            </div>
          </div>
        )}

        {/* Error banner (shown alongside partial data) */}
        {error && (
          <div className="mb-6 rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] p-4">
            <p className="text-sm text-[var(--accent-coral)]">{error}</p>
          </div>
        )}

        {/* Metric cards -- always render once loading finishes, even with zeros */}
        {!loading && (
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            {/* Bots */}
            <MetricCard
              label="Bots"
              value={metrics.botsTotal}
              sub={`${metrics.botsConnected} connected, ${metrics.botsDisconnected} disconnected`}
            />

            {/* Sessions */}
            <MetricCard
              label="Sessions"
              value={metrics.sessionsActive}
              sub={`${metrics.sessionsActive} active of ${metrics.sessionsTotal} total`}
            />

            {/* Cron Jobs */}
            <MetricCard
              label="Cron Jobs"
              value={metrics.cronTotal}
              sub={`${metrics.cronEnabled} enabled`}
            />

            {/* Gateway Uptime */}
            <MetricCard
              label="Gateway"
              value={metrics.gatewayOnline ? 'Running' : 'Offline'}
              sub={
                metrics.gatewayOnline
                  ? 'All endpoints responding'
                  : 'Unable to reach gateway'
              }
            />
          </div>
        )}
      </div>
    </div>
  );
}

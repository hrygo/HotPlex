'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { downloadActivity, listActivity, type ActivityFilters } from '@/lib/api/admin-activity';
import type { AuditActivity } from '@/lib/types/admin';
import { LoadingState, ErrorState, EmptyState } from '@/components/admin/resource-states';
import { useAdminUI } from '@/context/admin-ui-context';
import { useTranslation } from 'react-i18next';

type OutcomeFilter = '' | 'success' | 'failure' | 'denied';

function truncate(value: string, size = 18): string {
  if (!value || value.length <= size) return value;
  return `${value.slice(0, Math.max(0, size - 7))}...${value.slice(-4)}`;
}

function detailSummary(row: AuditActivity): string {
  if (!row.detail_json) return '';
  try {
    const parsed = JSON.parse(row.detail_json);
    if (typeof parsed !== 'object' || parsed === null) return row.detail_json;
    return Object.entries(parsed)
      .slice(0, 4)
      .map(([key, value]) => `${key}=${String(value)}`)
      .join(' · ');
  } catch {
    return row.detail_json;
  }
}

function outcomeClass(outcome: string): string {
  switch (outcome) {
    case 'success':
      return 'text-emerald-400 bg-emerald-500/10 border-emerald-500/20';
    case 'denied':
      return 'text-amber-300 bg-amber-500/10 border-amber-500/20';
    default:
      return 'text-red-300 bg-red-500/10 border-red-500/20';
  }
}

export default function AdminActivityPage() {
  const { t } = useTranslation('admin');
  const { t: tCommon } = useTranslation('common');
  const { showToast } = useAdminUI();
  const [rows, setRows] = useState<AuditActivity[]>([]);
  const [resolvedUserIDs, setResolvedUserIDs] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [exporting, setExporting] = useState<'json' | 'csv' | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [userId, setUserId] = useState('');
  const [principalUserId, setPrincipalUserId] = useState('');
  const [action, setAction] = useState('');
  const [outcome, setOutcome] = useState<OutcomeFilter>('');
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');

  const filters = useMemo<ActivityFilters>(() => ({
    userId: userId.trim(),
    principalUserId: principalUserId.trim(),
    action: action.trim(),
    outcome,
    from,
    to,
    limit: 100,
  }), [action, from, outcome, principalUserId, to, userId]);

  const load = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await listActivity(filters);
      setRows(data.rows ?? []);
      setResolvedUserIDs(data.resolved_user_ids ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : t('activity.error.load_failed'));
    } finally {
      setLoading(false);
    }
  }, [filters, t]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial remote load
    load();
  }, [load]);

  const handleExport = async (format: 'json' | 'csv') => {
    try {
      setExporting(format);
      await downloadActivity(filters, format);
      showToast(t('activity.toast.exported'), 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('activity.error.export_failed'), 'error');
    } finally {
      setExporting(null);
    }
  };

  const tableHeaders = [
    t('activity.table.time'),
    t('activity.table.user'),
    t('activity.table.platform'),
    t('activity.table.action'),
    t('activity.table.outcome'),
    t('activity.table.detail'),
  ];

  const labelForOutcome = (value: string) => {
    switch (value) {
      case 'success':
        return t('activity.outcomes.success');
      case 'failure':
        return t('activity.outcomes.failure');
      case 'denied':
        return t('activity.outcomes.denied');
      default:
        return value;
    }
  };

  return (
    <div className="min-h-screen bg-[var(--bg-base)] p-6">
      <div className="max-w-7xl mx-auto">
        <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between mb-5">
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-display font-bold text-[var(--text-primary)]">
              {t('activity.title')}
            </h1>
            {!loading && !error && (
              <span className="text-[11px] font-mono text-[var(--text-faint)] px-2 py-0.5 rounded-full bg-[var(--bg-hover)]">
                {rows.length}
              </span>
            )}
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => handleExport('json')}
              disabled={!!exporting || loading}
              className="px-3 py-1.5 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] text-xs text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] disabled:opacity-40"
            >
              {exporting === 'json' ? t('activity.action.exporting') : t('activity.action.export_json')}
            </button>
            <button
              type="button"
              onClick={() => handleExport('csv')}
              disabled={!!exporting || loading}
              className="px-3 py-1.5 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] text-xs text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] disabled:opacity-40"
            >
              {exporting === 'csv' ? t('activity.action.exporting') : t('activity.action.export_csv')}
            </button>
            <button
              type="button"
              onClick={load}
              disabled={loading}
              className="inline-flex items-center justify-center w-8 h-8 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] text-[var(--text-faint)] hover:text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] disabled:opacity-50"
              title={tCommon('action.refresh')}
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={loading ? 'animate-spin' : ''}>
                <path d="M21 2v6h-6" />
                <path d="M3 12a9 9 0 0 1 15-6.7L21 8" />
                <path d="M3 22v-6h6" />
                <path d="M21 12a9 9 0 0 1-15 6.7L3 16" />
              </svg>
            </button>
          </div>
        </div>

        <div className="mb-5 grid grid-cols-1 md:grid-cols-2 xl:grid-cols-6 gap-3">
          <input value={userId} onChange={(e) => setUserId(e.target.value)} placeholder={t('activity.filters.user_id')} className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-3 py-2 text-xs text-[var(--text-primary)] outline-none focus:border-[var(--accent-gold)]" />
          <input value={principalUserId} onChange={(e) => setPrincipalUserId(e.target.value)} placeholder={t('activity.filters.principal_user_id')} className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-3 py-2 text-xs text-[var(--text-primary)] outline-none focus:border-[var(--accent-gold)]" />
          <input value={action} onChange={(e) => setAction(e.target.value)} placeholder={t('activity.filters.action')} className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-3 py-2 text-xs text-[var(--text-primary)] outline-none focus:border-[var(--accent-gold)]" />
          <select value={outcome} onChange={(e) => setOutcome(e.target.value as OutcomeFilter)} className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-3 py-2 text-xs text-[var(--text-primary)] outline-none focus:border-[var(--accent-gold)]">
            <option value="">{t('activity.filters.any_outcome')}</option>
            <option value="success">{t('activity.outcomes.success')}</option>
            <option value="failure">{t('activity.outcomes.failure')}</option>
            <option value="denied">{t('activity.outcomes.denied')}</option>
          </select>
          <input type="datetime-local" value={from} onChange={(e) => setFrom(e.target.value)} className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-3 py-2 text-xs text-[var(--text-primary)] outline-none focus:border-[var(--accent-gold)]" aria-label={t('activity.filters.from')} />
          <input type="datetime-local" value={to} onChange={(e) => setTo(e.target.value)} className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-3 py-2 text-xs text-[var(--text-primary)] outline-none focus:border-[var(--accent-gold)]" aria-label={t('activity.filters.to')} />
        </div>

        {resolvedUserIDs.length > 0 && (
          <div className="mb-5 text-xs text-[var(--text-muted)]">
            {t('activity.resolved_ids')}: <span className="font-mono text-[var(--text-secondary)]">{resolvedUserIDs.join(', ')}</span>
          </div>
        )}

        {loading && <LoadingState label={t('activity.loading')} />}
        {error && <ErrorState message={error} onRetry={load} />}
        {!loading && !error && rows.length === 0 && (
          <EmptyState title={t('activity.empty.title')} description={t('activity.empty.description')} />
        )}

        {!loading && !error && rows.length > 0 && (
          <div className="overflow-hidden rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-surface)]">
            <div className="grid grid-cols-[150px_1.2fr_90px_1.2fr_95px_1.6fr] gap-3 px-4 py-2.5 bg-[var(--bg-hover)]/60 border-b border-[var(--border-subtle)]">
              {tableHeaders.map((label) => (
                <div key={label} className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest">
                  {label}
                </div>
              ))}
            </div>
            {rows.map((row) => (
              <div key={row.id} className="grid grid-cols-[150px_1.2fr_90px_1.2fr_95px_1.6fr] items-center gap-3 px-4 py-3 border-t border-[var(--border-subtle)] hover:bg-[var(--bg-hover)]/40">
                <div className="text-[11px] font-mono text-[var(--text-faint)]">{new Date(row.ts).toLocaleString()}</div>
                <div className="min-w-0">
                  <div className="text-xs font-mono text-[var(--text-primary)] truncate" title={row.user_id}>{truncate(row.user_id, 24)}</div>
                  <div className="text-[10px] text-[var(--text-faint)]">{row.user_id_type}</div>
                </div>
                <div className="text-xs text-[var(--text-secondary)]">{row.platform}</div>
                <div className="min-w-0">
                  <div className="text-xs font-mono text-[var(--text-primary)] truncate" title={row.action}>{row.action}</div>
                  {(row.resource_type || row.resource_id) && (
                    <div className="text-[10px] text-[var(--text-faint)] truncate">
                      {row.resource_type}{row.resource_id ? `:${truncate(row.resource_id, 16)}` : ''}
                    </div>
                  )}
                </div>
                <div className={`inline-flex w-fit rounded-full border px-2 py-0.5 text-[10px] font-bold ${outcomeClass(row.outcome)}`}>
                  {labelForOutcome(row.outcome)}
                </div>
                <div className="text-[11px] text-[var(--text-muted)] truncate" title={detailSummary(row)}>
                  {detailSummary(row)}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

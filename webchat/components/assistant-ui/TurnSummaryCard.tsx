'use client';

import React from 'react';
import { motion } from 'framer-motion';
import { useTranslation } from 'react-i18next';
import type { TurnSessionStats } from '@/lib/ai-sdk-transport/client/types';

type Severity = 'comfortable' | 'moderate' | 'high' | 'critical';

function getSeverity(pct: number): Severity {
  if (pct > 90) return 'critical';
  if (pct > 75) return 'high';
  if (pct >= 50) return 'moderate';
  return 'comfortable';
}

function formatTokens(n: number): string {
  if (n < 1000) return String(n);
  const k = n / 1000;
  return k % 1 === 0 ? `${k}K` : `~${k.toFixed(1)}K`;
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  const s = ms / 1000;
  if (s < 60) return `${Math.round(s)}s`;
  const m = Math.floor(s / 60);
  const rs = Math.round(s % 60);
  return rs > 0 ? `${m}m${rs}s` : `${m}m`;
}

function formatCost(usd: number): string {
  if (usd <= 0) return '';
  if (usd < 0.01) return `$${usd.toFixed(4)}`;
  return `$${usd.toFixed(2)}`;
}

const severityConfig: Record<Severity, { color: string; bg: string; border: string }> = {
  comfortable: {
    color: 'var(--accent-emerald)',
    bg: 'rgba(52, 211, 153, 0.06)',
    border: 'rgba(52, 211, 153, 0.15)',
  },
  moderate: {
    color: 'var(--accent-gold)',
    bg: 'rgba(251, 191, 36, 0.06)',
    border: 'rgba(251, 191, 36, 0.15)',
  },
  high: {
    color: 'var(--accent-gold)',
    bg: 'rgba(251, 191, 36, 0.08)',
    border: 'rgba(251, 191, 36, 0.25)',
  },
  critical: {
    color: 'var(--accent-coral)',
    bg: 'rgba(244, 63, 94, 0.08)',
    border: 'rgba(244, 63, 94, 0.25)',
  },
};

export function TurnSummaryCard({ data: rawData }: { data: TurnSessionStats }) {
  const { t } = useTranslation('chat');
  console.log("TurnSummaryCard stats data:", rawData);
  const data = (rawData || {}) as any;

  const pct = Math.max(0, Math.min(100, data.context_pct ?? data.contextPct ?? 0));
  const contextWindow = data.context_window ?? data.contextWindow ?? 0;
  const severity = getSeverity(pct);
  const cfg = severityConfig[severity];

  const items: React.ReactNode[] = [];

  const modelName = data.model_name ?? data.modelName;
  if (modelName) {
    items.push(
      <span key="model" className="font-semibold text-[var(--text-primary)]">
        {modelName}
      </span>
    );
  }

  if (pct > 0 && contextWindow > 0) {
    items.push(
      <span key="pct" className="font-mono text-[var(--text-secondary)]">
        {Math.round(pct)}%
      </span>
    );
  }

  const inputTok = data.turn_input_tok ?? data.turnInputTok ?? data.total_input_tok ?? data.totalInputTok ?? 0;
  const outputTok = data.turn_output_tok ?? data.turnOutputTok ?? data.total_output_tok ?? data.totalOutputTok ?? 0;
  if (inputTok > 0 || outputTok > 0) {
    items.push(
      <span key="tokens" className="inline-flex items-center gap-2">
        <span
          className="inline-flex items-center gap-0.5 text-sky-600 dark:text-sky-400 font-medium"
          title={t('label.input_tokens')}
        >
          <svg className="w-3 h-3 flex-shrink-0" fill="none" stroke="currentColor" strokeWidth={2.5} viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" d="M19 5L5 19M5 19h10M5 19V9" />
          </svg>
          <span>{formatTokens(inputTok)}</span>
        </span>
        <span className="text-[var(--text-faint)] select-none">/</span>
        <span
          className="inline-flex items-center gap-0.5 text-amber-600 dark:text-amber-400 font-medium"
          title={t('label.output_tokens')}
        >
          <svg className="w-3 h-3 flex-shrink-0" fill="none" stroke="currentColor" strokeWidth={2.5} viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" d="M5 19L19 5M19 5H9M19 5v10" />
          </svg>
          <span>{formatTokens(outputTok)}</span>
        </span>
      </span>
    );
  }

  const durationMs = data.turn_duration_ms ?? data.turnDurationMs ?? 0;
  if (durationMs > 0) {
    items.push(
      <span key="duration" className="inline-flex items-center gap-1" title={t('label.duration')}>
        <svg className="w-3 h-3 text-[var(--text-muted)] flex-shrink-0" fill="none" stroke="currentColor" strokeWidth={2} viewBox="0 0 24 24">
          <circle cx="12" cy="12" r="10" />
          <polyline points="12 6 12 12 16 14" />
        </svg>
        <span>{formatDuration(durationMs)}</span>
      </span>
    );
  }

  const gitBranch = data.git_branch ?? data.gitBranch;
  if (gitBranch) {
    items.push(
      <span key="branch" className="inline-flex items-center gap-1 text-[var(--text-muted)]" title={t('label.git_branch')}>
        <svg className="w-3 h-3 flex-shrink-0" fill="none" stroke="currentColor" strokeWidth={2} viewBox="0 0 24 24">
          <line x1="6" x2="6" y1="3" y2="15" />
          <circle cx="18" cy="6" r="3" />
          <circle cx="6" cy="18" r="3" />
          <path d="M18 9a9 9 0 0 1-9 9" />
        </svg>
        <span>{gitBranch}</span>
      </span>
    );
  }

  const toolCallCount = data.tool_call_count ?? data.toolCallCount ?? 0;
  if (toolCallCount > 0) {
    let toolStr = `${toolCallCount}`;
    const toolNames = data.tool_names ?? data.toolNames;
    if (toolNames && Object.keys(toolNames).length > 0) {
      const names = Object.entries(toolNames)
        .map(([name, count]) => (count as number) > 1 ? `${name}×${count}` : name)
        .join(', ');
      toolStr += ` (${names})`;
    }
    items.push(
      <span key="tools" className="inline-flex items-center gap-1" title={t('label.tool_calls')}>
        <svg className="w-3 h-3 text-[var(--text-muted)] flex-shrink-0" fill="none" stroke="currentColor" strokeWidth={2} viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z" />
        </svg>
        <span>{toolStr}</span>
      </span>
    );
  }

  const costVal = data.turn_cost_usd ?? data.turnCostUsd ?? data.turnCostUSD ?? data.total_cost_usd ?? data.totalCostUsd ?? data.totalCostUSD ?? 0;
  const cost = costVal ? formatCost(costVal) : '';
  if (cost) {
    items.push(
      <span key="cost" className="inline-flex items-center gap-1 text-[var(--accent-emerald)] font-medium" title={t('label.cost')}>
        <span>{cost}</span>
      </span>
    );
  }

  if (items.length === 0) return null;

  return (
    <motion.div
      initial={{ opacity: 0, y: 4 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.25, ease: [0.2, 0, 0, 1] }}
      className="my-1.5 rounded-[var(--radius-sm)] px-3 py-1.5 flex items-center gap-2 flex-wrap w-fit"
      style={{ background: cfg.bg, border: `1px solid ${cfg.border}` }}
    >
      {pct > 0 && contextWindow > 0 && (
        <div className="flex items-center gap-1.5">
          <span
            className="block w-1.5 h-1.5 rounded-full"
            style={{ background: cfg.color, boxShadow: `0 0 4px ${cfg.color}` }}
          />
          <div className="w-12 h-1 rounded-full bg-[var(--bg-elevated)] overflow-hidden">
            <motion.div
              className="h-full rounded-full"
              style={{ background: cfg.color }}
              initial={{ width: 0 }}
              animate={{ width: `${pct}%` }}
              transition={{ duration: 0.5, ease: [0.2, 0, 0, 1] }}
            />
          </div>
        </div>
      )}
      <div className="text-[10px] font-mono text-[var(--text-secondary)] flex items-center gap-2 flex-wrap">
        {items.map((item, index) => (
          <React.Fragment key={index}>
            {index > 0 && <span className="text-[var(--text-faint)] select-none">·</span>}
            {item}
          </React.Fragment>
        ))}
      </div>
    </motion.div>
  );
}

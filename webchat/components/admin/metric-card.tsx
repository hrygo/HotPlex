'use client';

import type { ReactNode } from 'react';

type MetricAccent = 'gold' | 'emerald' | 'amber' | 'muted';

const ACCENT_VALUE_CLASS: Record<MetricAccent, string> = {
  gold: 'text-[var(--accent-gold)]',
  emerald: 'text-[var(--accent-emerald)]',
  amber: 'text-[var(--accent-amber)]',
  muted: 'text-[var(--text-muted)]',
};

interface MetricCardProps {
  label: string;
  value: string | number;
  sub?: string;
  suffix?: string;
  accent?: MetricAccent;
  icon?: ReactNode;
  pulse?: boolean;
}

// MetricCard — L2 面板/指标卡（admin 卡片层级规范）。
// 朴素 bg-surface + shadow-sm + hover:border-bright；可选 accent/icon/pulse/suffix
// 覆盖 sessions 风格的强调指标卡，避免毛玻璃层级错位。
export function MetricCard({
  label,
  value,
  sub,
  suffix,
  accent,
  icon,
  pulse,
}: MetricCardProps) {
  return (
    <div className="relative overflow-hidden flex flex-col rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4 shadow-[var(--shadow-sm)] transition-colors hover:border-[var(--border-bright)]">
      {icon && (
        <div className="absolute top-0 right-0 p-3 opacity-10 pointer-events-none">
          {icon}
        </div>
      )}
      <div className="flex items-center gap-1.5">
        {pulse && (
          <span className="relative flex h-2 w-2">
            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-[var(--accent-emerald)] opacity-75" />
            <span className="relative inline-flex rounded-full h-2 w-2 bg-[var(--accent-emerald)]" />
          </span>
        )}
        <span className="text-[10px] font-bold uppercase tracking-wider text-[var(--text-faint)]">
          {label}
        </span>
      </div>
      <div className="mt-2 flex items-baseline gap-2">
        <span
          className={`text-2xl font-display font-extrabold ${
            accent ? ACCENT_VALUE_CLASS[accent] : 'text-[var(--text-primary)]'
          }`}
        >
          {value}
        </span>
        {suffix && (
          <span className="text-[10px] text-[var(--text-muted)] font-mono">
            {suffix}
          </span>
        )}
      </div>
      {sub && <p className="text-[11px] text-[var(--text-muted)] mt-1.5">{sub}</p>}
    </div>
  );
}

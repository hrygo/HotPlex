'use client';

interface MetricCardProps {
  label: string;
  value: string | number;
  sub?: string;
}

export function MetricCard({ label, value, sub }: MetricCardProps) {
  return (
    <div className="flex flex-col gap-1 rounded-[var(--radius-md)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] p-4">
      <span className="text-[10px] font-bold uppercase tracking-wider text-[var(--text-faint)]">
        {label}
      </span>
      <span className="text-2xl font-display font-bold text-[var(--text-primary)]">{value}</span>
      {sub && (
        <span className="text-[11px] text-[var(--text-muted)]">{sub}</span>
      )}
    </div>
  );
}

'use client';

interface StatusBadgeProps {
  status: string;
}

const STATUS_STYLES: Record<string, { bg: string; text: string; dot: string; label: string }> = {
  connected: {
    bg: 'rgba(52, 211, 153, 0.12)',
    text: 'text-[var(--accent-emerald)]',
    dot: 'bg-[var(--accent-emerald)]',
    label: 'Connected',
  },
  disconnected: {
    bg: 'rgba(244, 63, 94, 0.12)',
    text: 'text-[var(--accent-coral)]',
    dot: 'bg-[var(--accent-coral)]',
    label: 'Disconnected',
  },
  error: {
    bg: 'rgba(244, 63, 94, 0.15)',
    text: 'text-[var(--accent-coral)]',
    dot: 'bg-[var(--accent-coral)]',
    label: 'Error',
  },
};

const DEFAULT_STYLE = {
  bg: 'rgba(255, 255, 255, 0.06)',
  text: 'text-[var(--text-muted)]',
  dot: 'bg-[var(--text-muted)]',
  label: '',
};

export function StatusBadge({ status }: StatusBadgeProps) {
  const style = STATUS_STYLES[status] ?? DEFAULT_STYLE;
  const label = style.label || status;

  return (
    <span
      className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wider ${style.text}`}
      style={{ background: style.bg }}
    >
      <span className={`w-1.5 h-1.5 rounded-full ${style.dot}`} />
      {label}
    </span>
  );
}

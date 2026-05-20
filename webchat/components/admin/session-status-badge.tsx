const SESSION_STATUS_MAP: Record<string, { bg: string; text: string; dot: string; label: string }> = {
  active: {
    bg: 'rgba(52, 211, 153, 0.12)',
    text: 'text-[var(--accent-emerald)]',
    dot: 'bg-[var(--accent-emerald)]',
    label: 'Active',
  },
  working: {
    bg: 'rgba(52, 211, 153, 0.12)',
    text: 'text-[var(--accent-emerald)]',
    dot: 'bg-[var(--accent-emerald)]',
    label: 'Working',
  },
  idle: {
    bg: 'rgba(245, 158, 11, 0.12)',
    text: 'text-[var(--accent-amber)]',
    dot: 'bg-[var(--accent-amber)]',
    label: 'Idle',
  },
  terminated: {
    bg: 'rgba(161, 161, 170, 0.12)',
    text: 'text-[var(--text-muted)]',
    dot: 'bg-[var(--text-muted)]',
    label: 'Terminated',
  },
  error: {
    bg: 'rgba(244, 63, 94, 0.12)',
    text: 'text-[var(--accent-coral)]',
    dot: 'bg-[var(--accent-coral)]',
    label: 'Error',
  },
};

const DEFAULT_SESSION_STYLE = {
  bg: 'rgba(255, 255, 255, 0.06)',
  text: 'text-[var(--text-muted)]',
  dot: 'bg-[var(--text-muted)]',
  label: '',
};

export function SessionStatusBadge({ state }: { state: string }) {
  const style = SESSION_STATUS_MAP[state] ?? DEFAULT_SESSION_STYLE;
  const label = style.label || state;

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

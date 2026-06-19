'use client';

export interface StatusStyle {
  bg: string;
  text: string;
  dot: string;
  label: string;
}

export type StatusMap = Record<string, StatusStyle>;

const DEFAULT_STYLE: StatusStyle = {
  bg: 'rgba(255, 255, 255, 0.06)',
  text: 'text-[var(--text-muted)]',
  dot: 'bg-[var(--text-muted)]',
  label: '',
};

// Bot connection status (connected / disconnected / error).
const BOT_STATUS_MAP: StatusMap = {
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

// Session lifecycle status (running / created / idle / terminated / deleted / error).
export const SESSION_STATUS_MAP: StatusMap = {
  running: {
    bg: 'rgba(52, 211, 153, 0.12)',
    text: 'text-[var(--accent-emerald)]',
    dot: 'bg-[var(--accent-emerald)]',
    label: 'Running',
  },
  created: {
    bg: 'rgba(96, 165, 250, 0.12)',
    text: 'text-[var(--accent-blue)]',
    dot: 'bg-[var(--accent-blue)]',
    label: 'Created',
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
  deleted: {
    bg: 'rgba(161, 161, 170, 0.12)',
    text: 'text-[var(--text-muted)]',
    dot: 'bg-[var(--text-muted)]',
    label: 'Deleted',
  },
  error: {
    bg: 'rgba(244, 63, 94, 0.12)',
    text: 'text-[var(--accent-coral)]',
    dot: 'bg-[var(--accent-coral)]',
    label: 'Error',
  },
};

// Generic status badge. Defaults to the bot connection map; pass `map` for
// other status domains (e.g. SESSION_STATUS_MAP). Previously this and
// SessionStatusBadge were two near-identical components differing only in
// their status→style map (PR-1b consolidation).
export function StatusBadge({ status, map = BOT_STATUS_MAP }: { status: string; map?: StatusMap }) {
  const style = map[status] ?? DEFAULT_STYLE;
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

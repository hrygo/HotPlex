// Audit outcome → StatusBadge style map. Mirrors the shape of
// SESSION_STATUS_MAP / BOT_STATUS_MAP in status-badge.tsx so the timeline and
// the drawer can render outcomes via the shared <StatusBadge> component.
//
// Colors intentionally match the prior hardcoded outcomeClass() in the
// activity page (emerald=success, coral=failure, amber=denied) so the visual
// language is unchanged.
import type { StatusMap } from '@/components/admin/status-badge';

export const OUTCOME_STATUS_MAP: StatusMap = {
  success: {
    bg: 'rgba(52, 211, 153, 0.12)',
    text: 'text-[var(--accent-emerald)]',
    dot: 'bg-[var(--accent-emerald)]',
    label: 'Success',
  },
  failure: {
    bg: 'rgba(244, 63, 94, 0.12)',
    text: 'text-[var(--accent-coral)]',
    dot: 'bg-[var(--accent-coral)]',
    label: 'Failure',
  },
  denied: {
    bg: 'rgba(245, 158, 11, 0.12)',
    text: 'text-[var(--accent-amber)]',
    dot: 'bg-[var(--accent-amber)]',
    label: 'Denied',
  },
};

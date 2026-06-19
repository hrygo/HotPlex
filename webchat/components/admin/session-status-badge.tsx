import { StatusBadge, SESSION_STATUS_MAP } from './status-badge';

// Thin wrapper kept so session call sites stay unchanged
// (`<SessionStatusBadge state={x} />`). The rendering lives in the shared
// StatusBadge; only the status→style map differs.
export function SessionStatusBadge({ state }: { state: string }) {
  return <StatusBadge status={state} map={SESSION_STATUS_MAP} />;
}

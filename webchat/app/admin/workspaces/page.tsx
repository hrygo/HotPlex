'use client';

import { useEffect, useState } from 'react';
import {
  listAdminWorkspaces,
  updateAdminWorkspacePermissionMode,
} from '@/lib/api/admin-workspaces';
import type { AdminWorkspace } from '@/lib/types/admin';
import { useResource } from '@/hooks/use-resource';
import {
  LoadingState,
  ErrorState,
  EmptyState,
} from '@/components/admin/resource-states';
import { useAdminUI } from '@/context/admin-ui-context';

// 4 tiers + "" (clear override → config global default, which r3 made "workspace").
// Mirrors GeneralTab's set with an explicit "" entry for the admin console.
const PERMISSION_MODE_OPTIONS: { value: string; label: string }[] = [
  { value: '', label: 'Default (workspace)' },
  { value: 'workspace', label: 'Workspace' },
  { value: 'auto-edit', label: 'Auto Edit' },
  { value: 'read-only', label: 'Read Only' },
  { value: 'bypass', label: 'Bypass' },
];

const selectClass =
  'w-full rounded-[var(--radius-sm)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] px-3 py-1.5 text-xs text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] appearance-none cursor-pointer';

// WorkspaceRow renders one workspace with an inline permission_mode editor.
// Effect semantics (spec §3.2): the write only changes the stored row; running
// sessions keep their injected mode, new sessions pick up the new value —
// surfaced in the toast so the admin isn't surprised a live chat didn't change.
function WorkspaceRow({
  ws,
  onUpdated,
}: {
  ws: AdminWorkspace;
  onUpdated: () => void;
}) {
  const { showToast } = useAdminUI();
  const [mode, setMode] = useState(ws.permission_mode || '');
  const [saving, setSaving] = useState(false);

  // Resync when the row data changes after a list reload (PR #779 pattern).
  useEffect(() => {
    setMode(ws.permission_mode || '');
  }, [ws.permission_mode, ws.updated_at]);

  const dirty = mode !== (ws.permission_mode || '');

  const handleSave = async () => {
    setSaving(true);
    try {
      await updateAdminWorkspacePermissionMode(ws.id, mode);
      showToast('Updated — takes effect for new sessions.', 'success');
      onUpdated(); // refetch so updated_at + any reordering propagate
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Update failed.', 'error');
      setMode(ws.permission_mode || ''); // revert local selection
    } finally {
      setSaving(false);
    }
  };

  const owner =
    ws.owner_display_name || ws.owner_username
      ? `${ws.owner_display_name}${ws.owner_username ? ` (${ws.owner_username})` : ''}`
      : ws.owner_user_id; // orphaned owner fallback (LEFT JOIN collapsed to '')

  return (
    <div className="grid grid-cols-[1.4fr_1fr_1.6fr_1.3fr_auto] items-center gap-3 px-4 py-3 border-t border-[var(--border-subtle)] hover:bg-[var(--bg-hover)]/40 transition-colors">
      {/* Owner — primary identity, not the raw UUID */}
      <div className="min-w-0">
        <div className="text-sm text-[var(--text-primary)] truncate">{owner}</div>
        <div className="text-[10px] font-mono text-[var(--text-faint)] truncate">
          {ws.owner_user_id}
        </div>
      </div>

      {/* Workspace name */}
      <div className="text-sm text-[var(--text-secondary)] truncate" title={ws.name}>
        {ws.name}
      </div>

      {/* Work dir — truncated, full path on hover */}
      <div
        className="text-[11px] font-mono text-[var(--text-faint)] truncate"
        title={ws.work_dir}
      >
        {ws.work_dir}
      </div>

      {/* Permission mode inline editor */}
      <div className="relative">
        <select
          value={mode}
          onChange={(e) => setMode(e.target.value)}
          className={selectClass}
          disabled={saving}
        >
          {PERMISSION_MODE_OPTIONS.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </select>
        <div className="pointer-events-none absolute inset-y-0 right-2 flex items-center text-[var(--text-faint)]">
          <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
          </svg>
        </div>
      </div>

      {/* Save */}
      <button
        onClick={handleSave}
        disabled={!dirty || saving}
        className="px-3 py-1.5 rounded-[var(--radius-sm)] text-[11px] font-bold bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-colors disabled:opacity-30 disabled:cursor-not-allowed whitespace-nowrap"
      >
        {saving ? 'Saving…' : 'Save'}
      </button>
    </div>
  );
}

export default function AdminWorkspacesPage() {
  const { data, loading, error, reload } = useResource<AdminWorkspace[]>(
    () => listAdminWorkspaces(),
    [],
  );
  const list = data ?? [];

  return (
    <div className="min-h-screen bg-[var(--bg-base)] p-6">
      <div className="max-w-6xl mx-auto">
        {/* Header */}
        <div className="flex items-center justify-between mb-5">
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-display font-bold text-[var(--text-primary)]">
              Workspaces
            </h1>
            {!loading && !error && (
              <span className="text-[11px] font-mono text-[var(--text-faint)] px-2 py-0.5 rounded-full bg-[var(--bg-hover)]">
                {list.length}
              </span>
            )}
          </div>
          <button
            onClick={reload}
            disabled={loading}
            className="inline-flex items-center justify-center w-8 h-8 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] text-[var(--text-faint)] hover:text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] transition-colors disabled:opacity-50"
            title="Refresh"
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              className={loading ? 'animate-spin' : ''}
            >
              <path d="M21 2v6h-6" />
              <path d="M3 12a9 9 0 0 1 15-6.7L21 8" />
              <path d="M3 22v-6h6" />
              <path d="M21 12a9 9 0 0 1-15 6.7L3 16" />
            </svg>
          </button>
        </div>

        {/* Effect-semantics banner (spec §3.4) */}
        <div className="mb-5 flex items-start gap-2 px-4 py-3 rounded-[var(--radius-md)] bg-[rgba(245,158,11,0.06)] border border-[rgba(245,158,11,0.15)] text-xs text-[var(--text-secondary)]">
          <svg className="w-4 h-4 shrink-0 mt-0.5 text-[var(--accent-gold)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v3.75m9-.75a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9 3.75h.008v.008H12v-.008Z" />
          </svg>
          <span>
            Permission mode changes apply to <strong>new sessions only</strong>; running
            conversations keep their current mode. The user self-service endpoints
            (<code className="font-mono">/api/workspaces</code>) remain admin-gated for this
            field.
          </span>
        </div>

        {loading && <LoadingState label="Loading workspaces..." />}
        {error && <ErrorState message={error} onRetry={reload} />}

        {!loading && !error && list.length === 0 && (
          <EmptyState
            title="No workspaces"
            description="Workspaces appear here once users create them via the chat client."
          />
        )}

        {/* Table */}
        {!loading && !error && list.length > 0 && (
          <div className="rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] overflow-hidden">
            {/* Header row */}
            <div className="grid grid-cols-[1.4fr_1fr_1.6fr_1.3fr_auto] items-center gap-3 px-4 py-2.5 bg-[var(--bg-hover)]/60 border-b border-[var(--border-subtle)]">
              {['Owner', 'Workspace', 'Work Dir', 'Permission Mode', ''].map((h, i) => (
                <div
                  key={i}
                  className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest"
                >
                  {h}
                </div>
              ))}
            </div>
            {list.map((ws) => (
              <WorkspaceRow key={ws.id} ws={ws} onUpdated={reload} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

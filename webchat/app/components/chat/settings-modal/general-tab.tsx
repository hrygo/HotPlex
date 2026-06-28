'use client';

import { useState, useEffect, useRef } from 'react';
import { updateWorkspace, getWorkspace, type Workspace } from '@/lib/api/workspaces';
import { ApiError } from '@/lib/api/errors';
import { TabPanel } from './tab-panel';
import {
  resolveSandboxAnchor,
  sanitizeWorkspaceDir,
} from '@/lib/utils/workspace-path';

const WORKER_OPTIONS: { value: string; label: string }[] = [
  { value: '', label: 'Default (Inherit team settings)' },
  { value: 'claude_code', label: 'Claude Code' },
  { value: 'opencode_server', label: 'OpenCode Server' },
  { value: 'codex_cli', label: 'Codex CLI' },
  { value: 'acp', label: 'ACP (Any ACP-compatible Agent)' },
];

// Workspace permission tier → worker native mapping (issue #789). An empty
// server value normalizes to bypass (global default); the select exposes the 4
// tiers directly so the chosen blast radius is explicit, not inherited.
const PERMISSION_MODE_OPTIONS: { value: string; label: string }[] = [
  { value: 'bypass', label: 'Bypass — Full access (default)' },
  { value: 'auto-edit', label: 'Auto Edit — Auto-approve edits' },
  { value: 'workspace', label: 'Workspace — Edits within workspace' },
  { value: 'read-only', label: 'Read Only — Plan only' },
];

interface GeneralTabProps {
  workspace: Workspace;
  onUpdated?: (ws: Workspace) => void;
}

export function GeneralTab({ workspace, onUpdated }: GeneralTabProps) {
  // Anchor the sandbox by owner_id rather than a hard-coded ~/ prefix: backend
  // ExpandAndAbs stores work_dir as an absolute $HOME path, so the on-disk form
  // differs from the ~/ form used by the create flow. resolveSandboxAnchor reads
  // the real prefix straight off work_dir, so both forms resolve correctly.
  const anchor = resolveSandboxAnchor(workspace.work_dir, workspace.owner_user_id);
  const segEditable = anchor !== null;
  const prefix = anchor?.prefix ?? '';
  const segBaseline = anchor?.seg ?? '';

  const [name, setName] = useState(workspace.name);
  const [worker, setWorker] = useState(workspace.worker_preference || '');
  const [permMode, setPermMode] = useState(workspace.permission_mode || 'bypass');
  const [seg, setSeg] = useState(segBaseline);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);

  // Tracked so unmount clears the pending setState (PR #779 review P3-8).
  const successTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Resync when the workspace prop changes (e.g. after a parent re-fetch).
  useEffect(() => {
    setName(workspace.name);
    setWorker(workspace.worker_preference || '');
    setPermMode(workspace.permission_mode || 'bypass');
    const a = resolveSandboxAnchor(workspace.work_dir, workspace.owner_user_id);
    setSeg(a?.seg ?? workspace.work_dir);
    setError(null);
    setSuccess(false);
  }, [workspace.id, workspace.name, workspace.worker_preference, workspace.permission_mode, workspace.work_dir, workspace.owner_user_id, workspace.updated_at]);

  useEffect(() => () => {
    if (successTimer.current) clearTimeout(successTimer.current);
  }, []);

  const dirty =
    (name.trim() !== workspace.name && name.trim().length > 0) ||
    worker !== (workspace.worker_preference || '') ||
    permMode !== (workspace.permission_mode || 'bypass') ||
    (segEditable && seg.trim() !== segBaseline);

  const previewSeg = segEditable ? sanitizeWorkspaceDir(seg) : '';
  const previewPath = segEditable
    ? previewSeg
      ? `${prefix}${previewSeg}`
      : prefix.replace(/\/$/, '')
    : workspace.work_dir;

  const handleSave = async () => {
    setSaving(true);
    setError(null);
    setSuccess(false);
    try {
      // seg is only editable when it resolves inside the sandbox prefix, so
      // rejoining prefix + sanitized seg always yields a sandbox-conformant
      // work_dir (backend ValidateWorkspaceWorkDir accepts it).
      const workDir = segEditable ? `${prefix}${sanitizeWorkspaceDir(seg)}` : workspace.work_dir;
      const updated = await updateWorkspace(workspace.id, {
        name: name.trim(),
        workerPreference: worker,
        permissionMode: permMode,
        workDir,
      });
      onUpdated?.(updated);
      setSuccess(true);
      if (successTimer.current) clearTimeout(successTimer.current);
      successTimer.current = setTimeout(() => setSuccess(false), 2500);
    } catch (err) {
      // CAS 409 (PR #779 review P2-2): re-fetch latest and repopulate so the
      // form does not sit on a stale updated_at and 409 again on retry.
      if (err instanceof ApiError && err.status === 409) {
        setError('Workspace was modified elsewhere — refreshed to latest, please retry.');
        try {
          onUpdated?.(await getWorkspace(workspace.id));
        } catch {
          // re-fetch failed; keep the 409 message above
        }
      } else {
        setError(err instanceof Error ? err.message : 'Save failed');
      }
    } finally {
      setSaving(false);
    }
  };

  return (
    <TabPanel>
      {/* Workspace Name Input Field */}
      <div>
        <label className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest block mb-2">
          Workspace Name
        </label>
        <input
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="w-full px-3.5 py-2.5 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-default)] text-sm text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)]/20 transition-all placeholder:text-[var(--text-faint)]"
          placeholder="Enter workspace name"
        />
        <p className="text-[10px] text-[var(--text-muted)] mt-1.5 leading-relaxed">
          The display name for your workspace, used inside the workspace switcher and header.
        </p>
      </div>

      {/* Worker Engine Preference Selection */}
      <div>
        <label className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest block mb-2">
          Worker Engine Preference
        </label>
        <div className="relative">
          <select
            value={worker}
            onChange={(e) => setWorker(e.target.value)}
            className="w-full pl-3.5 pr-10 py-2.5 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-default)] text-sm text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)]/20 transition-all appearance-none cursor-pointer"
          >
            {WORKER_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
          <div className="pointer-events-none absolute inset-y-0 right-0 flex items-center px-3.5 text-[var(--text-faint)]">
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
            </svg>
          </div>
        </div>
        <p className="text-[10px] text-[var(--text-muted)] mt-1.5 leading-relaxed">
          Select which agent binary is used to power new sessions in this workspace. Existing sessions keep their current engines.
        </p>
      </div>

      {/* Permission Mode Selection (issue #789) */}
      <div>
        <label className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest block mb-2">
          Permission Mode
        </label>
        <div className="relative">
          <select
            value={permMode}
            onChange={(e) => setPermMode(e.target.value)}
            className="w-full pl-3.5 pr-10 py-2.5 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-default)] text-sm text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)]/20 transition-all appearance-none cursor-pointer"
          >
            {PERMISSION_MODE_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
          <div className="pointer-events-none absolute inset-y-0 right-0 flex items-center px-3.5 text-[var(--text-faint)]">
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
            </svg>
          </div>
        </div>
        <p className="text-[10px] text-[var(--text-muted)] mt-1.5 leading-relaxed">
          Controls the blast radius for new sessions: how aggressively the agent may edit files or run commands. Applies to new sessions only; existing sessions keep their current mode.
        </p>
      </div>

      {/* Working Directory: sandbox prefix (read-only) + editable segment */}
      <div>
        <label className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest block mb-2">
          Working Directory
        </label>
        {segEditable ? (
          <>
            <div className="flex flex-col gap-1.5">
              {/* Read-only sandbox prefix */}
              <div className="px-3.5 py-2 rounded-t-[var(--radius-md)] bg-[var(--bg-surface)] border border-b-0 border-[var(--border-subtle)] text-xs text-[var(--text-faint)] font-mono truncate select-all" title={prefix}>
                {prefix}
              </div>
              {/* Editable last segment */}
              <input
                type="text"
                value={seg}
                onChange={(e) => setSeg(e.target.value)}
                className="w-full px-3.5 py-2.5 rounded-b-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-default)] text-sm text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)]/20 transition-all placeholder:text-[var(--text-faint)] font-mono"
                placeholder="my-project"
                autoCorrect="off"
                autoCapitalize="off"
                spellCheck={false}
              />
            </div>
            {/* Live preview of the rejoined absolute path */}
            <p className="text-[10px] font-mono text-[var(--text-faint)] mt-1.5 break-all leading-relaxed">
              Path: <span className="text-[var(--text-secondary)]">{previewPath}</span>
            </p>
          </>
        ) : (
          <>
            <div className="px-3.5 py-2.5 rounded-[var(--radius-md)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] text-sm text-[var(--text-faint)] font-mono truncate select-all" title={workspace.work_dir}>
              {workspace.work_dir}
            </div>
            <p className="text-[10px] text-[var(--accent-coral)] mt-1.5 leading-relaxed font-bold">
              This workspace uses a non-standard path outside the owner sandbox and can’t be edited here.
            </p>
          </>
        )}
        <p className="text-[10px] text-[var(--text-muted)] mt-1 leading-relaxed">
          The local directory where CLI agents read, write, and execute files. Session-level runs inherit this setting and cannot change it per session.
        </p>
      </div>

      {/* Notifications */}
      {error && (
        <div className="flex gap-2 px-4 py-3 rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] text-xs text-[var(--accent-coral)] font-bold items-start animate-fade-in-up">
          <svg className="w-4 h-4 shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
          </svg>
          <span className="break-words">{error}</span>
        </div>
      )}

      {success && (
        <div className="flex gap-2 px-4 py-3 rounded-[var(--radius-md)] bg-[rgba(52,211,153,0.08)] border border-[rgba(52,211,153,0.15)] text-xs text-[var(--accent-emerald)] font-bold items-start animate-fade-in-up">
          <svg className="w-4 h-4 shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
          </svg>
          <span>Workspace configuration saved successfully.</span>
        </div>
      )}

      {/* Save Button Action */}
      <div className="flex justify-end pt-2">
        <button
          onClick={handleSave}
          disabled={!dirty || saving}
          className="px-5 py-2.5 rounded-[var(--radius-md)] bg-[var(--accent-gold)] text-black text-xs font-bold transition-all hover:bg-[var(--accent-gold-bright)] active:scale-[0.98] disabled:opacity-40 disabled:cursor-not-allowed flex items-center gap-1.5 shadow-sm hover:shadow-glow cursor-pointer"
        >
          {saving ? (
            <>
              <svg className="w-3.5 h-3.5 animate-spin" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <circle className="opacity-25" cx={12} cy={12} r={10} stroke="currentColor" strokeWidth={4} />
                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
              </svg>
              Saving Changes…
            </>
          ) : (
            'Save Changes'
          )}
        </button>
      </div>
    </TabPanel>
  );
}

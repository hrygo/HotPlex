'use client';

import { useState, useEffect, useRef } from 'react';
import { updateWorkspace, getWorkspace, type Workspace } from '@/lib/api/workspaces';
import { ApiError } from '@/lib/api/errors';

const WORKER_OPTIONS: { value: string; label: string }[] = [
  { value: '', label: 'Default (Inherit team settings)' },
  { value: 'claude_code', label: 'Claude Code' },
  { value: 'opencode_server', label: 'OpenCode Server' },
  { value: 'codex_cli', label: 'Codex CLI' },
  { value: 'acp', label: 'ACP (Any ACP-compatible Agent)' },
];

interface GeneralTabProps {
  workspace: Workspace;
  onUpdated?: (ws: Workspace) => void;
}

export function GeneralTab({ workspace, onUpdated }: GeneralTabProps) {
  const [name, setName] = useState(workspace.name);
  const [worker, setWorker] = useState(workspace.worker_preference || '');
  const [workDir, setWorkDir] = useState(workspace.work_dir);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  
  // Tracked so unmount clears the pending setState (PR #779 review P3-8).
  const successTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Resync when the workspace prop changes (e.g. after a parent re-fetch).
  useEffect(() => {
    setName(workspace.name);
    setWorker(workspace.worker_preference || '');
    setWorkDir(workspace.work_dir);
    setError(null);
    setSuccess(false);
  }, [workspace.id, workspace.name, workspace.worker_preference, workspace.work_dir, workspace.updated_at]);

  useEffect(() => () => {
    if (successTimer.current) clearTimeout(successTimer.current);
  }, []);

  const dirty = 
    (name.trim() !== workspace.name && name.trim().length > 0) || 
    worker !== (workspace.worker_preference || '') ||
    (workDir.trim() !== workspace.work_dir && workDir.trim().length > 0);

  const handleSave = async () => {
    setSaving(true);
    setError(null);
    setSuccess(false);
    try {
      const updated = await updateWorkspace(workspace.id, { 
        name: name.trim(),
        workerPreference: worker,
        workDir: workDir.trim(),
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
    <div className="space-y-6">
      {/* Workspace Name Input Field */}
      <div>
        <label className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest block mb-2">
          Workspace Name
        </label>
        <div className="relative group">
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="w-full px-3.5 py-2.5 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-default)] text-sm text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)]/20 transition-all placeholder:text-[var(--text-faint)]"
            placeholder="Enter workspace name"
          />
        </div>
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

      {/* Working Directory Input Field (Now Mutable) */}
      <div>
        <label className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest block mb-2">
          Working Directory
        </label>
        <div className="relative group">
          <input
            type="text"
            value={workDir}
            onChange={(e) => setWorkDir(e.target.value)}
            className="w-full px-3.5 py-2.5 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-default)] text-sm text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)]/20 transition-all placeholder:text-[var(--text-faint)] font-mono"
            placeholder="Enter working directory absolute path"
          />
        </div>
        <p className="text-[10px] text-[var(--text-muted)] mt-1.5 leading-relaxed">
          The local directory where the CLI agents read, write, and execute files. Session-level runs automatically inherit this setting and cannot be changed dynamically per session.
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
    </div>
  );
}

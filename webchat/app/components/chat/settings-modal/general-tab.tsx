'use client';

import { useState, useEffect, useRef } from 'react';
import { updateWorkspace, getWorkspace, type Workspace } from '@/lib/api/workspaces';
import { ApiError } from '@/lib/api/errors';

interface GeneralTabProps {
  workspace: Workspace;
  onUpdated?: (ws: Workspace) => void;
}

export function GeneralTab({ workspace, onUpdated }: GeneralTabProps) {
  const [name, setName] = useState(workspace.name);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  // Tracked so unmount clears the pending setState (PR #779 review P3-8).
  const successTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Resync when the workspace prop changes (e.g. after a parent re-fetch).
  useEffect(() => {
    setName(workspace.name);
    setError(null);
    setSuccess(false);
  }, [workspace.id, workspace.name, workspace.updated_at]);

  useEffect(() => () => {
    if (successTimer.current) clearTimeout(successTimer.current);
  }, []);

  const dirty = name.trim() !== workspace.name && name.trim().length > 0;

  const handleSave = async () => {
    setSaving(true);
    setError(null);
    setSuccess(false);
    try {
      const updated = await updateWorkspace(workspace.id, { name: name.trim() });
      onUpdated?.(updated);
      setSuccess(true);
      if (successTimer.current) clearTimeout(successTimer.current);
      successTimer.current = setTimeout(() => setSuccess(false), 2000);
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
    <div className="space-y-5">
      <div>
        <label className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest block mb-2">
          Workspace Name
        </label>
        <input
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="w-full px-3 py-2.5 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-default)] text-sm text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-2 focus:ring-[rgba(251,191,36,0.1)] transition-all"
        />
      </div>

      <div>
        <label className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest block mb-2">
          Working Directory <span className="normal-case text-[var(--text-faint)]">(immutable)</span>
        </label>
        <div className="px-3 py-2.5 rounded-[var(--radius-md)] bg-[var(--bg-base)] border border-[var(--border-subtle)] text-sm text-[var(--text-muted)] font-mono break-all">
          {workspace.work_dir}
        </div>
      </div>

      {error && (
        <div className="px-3 py-2 rounded-[var(--radius-md)] bg-[var(--accent-coral)]/10 border border-[var(--accent-coral)]/20 text-xs text-[var(--accent-coral)] font-bold break-words">
          {error}
        </div>
      )}
      {success && (
        <div className="px-3 py-2 rounded-[var(--radius-md)] bg-[var(--accent-emerald)]/10 border border-[var(--accent-emerald)]/20 text-xs text-[var(--accent-emerald)] font-bold">
          Saved
        </div>
      )}

      <div className="flex justify-end">
        <button
          onClick={handleSave}
          disabled={!dirty || saving}
          className="px-5 py-2 rounded-[var(--radius-md)] bg-[var(--accent-gold)] text-black text-xs font-bold transition-all hover:bg-[var(--accent-gold-bright)] active:scale-[0.98] disabled:opacity-40 disabled:cursor-not-allowed"
        >
          {saving ? 'Saving…' : 'Save Changes'}
        </button>
      </div>
    </div>
  );
}

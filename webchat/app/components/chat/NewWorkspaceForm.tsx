'use client';

import { useState } from 'react';
import { createWorkspace, type Workspace } from '@/lib/api/workspaces';
import { createSession, ANCHOR_CLIENT_SESSION_ID } from '@/lib/api/sessions';
import { workerType } from '@/lib/config';
import { buildWorkspaceWorkDir } from '@/lib/utils/workspace-path';

interface NewWorkspaceFormProps {
  uid: string;
  onCreated: (ws: Workspace) => void;
  onCancel?: () => void;
}

const inputClass =
  'w-full rounded-[var(--radius-sm)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] px-3 py-2 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-faint)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors font-mono';

const labelClass =
  'block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5';

export function NewWorkspaceForm({ uid, onCreated, onCancel }: NewWorkspaceFormProps) {
  const [name, setName] = useState('');
  const [subdir, setSubdir] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const preview = name.trim()
    ? buildWorkspaceWorkDir(uid, name, subdir)
    : `~/.hotplex/workspaces/${uid}/…`;

  async function submit() {
    if (!name.trim() || submitting) return;
    setSubmitting(true);
    setError(null);
    try {
      const ws = await createWorkspace(name.trim(), buildWorkspaceWorkDir(uid, name, subdir));
      // Pre-create the anchor session so the new workspace is immediately
      // usable on switch (listSessions returns it, no empty-state window).
      // Best-effort + idempotent: useSessions retries on switch, and
      // DeriveSessionKey maps (clientSessionId, ws, workDir) to one session.
      try {
        await createSession({
          clientSessionId: ANCHOR_CLIENT_SESSION_ID,
          workerType,
          title: ANCHOR_CLIENT_SESSION_ID,
          workspaceId: ws.id,
        });
      } catch {
        // Swallow — useSessions will (re)create the anchor session on switch.
      }
      onCreated(ws);
      setName('');
      setSubdir('');
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
      className="space-y-3"
    >
      <div>
        <label className={labelClass}>Workspace Name</label>
        <input
          className={inputClass}
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="My Project"
          disabled={submitting}
          autoFocus
        />
      </div>
      <div>
        <label className={labelClass}>Directory (optional)</label>
        <input
          className={inputClass}
          value={subdir}
          onChange={(e) => setSubdir(e.target.value)}
          placeholder="Leave empty to use name"
          disabled={submitting}
        />
      </div>
      <div className="text-[10px] font-mono text-[var(--text-faint)] break-all">
        Path: {preview}
      </div>
      {error && <div className="text-xs text-[var(--accent-coral)]">{error}</div>}
      <div className="flex items-center gap-2">
        <button
          type="submit"
          className="px-3 py-1.5 rounded-[var(--radius-sm)] bg-[var(--accent-gold)] text-black text-sm font-bold transition-opacity hover:opacity-90 disabled:opacity-50"
          disabled={!name.trim() || submitting}
        >
          {submitting ? 'Creating…' : 'Create Workspace'}
        </button>
        {onCancel && (
          <button
            type="button"
            onClick={onCancel}
            className="px-3 py-1.5 rounded-[var(--radius-sm)] text-sm text-[var(--text-muted)] hover:text-[var(--text-primary)] disabled:opacity-50"
            disabled={submitting}
          >
            Cancel
          </button>
        )}
      </div>
    </form>
  );
}

'use client';

import { useState, useEffect } from 'react';
import { updateWorkspace, getWorkspace, type Workspace } from '@/lib/api/workspaces';
import { AgentConfigEditor } from '@/components/admin/agent-config-editor';

const WORKER_OPTIONS: { value: string; label: string }[] = [
  { value: '', label: 'Default (inherit team setting)' },
  { value: 'claude_code', label: 'Claude Code' },
  { value: 'opencode_server', label: 'OpenCode Server' },
  { value: 'codex_cli', label: 'Codex CLI' },
  { value: 'acp', label: 'ACP' },
];

interface AIConfigTabProps {
  workspace: Workspace;
  onUpdated?: (ws: Workspace) => void;
}

export function AIConfigTab({ workspace, onUpdated }: AIConfigTabProps) {
  const [worker, setWorker] = useState(workspace.worker_preference || '');
  const [savingWorker, setSavingWorker] = useState(false);
  const [workerError, setWorkerError] = useState<string | null>(null);
  const [workerSaved, setWorkerSaved] = useState(false);

  useEffect(() => {
    setWorker(workspace.worker_preference || '');
    setWorkerError(null);
    setWorkerSaved(false);
  }, [workspace.id, workspace.worker_preference, workspace.updated_at]);

  const dirty = worker !== (workspace.worker_preference || '');

  const handleSaveWorker = async () => {
    setSavingWorker(true);
    setWorkerError(null);
    setWorkerSaved(false);
    try {
      const updated = await updateWorkspace(workspace.id, { workerPreference: worker });
      onUpdated?.(updated);
      setWorkerSaved(true);
      setTimeout(() => setWorkerSaved(false), 2000);
    } catch (err) {
      if ((err as { status?: number }).status === 409) {
        setWorkerError('Workspace was modified elsewhere — refreshed to latest, please retry.');
        try {
          onUpdated?.(await getWorkspace(workspace.id));
        } catch {
          // re-fetch failed; keep the 409 message above
        }
      } else {
        setWorkerError(err instanceof Error ? err.message : 'Save failed');
      }
    } finally {
      setSavingWorker(false);
    }
  };

  return (
    <div className="space-y-6">
      {/* Worker preference */}
      <div>
        <label className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest block mb-2">
          Worker Engine
        </label>
        <p className="text-xs text-[var(--text-muted)] mb-2">
          Selects which worker binary new sessions use. Existing sessions keep their worker.
        </p>
        <div className="flex gap-2">
          <select
            value={worker}
            onChange={(e) => setWorker(e.target.value)}
            className="flex-1 px-3 py-2.5 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-default)] text-sm text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-2 focus:ring-[rgba(251,191,36,0.1)] transition-all"
          >
            {WORKER_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>{opt.label}</option>
            ))}
          </select>
          <button
            onClick={handleSaveWorker}
            disabled={!dirty || savingWorker}
            className="px-4 py-2 rounded-[var(--radius-md)] bg-[var(--accent-gold)] text-black text-xs font-bold transition-all hover:bg-[var(--accent-gold-bright)] active:scale-[0.98] disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {savingWorker ? 'Saving…' : 'Save'}
          </button>
        </div>
        {workerError && <p className="mt-2 text-xs text-[var(--accent-coral)] font-bold break-words">{workerError}</p>}
        {workerSaved && <p className="mt-2 text-xs text-[var(--accent-emerald)] font-bold">Saved</p>}
      </div>

      {/* Agent config overrides — reuse admin AgentConfigEditor */}
      <div>
        <label className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest block mb-2">
          Agent Config Overrides
        </label>
        <p className="text-xs text-[var(--text-muted)] mb-3">
          Per-workspace customization (SOUL.md / AGENTS.md / SKILLS.md / USER.md / MEMORY.md). Blank entries inherit the team default.
        </p>
        <AgentConfigEditor workspaceId={workspace.id} overrides={workspace.agent_config_overrides || {}} />
      </div>
    </div>
  );
}

'use client';

import { useState, useEffect } from 'react';
import Link from 'next/link';
import { useRouter, useSearchParams } from 'next/navigation';
import { getWorkspace, updateWorkspace, deleteWorkspace } from '@/lib/api/workspaces';
import type { Workspace } from '@/lib/api/workspaces';
import { AgentConfigEditor } from '@/components/admin/agent-config-editor';

type WorkerType = 'claude_code' | 'opencode_server' | 'codex_cli' | 'acp';

const selectClass =
  'w-full rounded-[var(--radius-sm)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] px-3 py-2 text-sm text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors appearance-none';

const inputClass =
  'w-full rounded-[var(--radius-sm)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] px-3 py-2 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-faint)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors font-mono';

function formatDate(ms: number): string {
  if (!ms) return '—';
  try {
    return new Date(ms * 1000).toLocaleString();
  } catch {
    return '—';
  }
}

// ---------------------------------------------------------------------------
// GeneralTab — name (editable) + immutable fields + delete
// ---------------------------------------------------------------------------

function GeneralTab({
  workspace,
  onChanged,
}: {
  workspace: Workspace;
  onChanged: (w: Workspace) => void;
}) {
  const [name, setName] = useState(workspace.name);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);
  const dirty = name.trim() !== workspace.name && name.trim().length > 0;

  const handleSave = async () => {
    setSaving(true);
    setMessage(null);
    try {
      const updated = await updateWorkspace(workspace.id, { name: name.trim() });
      onChanged(updated);
      setName(updated.name);
      setMessage({ type: 'success', text: 'Saved.' });
      setTimeout(() => setMessage(null), 3000);
    } catch (err) {
      setMessage({ type: 'error', text: err instanceof Error ? err.message : 'Failed to update' });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div>
      <div className="space-y-4">
        <div>
          <label className="block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5">
            Name
          </label>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className={inputClass}
          />
        </div>

        <div className="grid grid-cols-1 gap-4">
          <div>
            <label className="block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5">
              Workspace ID
            </label>
            <div className="px-4 py-3 rounded-xl bg-[var(--bg-surface)] border border-[var(--border-subtle)]">
              <p className="text-sm text-[var(--text-primary)] font-mono break-all">{workspace.id}</p>
            </div>
          </div>

          <div>
            <label className="block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5">
              Work Directory <span className="normal-case text-[var(--text-faint)]">(immutable)</span>
            </label>
            <div className="px-4 py-3 rounded-xl bg-[var(--bg-surface)] border border-[var(--border-subtle)]">
              <p className="text-sm text-[var(--text-primary)] font-mono break-all">{workspace.work_dir || '—'}</p>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5">
                Owner
              </label>
              <div className="px-4 py-3 rounded-xl bg-[var(--bg-surface)] border border-[var(--border-subtle)]">
                <p className="text-sm text-[var(--text-primary)] font-mono break-all">{workspace.owner_user_id || '—'}</p>
              </div>
            </div>
            <div>
              <label className="block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5">
                Created
              </label>
              <div className="px-4 py-3 rounded-xl bg-[var(--bg-surface)] border border-[var(--border-subtle)]">
                <p className="text-sm text-[var(--text-primary)]">{formatDate(workspace.created_at)}</p>
              </div>
            </div>
          </div>
        </div>
      </div>

      {message && (
        <div
          className={`mt-4 px-4 py-3 rounded-[var(--radius-md)] text-xs ${
            message.type === 'success'
              ? 'bg-[var(--accent-emerald-glow)] text-[var(--accent-emerald)]'
              : 'bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] text-[var(--accent-coral)]'
          }`}
        >
          {message.text}
        </div>
      )}

      <div className="flex items-center justify-end mt-6 pt-6 border-t border-[var(--border-subtle)]">
        <button
          onClick={handleSave}
          disabled={saving || !dirty}
          className="px-4 py-2 rounded-[var(--radius-sm)] text-xs font-bold uppercase tracking-wider bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
        >
          {saving ? 'Saving...' : 'Save Changes'}
        </button>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// DeleteButton — two-step confirm
// ---------------------------------------------------------------------------

function DeleteButton({ workspaceId, workspaceName }: { workspaceId: string; workspaceName: string }) {
  const router = useRouter();
  const [confirming, setConfirming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleDelete = async () => {
    setDeleting(true);
    setError(null);
    try {
      await deleteWorkspace(workspaceId);
      router.push('/admin/workspaces');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete workspace');
      setDeleting(false);
    }
  };

  if (confirming) {
    return (
      <div className="mt-6 pt-6 border-t border-[var(--border-subtle)]">
        <div className="rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.06)] border border-[rgba(244,63,94,0.12)] p-4">
          <p className="text-sm text-[var(--accent-coral)] font-medium mb-3">
            Delete &ldquo;{workspaceName}&rdquo;? This action cannot be undone.
          </p>
          {error && <p className="text-xs text-[var(--accent-coral)] mb-3">{error}</p>}
          <div className="flex items-center gap-2">
            <button
              onClick={handleDelete}
              disabled={deleting}
              className="px-3 py-1.5 rounded-[var(--radius-sm)] text-xs font-bold bg-[var(--accent-coral)] text-white hover:opacity-90 transition-opacity disabled:opacity-50"
            >
              {deleting ? 'Deleting...' : 'Yes, Delete'}
            </button>
            <button
              onClick={() => { setConfirming(false); setError(null); }}
              className="px-3 py-1.5 rounded-[var(--radius-sm)] text-xs font-semibold text-[var(--text-faint)] hover:text-[var(--text-secondary)] transition-colors"
            >
              Cancel
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="mt-6 pt-6 border-t border-[var(--border-subtle)]">
      <button
        onClick={() => setConfirming(true)}
        className="px-3 py-1.5 rounded-[var(--radius-sm)] text-xs font-semibold text-[var(--accent-coral)] border border-[rgba(244,63,94,0.2)] hover:bg-[rgba(244,63,94,0.06)] transition-colors"
      >
        Delete Workspace
      </button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// WorkerTab — worker preference
// ---------------------------------------------------------------------------

function WorkerTab({ workspace }: { workspace: Workspace }) {
  const [pref, setPref] = useState<WorkerType>((workspace.worker_preference as WorkerType) || 'claude_code');
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);
  const dirty = pref !== (workspace.worker_preference || 'claude_code');

  const handleSave = async () => {
    setSaving(true);
    setMessage(null);
    try {
      await updateWorkspace(workspace.id, { workerPreference: pref });
      setMessage({ type: 'success', text: 'Worker preference updated.' });
      setTimeout(() => setMessage(null), 3000);
    } catch (err) {
      setMessage({ type: 'error', text: err instanceof Error ? err.message : 'Failed to update' });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div>
      <div className="space-y-4">
        <div>
          <label className="block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5">
            Worker Type
          </label>
          <select value={pref} onChange={(e) => setPref(e.target.value as WorkerType)} className={selectClass}>
            <option value="claude_code">claude_code</option>
            <option value="opencode_server">opencode_server</option>
            <option value="codex_cli">codex_cli</option>
            <option value="acp">acp (ACP)</option>
          </select>
          <p className="mt-2 text-[11px] text-[var(--text-faint)]">
            Preferred worker for new sessions in this workspace. Falls back to the platform default when unset.
          </p>
        </div>
      </div>

      {message && (
        <div
          className={`mt-4 px-4 py-3 rounded-[var(--radius-md)] text-xs ${
            message.type === 'success'
              ? 'bg-[var(--accent-emerald-glow)] text-[var(--accent-emerald)]'
              : 'bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] text-[var(--accent-coral)]'
          }`}
        >
          {message.text}
        </div>
      )}

      <div className="flex items-center justify-end mt-6 pt-6 border-t border-[var(--border-subtle)]">
        <button
          onClick={handleSave}
          disabled={saving || !dirty}
          className="px-4 py-2 rounded-[var(--radius-sm)] text-xs font-bold uppercase tracking-wider bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
        >
          {saving ? 'Saving...' : 'Save Changes'}
        </button>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

type TabKey = 'general' | 'worker' | 'agent-configs';

const TABS: { key: TabKey; label: string }[] = [
  { key: 'general', label: 'General' },
  { key: 'worker', label: 'Worker' },
  { key: 'agent-configs', label: 'Agent Configs' },
];

export function WorkspaceDetailView() {
  const searchParams = useSearchParams();
  const id = searchParams.get('id') ?? '';

  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<TabKey>('general');

  useEffect(() => {
    if (!id) return;
    let cancelled = false;
    setLoading(true);
    getWorkspace(id)
      .then((data) => {
        if (!cancelled) setWorkspace(data);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(String(err));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => { cancelled = true; };
  }, [id]);

  if (!id) {
    return (
      <div className="flex items-center justify-center min-h-[60vh]">
        <p className="text-sm text-[var(--text-faint)]">No workspace ID specified</p>
      </div>
    );
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-[60vh]">
        <div className="flex flex-col items-center gap-3">
          <div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
          <span className="text-xs text-[var(--text-faint)]">Loading workspace...</span>
        </div>
      </div>
    );
  }

  if (error || !workspace) {
    return (
      <div className="flex flex-col items-center justify-center min-h-[60vh] gap-4">
        <div className="rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] p-4">
          <p className="text-sm text-[var(--accent-coral)]">{error || 'Workspace not found'}</p>
        </div>
        <Link href="/admin/workspaces" className="text-xs text-[var(--accent-gold)] hover:underline">
          Back to Workspaces
        </Link>
      </div>
    );
  }

  return (
    <div className="max-w-5xl mx-auto px-6 py-8">
      {/* Breadcrumb */}
      <div className="flex items-center gap-2 mb-6 text-xs text-[var(--text-faint)]">
        <Link
          href="/admin/workspaces"
          className="hover:text-[var(--text-secondary)] transition-colors flex items-center gap-1"
        >
          <svg width="12" height="12" viewBox="0 0 16 16" fill="none">
            <path d="M10 3L5 8l5 5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
          Workspaces
        </Link>
        <span className="text-[var(--border-subtle)]">/</span>
        <span className="text-[var(--text-secondary)]">{workspace.name}</span>
      </div>

      {/* Header */}
      <div className="flex items-center gap-3 mb-6">
        <h1 className="text-xl font-display font-bold text-[var(--text-primary)]">{workspace.name}</h1>
        {workspace.worker_preference && (
          <span className="px-2 py-0.5 rounded-full text-[10px] font-mono font-semibold bg-[var(--bg-elevated)] text-[var(--text-secondary)] border border-[var(--border-subtle)] uppercase">
            {workspace.worker_preference}
          </span>
        )}
      </div>

      {/* Tabs */}
      <div className="flex gap-1 mb-6 border-b border-[var(--border-subtle)]">
        {TABS.map((tab) => (
          <button
            key={tab.key}
            onClick={() => setActiveTab(tab.key)}
            className={`px-4 py-2.5 text-xs font-semibold transition-colors border-b-2 -mb-px ${
              activeTab === tab.key
                ? 'border-[var(--accent-gold)] text-[var(--accent-gold)]'
                : 'border-transparent text-[var(--text-faint)] hover:text-[var(--text-secondary)]'
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* Tab content */}
      {activeTab === 'general' && (
        <div>
          <GeneralTab workspace={workspace} onChanged={setWorkspace} />
          <DeleteButton workspaceId={workspace.id} workspaceName={workspace.name} />
        </div>
      )}

      {activeTab === 'worker' && <WorkerTab workspace={workspace} />}

      {activeTab === 'agent-configs' && (
        <AgentConfigEditor
          key={workspace.id}
          workspaceId={workspace.id}
          overrides={workspace.agent_config_overrides ?? {}}
        />
      )}
    </div>
  );
}

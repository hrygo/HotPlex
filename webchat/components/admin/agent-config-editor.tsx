'use client';

import { useState, useCallback, useEffect } from 'react';
import { updateWorkspace } from '@/lib/api/workspaces';
import { MarkdownText } from '@/components/assistant-ui/MarkdownText';
import { AgentConfigFileList, CONFIG_FILES } from './agent-config-file-list';

const MAX_FILE_CHARS = 8000;

type OverridesMap = Record<string, string>;
type ViewMode = 'edit' | 'split' | 'preview';

// Empty/whitespace-only values are dropped so the override map only carries
// real overrides — an absent key means "inherit team default" (matches the
// backend ValidateOverrides semantics and keeps the override badge honest).
function removeEmpty(map: OverridesMap): OverridesMap {
  const out: OverridesMap = {};
  for (const [k, v] of Object.entries(map)) {
    if (v && v.trim()) out[k] = v;
  }
  return out;
}

function overridesEqual(a: OverridesMap, b: OverridesMap): boolean {
  const ca = removeEmpty(a);
  const cb = removeEmpty(b);
  const ka = Object.keys(ca);
  const kb = Object.keys(cb);
  if (ka.length !== kb.length) return false;
  return ka.every((k) => ca[k] === cb[k]);
}

export function AgentConfigEditor({
  workspaceId,
  overrides,
}: {
  workspaceId: string;
  overrides: OverridesMap;
}) {
  const [map, setMap] = useState<OverridesMap>(overrides ?? {});
  const [saved, setSaved] = useState<OverridesMap>(overrides ?? {});
  const [activeKey, setActiveKey] = useState<string>('soul');
  const [viewMode, setViewMode] = useState<ViewMode>('edit');
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

  const activeDef = CONFIG_FILES.find((f) => f.key === activeKey) ?? CONFIG_FILES[0];
  const content = map[activeDef.file] ?? '';
  const dirty = !overridesEqual(map, saved);
  const charCount = content.length;
  const charWarning = charCount > MAX_FILE_CHARS;
  const hasOverride = !!(content && content.trim());

  const setContent = useCallback(
    (v: string) => {
      setMap((prev) => ({ ...prev, [activeDef.file]: v }));
    },
    [activeDef.file],
  );

  const handleSwitchFile = (key: string) => {
    if (key === activeKey) return;
    if (dirty) {
      if (!window.confirm('You have unsaved changes. Discard them and switch file?')) {
        return;
      }
    }
    setActiveKey(key);
  };

  const handleSave = async () => {
    const cleaned = removeEmpty(map);
    setSaving(true);
    setMessage(null);
    try {
      await updateWorkspace(workspaceId, { agentConfigOverrides: cleaned });
      setSaved(cleaned);
      setMap(cleaned);
      setMessage({ type: 'success', text: 'Saved. Changes apply to new sessions in this workspace.' });
      setTimeout(() => setMessage(null), 4000);
    } catch (err) {
      setMessage({ type: 'error', text: err instanceof Error ? err.message : 'Failed to save' });
    } finally {
      setSaving(false);
    }
  };

  // Ctrl/Cmd+S to save
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key === 's') {
        e.preventDefault();
        if (dirty && !saving) handleSave();
      }
    }
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  });

  // Warn before navigating away with unsaved changes
  useEffect(() => {
    function handleBeforeUnload(e: BeforeUnloadEvent) {
      if (dirty) e.preventDefault();
    }
    window.addEventListener('beforeunload', handleBeforeUnload);
    return () => window.removeEventListener('beforeunload', handleBeforeUnload);
  }, [dirty]);

  return (
    <div>
      {/* Banner */}
      <div className="mb-4 px-4 py-3 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-[11px] text-[var(--text-muted)]">
        Overrides stack on top of team defaults. Changes take effect for{' '}
        <span className="text-[var(--text-secondary)] font-medium">new sessions</span> only — running
        sessions are unaffected.
      </div>

      <div className="flex gap-4">
        <AgentConfigFileList activeKey={activeKey} overrides={map} onSelect={handleSwitchFile} />

        <div className="flex-1 flex flex-col min-w-0">
          {/* Header */}
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-3">
              <span className="text-sm font-semibold text-[var(--text-primary)]">{activeDef.file}</span>
              {hasOverride ? (
                <span className="px-1.5 py-0.5 rounded text-[9px] font-bold bg-[var(--accent-gold)]/15 text-[var(--accent-gold)] uppercase tracking-wide">
                  overridden
                </span>
              ) : (
                <span className="px-1.5 py-0.5 rounded text-[9px] font-bold bg-[var(--bg-hover)] text-[var(--text-faint)] uppercase tracking-wide">
                  inherits default
                </span>
              )}
              <span
                className={`text-[10px] font-mono ${charWarning ? 'text-[var(--accent-coral)]' : 'text-[var(--text-faint)]'}`}
              >
                {charCount.toLocaleString()}
                {charWarning && ` / ${MAX_FILE_CHARS}`}
              </span>
              {dirty && (
                <span className="px-1.5 py-0.5 rounded text-[9px] font-bold bg-[var(--accent-gold)]/15 text-[var(--accent-gold)]">
                  unsaved
                </span>
              )}
            </div>

            <div className="flex items-center gap-2">
              {/* view mode toggle */}
              <div className="flex rounded-[var(--radius-sm)] border border-[var(--border-subtle)] overflow-hidden">
                {(['edit', 'split', 'preview'] as ViewMode[]).map((m) => (
                  <button
                    key={m}
                    onClick={() => setViewMode(m)}
                    className={`px-2.5 py-1 text-[10px] font-semibold uppercase tracking-wide transition-colors ${
                      viewMode === m
                        ? 'bg-[var(--accent-gold)] text-black'
                        : 'text-[var(--text-faint)] hover:text-[var(--text-secondary)] hover:bg-[var(--bg-hover)]'
                    }`}
                  >
                    {m}
                  </button>
                ))}
              </div>
              <button
                onClick={handleSave}
                disabled={saving || !dirty}
                className="px-4 py-1.5 rounded-lg text-xs font-semibold transition-all disabled:opacity-40 disabled:cursor-not-allowed bg-[var(--accent-gold)] text-[var(--text-contrast)] hover:bg-[var(--accent-gold-bright)]"
              >
                {saving ? 'Saving...' : 'Save'}
              </button>
            </div>
          </div>

          {/* Status message */}
          {message && (
            <div
              className={`mb-3 px-4 py-3 rounded-[var(--radius-md)] text-xs ${
                message.type === 'success'
                  ? 'bg-[var(--accent-emerald-glow)] text-[var(--accent-emerald)]'
                  : 'bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] text-[var(--accent-coral)]'
              }`}
            >
              {message.text}
            </div>
          )}

          {/* Content area */}
          {viewMode === 'edit' && (
            <textarea
              value={content}
              onChange={(e) => setContent(e.target.value)}
              className="w-full min-h-[600px] p-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[var(--text-primary)] font-mono text-sm leading-relaxed resize-y focus:outline-none focus:border-[var(--border-active)] transition-colors placeholder:text-[var(--text-faint)]"
              placeholder={`Edit ${activeDef.file}...`}
              spellCheck={false}
            />
          )}

          {viewMode === 'split' && (
            <div className="grid grid-cols-2 gap-4 min-h-[600px]">
              <textarea
                value={content}
                onChange={(e) => setContent(e.target.value)}
                className="w-full h-full min-h-[600px] p-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[var(--text-primary)] font-mono text-sm leading-relaxed resize-none focus:outline-none focus:border-[var(--border-active)] transition-colors placeholder:text-[var(--text-faint)]"
                placeholder={`Edit ${activeDef.file}...`}
                spellCheck={false}
              />
              <div className="h-full min-h-[600px] overflow-y-auto custom-scrollbar p-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)]">
                {content ? (
                  <MarkdownText text={content} />
                ) : (
                  <p className="text-xs text-[var(--text-faint)]">Preview will appear here.</p>
                )}
              </div>
            </div>
          )}

          {viewMode === 'preview' && (
            <div className="min-h-[600px] overflow-y-auto custom-scrollbar p-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)]">
              {content ? (
                <MarkdownText text={content} />
              ) : (
                <p className="text-xs text-[var(--text-faint)]">
                  No content to preview. This file inherits the team default.
                </p>
              )}
            </div>
          )}

          {/* Footer */}
          <div className="mt-2 flex items-center justify-between text-[10px] text-[var(--text-faint)]">
            <span>
              {activeDef.description} · Ctrl+S to save
            </span>
            {charWarning && <span className="text-[var(--accent-coral)]">Exceeds {MAX_FILE_CHARS} char limit</span>}
          </div>
        </div>
      </div>
    </div>
  );
}

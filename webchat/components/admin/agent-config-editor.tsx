'use client';

import { useState, useCallback, useEffect } from 'react';
import { updateWorkspace, type Workspace } from '@/lib/api/workspaces';
import { MarkdownText } from '@/components/assistant-ui/MarkdownText';
import { AgentConfigFileList, CONFIG_FILES } from './agent-config-file-list';
import {
  agentConfigOverridesEqual,
  hasAgentConfigOverride,
  prepareAgentConfigOverrides,
} from '@/lib/agent-config-overrides';

const MAX_FILE_CHARS = 8000;

type OverridesMap = Record<string, string>;
type ViewMode = 'edit' | 'split' | 'preview';

export function AgentConfigEditor({
  workspaceId,
  overrides,
  onSaved,
}: {
  workspaceId: string;
  overrides: OverridesMap;
  onSaved?: (ws: Workspace) => void;
}) {
  const initial = prepareAgentConfigOverrides(overrides ?? {});
  const [map, setMap] = useState<OverridesMap>(initial);
  const [saved, setSaved] = useState<OverridesMap>(initial);
  const [activeKey, setActiveKey] = useState<string>('soul');
  const [viewMode, setViewMode] = useState<ViewMode>('edit');
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

  const activeDef = CONFIG_FILES.find((f) => f.key === activeKey) ?? CONFIG_FILES[0];
  const content = map[activeDef.file] ?? '';
  const dirty = !agentConfigOverridesEqual(map, saved);
  const charCount = content.length;
  const charWarning = charCount > MAX_FILE_CHARS;
  const hasOverride = hasAgentConfigOverride(map, activeDef.file);

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
    const cleaned = prepareAgentConfigOverrides(map);
    setSaving(true);
    setMessage(null);
    try {
      const updated = await updateWorkspace(workspaceId, { agentConfigOverrides: cleaned });
      setSaved(cleaned);
      setMap(cleaned);
      onSaved?.(updated);
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
    <div className="space-y-4">
      {/* Banner */}
      <div className="px-4 py-3 rounded-[var(--radius-md)] bg-[var(--bg-elevated)]/20 border border-[var(--border-subtle)] text-[10px] font-mono text-[var(--text-faint)] uppercase tracking-wider flex items-center gap-2">
        <span className="w-1.5 h-1.5 rounded-full bg-[var(--accent-gold)] animate-pulse" />
        <span>
          Overrides replace team defaults; an empty override explicitly clears a slot. Changes apply to{' '}
          <span className="text-[var(--text-secondary)] font-bold">new sessions</span> only.
        </span>
      </div>

      {/* Main editor layout (Vertical stacked to maximize horizontal space) */}
      <div className="flex flex-col gap-4">
        {/* Horizontal selector tab bar at the top */}
        <AgentConfigFileList activeKey={activeKey} overrides={map} onSelect={handleSwitchFile} />

        <div className="flex-1 flex flex-col min-w-0 mt-1">
          {/* Header Controls Toolbar */}
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 mb-4">
            <div className="flex flex-wrap items-center gap-3">
              <span className="text-sm font-mono font-bold text-[var(--text-primary)]">{activeDef.file}</span>
              {hasOverride ? (
                <span className="px-2 py-0.5 rounded text-[8px] font-bold bg-[var(--accent-gold)]/10 text-[var(--accent-gold)] uppercase tracking-wider border border-[var(--accent-gold)]/20">
                  Overridden
                </span>
              ) : (
                <span className="px-2 py-0.5 rounded text-[8px] font-bold bg-[var(--bg-hover)] text-[var(--text-faint)] uppercase tracking-wider border border-[var(--border-subtle)]">
                  Inherits Default
                </span>
              )}
              <span
                className={`text-[10px] font-mono font-bold ${charWarning ? 'text-[var(--accent-coral)]' : 'text-[var(--text-faint)]'}`}
              >
                {charCount.toLocaleString()}
                {charWarning && ` / ${MAX_FILE_CHARS}`} chars
              </span>
              {dirty && (
                <span className="px-2 py-0.5 rounded text-[8px] font-bold bg-[var(--accent-gold)]/20 text-[var(--accent-gold)] uppercase tracking-wider animate-pulse border border-[var(--accent-gold)]/30">
                  Unsaved Changes
                </span>
              )}
            </div>

            <div className="flex items-center gap-3 justify-end">
              {/* view mode toggle */}
              <div className="flex rounded-[var(--radius-md)] border border-[var(--border-subtle)] overflow-hidden bg-[var(--bg-elevated)] p-0.5">
                {(['edit', 'split', 'preview'] as ViewMode[]).map((m) => (
                  <button
                    key={m}
                    onClick={() => setViewMode(m)}
                    className={`px-3 py-1 rounded-[var(--radius-sm)] text-[9px] font-bold uppercase tracking-widest transition-all cursor-pointer ${
                      viewMode === m
                        ? 'bg-[var(--accent-gold)] text-black font-extrabold shadow-sm'
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
                className="px-4 py-1.5 rounded-lg text-xs font-bold transition-all disabled:opacity-40 disabled:cursor-not-allowed bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] cursor-pointer active:scale-[0.98] shadow-sm hover:shadow-glow flex items-center gap-1.5"
              >
                {saving ? (
                  <>
                    <svg className="w-3 h-3 animate-spin" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <circle className="opacity-25" cx={12} cy={12} r={10} stroke="currentColor" strokeWidth={4} />
                      <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
                    </svg>
                    Saving
                  </>
                ) : (
                  'Save'
                )}
              </button>
            </div>
          </div>

          {/* Status message */}
          {message && (
            <div
              className={`mb-4 px-4 py-3 rounded-[var(--radius-md)] text-xs font-bold flex items-center gap-2 animate-fade-in-up ${
                message.type === 'success'
                  ? 'bg-[rgba(52,211,153,0.08)] border border-[rgba(52,211,153,0.15)] text-[var(--accent-emerald)]'
                  : 'bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] text-[var(--accent-coral)]'
              }`}
            >
              {message.type === 'success' ? (
                <svg className="w-4 h-4 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
                </svg>
              ) : (
                <svg className="w-4 h-4 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                </svg>
              )}
              <span>{message.text}</span>
            </div>
          )}

          {/* Content editing / preview panel (Viewport-bound height, no layout overflow) */}
          <div className="h-[300px] lg:h-[calc(100vh-500px)] min-h-[220px] flex flex-col">
            {viewMode === 'edit' && (
              <textarea
                value={content}
                onChange={(e) => setContent(e.target.value)}
                className="w-full h-full p-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)]/60 text-[var(--text-primary)] font-mono text-sm leading-relaxed resize-none focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)]/20 transition-all placeholder:text-[var(--text-faint)] overflow-y-auto"
                placeholder={`Edit ${activeDef.file}...`}
                spellCheck={false}
              />
            )}

            {viewMode === 'split' && (
              <div className="grid grid-cols-1 lg:grid-cols-2 gap-4 h-full min-h-0">
                <textarea
                  value={content}
                  onChange={(e) => setContent(e.target.value)}
                  className="w-full h-full p-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)]/60 text-[var(--text-primary)] font-mono text-sm leading-relaxed resize-none focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)]/20 transition-all placeholder:text-[var(--text-faint)] overflow-y-auto"
                  placeholder={`Edit ${activeDef.file}...`}
                  spellCheck={false}
                />
                <div className="h-full overflow-y-auto p-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)]/30">
                  {content ? (
                    <MarkdownText text={content} />
                  ) : (
                    <p className="text-xs text-[var(--text-faint)] font-mono">Preview will appear here when content is added.</p>
                  )}
                </div>
              </div>
            )}

            {viewMode === 'preview' && (
              <div className="h-full overflow-y-auto p-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)]/30">
                {content ? (
                  <MarkdownText text={content} />
                ) : (
                  <p className="text-xs text-[var(--text-faint)] font-mono">
                    {hasOverride
                      ? 'This slot is explicitly cleared.'
                      : 'No content to preview. This file inherits the default settings.'}
                  </p>
                )}
              </div>
            )}
          </div>

          {/* Footer Toolbar Info */}
          <div className="mt-3 flex items-center justify-between text-[10px] font-mono text-[var(--text-faint)]">
            <span>
              {activeDef.description} · Press <kbd className="px-1 rounded bg-[var(--bg-elevated)] border border-[var(--border-subtle)]">Ctrl+S</kbd> to save
            </span>
            {charWarning && <span className="text-[var(--accent-coral)] font-bold">Exceeds {MAX_FILE_CHARS.toLocaleString()} character limit</span>}
          </div>
        </div>
      </div>
    </div>
  );
}

'use client';

import { useState, useCallback, useEffect, useRef } from 'react';
import { getChannelConfigFile, writeChannelConfigFile } from '@/lib/api/admin-bots';
import type { AgentConfigFile } from '@/lib/types/admin';
import { CONFIG_FILES } from './agent-config-file-list';

/**
 * ChannelConfigEditor edits platform-level (channel team-default) agent config
 * files for a channel that has no bot instance — webchat owns dir/webchat/
 * defaults but never appears in the bot registry. Mirrors BotConfigEditor's UX
 * but addresses the dir/<platform>/ layer directly. See issue #796.
 */
export function ChannelConfigEditor({ platform }: { platform: string }) {
  const [activeFile, setActiveFile] = useState<string>('soul');
  const [fileData, setFileData] = useState<AgentConfigFile | null>(null);
  const [content, setContent] = useState('');
  const [savedContent, setSavedContent] = useState('');
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);
  const dirty = content !== savedContent;

  // Request-sequence guard: when the user switches files quickly, a stale
  // in-flight load must not overwrite the newer file's state.
  const loadIdRef = useRef(0);

  const loadFile = useCallback(async (fileKey: string) => {
    const def = CONFIG_FILES.find((f) => f.key === fileKey);
    if (!def) return;

    const reqId = ++loadIdRef.current;
    setLoading(true);
    setMessage(null);
    try {
      const data = await getChannelConfigFile(platform, def.file);
      if (reqId !== loadIdRef.current) return; // superseded by a newer switch
      setFileData(data);
      setContent(data.content);
      setSavedContent(data.content);
    } catch (err) {
      if (reqId !== loadIdRef.current) return;
      setMessage({ type: 'error', text: String(err) });
      setFileData(null);
      setContent('');
      setSavedContent('');
    } finally {
      if (reqId === loadIdRef.current) setLoading(false);
    }
  }, [platform]);

  useEffect(() => {
    loadFile(activeFile);
  }, [activeFile, loadFile]);

  // Warn before navigating away with unsaved changes
  useEffect(() => {
    function handleBeforeUnload(e: BeforeUnloadEvent) {
      if (dirty) {
        e.preventDefault();
      }
    }
    window.addEventListener('beforeunload', handleBeforeUnload);
    return () => window.removeEventListener('beforeunload', handleBeforeUnload);
  }, [dirty]);

  const handleSwitchFile = (key: string) => {
    if (key === activeFile) return;

    if (dirty) {
      if (!window.confirm('You have unsaved changes. Discard them and switch file?')) {
        return;
      }
    }
    setActiveFile(key);
  };

  const handleSave = useCallback(async () => {
    const def = CONFIG_FILES.find((f) => f.key === activeFile);
    if (!def) return;

    setSaving(true);
    setMessage(null);
    try {
      await writeChannelConfigFile(platform, def.file, content);
      setSavedContent(content);
      setMessage({ type: 'success', text: 'Saved successfully' });
      const data = await getChannelConfigFile(platform, def.file);
      setFileData(data);
      setTimeout(() => setMessage(null), 3000);
    } catch (err) {
      setMessage({ type: 'error', text: String(err) });
    } finally {
      setSaving(false);
    }
  }, [platform, activeFile, content]);

  // Keyboard shortcut: Ctrl/Cmd+S to save. Depends on the save handler and
  // editor state so the listener never closes over stale values.
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key === 's') {
        e.preventDefault();
        if (dirty && !saving && !loading) {
          handleSave();
        }
      }
    }
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [handleSave, dirty, saving, loading]);

  const currentDef = CONFIG_FILES.find((f) => f.key === activeFile);
  const charCount = content.length;
  const charWarning = charCount > 8000;

  return (
    <div className="flex flex-col gap-3">
      {/* Channel-default banner */}
      <div className="px-3 py-2 rounded-lg bg-[var(--bg-hover)] border border-[var(--border-subtle)] text-[11px] text-[var(--text-muted)]">
        Editing <span className="font-mono text-[var(--text-secondary)]">{platform}/</span> channel
        team defaults. These apply to every {platform} workspace unless overridden per-workspace in
        Settings &rarr; AI.
      </div>

      <div className="flex gap-4">
        {/* Left sidebar */}
        <div className="w-48 flex-shrink-0 space-y-1">
          {CONFIG_FILES.map((def) => {
            const isActive = activeFile === def.key;
            return (
              <button
                key={def.key}
                onClick={() => handleSwitchFile(def.key)}
                className={`w-full text-left px-3 py-2.5 rounded-xl transition-all text-sm ${
                  isActive
                    ? 'bg-[var(--bg-active)] border border-[var(--border-active)] text-[var(--accent-gold)]'
                    : 'hover:bg-[var(--bg-hover)] text-[var(--text-secondary)] border border-transparent'
                }`}
              >
                <span className="font-semibold block">{def.label}</span>
                <span className="text-[10px] text-[var(--text-faint)] block mt-0.5">
                  {def.description}
                </span>
              </button>
            );
          })}
        </div>

        {/* Right side */}
        <div className="flex-1 flex flex-col min-w-0">
          {/* Header */}
          {currentDef && fileData && (
            <div className="flex items-center justify-between mb-3">
              <div className="flex items-center gap-3">
                <span className="text-sm font-semibold text-[var(--text-primary)]">
                  {currentDef.file}
                </span>
                <span className="px-2 py-0.5 rounded-full text-[10px] font-mono bg-[var(--bg-elevated)] text-[var(--text-faint)] border border-[var(--border-subtle)]">
                  {fileData.source}
                </span>
                <span
                  className={`text-[10px] font-mono ${
                    charWarning ? 'text-[var(--accent-coral)]' : 'text-[var(--text-faint)]'
                  }`}
                >
                  {charCount.toLocaleString()} chars
                  {charWarning && ' (> 8000)'}
                </span>
                {dirty && (
                  <span className="px-1.5 py-0.5 rounded text-[9px] font-bold bg-[var(--accent-gold)]/15 text-[var(--accent-gold)]">
                    unsaved
                  </span>
                )}
              </div>
              <button
                onClick={handleSave}
                disabled={saving || loading || !dirty}
                className="px-4 py-1.5 rounded-lg text-xs font-semibold transition-all disabled:opacity-40 disabled:cursor-not-allowed bg-[var(--accent-gold)] text-[var(--text-contrast)] hover:bg-[var(--accent-gold-bright)]"
              >
                {saving ? 'Saving...' : 'Save'}
              </button>
            </div>
          )}

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

          {/* Textarea */}
          {loading ? (
            <div className="flex-1 flex items-center justify-center rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] min-h-[600px]">
              <div className="w-5 h-5 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
            </div>
          ) : (
            <textarea
              value={content}
              onChange={(e) => setContent(e.target.value)}
              className="flex-1 w-full min-h-[600px] p-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[var(--text-primary)] font-mono text-sm leading-relaxed resize-none focus:outline-none focus:border-[var(--border-active)] transition-colors placeholder:text-[var(--text-faint)]"
              placeholder={currentDef ? `Edit ${currentDef.file}...` : ''}
              spellCheck={false}
            />
          )}

          {/* Footer hint */}
          <div className="mt-2 flex items-center justify-between text-[10px] text-[var(--text-faint)]">
            <span>Ctrl+S to save</span>
            {dirty && <span>Changes not saved</span>}
          </div>
        </div>
      </div>
    </div>
  );
}

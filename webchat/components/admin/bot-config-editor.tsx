'use client';

import { useState, useCallback, useEffect } from 'react';
import { getAgentFile, writeAgentFile } from '@/lib/api/admin-bots';
import type { AgentConfigFile } from '@/lib/types/admin';

// ---------------------------------------------------------------------------
// Config file definitions
// ---------------------------------------------------------------------------

interface ConfigFileDef {
  key: string;
  file: string;
  label: string;
  description: string;
}

const CONFIG_FILES: ConfigFileDef[] = [
  { key: 'soul', file: 'SOUL.md', label: 'Soul', description: 'Bot personality & identity' },
  { key: 'agents', file: 'AGENTS.md', label: 'Agents', description: 'Behavior rules & guidelines' },
  { key: 'skills', file: 'SKILLS.md', label: 'Skills', description: 'Capabilities & tool usage' },
  { key: 'user', file: 'USER.md', label: 'User', description: 'User-specific preferences' },
  { key: 'memory', file: 'MEMORY.md', label: 'Memory', description: 'Persistent context & notes' },
];

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function BotConfigEditor({ botName }: { botName: string }) {
  const [activeFile, setActiveFile] = useState<string>('soul');
  const [fileData, setFileData] = useState<AgentConfigFile | null>(null);
  const [content, setContent] = useState('');
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

  const loadFile = useCallback(async (fileKey: string) => {
    const def = CONFIG_FILES.find((f) => f.key === fileKey);
    if (!def) return;

    setLoading(true);
    setMessage(null);
    try {
      const data = await getAgentFile(botName, def.file);
      setFileData(data);
      setContent(data.content);
    } catch (err) {
      setMessage({ type: 'error', text: String(err) });
      setFileData(null);
      setContent('');
    } finally {
      setLoading(false);
    }
  }, [botName]);

  useEffect(() => {
    loadFile(activeFile);
  }, [activeFile, loadFile]);

  const handleSave = async () => {
    const def = CONFIG_FILES.find((f) => f.key === activeFile);
    if (!def) return;

    setSaving(true);
    setMessage(null);
    try {
      await writeAgentFile(botName, def.file, content);
      setMessage({ type: 'success', text: 'Saved successfully' });
      // Refresh file data to update size/source
      const data = await getAgentFile(botName, def.file);
      setFileData(data);
    } catch (err) {
      setMessage({ type: 'error', text: String(err) });
    } finally {
      setSaving(false);
    }
  };

  const currentDef = CONFIG_FILES.find((f) => f.key === activeFile);
  const charCount = content.length;
  const charWarning = charCount > 8000;

  return (
    <div className="flex gap-4 h-full">
      {/* Left sidebar */}
      <div className="w-48 flex-shrink-0 space-y-1">
        {CONFIG_FILES.map((def) => (
          <button
            key={def.key}
            onClick={() => setActiveFile(def.key)}
            className={`w-full text-left px-3 py-2.5 rounded-xl transition-all text-sm ${
              activeFile === def.key
                ? 'bg-[var(--bg-active)] border border-[var(--border-active)] text-[var(--accent-gold)]'
                : 'hover:bg-[var(--bg-hover)] text-[var(--text-secondary)] border border-transparent'
            }`}
          >
            <span className="font-semibold block">{def.label}</span>
            <span className="text-[10px] text-[var(--text-faint)] block mt-0.5">
              {def.description}
            </span>
          </button>
        ))}
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
                {charWarning && ' (>'}
                {charWarning && ' 8000)'}
              </span>
            </div>
            <button
              onClick={handleSave}
              disabled={saving || loading}
              className="px-4 py-1.5 rounded-lg text-xs font-semibold bg-[var(--accent-gold)] text-[var(--text-contrast)] hover:bg-[var(--accent-gold-bright)] transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
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
          <div className="flex-1 flex items-center justify-center rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)]">
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
      </div>
    </div>
  );
}

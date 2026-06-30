"use client";

import { useState } from "react";
import { motion } from "framer-motion";
import { WorkerIcon } from "@/components/icons";

interface WorkerOption {
  id: string;
  name: string;
  description: string;
}

const WORKER_OPTIONS: WorkerOption[] = [
  { id: "claude_code", name: "Claude Code", description: "Anthropic coding agent" },
  { id: "opencode_server", name: "OpenCode", description: "Server-based code agent" },
  { id: "codex_cli", name: "Codex CLI", description: "OpenAI coding agent" },
  { id: "acp", name: "ACP", description: "JSON-RPC agent protocol" },
];

interface NewSessionModalProps {
  onConfirm: (title: string, workerType: string) => void;
  onCancel: () => void;
}

export function NewSessionModal({ onConfirm, onCancel }: NewSessionModalProps) {
  const [title, setTitle] = useState("");
  const [selectedWorker, setSelectedWorker] = useState("claude_code");

  const trimmedTitle = title.trim();

  const handleConfirm = () => {
    onConfirm(trimmedTitle, selectedWorker);
  };

  return (
    <motion.div
      className="fixed inset-0 z-[300] flex items-center justify-center"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
    >
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/60 backdrop-blur-sm"
        onClick={onCancel}
      />

      {/* Modal */}
      <motion.div
        className="relative w-full max-w-lg mx-4 rounded-[var(--radius-xl)] border border-[var(--border-default)] bg-[var(--bg-surface)] backdrop-blur-2xl shadow-[var(--shadow-lg)]"
        initial={{ opacity: 0, scale: 0.95, y: 20 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        transition={{ type: "spring" as const, stiffness: 300, damping: 28 }}
      >
        {/* Header */}
        <div className="px-6 pt-6 pb-4">
          <h2 className="text-lg font-display font-bold text-[var(--text-primary)]">
            New Session
          </h2>
          <p className="text-sm text-[var(--text-muted)] mt-1">
            Configure your coding environment
          </p>
        </div>

        {/* Session Title */}
        <div className="px-6 pb-4">
          <label className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest block mb-2">
            Session Name (optional)
          </label>
          <input
            id="session-title"
            name="title"
            type="text"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="e.g. HotPlex Bug Fix"
            autoFocus
            className={`w-full px-3 py-2.5 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border text-sm text-[var(--text-primary)] placeholder:text-[var(--text-faint)] focus:outline-none focus:ring-2 transition-all font-mono ${
              trimmedTitle.length > 0
                ? 'border-[var(--accent-emerald)] focus:ring-[rgba(16,185,129,0.15)]'
                : 'border-[var(--border-default)] focus:ring-[rgba(251,191,36,0.1)] focus:border-[var(--amber-border)]'
            }`}
            onKeyDown={(e) => e.stopPropagation()}
          />
        </div>

        {/* Worker Selection */}
        <div className="px-6 pb-4">
          <label className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest block mb-2">
            Worker Engine
          </label>
          <div className="grid grid-cols-2 gap-2">
            {WORKER_OPTIONS.map((w) => (
              <button
                key={w.id}
                onClick={() => setSelectedWorker(w.id)}
                className={`p-3 rounded-[var(--radius-md)] border text-left transition-all active:scale-[0.98] ${
                  selectedWorker === w.id
                    ? "bg-[var(--amber-light)] border-[var(--amber-border)] shadow-[0_0_20px_rgba(251,191,36,0.08)]"
                    : "bg-[var(--bg-elevated)] border-[var(--border-default)] hover:border-[var(--border-bright)] hover:bg-[var(--bg-hover)]"
                }`}
              >
                <div className="flex items-center gap-2 mb-1">
                  <WorkerIcon type={w.id} className={`w-4 h-4 ${selectedWorker === w.id ? "text-[var(--accent-gold)]" : "text-[var(--text-muted)]"}`} />
                  <span className={`text-xs font-bold whitespace-nowrap ${selectedWorker === w.id ? "text-[var(--text-primary)]" : "text-[var(--text-secondary)]"}`}>
                    {w.name}
                  </span>
                </div>
                <p className="text-[10px] text-[var(--text-faint)] whitespace-nowrap">
                  {w.description}
                </p>
              </button>
            ))}
          </div>
        </div>

        {/* Work Directory — removed: work_dir is immutable, derived from the workspace (spec §6.2). */}

        {/* Actions */}
        <div className="px-6 py-4 border-t border-[var(--border-subtle)] flex items-center justify-end gap-2">
          <button
            onClick={onCancel}
            className="px-4 py-2 rounded-[var(--radius-md)] text-xs font-bold text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)] transition-all"
          >
            Cancel
          </button>
          <button
            onClick={handleConfirm}
            className="px-6 py-2 rounded-[var(--radius-md)] bg-[var(--accent-gold)] text-black text-xs font-bold transition-all hover:bg-[var(--accent-gold-bright)] active:scale-[0.98] shadow-[0_4px_16px_rgba(251,191,36,0.15)]"
          >
            Start Session
          </button>
        </div>
      </motion.div>
    </motion.div>
  );
}

"use client";

import { useState, useEffect } from "react";
import { motion } from "framer-motion";
import { WorkerIcon } from "@/components/icons";
import { getWorkers, WorkerInstallationStatus } from "@/lib/api/sessions";
import { useTranslation } from "react-i18next";

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
  const { t } = useTranslation(['chat', 'common']);
  const [title, setTitle] = useState("");
  const [selectedWorker, setSelectedWorker] = useState("");
  const [workers, setWorkers] = useState<WorkerInstallationStatus[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    getWorkers()
      .then((data) => {
        if (!active) return;
        setWorkers(data);
        const firstInstalled = data.find((w) => w.installed);
        if (firstInstalled) {
          setSelectedWorker(firstInstalled.type);
        }
        setLoading(false);
      })
      .catch((err) => {
        if (!active) return;
        console.error("Failed to fetch workers", err);
        setError("Failed to load worker engines");
        setLoading(false);
      });
    return () => {
      active = false;
    };
  }, []);

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
            {t('chat:title.new_session')}
          </h2>
          <p className="text-sm text-[var(--text-muted)] mt-1">
            {t('chat:text.configure_env')}
          </p>
        </div>

        {/* Session Title */}
        <div className="px-6 pb-4">
          <label className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest block mb-2">
            {t('chat:label.session_name_optional')}
          </label>
          <input
            id="session-title"
            name="title"
            type="text"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder={t('chat:placeholder.session_name')}
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
            {t('chat:label.worker_engine')}
          </label>
          {loading ? (
            <div className="flex items-center justify-center py-6 text-xs text-[var(--text-muted)] font-mono">
              <span className="animate-pulse">{t('chat:status.loading_workers')}</span>
            </div>
          ) : error ? (
            <div className="text-xs text-[var(--text-danger)] font-mono p-3 rounded-[var(--radius-md)] bg-[var(--bg-danger-subtle)] border border-[var(--border-danger)] w-full text-center">
              {error}
            </div>
          ) : (
            <>
              <div className="grid grid-cols-2 gap-2 w-full">
                {workers.map((w) => {
                  const meta = WORKER_OPTIONS.find((opt) => opt.id === w.type);
                  if (!meta) return null;
                  return (
                    <button
                      key={w.type}
                      type="button"
                      disabled={!w.installed}
                      onClick={() => setSelectedWorker(w.type)}
                      className={`p-3 rounded-[var(--radius-md)] border text-left transition-all relative ${
                        !w.installed
                          ? "opacity-50 cursor-not-allowed bg-[var(--bg-disabled)] border-[var(--border-subtle)]"
                          : selectedWorker === w.type
                          ? "bg-[var(--amber-light)] border-[var(--amber-border)] shadow-[0_0_20px_rgba(251,191,36,0.08)] active:scale-[0.98]"
                          : "bg-[var(--bg-elevated)] border-[var(--border-default)] hover:border-[var(--border-bright)] hover:bg-[var(--bg-hover)] active:scale-[0.98]"
                      }`}
                    >
                      <div className="flex items-center gap-2 mb-1">
                        <WorkerIcon type={w.type} className={`w-4 h-4 ${!w.installed ? "text-[var(--text-disabled)]" : selectedWorker === w.type ? "text-[var(--accent-gold)]" : "text-[var(--text-muted)]"}`} />
                        <span className={`text-xs font-bold whitespace-nowrap ${!w.installed ? "text-[var(--text-disabled)]" : selectedWorker === w.type ? "text-[var(--text-primary)]" : "text-[var(--text-secondary)]"}`}>
                          {meta.name}
                        </span>
                      </div>
                      <p className="text-[10px] text-[var(--text-faint)] whitespace-nowrap">
                        {w.installed ? t('chat:worker.description.' + w.type, { defaultValue: meta.description }) : t('chat:text.not_installed')}
                      </p>
                    </button>
                  );
                })}
              </div>
              {workers.length > 0 && !workers.some(w => w.installed) && (
                <div className="text-[10px] text-[var(--text-danger)] mt-2 font-mono bg-[rgba(239,68,68,0.1)] p-2 rounded border border-[rgba(239,68,68,0.2)] w-full">
                  {t('chat:error.no_workers')}
                </div>
              )}
            </>
          )}
        </div>

        {/* Work Directory — removed: work_dir is immutable, derived from the workspace (spec §6.2). */}

        {/* Actions */}
        <div className="px-6 py-4 border-t border-[var(--border-subtle)] flex items-center justify-end gap-2">
          <button
            type="button"
            onClick={onCancel}
            className="px-4 py-2 rounded-[var(--radius-md)] text-xs font-bold text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)] transition-all"
          >
            {t('common:action.cancel')}
          </button>
          <button
            type="button"
            onClick={handleConfirm}
            disabled={loading || !selectedWorker}
            className="px-6 py-2 rounded-[var(--radius-md)] bg-[var(--accent-gold)] text-black text-xs font-bold transition-all hover:bg-[var(--accent-gold-bright)] active:scale-[0.98] shadow-[0_4px_16px_rgba(251,191,36,0.15)] disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {t('chat:action.start_session')}
          </button>
        </div>
      </motion.div>
    </motion.div>
  );
}

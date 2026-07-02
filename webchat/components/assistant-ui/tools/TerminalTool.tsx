"use client";

import { useState } from "react";
import { motion } from "framer-motion";
import { ToolLoadingSkeleton } from "./ToolLoadingSkeleton";
import { useTranslation } from "react-i18next";

interface TerminalToolProps {
  command: string;
  stdout?: string;
  stderr?: string;
  status: "running" | "complete" | "error";
  onToggle?: () => void;
}

const MAX_VISIBLE_LINES = 15;

export function TerminalTool({ command, stdout, stderr, status, onToggle }: TerminalToolProps) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const output = stderr || stdout || "";
  const lines = output.split("\n").filter(Boolean);
  const needsCollapse = lines.length > MAX_VISIBLE_LINES && !expanded;
  const displayLines = needsCollapse ? lines.slice(0, MAX_VISIBLE_LINES) : lines;
  const hasError = !!stderr;

  return (
    <div className="rounded-[var(--radius-md)] overflow-hidden border border-[var(--border-subtle)] my-6 shadow-[var(--shadow-md)] group/terminal">
      {/* Terminal header */}
      <div 
        className={`flex items-center gap-3 px-4 py-2.5 bg-[var(--bg-elevated)] border-b border-[var(--border-subtle)] ${onToggle ? "cursor-pointer hover:bg-[var(--bg-hover)] transition-all" : ""}`}
        onClick={onToggle}
      >
        <div className="flex gap-1.5">
          <div className="w-2.5 h-2.5 rounded-full bg-[var(--accent-coral)]/60 group-hover/terminal:bg-[var(--accent-coral)] transition-colors" />
          <div className="w-2.5 h-2.5 rounded-full bg-[var(--accent-gold)]/60 group-hover/terminal:bg-[var(--accent-gold)] transition-colors" />
          <div className="w-2.5 h-2.5 rounded-full bg-[var(--accent-emerald)]/60 group-hover/terminal:bg-[var(--accent-emerald)] transition-colors" />
        </div>
        <span className="text-[11px] font-display font-black tracking-[0.1em] text-[var(--text-faint)] uppercase ml-2">
          {t('chat:tool.terminal.title', { defaultValue: 'SHELL' })}
        </span>
        <div className="flex-1 h-[1px] bg-gradient-to-r from-[var(--border-subtle)] to-transparent ml-2" />
        {status === "running" ? (
          <motion.span
            className="text-[10px] font-mono text-[var(--accent-emerald)] font-bold flex items-center gap-2"
            animate={{ opacity: [0.6, 1, 0.6] }}
            transition={{ repeat: Infinity, duration: 2 }}
          >
            <span className="w-1.5 h-1.5 rounded-full bg-[var(--accent-emerald)] shadow-[0_0_8px_var(--accent-emerald)]" />
            {t('chat:tool.terminal.live', { defaultValue: 'LIVE' })}
          </motion.span>
        ) : status === "error" ? (
          <span className="text-[10px] font-mono text-[var(--accent-coral)] font-bold flex items-center gap-2">
            <span className="w-1.5 h-1.5 rounded-full bg-[var(--accent-coral)]" />
            {t('common:status.error', { defaultValue: 'ERROR' })}
          </span>
        ) : (
          <span className="text-[10px] font-mono text-[var(--text-faint)] font-bold flex items-center gap-2">
            <span className="w-1.5 h-1.5 rounded-full bg-[var(--text-faint)]" />
            {t('chat:tool.terminal.processed', { defaultValue: 'PROCESSED' })}
          </span>
        )}
        {onToggle && status !== "running" && (
          <div className="text-[var(--text-faint)] transform group-hover/terminal:scale-110 transition-transform">
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 15l7-7 7 7" />
            </svg>
          </div>
        )}
      </div>

      {/* Command */}
      <div className="px-5 py-3.5 bg-[var(--bg-base)] border-b border-[var(--border-subtle)]/50">
        <div className="flex items-start gap-3">
          <span className="text-[var(--accent-emerald)] font-mono text-[14px] font-bold select-none leading-[21px] flex-shrink-0">❯</span>
          <code className="font-mono text-[14px] text-[var(--text-primary)] leading-[21px] break-all">
            {command}
            {status === "running" && (
              <motion.span
                className="inline-block w-2 h-[15px] ml-1.5 bg-[var(--accent-emerald)] align-middle"
                animate={{ opacity: [1, 0] }}
                transition={{ repeat: Infinity, duration: 0.8, ease: "linear" }}
              />
            )}
          </code>
        </div>
      </div>

      {/* Output */}
      {output && (
        <div className="bg-[var(--bg-base)] px-5 py-4 max-h-[500px] overflow-y-auto scrollbar-thin">
          <div className="space-y-1">
            {displayLines.map((line, i) => (
              <div
                key={i}
                className={`font-mono text-[13px] leading-relaxed break-all ${
                  hasError ? "text-[var(--accent-coral)]" : "text-[var(--text-muted)]"
                }`}
              >
                {line}
              </div>
            ))}
          </div>
          {lines.length > MAX_VISIBLE_LINES && (
            <button
              onClick={() => setExpanded(!expanded)}
              className="mt-4 text-[11px] font-mono font-bold text-[var(--accent-emerald)] hover:text-[var(--accent-emerald)]/80 transition-colors uppercase tracking-wider flex items-center gap-2"
            >
              {expanded ? (
                <>
                  <span>- {t('chat:tool.terminal.collapse_lines', { defaultValue: 'Collapse output' })}</span>
                  <div className="flex-1 h-[1px] bg-[var(--accent-emerald)]/20" />
                </>
              ) : (
                <>
                  <span>+ {t('chat:tool.terminal.more_lines', { count: lines.length - MAX_VISIBLE_LINES, defaultValue: `+ ${lines.length - MAX_VISIBLE_LINES} more lines` })}</span>
                  <div className="flex-1 h-[1px] bg-[var(--accent-emerald)]/20" />
                </>
              )}
            </button>
          )}
        </div>
      )}

      {/* Running skeleton */}
      {status === "running" && !output && (
        <div className="bg-[var(--bg-base)] p-8 border-t border-[var(--border-subtle)]/30">
          <ToolLoadingSkeleton color="var(--accent-emerald)" label={t('chat:tool.terminal.awaiting_response', { defaultValue: 'Awaiting response from shell...' })} />
        </div>
      )}
    </div>
  );
}

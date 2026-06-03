"use client";

import { motion } from "framer-motion";
import { useAuiState } from "@assistant-ui/store";
import { BrandIcon } from "@/components/icons";

export function PreAssistantIndicator() {
  const isRunning = useAuiState((s) => s.thread.isRunning);
  const messages = useAuiState((s) => s.thread.messages);
  const lastMessage = messages[messages.length - 1];

  const isWaiting = isRunning && lastMessage?.role === 'user';

  if (!isWaiting) return null;

  return (
    <motion.div
      className="group msg-assistant flex items-start gap-4 mb-8"
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.4, ease: "easeOut" }}
    >
      <div className="flex-shrink-0">
        <div className="w-9 h-9 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] flex items-center justify-center border border-[var(--border-subtle)] relative overflow-hidden">
          <div className="absolute inset-0 bg-gradient-to-tr from-[var(--accent-gold)]/10 to-transparent animate-pulse" />
          <BrandIcon size={24} className="opacity-40 animate-pulse-subtle" />
        </div>
      </div>

      <div className="msg-assistant-body flex flex-col gap-3">
        <div className="flex items-center gap-3 mb-1">
          <div className="flex items-center gap-1.5">
            <span className="thinking-dot" />
            <span className="thinking-dot" style={{ animationDelay: '0.2s' }} />
            <span className="thinking-dot" style={{ animationDelay: '0.4s' }} />
          </div>
          <span className="text-[11px] font-display font-bold text-[var(--accent-gold)] tracking-[0.1em] uppercase">
            Synthesizing Strategy
          </span>
        </div>
        <div className="flex flex-col gap-3 max-w-sm">
          <div className="skeleton-text w-full h-2 rounded-[var(--radius-xs)] animate-shimmer" />
          <div className="skeleton-text w-[92%] h-2 rounded-[var(--radius-xs)] animate-shimmer" style={{ animationDelay: '0.15s' }} />
          <div className="skeleton-text w-[78%] h-2 rounded-[var(--radius-xs)] animate-shimmer" style={{ animationDelay: '0.3s' }} />
        </div>
      </div>
    </motion.div>
  );
}

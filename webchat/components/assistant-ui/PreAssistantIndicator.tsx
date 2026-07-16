"use client";

import { motion } from "framer-motion";
import { useAuiState } from "@assistant-ui/store";
import { BrandIcon } from "@/components/icons";
import { useTranslation } from "react-i18next";

export function PreAssistantIndicator() {
  const { t } = useTranslation('chat');
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
      <div className="flex-shrink-0 relative">
        <div className="absolute inset-0 rounded-[var(--radius-md)] bg-[var(--accent-gold)]/10 animate-avatar-ripple-1 -z-10" />
        <div className="absolute inset-0 rounded-[var(--radius-md)] bg-[var(--accent-gold)]/10 animate-avatar-ripple-2 -z-10" />
        <div className="w-9 h-9 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--accent-gold)]/40 shadow-[0_0_15px_rgba(251,191,36,0.25)] scale-[1.02] flex items-center justify-center relative overflow-hidden transition-all duration-500">
          <div className="absolute inset-0 bg-gradient-to-br from-[var(--accent-gold)]/20 via-indigo-500/5 to-transparent animate-gradient-shift" />
          <div className="absolute inset-[1px] rounded-[calc(var(--radius-md)-1px)] border border-white/5 bg-transparent" />
          <BrandIcon size={24} className="transition-all duration-500 relative z-10 opacity-100 scale-105 filter drop-shadow-[0_0_4px_rgba(251,191,36,0.5)] animate-avatar-breath" />
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
            {t('status.synthesizing')}
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

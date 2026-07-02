"use client";

import { useState, useEffect } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { MarkdownText } from "./MarkdownText";

export function ReasoningBlock({ text, isStreaming }: { text: string; isStreaming?: boolean }) {
  const [expanded, setExpanded] = useState(false);
  const [hasInteracted, setHasInteracted] = useState(false);

  useEffect(() => {
    if (isStreaming && !hasInteracted) {
      setExpanded(true);
    }
  }, [isStreaming, hasInteracted]);

  if (!text.trim()) return null;
  const estimatedSeconds = Math.max(1, Math.round(text.length / 200));

  const handleToggle = () => {
    setExpanded(!expanded);
    setHasInteracted(true);
  };

  return (
    <div className="reasoning-block group/reasoning border-[var(--border-subtle)] hover:border-[var(--accent-gold)]/20 transition-colors">
      <div
        className="reasoning-header px-4 py-2.5 flex items-center gap-3 cursor-pointer select-none"
        onClick={handleToggle}
      >
        <motion.div
          animate={{ rotate: expanded ? 90 : 0 }}
          transition={{ type: "spring", stiffness: 400, damping: 30 }}
        >
          <svg className="w-3.5 h-3.5 text-[var(--accent-gold)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={3} d="M9 5l7 7-7 7" />
          </svg>
        </motion.div>
        <span className="text-[11px] font-display font-bold tracking-[0.1em] text-[var(--text-secondary)]">THOUGHT PROCESS</span>
        <div className="flex-1 h-[1px] bg-gradient-to-r from-[var(--border-subtle)] to-transparent" />
        <span className="font-mono text-[10px] text-[var(--text-faint)] tabular-nums">
          {estimatedSeconds}s elapsed
        </span>
      </div>
      <AnimatePresence initial={false}>
        {expanded && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.3, ease: "easeInOut" }}
            className="overflow-hidden"
          >
            <div className="reasoning-content border-t border-[var(--border-subtle)]/50 leading-relaxed">
              <MarkdownText text={text.replace(/\n{3,}/g, "\n\n").trim()} />
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

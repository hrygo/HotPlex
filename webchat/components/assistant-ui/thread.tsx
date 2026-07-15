"use client";

import React, { useCallback, useRef, useState } from "react";
import { version } from "../../package.json";
import {
  ThreadPrimitive,
  ComposerPrimitive,
} from "@assistant-ui/react";
import { useAui, useAuiState } from "@assistant-ui/store";
import { AnimatePresence, motion } from "framer-motion";
import { CommandMenu } from "./CommandMenu";
import type { SkillEntry } from "@/lib/ai-sdk-transport/client/types";
import type { ConnectionState } from "@/lib/config";
import { AssistantMessage } from "./AssistantMessage";
import { UserMessage } from "./UserMessage";
import { WelcomeScreen } from "./WelcomeScreen";
import { PreAssistantIndicator } from "./PreAssistantIndicator";
import { useTranslation } from "react-i18next";

interface ThreadProps {
  skills?: SkillEntry[];
  hasMore?: boolean;
  connectionState?: ConnectionState;
  onLoadHistory?: () => Promise<{ hasMore: boolean }>;
  onInteractionRespond?: (toolCallId: string, allowed: boolean) => void;
  suggestions?: readonly { title: string; label: string; prompt: string }[];
  isStopping?: boolean;
  onRetryConnection?: () => void;
}

const connLabelKey = {
  connected: 'status.connection.connected',
  connecting: 'status.connection.connecting',
  reconnecting: 'status.connection.reconnecting',
  disconnected: 'status.connection.disconnected',
  already_connected: 'status.connection.already_connected',
} as const satisfies Record<ConnectionState, string>;

const connDot: Record<ConnectionState, string> = {
  connected: 'bg-emerald-400',
  connecting: 'bg-amber-400 animate-pulse',
  reconnecting: 'bg-amber-400 animate-pulse',
  disconnected: 'bg-red-400',
  already_connected: 'bg-[var(--accent-coral)]',
};

export function Thread({ skills, hasMore, connectionState: conn, onLoadHistory, onInteractionRespond, suggestions, isStopping: isStoppingProp, onRetryConnection }: ThreadProps) {
  const { t } = useTranslation('chat');
  const [loadingHistory, setLoadingHistory] = useState(false);
  const [historyHasMore, setHistoryHasMore] = useState(hasMore);
  const aui = useAui();
  const isRunning = useAuiState((s) => s.thread.isRunning);
  const isEmpty = useAuiState((s) => s.thread.isEmpty);

  const handleLoadEarlier = useCallback(async () => {
    if (!onLoadHistory || loadingHistory) return;
    setLoadingHistory(true);
    try {
      const result = await onLoadHistory();
      setHistoryHasMore(result.hasMore);
    } finally {
      setLoadingHistory(false);
    }
  }, [onLoadHistory, loadingHistory]);

  const handleSuggestionClick = useCallback((prompt: string) => {
    aui.composer().setText(prompt);
  }, [aui]);

  // Resolve the connection badge once per render — the label is reused for both
  // the dot's title and the sr-only text, avoiding a duplicate i18n lookup.
  const connStatus = conn ? { dot: connDot[conn], label: t(connLabelKey[conn]) } : null;

  return (
    <ThreadPrimitive.Root className="flex flex-col h-full relative overflow-hidden bg-[var(--bg-base)]">
      <ThreadPrimitive.Viewport className="thread-viewport relative px-4 py-8">
        <div className="max-w-4xl mx-auto w-full">
          {isEmpty && <WelcomeScreen suggestions={suggestions} onSuggestionClick={handleSuggestionClick} />}
          {historyHasMore && (
            <div className="flex justify-center py-4 mb-4">
              <button
                onClick={handleLoadEarlier}
                disabled={loadingHistory}
                className="px-4 py-2 text-[11px] font-mono uppercase tracking-wider font-bold text-[var(--text-muted)] hover:text-[var(--text-primary)] bg-[var(--bg-elevated)] border border-[var(--border-subtle)] rounded-full hover:bg-[var(--bg-hover)] transition-all active:scale-95 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {loadingHistory ? (
                  <span className="flex items-center gap-2">
                    <svg className="w-3 h-3 animate-spin" viewBox="0 0 24 24" fill="none">
                      <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" strokeDasharray="31.4 31.4" strokeLinecap="round" />
                    </svg>
                    {t('status.loading')}
                  </span>
                ) : t('action.load_earlier')}
              </button>
            </div>
          )}
          <ThreadPrimitive.Messages>
            {({ message }) =>
              message.role === "user" ? <UserMessage message={message} /> : <AssistantMessage message={message} onInteractionRespond={onInteractionRespond} />
            }
          </ThreadPrimitive.Messages>
          <ThreadPrimitive.ScrollToBottom className="scroll-bottom-btn">
            <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M19 14l-7 7m0 0l-7-7m7 7V3" />
            </svg>
            <span>{t('action.new_messages')}</span>
          </ThreadPrimitive.ScrollToBottom>
          <PreAssistantIndicator />
        </div>
      </ThreadPrimitive.Viewport>

      <AnimatePresence>
        {conn === 'already_connected' && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            className="absolute inset-0 z-50 flex items-center justify-center bg-[var(--bg-base)]/50 backdrop-blur-md px-6"
          >
            <motion.section
              role="alert"
              initial={{ scale: 0.98, opacity: 0 }}
              animate={{ scale: 1, opacity: 1 }}
              exit={{ scale: 0.98, opacity: 0 }}
              transition={{ duration: 0.15, ease: "easeOut" }}
              className="w-full max-w-[320px] rounded-2xl border border-[var(--border-subtle)] bg-[var(--bg-elevated)] p-6 text-center shadow-xl flex flex-col items-center justify-center gap-5"
            >
              {/* Symmetrical, Minimalist Icon */}
              <div className="w-10 h-10 rounded-full border border-[var(--border-subtle)] bg-[var(--bg-base)] flex items-center justify-center text-[var(--accent-coral)]">
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
              </div>

              {/* Clean Symmetrical Texts */}
              <div className="space-y-1">
                <h2 className="text-sm font-semibold text-[var(--text-primary)]">
                  {t('status.session_already_connected_title')}
                </h2>
                <p className="text-[11px] text-[var(--text-muted)] leading-relaxed max-w-[240px] mx-auto">
                  {t('status.session_already_connected_description')}
                </p>
              </div>

              {/* Symmetrical Flat Button */}
              <motion.button
                whileHover={{ scale: 1.01 }}
                whileTap={{ scale: 0.99 }}
                type="button"
                onClick={onRetryConnection}
                disabled={!onRetryConnection}
                className="w-full rounded-xl bg-[var(--accent-coral)] hover:bg-[var(--accent-coral)]/90 py-2 text-xs font-medium text-white transition-colors focus:outline-none focus:ring-1 focus:ring-[var(--accent-coral)]/40 focus:ring-offset-2 focus:ring-offset-[var(--bg-elevated)] disabled:cursor-not-allowed disabled:opacity-50 flex items-center justify-center gap-1.5"
              >
                <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 1121.21 8H18.2" />
                </svg>
                {t('action.retry_connection')}
              </motion.button>
            </motion.section>
          </motion.div>
        )}
      </AnimatePresence>

      <div className="composer-wrapper px-4 pb-12">
        {(conn === 'disconnected' || conn === 'reconnecting') && (
          <div className="max-w-3xl mx-auto mb-3 flex items-center justify-center gap-2 px-3 py-1.5 rounded-full bg-[var(--accent-coral)]/10 border border-[var(--accent-coral)]/30 text-[var(--accent-coral)] text-[11px] font-medium">
            <span className="w-1.5 h-1.5 rounded-full bg-[var(--accent-coral)] animate-pulse" />
            {t(conn === 'reconnecting' ? 'status.reconnecting_banner' : 'status.disconnected_banner')}
          </div>
        )}
        <ThreadComposer
          skills={skills}
          isRunning={isRunning}
          isStoppingProp={isStoppingProp}
          disabled={conn === 'already_connected'}
        />
        <div className="mt-2 flex justify-between items-center max-w-3xl mx-auto px-2">
          <div className="flex gap-4">
            <span className="text-[10px] text-[var(--text-faint)] font-mono uppercase tracking-widest flex items-center gap-1.5">
              <kbd className="px-1.5 py-0.5 rounded bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-[9px]">Enter</kbd> {t('text.kbd_send_hint')}
            </span>
            <span className="text-[10px] text-[var(--text-faint)] font-mono uppercase tracking-widest flex items-center gap-1.5">
              <kbd className="px-1.5 py-0.5 rounded bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-[9px]">Shift</kbd> + <kbd className="px-1.5 py-0.5 rounded bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-[9px]">Enter</kbd> {t('text.kbd_newline_hint')}
            </span>
          </div>
          <span className="text-[10px] text-[var(--text-faint)] font-mono uppercase tracking-widest flex items-center gap-1.5">
            {connStatus && (
              <>
                <span className={`w-1.5 h-1.5 rounded-full ${connStatus.dot}`} title={connStatus.label} />
                <span className="sr-only">{connStatus.label}</span>
              </>
            )}
            v{version}-stable
          </span>
        </div>
      </div>
    </ThreadPrimitive.Root>
  );
}

interface ThreadComposerProps {
  skills?: SkillEntry[];
  isRunning: boolean;
  isStoppingProp?: boolean;
  disabled?: boolean;
}

const ThreadComposer = React.memo(function ThreadComposer({ skills, isRunning, isStoppingProp, disabled }: ThreadComposerProps) {
  const { t } = useTranslation('chat');
  const [localText, setLocalText] = useState("");
  const [menuOpen, setMenuOpen] = useState(false);
  const aui = useAui();
  const text = useAuiState((s) => s.composer.text);
  const composingRef = useRef(false);

  React.useEffect(() => {
    if (!composingRef.current) {
      setLocalText(text || "");
      if (!text) {
        // eslint-disable-next-line react-hooks/set-state-in-effect -- close menu when external text cleared
        setMenuOpen(false);
      }
    }
  }, [text]);

  const handleCompositionStart = useCallback(() => { composingRef.current = true; }, []);
  const handleCompositionEnd = useCallback((e: React.CompositionEvent<HTMLTextAreaElement>) => {
    composingRef.current = false;
    const val = e.currentTarget.value;
    setLocalText(val);
    aui.composer().setText(val);
  }, [aui]);

  const handleChange = useCallback((e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const val = e.target.value;
    setLocalText(val);
    setMenuOpen(val.startsWith("/"));
    if (!composingRef.current) aui.composer().setText(val);
  }, [aui]);

  const handleSelectCommand = useCallback((cmd: string) => {
    setLocalText(cmd);
    aui.composer().setText(cmd);
    setMenuOpen(false);
  }, [aui]);

  return (
    <div className="composer-container relative max-w-3xl mx-auto">
      <AnimatePresence>
        {menuOpen && <CommandMenu isOpen={menuOpen} inputValue={localText} onSelect={handleSelectCommand} onClose={() => setMenuOpen(false)} skills={skills} />}
      </AnimatePresence>
      <div className="relative">
        <div className="absolute bottom-full left-0 right-0 z-20 mb-3 flex items-center justify-between px-1">
          {/* Left Side: Agent Skills */}
          <div className="flex items-center gap-2 overflow-x-auto no-scrollbar animate-fadeIn max-w-[70%]">
            <div className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-full bg-[var(--accent-gold)]/10 border border-[var(--accent-gold)]/20 shadow-sm whitespace-nowrap">
              <span className="text-[9px] font-display font-black text-[var(--accent-gold)] uppercase tracking-[0.05em]">{t('label.agent_skills')}</span>
              <div className="w-1 h-1 rounded-full bg-[var(--accent-gold)] animate-pulse" />
            </div>
            {skills?.slice(0, 3).map(skill => (
              <div key={skill.name} className="px-3 py-1.5 rounded-full bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-[10px] font-medium text-[var(--text-muted)] whitespace-nowrap hover:border-[var(--text-faint)] transition-colors cursor-default">
                {skill.name}
              </div>
            ))}
            {skills && skills.length > 3 && (
              <div className="px-1 py-1 text-[10px] font-mono text-[var(--text-faint)] uppercase tracking-tighter">
                +{skills.length - 3}
              </div>
            )}
          </div>

          {/* Right Side: Scroll to Bottom */}
          <ThreadPrimitive.ScrollToBottom className="flex items-center gap-1.5 px-3 py-1.5 rounded-full bg-[var(--bg-glass)] border border-[var(--border-subtle)] backdrop-blur-md shadow-[var(--shadow-sm)] text-[var(--text-muted)] hover:text-[var(--accent-gold)] hover:border-[var(--accent-gold)]/30 hover:bg-[var(--bg-hover)] transition-all active:scale-95 group/scroll whitespace-nowrap">
            <svg className="w-3.5 h-3.5 animate-bounce-subtle" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M19 14l-7 7m0 0l-7-7m7 7V3" />
            </svg>
            <span className="text-[10px] font-bold uppercase tracking-widest">{t('label.latest_messages')}</span>
          </ThreadPrimitive.ScrollToBottom>
        </div>

        <ComposerPrimitive.Root className="composer-root">
          <div className="composer-input-row">
            <ComposerPrimitive.Input
              className="composer-input"
              rows={1}
              autoFocus
              submitMode="enter"
              placeholder={t('placeholder.composer')}
              disabled={disabled}
              value={localText}
              onChange={handleChange}
              onCompositionStart={handleCompositionStart}
              onCompositionEnd={handleCompositionEnd}
            />
            <div className="flex items-center gap-2">
              {(isRunning || isStoppingProp) && (
                <ComposerPrimitive.Cancel className={`btn-icon ${isStoppingProp ? 'btn-stop-stopping' : 'btn-stop'}`} disabled={disabled || isStoppingProp} aria-label={t(isStoppingProp ? 'aria.stopping' : 'aria.stop')}>
                  {isStoppingProp ? (
                    <svg className="w-5 h-5 animate-spin" fill="none" viewBox="0 0 24 24">
                      <circle className="opacity-25" cx={12} cy={12} r="10" stroke="currentColor" strokeWidth="4" />
                      <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                    </svg>
                  ) : (
                    <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24"><rect x="6" y="6" width="12" height="12" rx="2" /></svg>
                  )}
                </ComposerPrimitive.Cancel>
              )}
              <ComposerPrimitive.Send className="btn-icon btn-primary" disabled={disabled} aria-label={t('aria.send')}><svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 12h14M12 5l7 7-7 7" /></svg></ComposerPrimitive.Send>
            </div>
          </div>
        </ComposerPrimitive.Root>
      </div>
    </div>
  );
});

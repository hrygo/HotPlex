"use client";

import React, { useCallback, useRef, useState } from "react";
import { version } from "../../package.json";
import {
  ThreadPrimitive,
  ComposerPrimitive,
} from "@assistant-ui/react";
import { useAui, useAuiState } from "@assistant-ui/store";
import { AnimatePresence } from "framer-motion";
import { CommandMenu } from "./CommandMenu";
import type { SkillEntry } from "@/lib/ai-sdk-transport/client/types";
import type { ConnectionState } from "@/lib/config";
import { AssistantMessage } from "./AssistantMessage";
import { UserMessage } from "./UserMessage";
import { WelcomeScreen } from "./WelcomeScreen";
import { PreAssistantIndicator } from "./PreAssistantIndicator";

interface ThreadProps {
  skills?: SkillEntry[];
  hasMore?: boolean;
  connectionState?: ConnectionState;
  onLoadHistory?: () => Promise<{ hasMore: boolean }>;
  onInteractionRespond?: (toolCallId: string, allowed: boolean) => void;
  suggestions?: readonly { title: string; label: string; prompt: string }[];
  isStopping?: boolean;
}

const connLabel: Record<ConnectionState, string> = {
  connected: 'Connected',
  connecting: 'Connecting...',
  disconnected: 'Disconnected',
};

const connDot: Record<ConnectionState, string> = {
  connected: 'bg-emerald-400',
  connecting: 'bg-amber-400 animate-pulse',
  disconnected: 'bg-red-400',
};

export function Thread({ skills, hasMore, connectionState: conn, onLoadHistory, onInteractionRespond, suggestions, isStopping: isStoppingProp }: ThreadProps) {
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
                    Loading...
                  </span>
                ) : "Load earlier messages"}
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
            <span>New</span>
          </ThreadPrimitive.ScrollToBottom>
          <PreAssistantIndicator />
        </div>
      </ThreadPrimitive.Viewport>

      <div className="composer-wrapper px-4 pb-12">
        <ThreadComposer
          skills={skills}
          isRunning={isRunning}
          isStoppingProp={isStoppingProp}
        />
        <div className="mt-2 flex justify-between items-center px-2">
          <div className="flex gap-4">
            <span className="text-[10px] text-[var(--text-faint)] font-mono uppercase tracking-widest flex items-center gap-1.5">
              <kbd className="px-1.5 py-0.5 rounded bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-[9px]">Enter</kbd> to send
            </span>
            <span className="text-[10px] text-[var(--text-faint)] font-mono uppercase tracking-widest flex items-center gap-1.5">
              <kbd className="px-1.5 py-0.5 rounded bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-[9px]">Shift</kbd> + <kbd className="px-1.5 py-0.5 rounded bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-[9px]">Enter</kbd> new line
            </span>
          </div>
          <span className="text-[10px] text-[var(--text-faint)] font-mono uppercase tracking-widest flex items-center gap-1.5">
            {conn && (
              <>
                <span className={`w-1.5 h-1.5 rounded-full ${connDot[conn]}`} title={connLabel[conn]} />
                <span className="sr-only">{connLabel[conn]}</span>
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
}

const ThreadComposer = React.memo(function ThreadComposer({ skills, isRunning, isStoppingProp }: ThreadComposerProps) {
  const [localText, setLocalText] = useState("");
  const [menuOpen, setMenuOpen] = useState(false);
  const aui = useAui();
  const text = useAuiState((s) => s.composer.text);
  const composingRef = useRef(false);

  React.useEffect(() => {
    if (!composingRef.current) {
      setLocalText(text || "");
      if (!text) setMenuOpen(false);
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
              <span className="text-[9px] font-display font-black text-[var(--accent-gold)] uppercase tracking-[0.05em]">Agent Skills</span>
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
            <span className="text-[10px] font-bold uppercase tracking-widest">Latest Messages</span>
          </ThreadPrimitive.ScrollToBottom>
        </div>

        <ComposerPrimitive.Root className="composer-root">
          <div className="composer-input-row">
            <ComposerPrimitive.Input
              className="composer-input"
              rows={1}
              autoFocus
              submitMode="enter"
              placeholder="Type a message or '/' for commands..."
              value={localText}
              onChange={handleChange}
              onCompositionStart={handleCompositionStart}
              onCompositionEnd={handleCompositionEnd}
            />
            <div className="flex items-center gap-2">
              {(isRunning || isStoppingProp) && (
                <ComposerPrimitive.Cancel className={`btn-icon ${isStoppingProp ? 'btn-stop-stopping' : 'btn-stop'}`} disabled={isStoppingProp}>
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
              <ComposerPrimitive.Send className="btn-icon btn-primary"><svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 12h14M12 5l7 7-7 7" /></svg></ComposerPrimitive.Send>
            </div>
          </div>
        </ComposerPrimitive.Root>
      </div>
    </div>
  );
});

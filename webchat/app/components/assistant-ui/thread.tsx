'use client';

import { ThreadPrimitive, AuiIf } from '@assistant-ui/react';
import { AssistantMessage } from './assistant-message';
import { UserMessage, EditComposer } from './user-message';
import { Composer } from './composer';
import { BrandIcon, ScrollDownIcon } from './icons';

type SuggestionIcon = 'code' | 'learn' | 'debug' | 'refactor' | 'arch';

const SUGGESTIONS: readonly { prompt: string; icon: SuggestionIcon }[] = [
  { prompt: '帮我写一个 React 组件', icon: 'code' },
  { prompt: '解释这段代码的逻辑', icon: 'learn' },
  { prompt: '帮我调试这个错误', icon: 'debug' },
  { prompt: '重构这段代码让它更简洁', icon: 'refactor' },
  { prompt: '解释系统架构设计', icon: 'arch' },
];

const ICON_PATHS: Record<SuggestionIcon, string> = {
  code: 'M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4',
  learn: 'M12 6.253v13m0-13C10.832 5.477 9.246 5 7.5 5S4.168 5.477 3 6.253v13C4.168 18.477 5.754 18 7.5 18s3.332.477 4.5 1.253m0-13C13.168 5.477 14.754 5 16.5 5c1.747 0 3.332.477 4.5 1.253v13C19.832 18.477 18.247 18 16.5 18c-1.746 0-3.332.477-4.5 1.253',
  debug: 'M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z',
  refactor: 'M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15',
  arch: 'M19 21V5a2 2 0 00-2-2H7a2 2 0 00-2 2v16m14 0h2m-2 0h-5m-9 0H3m2 0h5M9 7h1m-1 4h1m4-4h1m-1 4h1m-5 10v-5a1 1 0 011-1h2a1 1 0 011 1v5m-4 0h4',
};

export function Thread() {
  return (
    <ThreadPrimitive.Root className="flex flex-col h-full" style={{ background: 'var(--bg-base)' }}>
      <AuiIf condition={(s) => s.thread.isEmpty}>
        <WelcomeScreen />
      </AuiIf>

      <ThreadPrimitive.Viewport className="flex-1 overflow-y-auto">
        <div className="thread-content">
          <ThreadPrimitive.Messages
            components={{ UserMessage, AssistantMessage, EditComposer }}
          />
        </div>

        <ThreadPrimitive.ViewportFooter className="sticky bottom-0 composer-footer">
          <div className="thread-content">
            <Composer />
          </div>
        </ThreadPrimitive.ViewportFooter>
      </ThreadPrimitive.Viewport>

      <ThreadPrimitive.ScrollToBottom className="scroll-to-bottom">
        <button className="scroll-to-bottom-btn" title="Scroll to bottom">
          <ScrollDownIcon />
        </button>
      </ThreadPrimitive.ScrollToBottom>
    </ThreadPrimitive.Root>
  );
}

function WelcomeScreen() {
  return (
    <div className="mesh-bg welcome-screen">
      <BrandIcon size={56} style={{ marginBottom: '1.25rem' }} />

      <h2 className="welcome-title">Hi, how can I help?</h2>
      <p className="welcome-subtitle">
        Ask me anything about code, debugging, or architecture.
      </p>

      <div className="suggestions-stagger suggestions-grid">
        {SUGGESTIONS.map((s) => (
          <ThreadPrimitive.Suggestion
            key={s.prompt}
            prompt={s.prompt}
            send
            className="suggestion-btn"
          >
            <SuggestionIcon type={s.icon} />
            {s.prompt}
          </ThreadPrimitive.Suggestion>
        ))}
      </div>
    </div>
  );
}

function SuggestionIcon({ type }: { type: SuggestionIcon }) {
  return (
    <svg
      className="suggestion-icon"
      fill="none" stroke="currentColor" viewBox="0 0 24 24"
    >
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d={ICON_PATHS[type]} />
    </svg>
  );
}

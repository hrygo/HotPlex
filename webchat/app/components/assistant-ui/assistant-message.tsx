'use client';

import { useState, useCallback } from 'react';
import { MessagePrimitive, ActionBarPrimitive } from '@assistant-ui/react';
import { MarkdownText } from './markdown-text';
import { BrandIcon } from './icons';

export function AssistantMessage() {
  return (
    <MessagePrimitive.Root
      className="group"
      style={{ display: 'flex', gap: '0.75rem', padding: '1rem 0' }}
    >
      <BrandIcon size={28} className="flex-shrink-0" style={{ marginTop: 2 }} />

      <div style={{ flex: 1, minWidth: 0 }}>
        <MessagePrimitive.Parts>
          {({ part }) => {
            if (part.type === 'reasoning') {
              return <ReasoningBlock text={(part as { type: 'reasoning'; text: string }).text} />;
            }
            if (part.type === 'text') {
              return <MarkdownText text={(part as { type: 'text'; text: string }).text} />;
            }
            return null;
          }}
        </MessagePrimitive.Parts>

        <ActionBarPrimitive.Root
          className="aui-action-bar opacity-0 group-hover:opacity-100 transition-opacity"
        >
          <ActionBarPrimitive.Copy className="aui-copy-btn" />
        </ActionBarPrimitive.Root>
      </div>
    </MessagePrimitive.Root>
  );
}

function ReasoningBlock({ text }: { text: string }) {
  const [expanded, setExpanded] = useState(false);
  const toggle = useCallback(() => setExpanded(v => !v), []);

  if (!text.trim()) return null;

  return (
    <div className="reasoning-block">
      <button onClick={toggle} className="reasoning-toggle">
        <svg
          className="reasoning-chevron"
          style={{ transform: expanded ? 'rotate(90deg)' : 'rotate(0)' }}
          fill="none" stroke="currentColor" viewBox="0 0 24 24"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
        </svg>
        <span style={{ color: 'var(--accent-cyan)' }}>Reasoning</span>
        {text.length > 100 && !expanded && (
          <span className="reasoning-preview">{text.slice(0, 100)}...</span>
        )}
      </button>
      {expanded && (
        <div className="reasoning-content animate-fade-in">{text}</div>
      )}
    </div>
  );
}

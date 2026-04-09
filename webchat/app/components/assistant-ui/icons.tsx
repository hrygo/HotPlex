'use client';

import type { CSSProperties } from 'react';

const LIGHTBULB_PATH = 'M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z';
const SEND_ARROW_PATH = 'M5 12h14M12 5l7 7-7 7';

export function BrandIcon({ size = 28, className, style }: {
  size?: number;
  className?: string;
  style?: CSSProperties;
}) {
  return (
    <div
      className={className}
      style={{
        width: size,
        height: size,
        borderRadius: size * 0.36,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'linear-gradient(135deg, rgba(16,185,129,0.15) 0%, rgba(6,182,212,0.1) 100%)',
        border: '1px solid var(--emerald-border)',
        boxShadow: `0 0 ${size * 0.43}px rgba(16,185,129,0.1)`,
        ...style,
      }}
    >
      <svg
        style={{ width: size * 0.5, height: size * 0.5, color: 'var(--accent-emerald)' }}
        fill="none"
        stroke="currentColor"
        viewBox="0 0 24 24"
      >
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d={LIGHTBULB_PATH} />
      </svg>
    </div>
  );
}

export function SendIcon({ size = 16 }: { size?: number }) {
  return (
    <svg style={{ width: size, height: size }} fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d={SEND_ARROW_PATH} />
    </svg>
  );
}

export function StopIcon({ size = 14 }: { size?: number }) {
  return (
    <svg style={{ width: size, height: size }} fill="currentColor" viewBox="0 0 24 24">
      <rect x="6" y="6" width="12" height="12" rx="2" />
    </svg>
  );
}

export function ScrollDownIcon({ size = 16 }: { size?: number }) {
  return (
    <svg style={{ width: size, height: size }} fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 14l-7 7m0 0l-7-7m7 7V3" />
    </svg>
  );
}

export function EditIcon({ size = 13 }: { size?: number }) {
  return (
    <svg style={{ width: size, height: size }} fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z" />
    </svg>
  );
}

export function ChevronIcon({ size = 14 }: { size?: number }) {
  return (
    <svg style={{ width: size, height: size }} fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
    </svg>
  );
}

'use client';

import { useEffect } from 'react';
import i18n from '@/lib/i18n/config';
import './globals.css';

// global-error.tsx 在 RootLayout 之外渲染，且自带 <html><body>，因此必须自行
// 引入 globals.css 才能拿到 design token。:root 的 token 默认即 Obsidian 暗色
// 值，与 app/error.tsx 共用同一套暗色 token —— 单一事实源，避免早期版本里
// 硬编码 hex（#050506 等）与暗色 token 漂移的问题。
const COLORS = {
  bg: 'var(--bg-base)',
  text: 'var(--text-primary)',
  muted: 'var(--text-faint)',
  accent: 'var(--accent-gold)',
  accentText: 'var(--text-contrast)',
} as const;

export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  useEffect(() => {
    console.error('[WebChat GlobalError]', {
      message: error.message,
      digest: error.digest,
      stack: error.stack,
    });
  }, [error]);

  return (
    <html lang={i18n.language || 'zh-CN'}>
      <body>
        <div style={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          height: '100vh',
          background: COLORS.bg,
          color: COLORS.text,
          fontFamily: 'system-ui, sans-serif',
        }}>
          <h2 style={{ fontSize: '1.25rem', marginBottom: '0.5rem' }}>
            {i18n.t('errors:title', { defaultValue: 'Something went wrong' })}
          </h2>
          <p style={{ fontSize: '0.875rem', color: COLORS.muted, marginBottom: '1.5rem' }}>
            {error.message || i18n.t('errors:unexpected', { defaultValue: 'An unexpected error occurred.' })}
          </p>
          <button
            onClick={reset}
            style={{
              padding: '0.625rem 1.5rem',
              borderRadius: '9999px',
              background: COLORS.accent,
              color: COLORS.accentText,
              border: 'none',
              fontWeight: 'bold',
              cursor: 'pointer',
            }}
          >
            {i18n.t('errors:retry', { defaultValue: 'Try Again' })}
          </button>
        </div>
      </body>
    </html>
  );
}

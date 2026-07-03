'use client';

import { useEffect } from 'react';
import i18n from '@/lib/i18n/config';

// global-error.tsx 在 RootLayout 之外渲染 —— 既无法访问 globals.css 的 design
// token，也无法依赖 ThemeContext。此页显式采用恒暗配色（取值对齐暗色 token），
// 作为整个应用崩溃时的最后兜底界面。
const COLORS = {
  bg: '#050506',
  text: '#e5e5e5',
  muted: '#888',
  accent: '#e5a00d',
  accentText: '#000',
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

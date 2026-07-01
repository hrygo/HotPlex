'use client';

import { useEffect } from 'react';
import i18n from '@/lib/i18n/config';

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
          background: '#050506',
          color: '#e5e5e5',
          fontFamily: 'system-ui, sans-serif',
        }}>
          <h2 style={{ fontSize: '1.25rem', marginBottom: '0.5rem' }}>
            {i18n.t('errors:title', { defaultValue: 'Something went wrong' })}
          </h2>
          <p style={{ fontSize: '0.875rem', color: '#888', marginBottom: '1.5rem' }}>
            {error.message || i18n.t('errors:unexpected', { defaultValue: 'An unexpected error occurred.' })}
          </p>
          <button
            onClick={reset}
            style={{
              padding: '0.625rem 1.5rem',
              borderRadius: '9999px',
              background: '#e5a00d',
              color: '#000',
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

'use client';

import { useState } from 'react';
import { adminUrl } from '@/lib/config';

export default function LoginPage() {
  const [url, setUrl] = useState(adminUrl);
  const [token, setToken] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [showHelp, setShowHelp] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');

    if (!url.trim() || !token.trim()) return;

    setLoading(true);
    try {
      const { testConnection, storeAdminConnection } = await import('@/lib/api/admin-client');
      const ok = await testConnection({ url: url.trim(), token: token.trim() });
      if (ok) {
        storeAdminConnection({ url: url.trim(), token: token.trim() });
        // Full page reload to re-mount AdminShell and re-read localStorage.
        // router.replace() does not trigger useAdminAuth to re-check credentials.
        window.location.replace('/admin');
        return;
      } else {
        setError('Connection failed. Check the URL and token.');
      }
    } catch {
      setError('Connection failed. Check the URL and token.');
    } finally {
      setLoading(false);
    }
  };

  const canSubmit = url.trim() !== '' && token.trim() !== '' && !loading;

  return (
    <div className="flex min-h-screen items-center justify-center bg-[var(--bg-base)] px-4">
      <div className="w-full max-w-sm animate-fade-in-up">
        <div className="rounded-xl border border-[var(--border-default)] bg-[var(--bg-surface)] p-8 shadow-[var(--shadow-lg)]">
          {/* Header */}
          <div className="mb-8 text-center">
            <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-lg bg-[var(--accent-gold)]/10">
              <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                strokeWidth={1.5}
                stroke="var(--accent-gold)"
                className="h-6 w-6"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M9 12.75 11.25 15 15 9.75m-3-7.036A11.959 11.959 0 0 1 3.598 6 11.99 11.99 0 0 0 3 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285Z"
                />
              </svg>
            </div>
            <h1 className="font-[family-name:var(--font-display)] text-xl font-bold text-[var(--text-primary)]">
              HotPlex Admin
            </h1>
            <p className="mt-1 text-sm text-[var(--text-muted)]">
              Connect to your gateway
            </p>
          </div>

          {/* Form */}
          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label
                htmlFor="admin-url"
                className="mb-1.5 block text-xs font-medium text-[var(--text-muted)]"
              >
                Admin URL
              </label>
              <input
                id="admin-url"
                type="url"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="http://127.0.0.1:9999"
                className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3 py-2.5 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-faint)] outline-none transition-colors focus:border-[var(--accent-gold)]/40 focus:ring-1 focus:ring-[var(--accent-gold)]/20"
              />
            </div>

            <div>
              <label
                htmlFor="admin-token"
                className="mb-1.5 block text-xs font-medium text-[var(--text-muted)]"
              >
                Admin Token
              </label>
              <input
                id="admin-token"
                type="password"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                placeholder="Enter admin token"
                className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3 py-2.5 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-faint)] outline-none transition-colors focus:border-[var(--accent-gold)]/40 focus:ring-1 focus:ring-[var(--accent-gold)]/20"
              />
            </div>

            {/* Error */}
            {error && (
              <p className="text-sm text-[var(--accent-coral)]">{error}</p>
            )}

            {/* Submit */}
            <button
              type="submit"
              disabled={!canSubmit}
              className="w-full rounded-lg bg-[var(--accent-gold)] px-4 py-2.5 text-sm font-semibold text-black transition-all hover:bg-[var(--accent-gold-bright)] disabled:cursor-not-allowed disabled:opacity-30"
            >
              {loading ? 'Connecting...' : 'Connect'}
            </button>
          </form>

          {/* Help Accordion */}
          <div className="mt-6 pt-4 border-t border-[var(--border-subtle)]">
            <button
              type="button"
              onClick={() => setShowHelp(!showHelp)}
              className="flex w-full items-center justify-between text-xs text-[var(--text-muted)] hover:text-[var(--text-primary)] transition-colors"
            >
              <span className="flex items-center gap-1.5 font-medium">
                <svg
                  xmlns="http://www.w3.org/2000/svg"
                  fill="none"
                  viewBox="0 0 24 24"
                  strokeWidth={1.8}
                  stroke="var(--accent-gold)"
                  className="h-3.5 w-3.5"
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    d="M9.879 7.519c1.171-1.025 3.071-1.025 4.242 0 1.172 1.025 1.172 2.687 0 3.712-.203.179-.43.326-.67.442-.745.361-1.45.999-1.45 1.827v.75M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9 5.25h.008v.008H12v-.008Z"
                  />
                </svg>
                How to set or get Admin Token?
              </span>
              <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                strokeWidth={2.5}
                stroke="currentColor"
                className={`h-3 w-3 text-[var(--text-faint)] transition-transform duration-200 ${
                  showHelp ? 'rotate-180 text-[var(--text-primary)]' : ''
                }`}
              >
                <path strokeLinecap="round" strokeLinejoin="round" d="m19.5 8.25-7.5 7.5-7.5-7.5" />
              </svg>
            </button>

            {showHelp && (
              <div className="mt-3.5 space-y-3 text-xs text-[var(--text-muted)] animate-[fadeInScale_0.15s_ease-out] bg-[var(--bg-elevated)]/40 border border-[var(--border-subtle)]/40 p-3.5 rounded-lg leading-relaxed">
                <p>
                  The Admin Token is used to authorize connection to your gateway. It is configured inside your backend environment settings:
                </p>
                <div className="space-y-2">
                  <div className="flex gap-2 items-start">
                    <span className="text-[var(--accent-gold)] font-bold mt-0.5">•</span>
                    <span>
                      <strong>Development Mode</strong>: Default value is <code className="font-mono bg-[var(--bg-hover)] px-1 py-0.5 rounded text-[var(--accent-gold)] border border-[var(--border-subtle)]">admin-token-dev</code>, or read from <code className="font-mono bg-[var(--bg-hover)] px-1 py-0.5 rounded border border-[var(--border-subtle)]">ADMIN_TOKEN</code> inside local <code className="font-mono bg-[var(--bg-hover)] px-1 py-0.5 rounded border border-[var(--border-subtle)]">.env</code>.
                    </span>
                  </div>
                  <div className="flex gap-2 items-start">
                    <span className="text-[var(--accent-gold)] font-bold mt-0.5">•</span>
                    <span>
                      <strong>Production Mode</strong>: Configured via numbered environment variables in your gateway host config (e.g. <code className="font-mono bg-[var(--bg-hover)] px-1 py-0.5 rounded border border-[var(--border-subtle)]">HOTPLEX_ADMIN_TOKEN_1</code> ... <code className="font-mono bg-[var(--bg-hover)] px-1 py-0.5 rounded border border-[var(--border-subtle)]">HOTPLEX_ADMIN_TOKEN_N</code>) inside <code className="font-mono bg-[var(--bg-hover)] px-1 py-0.5 rounded border border-[var(--border-subtle)]">.env</code> or system configs.
                    </span>
                  </div>
                </div>
                <div className="pt-2 border-t border-[var(--border-subtle)]/30">
                  <p className="mb-2 font-medium text-[var(--text-primary)]">Generate a secure token using CLI:</p>
                  <div className="flex items-center justify-between rounded bg-[var(--bg-hover)] p-2 font-mono text-[10px] text-[var(--text-primary)] border border-[var(--border-subtle)]">
                    <code className="select-all">openssl rand -base64 32</code>
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

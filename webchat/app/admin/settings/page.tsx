'use client';

import { useState, useEffect } from 'react';
import { useRouter } from 'next/navigation';
import {
  getStoredAdminConnection,
  storeAdminConnection,
  clearAdminConnection,
  testConnection,
} from '@/lib/api/admin-client';
import { useAdminAuth } from '@/hooks/use-admin-auth';

type ConnectionStatus = 'idle' | 'testing' | 'connected' | 'failed';

export default function SettingsPage() {
  const router = useRouter();
  const { logout } = useAdminAuth();

  const [url, setUrl] = useState('');
  const [token, setToken] = useState('');
  const [status, setStatus] = useState<ConnectionStatus>('idle');
  const [error, setError] = useState<string | null>(null);
  const [initialized, setInitialized] = useState(false);

  // Load stored connection on mount
  useEffect(() => {
    const stored = getStoredAdminConnection();
    if (stored) {
      setUrl(stored.url);
      setToken(stored.token);
      setStatus('connected');
    }
    setInitialized(true);
  }, []);

  const handleTest = async () => {
    if (!url.trim() || !token.trim()) return;

    setError(null);
    setStatus('testing');
    try {
      const ok = await testConnection({ url: url.trim(), token: token.trim() });
      setStatus(ok ? 'connected' : 'failed');
      if (!ok) {
        setError('Connection failed. Check the URL and token.');
      }
    } catch {
      setStatus('failed');
      setError('Connection failed. Unable to reach the gateway.');
    }
  };

  const handleSave = async () => {
    if (!url.trim() || !token.trim()) {
      setError('Both fields are required.');
      return;
    }

    setError(null);
    setStatus('testing');
    try {
      const ok = await testConnection({ url: url.trim(), token: token.trim() });
      if (ok) {
        storeAdminConnection({ url: url.trim(), token: token.trim() });
        setStatus('connected');
      } else {
        setStatus('failed');
        setError('Connection test failed. Not saving.');
      }
    } catch {
      setStatus('failed');
      setError('Connection failed. Unable to reach the gateway.');
    }
  };

  const handleDisconnect = () => {
    clearAdminConnection();
    logout();
    router.replace('/admin/login');
  };

  const canTest = url.trim() !== '' && token.trim() !== '' && status !== 'testing';
  const canSave = url.trim() !== '' && token.trim() !== '' && status !== 'testing';

  return (
    <div className="min-h-screen bg-[var(--bg-base)] px-6 py-8">
      <div className="max-w-2xl mx-auto">
        {/* Header */}
        <div className="mb-8">
          <h1 className="text-xl font-display font-bold text-[var(--text-primary)]">
            Connection Settings
          </h1>
          <p className="mt-1 text-sm text-[var(--text-muted)]">
            Manage your gateway connection credentials
          </p>
        </div>

        {/* Loading initial state */}
        {!initialized && (
          <div className="flex items-center justify-center py-24">
            <div className="flex flex-col items-center gap-3">
              <div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
              <span className="text-xs text-[var(--text-faint)]">Loading settings...</span>
            </div>
          </div>
        )}

        {initialized && (
          <div className="space-y-6">
            {/* Connection status indicator */}
            <div className="rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4">
              <div className="flex items-center gap-3">
                <span
                  className={`w-2.5 h-2.5 rounded-full ${
                    status === 'connected'
                      ? 'bg-[var(--accent-emerald)]'
                      : status === 'testing'
                        ? 'bg-[var(--accent-gold)] animate-pulse'
                        : status === 'failed'
                          ? 'bg-[var(--accent-coral)]'
                          : 'bg-[var(--text-faint)]'
                  }`}
                />
                <span className="text-sm text-[var(--text-secondary)]">
                  {status === 'connected'
                    ? 'Connected'
                    : status === 'testing'
                      ? 'Testing connection...'
                      : status === 'failed'
                        ? 'Connection failed'
                        : 'Not connected'}
                </span>
              </div>
            </div>

            {/* Form */}
            <div className="space-y-4">
              <div>
                <label
                  htmlFor="settings-url"
                  className="mb-1.5 block text-xs font-semibold text-[var(--text-faint)] uppercase tracking-wider"
                >
                  Admin URL
                </label>
                <input
                  id="settings-url"
                  type="url"
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                  placeholder="http://localhost:9090"
                  className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3 py-2.5 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-faint)] outline-none transition-colors focus:border-[var(--accent-gold)]/40 focus:ring-1 focus:ring-[var(--accent-gold)]/20 font-mono"
                />
              </div>

              <div>
                <label
                  htmlFor="settings-token"
                  className="mb-1.5 block text-xs font-semibold text-[var(--text-faint)] uppercase tracking-wider"
                >
                  Admin Token
                </label>
                <input
                  id="settings-token"
                  type="password"
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                  placeholder="Enter admin token"
                  className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3 py-2.5 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-faint)] outline-none transition-colors focus:border-[var(--accent-gold)]/40 focus:ring-1 focus:ring-[var(--accent-gold)]/20 font-mono"
                />
              </div>
            </div>

            {/* Error */}
            {error && (
              <div className="rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] p-4">
                <p className="text-sm text-[var(--accent-coral)]">{error}</p>
              </div>
            )}

            {/* Actions */}
            <div className="flex items-center gap-3 pt-2">
              <button
                onClick={handleTest}
                disabled={!canTest}
                className="inline-flex items-center gap-1.5 px-4 py-2 rounded-[var(--radius-sm)] text-xs font-bold uppercase tracking-wider bg-[var(--bg-elevated)] text-[var(--text-secondary)] border border-[var(--border-subtle)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {status === 'testing' && (
                  <div className="w-3 h-3 border-2 border-current border-t-transparent rounded-full animate-spin" />
                )}
                {status === 'testing' ? 'Testing...' : 'Test Connection'}
              </button>

              <button
                onClick={handleSave}
                disabled={!canSave}
                className="inline-flex items-center gap-1.5 px-4 py-2 rounded-[var(--radius-sm)] text-xs font-bold uppercase tracking-wider bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {status === 'testing' ? (
                  <div className="w-3 h-3 border-2 border-black border-t-transparent rounded-full animate-spin" />
                ) : null}
                {status === 'testing' ? 'Saving...' : 'Save'}
              </button>

              <button
                onClick={handleDisconnect}
                disabled={status === 'testing'}
                className="inline-flex items-center gap-1.5 px-4 py-2 rounded-[var(--radius-sm)] text-xs font-bold uppercase tracking-wider text-[var(--accent-coral)] bg-[rgba(244,63,94,0.08)] hover:bg-[rgba(244,63,94,0.15)] transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              >
                Disconnect
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

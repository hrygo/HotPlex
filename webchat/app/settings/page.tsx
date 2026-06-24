'use client';

import { useState, useEffect, useCallback } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { getWorkspace, type Workspace } from '@/lib/api/workspaces';
import { getMe, type User } from '@/lib/api/auth';
import { ApiError } from '@/lib/api/errors';
import { BrandIcon } from '@/components/icons';
import { GeneralTab } from '@/app/components/chat/settings-modal/general-tab';
import { AIConfigTab } from '@/app/components/chat/settings-modal/ai-config-tab';
import { ProfileTab } from '@/app/components/chat/settings-modal/profile-tab';
import { MembersTab } from '@/app/components/chat/settings-modal/members-tab';

type TabId = 'general' | 'ai' | 'profile' | 'members';

export default function SettingsPage() {
  const router = useRouter();
  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  const [currentUser, setCurrentUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [authError, setAuthError] = useState(false);
  const [activeTab, setActiveTab] = useState<TabId>('general');

  // Load the active workspace (from localStorage selection) + current user.
  // Both are required to render the tabs; a 401/403 on getMe redirects to login.
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const me = await getMe();
      setCurrentUser(me);
      setAuthError(false);

      const wsId = localStorage.getItem('hotplex_active_workspace_id');
      if (wsId) {
        try {
          setWorkspace(await getWorkspace(wsId));
        } catch {
          // Active workspace no longer accessible — render without it; the
          // workspace-scoped tabs simply stay hidden until the user picks one.
          setWorkspace(null);
        }
      }
    } catch (err) {
      if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
        setAuthError(true);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const tabs: { id: TabId; label: string }[] = [
    { id: 'general', label: 'General' },
    { id: 'ai', label: 'AI Config' },
    { id: 'profile', label: 'Profile' },
    ...(currentUser?.role === 'admin' ? [{ id: 'members' as TabId, label: 'Members' }] : []),
  ];

  // Fall back to General if the active tab left the list (e.g. role resolved
  // non-admin while on Members) so the panel never renders empty.
  useEffect(() => {
    const hasTab = (id: TabId) =>
      id === 'general' || id === 'ai' || id === 'profile' ||
      (id === 'members' && currentUser?.role === 'admin');
    if (!hasTab(activeTab)) setActiveTab('general');
  }, [activeTab, currentUser?.role]);

  const handleWorkspaceUpdated = useCallback((ws: Workspace) => {
    setWorkspace(ws);
  }, []);

  if (authError) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-[var(--bg-base)]">
        <div className="text-center">
          <p className="text-sm text-[var(--accent-coral)] mb-4">Session expired — please re-login.</p>
          <button
            onClick={() => router.replace('/login')}
            className="px-4 py-2 rounded-lg bg-[var(--accent-gold)] text-black text-xs font-bold"
          >
            Go to Login
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-[var(--bg-base)] text-[var(--text-primary)]">
      {/* Top bar: back to chat + brand */}
      <header className="h-14 flex items-center px-4 gap-3 border-b border-[var(--border-subtle)] bg-[rgba(15,15,18,0.6)] backdrop-blur-xl sticky top-0 z-30">
        <Link
          href="/"
          className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)] transition-all text-xs font-bold"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M10 19l-7-7m0 0l7-7m-7 7h18" />
          </svg>
          Back to Chat
        </Link>
        <div className="flex-1 flex items-center justify-center gap-2">
          <BrandIcon size={18} />
          <span className="font-display font-bold text-sm tracking-tight">Settings</span>
        </div>
        <div className="w-[120px]" />
      </header>

      <div className="max-w-3xl mx-auto px-6 py-8">
        <div className="mb-6">
          <h1 className="text-xl font-display font-bold text-[var(--text-primary)]">Settings</h1>
          <p className="text-xs text-[var(--text-muted)] mt-0.5 truncate">
            {workspace?.name ?? (loading ? 'Loading…' : 'No workspace selected')}
          </p>
        </div>

        {loading ? (
          <div className="flex items-center justify-center py-20">
            <div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
          </div>
        ) : (
          <>
            {/* Tabs */}
            <div className="flex gap-1 mb-6 border-b border-[var(--border-subtle)]">
              {tabs.map((tab) => (
                <button
                  key={tab.id}
                  onClick={() => setActiveTab(tab.id)}
                  className={`px-4 py-2 text-sm font-bold transition-all border-b-2 -mb-px ${
                    activeTab === tab.id
                      ? 'text-[var(--accent-gold)] border-[var(--accent-gold)]'
                      : 'text-[var(--text-muted)] border-transparent hover:text-[var(--text-primary)]'
                  }`}
                >
                  {tab.label}
                </button>
              ))}
            </div>

            {/* Tab content */}
            {activeTab === 'general' && workspace && (
              <GeneralTab workspace={workspace} onUpdated={handleWorkspaceUpdated} />
            )}
            {activeTab === 'ai' && workspace && (
              <AIConfigTab workspace={workspace} onUpdated={handleWorkspaceUpdated} />
            )}
            {activeTab === 'profile' && currentUser && <ProfileTab user={currentUser} />}
            {activeTab === 'members' && currentUser?.role === 'admin' && (
              <MembersTab currentUser={currentUser} />
            )}

            {/* Workspace tabs require an active workspace */}
            {(activeTab === 'general' || activeTab === 'ai') && !workspace && (
              <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-4 py-6 text-center">
                <p className="text-sm text-[var(--text-muted)]">
                  No active workspace. Return to chat and select one to configure it.
                </p>
                <Link href="/" className="inline-block mt-3 text-xs font-bold text-[var(--accent-gold)] hover:underline">
                  Back to Chat
                </Link>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

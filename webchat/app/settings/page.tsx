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

const getTabHeading = (tab: TabId) => {
  switch (tab) {
    case 'general':
      return {
        title: 'General Settings',
        description: 'Manage your workspace name, directories, and main operational settings.',
      };
    case 'ai':
      return {
        title: 'AI Configuration',
        description: 'Configure your preferred worker engines and custom prompt rules.',
      };
    case 'profile':
      return {
        title: 'Personal Profile',
        description: 'View your account credentials, roles, and session information.',
      };
    case 'members':
      return {
        title: 'Member Management',
        description: 'Invite new users, manage active team accounts, and update permissions.',
      };
    default:
      return { title: 'Settings', description: '' };
  }
};

export default function SettingsPage() {
  const router = useRouter();
  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  const [currentUser, setCurrentUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [authError, setAuthError] = useState(false);
  const [activeTab, setActiveTab] = useState<TabId>('general');

  // Load the active workspace (from localStorage selection) + current user.
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

  const navigationGroups = [
    {
      title: 'Workspace Settings',
      items: [
        {
          id: 'general' as TabId,
          label: 'General',
          icon: (
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6V4m0 2a2 2 0 100 4m0-4a2 2 0 110 4m-6 8a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4m6 6v10m6-2a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4" />
            </svg>
          ),
        },
        {
          id: 'ai' as TabId,
          label: 'AI Config',
          icon: (
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z" />
            </svg>
          ),
        },
        {
          id: 'members' as TabId,
          label: 'Members',
          icon: (
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656.126-1.283.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0zm6 3a2 2 0 11-4 0 2 2 0 014 0zM7 10a2 2 0 11-4 0 2 2 0 014 0z" />
            </svg>
          ),
          adminOnly: true,
        },
      ],
    },
    {
      title: 'Personal Settings',
      items: [
        {
          id: 'profile' as TabId,
          label: 'Profile',
          icon: (
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
            </svg>
          ),
        },
      ],
    },
  ];

  if (authError) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-[var(--bg-base)]">
        <div className="text-center p-8 max-w-sm rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] backdrop-blur-xl shadow-xl">
          <p className="text-sm text-[var(--accent-coral)] mb-4 font-bold">Session expired — please re-login.</p>
          <button
            onClick={() => router.replace('/login')}
            className="w-full px-4 py-2.5 rounded-lg bg-[var(--accent-gold)] text-black text-xs font-bold transition-all hover:bg-[var(--accent-gold-bright)] active:scale-[0.98]"
          >
            Go to Login
          </button>
        </div>
      </div>
    );
  }

  const tabHeading = getTabHeading(activeTab);

  return (
    <div className="h-screen overflow-y-scroll [scrollbar-gutter:stable] bg-[var(--bg-base)] text-[var(--text-primary)] flex flex-col">
      {/* Mesh background effect matching Design System */}
      <div className="fixed inset-0 z-0 bg-mesh opacity-30 pointer-events-none" />
      <div className="fixed inset-0 z-0 noise-overlay pointer-events-none" />

      {/* Top Header Bar */}
      <header className="relative z-30 h-14 flex items-center px-4 md:px-6 gap-3 border-b border-[var(--border-subtle)] bg-[rgba(9,9,11,0.7)] backdrop-blur-xl sticky top-0">
        <Link
          href="/"
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)] transition-all text-xs font-bold border border-transparent hover:border-[var(--border-subtle)]"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M10 19l-7-7m0 0l7-7m-7 7h18" />
          </svg>
          Back to Chat
        </Link>
        <div className="flex-1 flex items-center justify-center gap-2 pr-12 md:pr-24">
          <BrandIcon size={18} />
          <span className="font-display font-bold text-sm tracking-tight">Settings</span>
        </div>
      </header>

      {/* Main Container */}
      <div className="relative z-10 w-full max-w-7xl mx-auto px-4 md:px-6 py-6 md:py-10">
        <div className="flex flex-col md:flex-row items-start gap-8 w-full">
          {/* Left Sidebar Navigation */}
          <aside className="w-full md:w-60 shrink-0 flex flex-col gap-5">
            {/* Active Workspace Status Widget */}
            <div className="p-4 rounded-xl border border-[var(--border-subtle)] bg-[rgba(24,24,27,0.3)] backdrop-blur-md">
              <span className="text-[9px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-wider block mb-1">
                Active Workspace
              </span>
              <div className="flex items-center gap-2">
                <span className="w-1.5 h-1.5 rounded-full bg-[var(--accent-emerald)] animate-pulse" />
                <span className="text-xs font-bold text-[var(--text-primary)] truncate">
                  {workspace?.name ?? (loading ? 'Loading…' : 'No workspace selected')}
                </span>
              </div>
            </div>

            {/* Navigation Lists */}
            <div className="flex flex-col gap-5">
              {navigationGroups.map((group) => {
                // Filter items by admin check
                const visibleItems = group.items.filter(
                  (item) => !item.adminOnly || currentUser?.role === 'admin'
                );

                if (visibleItems.length === 0) return null;

                return (
                  <div key={group.title} className="flex flex-col gap-1.5">
                    <h3 className="px-3 text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest">
                      {group.title}
                    </h3>
                    <div className="flex md:flex-col gap-1 overflow-x-auto md:overflow-visible pb-2 md:pb-0 scrollbar-none">
                      {visibleItems.map((item) => {
                        const isActive = activeTab === item.id;
                        return (
                          <button
                            key={item.id}
                            onClick={() => {
                              setActiveTab(item.id);
                              window.scrollTo(0, 0);
                            }}
                            className={`flex items-center gap-2.5 px-3 py-2 rounded-lg text-xs font-bold transition-all border shrink-0 md:shrink ${
                              isActive
                                ? 'text-[var(--accent-gold)] bg-[var(--bg-active)] border-[rgba(251,191,36,0.15)] md:border-l-2 md:border-l-[var(--accent-gold)] md:rounded-l-none'
                                : 'text-[var(--text-muted)] border-transparent hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)]'
                            }`}
                          >
                            <span className={isActive ? 'text-[var(--accent-gold)]' : 'text-[var(--text-faint)]'}>
                              {item.icon}
                            </span>
                            {item.label}
                          </button>
                        );
                      })}
                    </div>
                  </div>
                );
              })}
            </div>
          </aside>

          {/* Right Content Panel */}
          <main className="flex-1 min-w-0 w-full max-w-5xl">
            {loading ? (
              <div className="flex items-center justify-center py-24 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)]">
                <div className="w-8 h-8 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
              </div>
            ) : (
              <div className="flex flex-col gap-4">
                {/* Section Header — fixed min-height so description length/wrap
                    can't change the block height across tabs (no jump on switch). */}
                <div className="mb-2 min-h-[4rem]">
                  <h1 className="text-lg font-display font-bold text-[var(--text-primary)]">
                    {tabHeading.title}
                  </h1>
                  <p className="text-xs text-[var(--text-muted)] mt-1">
                    {tabHeading.description}
                  </p>
                </div>

                {/* Section Content Card */}
                <div className="relative w-full overflow-hidden rounded-xl border border-[var(--border-subtle)] bg-[rgba(12,12,14,0.7)] backdrop-blur-md px-6 py-7 shadow-lg shadow-black/20 min-h-[600px]">
                  {/* Subtle top amber-gold highlight line for active tab cards */}
                  <div className="absolute top-0 left-0 right-0 h-[2px] bg-gradient-to-r from-transparent via-[var(--accent-gold)]/20 to-transparent" />
                  
                  {activeTab === 'general' && workspace && currentUser && (
                    <GeneralTab
                      workspace={workspace}
                      isAdmin={currentUser.role === 'admin'}
                      onUpdated={handleWorkspaceUpdated}
                    />
                  )}
                  {activeTab === 'ai' && workspace && (
                    <AIConfigTab workspace={workspace} onUpdated={handleWorkspaceUpdated} />
                  )}
                  {activeTab === 'profile' && currentUser && <ProfileTab user={currentUser} />}
                  {activeTab === 'members' && currentUser?.role === 'admin' && (
                    <MembersTab currentUser={currentUser} />
                  )}

                  {/* Workspace fallback warning */}
                  {(activeTab === 'general' || activeTab === 'ai') && !workspace && (
                    <div className="text-center py-6">
                      <div className="w-10 h-10 rounded-full bg-[rgba(244,63,94,0.1)] border border-[rgba(244,63,94,0.2)] flex items-center justify-center mx-auto mb-3">
                        <svg className="w-5 h-5 text-[var(--accent-coral)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                        </svg>
                      </div>
                      <p className="text-sm text-[var(--text-secondary)] font-bold">No Active Workspace</p>
                      <p className="text-xs text-[var(--text-muted)] mt-1 max-w-sm mx-auto">
                        This tab requires an active workspace connection. Return to the chat page to select or create a workspace.
                      </p>
                      <Link
                        href="/"
                        className="inline-block mt-4 px-4 py-2 rounded-lg bg-[var(--bg-hover)] border border-[var(--border-subtle)] text-xs font-bold text-[var(--accent-gold)] hover:bg-[var(--bg-active)] transition-all"
                      >
                        Return to Chat
                      </Link>
                    </div>
                  )}
                </div>
              </div>
            )}
          </main>
        </div>
      </div>
    </div>
  );
}

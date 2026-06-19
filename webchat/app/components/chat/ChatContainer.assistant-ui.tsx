'use client';

import { useState, useCallback, useEffect } from 'react';
import Link from 'next/link';
import {
  AssistantRuntimeProvider,
  useExternalStoreRuntime,
} from '@assistant-ui/react';
import { useQueryState, parseAsString } from 'nuqs';
import { useHotPlexRuntime } from '@/lib/adapters/hotplex-runtime-adapter';
import { useSessions } from '@/lib/hooks/useSessions';
import { Thread } from '@/components/assistant-ui/thread';
import { BrandIcon, WORKER_DISPLAY } from '@/components/icons';
import { SessionPanel } from './SessionPanel';
import { NewSessionModal } from './NewSessionModal';
import { MetricsBar } from '@/components/assistant-ui/MetricsBar';
import { workerType as defaultWorkerType, httpBase, type ConnectionState } from '@/lib/config';
import type { SessionMetrics } from '@/lib/hooks/useMetrics';
import { useSkillsCache } from '@/lib/hooks/useSkillsCache';
import {
  listWorkspaces,
  createWorkspace,
  type Workspace,
} from '@/lib/api/workspaces';
import { logout } from '@/lib/api/auth';

function ChatInterface({
  sessionId,
  overrideWorkDir,
  onMetricsChange,
  onSessionStateChange,
  workspaceId,
}: {
  sessionId: string | null;
  overrideWorkDir?: string;
  onMetricsChange?: (metrics: SessionMetrics) => void;
  onSessionStateChange?: (state: string) => void;
  workspaceId?: string;
}) {
  const { skills, mergeSkills } = useSkillsCache(sessionId);
  const adapter = useHotPlexRuntime({
    sessionId: sessionId ?? undefined,
    overrideWorkDir,
    onMetricsChange,
    onSkillsChange: mergeSkills,
    onSessionStateChange,
    workspaceId,
  });

  const runtime = useExternalStoreRuntime(adapter);

  type AdapterExtras = {
    hasMore?: boolean;
    connectionState?: ConnectionState;
    onLoadHistory?: () => Promise<{ hasMore: boolean }>;
    onInteractionRespond?: (toolCallId: string, allowed: boolean) => void;
    isStopping?: boolean;
  };
  const extras = adapter.extras as AdapterExtras | undefined;
  const hasMore = extras?.hasMore ?? false;
  const connectionState = extras?.connectionState;
  const onLoadHistory = extras?.onLoadHistory;
  const onInteractionRespond = extras?.onInteractionRespond;
  const isStopping = extras?.isStopping ?? false;
  const suggestions = adapter.suggestions as readonly { title: string; label: string; prompt: string }[] | undefined;

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <Thread skills={skills} hasMore={hasMore} connectionState={connectionState} onLoadHistory={onLoadHistory} onInteractionRespond={onInteractionRespond} suggestions={suggestions} isStopping={isStopping} />
    </AssistantRuntimeProvider>
  );
}

export default function ChatContainer() {
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [showNewModal, setShowNewModal] = useState(false);
  const [sessionMetrics, setSessionMetrics] = useState<SessionMetrics | null>(null);

  // nuqs deep link params
  const [urlDir] = useQueryState('dir', parseAsString);

  // Workspaces State
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [activeWorkspace, setActiveWorkspace] = useState<Workspace | null>(null);
  
  // Fetch workspaces list
  const loadWorkspaces = useCallback(async (selectId?: string) => {
    try {
      const res = await listWorkspaces();
      let list = res.workspaces || [];

      // Fallback: If no workspaces exist, create a default one
      if (list.length === 0) {
        const defaultWS = await createWorkspace('Default Workspace', './workspace');
        list = [defaultWS];
      }

      setWorkspaces(list);

      // Determine active workspace
      const targetId = selectId || localStorage.getItem('hotplex_active_workspace_id');
      const found = list.find((w) => w.id === targetId);
      const active = found || list[0];
      
      setActiveWorkspace(active);
      if (active) {
        localStorage.setItem('hotplex_active_workspace_id', active.id);
      }
    } catch (err) {
      console.error('Failed to load workspaces', err);
    }
  }, []);

  useEffect(() => {
    loadWorkspaces();
  }, [loadWorkspaces]);

  const handleSwitchWorkspace = (ws: Workspace) => {
    setActiveWorkspace(ws);
    localStorage.setItem('hotplex_active_workspace_id', ws.id);
    setSessionMetrics(null); // Reset metrics on workspace switch
  };

  const handleLogout = async () => {
    try {
      await logout();
    } catch {
      // Ignore error and redirect
    }
    window.location.replace('/login');
  };

  // Sessions hook scoped to active workspace
  const {
    activeSession,
    isLoading: sessionsLoading,
    error: sessionError,
    selectSession,
    createNewSession,
    removeSession,
    sessions,
    updateSessionState,
  } = useSessions({
    onSelect: () => {},
    workspaceId: activeWorkspace?.id,
  });

  const activeSessionId = activeSession?.id || null;

  // Handle NewSessionModal confirm
  const handleModalConfirm = useCallback(async (title: string, wt: string, dir: string) => {
    setShowNewModal(false);
    await createNewSession(title, wt, dir || undefined);
  }, [createNewSession]);

  // Handle "New Chat" button
  const handleCreateNew = useCallback(() => {
    setShowNewModal(true);
  }, []);

  return (
    <div className="flex h-screen overflow-hidden bg-[var(--bg-base)]">
      {/* 1. Far-left compact workspace selection navigation bar */}
      <nav className="w-[72px] bg-[#0b0b0d] border-r border-[var(--border-subtle)] flex flex-col items-center py-4 justify-between z-40 flex-shrink-0">
        <div className="flex flex-col items-center gap-4 w-full">
          {/* Logo */}
          <div className="w-12 h-12 rounded-xl bg-[var(--bg-surface)] border border-[var(--border-subtle)] flex items-center justify-center relative group">
            <BrandIcon size={32} />
            <div className="absolute left-[80px] bg-[var(--bg-elevated)] border border-[var(--border-default)] px-2.5 py-1.5 rounded-lg text-xs font-bold font-display whitespace-nowrap opacity-0 group-hover:opacity-100 pointer-events-none transition-all duration-200 z-50 shadow-md">
              HotPlex WebChat
            </div>
          </div>

          <div className="w-8 h-[1px] bg-[var(--border-subtle)]" />

          {/* Workspaces list */}
          <div className="flex flex-col gap-3 w-full items-center max-h-[calc(100vh-280px)] overflow-y-auto custom-scrollbar py-1">
            {workspaces.map((ws) => {
              const isActive = activeWorkspace?.id === ws.id;
              const initials = ws.name.slice(0, 2).toUpperCase();
              return (
                <button
                  key={ws.id}
                  onClick={() => handleSwitchWorkspace(ws)}
                  className={`w-12 h-12 rounded-3xl hover:rounded-xl flex items-center justify-center text-sm font-black uppercase tracking-wider relative transition-all duration-300 group ${
                    isActive
                      ? 'bg-[var(--accent-gold)] text-black rounded-xl border border-[var(--accent-gold)] shadow-[var(--shadow-glow)]'
                      : 'bg-[var(--bg-surface)] text-[var(--text-secondary)] border border-[var(--border-subtle)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] hover:border-[var(--border-default)]'
                  }`}
                >
                  {/* Active Indicator Bar */}
                  {isActive && (
                    <div className="absolute left-0 w-1.5 h-6 bg-black rounded-r-md" />
                  )}
                  {initials}

                  {/* Tooltip */}
                  <div className="absolute left-[80px] bg-[var(--bg-elevated)] border border-[var(--border-default)] px-2.5 py-1.5 rounded-lg text-xs font-bold font-display text-[var(--text-primary)] whitespace-nowrap opacity-0 group-hover:opacity-100 pointer-events-none transition-all duration-200 z-50 shadow-md">
                    {ws.name}
                  </div>
                </button>
              );
            })}

            {/* Create Workspace Button → full-page workspace management */}
            <Link
              href="/admin/workspaces/new"
              className="w-12 h-12 rounded-3xl hover:rounded-xl bg-[var(--bg-surface)] hover:bg-[var(--bg-hover)] border border-[var(--border-subtle)] hover:border-[var(--border-default)] flex items-center justify-center text-[var(--text-muted)] hover:text-[var(--accent-gold)] transition-all duration-300 group"
              title="Create Workspace"
            >
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M12 4v16m8-8H4" />
              </svg>

              {/* Tooltip */}
              <div className="absolute left-[80px] bg-[var(--bg-elevated)] border border-[var(--border-default)] px-2.5 py-1.5 rounded-lg text-xs font-bold font-display text-[var(--text-primary)] whitespace-nowrap opacity-0 group-hover:opacity-100 pointer-events-none transition-all duration-200 z-50 shadow-md">
                Create Workspace
              </div>
            </Link>
          </div>
        </div>

        {/* Bottom profile actions */}
        <div className="flex flex-col items-center gap-3 w-full">
          {/* Settings & Admin button → admin console */}
          <Link
            href="/admin"
            className="w-12 h-12 rounded-xl bg-[var(--bg-surface)] hover:bg-[var(--bg-hover)] border border-[var(--border-subtle)] hover:border-[var(--border-default)] flex items-center justify-center text-[var(--text-secondary)] hover:text-[var(--text-primary)] transition-all duration-300 group"
            title="Settings & Admin"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.066 2.573c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.573 1.066c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.066-2.573c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
            </svg>

            {/* Tooltip */}
            <div className="absolute left-[80px] bg-[var(--bg-elevated)] border border-[var(--border-default)] px-2.5 py-1.5 rounded-lg text-xs font-bold font-display text-[var(--text-primary)] whitespace-nowrap opacity-0 group-hover:opacity-100 pointer-events-none transition-all duration-200 z-50 shadow-md">
              Settings & Admin
            </div>
          </Link>

          {/* Logout button */}
          <button
            onClick={handleLogout}
            className="w-12 h-12 rounded-xl bg-[var(--bg-surface)] hover:bg-[var(--accent-coral)]/10 border border-[var(--border-subtle)] hover:border-[var(--accent-coral)]/20 flex items-center justify-center text-[var(--text-secondary)] hover:text-[var(--accent-coral)] transition-all duration-300 group"
            title="Log Out"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1" />
            </svg>

            {/* Tooltip */}
            <div className="absolute left-[80px] bg-[var(--bg-elevated)] border border-[var(--border-default)] px-2.5 py-1.5 rounded-lg text-xs font-bold font-display text-[var(--text-primary)] whitespace-nowrap opacity-0 group-hover:opacity-100 pointer-events-none transition-all duration-200 z-50 shadow-md">
              Log Out
            </div>
          </button>
        </div>
      </nav>

      {/* 2. Main Sessions Sidebar (Workspace Scoped) */}
      <aside className={`transition-all duration-300 ease-in-out ${sidebarOpen ? 'w-[280px]' : 'w-0'} overflow-hidden flex-shrink-0 relative z-30`}>
        <SessionPanel
          sessions={sessions}
          activeSession={activeSession}
          isLoading={sessionsLoading}
          onSelect={selectSession}
          onCreate={handleCreateNew}
          onDelete={removeSession}
        />
      </aside>

      {/* 3. Main Chat View Content */}
      <main className="flex-1 flex flex-col min-w-0 relative">
        <header className="h-14 flex items-center px-6 border-b border-[var(--border-subtle)] bg-[rgba(15,15,18,0.6)] backdrop-blur-xl flex-shrink-0 z-20">
          <div className="flex items-center gap-4 w-full">
            <button
              onClick={() => setSidebarOpen(!sidebarOpen)}
              className="p-2 -ml-2 text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)] rounded-lg transition-all active:scale-95"
              title={sidebarOpen ? "Collapse sidebar" : "Expand sidebar"}
            >
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
              </svg>
            </button>

            <div className="flex items-center gap-3 flex-1 min-w-0">
               <div>
                  <h1 className="text-xs font-display font-bold text-[var(--text-primary)] leading-none mb-0.5 flex items-center gap-2">
                    <span className="text-[var(--accent-gold)] truncate max-w-[150px]">{activeWorkspace?.name || 'Workspace'}</span>
                    <span className="text-[10px] text-[var(--text-faint)]">/</span>
                    <span className="text-[var(--text-secondary)]">HotPlex Agent</span>
                  </h1>
                  <p className="text-[9px] text-[var(--text-faint)] font-mono uppercase tracking-widest flex items-center gap-1.5">
                    <span className="inline-block w-1.5 h-1.5 rounded-full bg-[var(--accent-emerald)] shadow-[0_0_6px_var(--accent-emerald)]" />
                    Active · {WORKER_DISPLAY[activeSession?.worker_type ?? defaultWorkerType] ?? activeSession?.worker_type ?? defaultWorkerType}
                  </p>
                  {(urlDir || activeWorkspace?.work_dir) && (
                    <p className="text-[9px] text-[var(--text-faint)] font-mono mt-0.5 truncate max-w-[200px]" title={urlDir || activeWorkspace?.work_dir}>
                      {(() => {
                        const d = urlDir || activeWorkspace?.work_dir || '';
                        return d.length > 30 ? `…${d.slice(-28)}` : d;
                      })()}
                    </p>
                  )}
               </div>
            </div>

            {/* MetricsBar */}
            {sessionMetrics && sessionMetrics.turnCount > 0 && (
              <MetricsBar session={sessionMetrics} />
            )}

            <div className="flex items-center gap-2 flex-shrink-0">
                <a
                  href={`${httpBase()}/docs/`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center gap-1.5 px-3 py-1.5 text-[var(--accent-gold)] bg-[var(--accent-gold)]/10 border border-[var(--accent-gold)]/20 hover:bg-[var(--accent-gold)] hover:text-black rounded-full transition-all font-bold text-[10px] uppercase tracking-wider shadow-sm"
                  title="Documentation"
                >
                  <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M12 6.253v13m0-13C10.832 5.477 9.246 5 7.5 5S4.168 5.477 3 6.253v13C4.168 18.477 5.754 18 7.5 18s3.332.477 4.5 1.253m0-13C13.168 5.477 14.754 5 16.5 5c1.747 0 3.332.477 4.5 1.253v13C19.832 18.477 18.247 18 16.5 18c-1.746 0-3.332.477-4.5 1.253" />
                  </svg>
                  Docs
                </a>
                {sessionError && (
                  <div className="flex items-center gap-1.5 px-3 py-1.5 rounded-full bg-[rgba(244,63,94,0.1)] border border-[rgba(244,63,94,0.2)]">
                    <div className="w-1.5 h-1.5 rounded-full bg-[var(--accent-coral)]" />
                    <span className="text-[10px] font-bold text-[var(--accent-coral)]">{sessionError}</span>
                  </div>
                )}
               <div className="flex items-center gap-1.5 px-3 py-1.5 rounded-full bg-[var(--bg-glass)] backdrop-blur-xl border border-[var(--border-default)]">
                  <div className={`w-1.5 h-1.5 rounded-full ${sessionsLoading ? 'bg-[var(--accent-gold)] animate-pulse' : sessionError ? 'bg-[var(--accent-coral)]' : 'bg-[var(--accent-emerald)] shadow-[0_0_8px_var(--accent-emerald)]'}`} />
                  <span className="text-[10px] font-bold text-[var(--text-secondary)]">{sessionsLoading ? 'PREPARING...' : sessionError ? 'ERROR' : 'GATEWAY ONLINE'}</span>
               </div>
            </div>
          </div>
        </header>

        {/* Chat Thread */}
        <div className="flex-1 relative overflow-hidden">
          {(!activeSessionId && sessionsLoading) ? (
            <div className="absolute inset-0 flex flex-col items-center justify-center bg-[var(--bg-base)] z-10 animate-fade-in">
              <div className="relative mb-6">
                <div className="absolute inset-0 bg-[var(--accent-gold)] opacity-15 blur-2xl rounded-full animate-pulse" />
                <BrandIcon size={56} className="relative z-10 animate-float" />
              </div>
              <p className="text-sm font-medium text-[var(--text-muted)] animate-pulse">Starting new session...</p>
            </div>
          ) : !activeSessionId ? (
            <div className="absolute inset-0 flex flex-col items-center justify-center bg-[var(--bg-base)] p-8 text-center">
               <div className="w-20 h-20 rounded-3xl glass-dark flex items-center justify-center mb-8">
                  <BrandIcon size={60} />
               </div>
               <h2 className="text-xl font-display font-bold text-[var(--text-primary)] mb-3">Empower Your Coding</h2>
               <p className="text-sm text-[var(--text-muted)] max-w-sm mb-10 leading-relaxed">
                 Select an existing session from the sidebar or start a new high-fidelity coding conversation.
               </p>
               <button
                 onClick={handleCreateNew}
                 className="px-8 py-3 rounded-full bg-[var(--accent-gold)] text-black text-sm font-bold shadow-[0_8px_32px_rgba(251,191,36,0.15)] hover:scale-105 active:scale-95 transition-all"
               >
                 {sessions.length === 0 ? 'Start Your First Project' : 'New Chat'}
               </button>
            </div>
          ) : (
            <ChatInterface
              key={activeSessionId}
              sessionId={activeSessionId}
              overrideWorkDir={urlDir ?? undefined}
              onMetricsChange={setSessionMetrics}
              onSessionStateChange={(state) => activeSessionId && updateSessionState(activeSessionId, state)}
              workspaceId={activeWorkspace?.id}
            />
          )}
        </div>
      </main>

      {/* 4. New Session Modal (with workspace defaultWorkDir propagation) */}
      {showNewModal && (
        <NewSessionModal
          onConfirm={handleModalConfirm}
          onCancel={() => setShowNewModal(false)}
          defaultWorkDir={activeWorkspace?.work_dir}
        />
      )}
    </div>
  );
}

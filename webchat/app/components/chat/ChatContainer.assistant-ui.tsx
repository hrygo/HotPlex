'use client';

import { useState, useCallback, useEffect } from 'react';
import { useRouter } from 'next/navigation';
import {
  AssistantRuntimeProvider,
  useExternalStoreRuntime,
} from '@assistant-ui/react';
import { useHotPlexRuntime } from '@/lib/adapters/hotplex-runtime-adapter';
import { useSessions } from '@/lib/hooks/useSessions';
import { Thread } from '@/components/assistant-ui/thread';
import { BrandIcon, WORKER_DISPLAY } from '@/components/icons';
import { SessionPanel } from './SessionPanel';
import { NewSessionModal } from './NewSessionModal';
import { NewWorkspaceModal } from './NewWorkspaceModal';
import { MetricsBar } from '@/components/assistant-ui/MetricsBar';
import { workerType as defaultWorkerType, httpBase, type ConnectionState } from '@/lib/config';
import type { SessionMetrics } from '@/lib/hooks/useMetrics';
import { useSkillsCache } from '@/lib/hooks/useSkillsCache';
import {
  listWorkspaces,
  createWorkspace,
  type Workspace,
} from '@/lib/api/workspaces';
import { buildWorkspaceWorkDir } from '@/lib/utils/workspace-path';
import { logout, getMe, type User } from '@/lib/api/auth';
import { ApiError } from '@/lib/api/errors';

// Clear all hotplex workspace/session selection state from localStorage.
// Called on logout so a different account logging into the same browser does
// not inherit a workspace_id it does not own — which the server rejects at
// WS handshake ("workspace access denied"). See internal/gateway/conn.go.
function clearHotplexSessionStorage(): void {
  try {
    const keys = Object.keys(localStorage).filter(
      (k) =>
        k.startsWith('hotplex_active_session_id') ||
        k === 'hotplex_active_workspace_id',
    );
    keys.forEach((k) => localStorage.removeItem(k));
  } catch {
    // localStorage may be unavailable (private mode); logout proceeds regardless.
  }
}

function ChatInterface({
  sessionId,
  onMetricsChange,
  onSessionStateChange,
  workspaceId,
  onWorkspaceError,
}: {
  sessionId: string | null;
  onMetricsChange?: (metrics: SessionMetrics) => void;
  onSessionStateChange?: (state: string) => void;
  workspaceId?: string;
  onWorkspaceError?: () => void;
}) {
  const { skills, mergeSkills } = useSkillsCache(sessionId);
  const adapter = useHotPlexRuntime({
    sessionId: sessionId ?? undefined,
    onMetricsChange,
    onSkillsChange: mergeSkills,
    onSessionStateChange,
    workspaceId,
    onWorkspaceError,
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
  const router = useRouter();
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [showNewModal, setShowNewModal] = useState(false);
  const [wsDropdownOpen, setWsDropdownOpen] = useState(false);
  const [authError, setAuthError] = useState(false);
  const [sessionMetrics, setSessionMetrics] = useState<SessionMetrics | null>(null);

  // Workspaces State
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [activeWorkspace, setActiveWorkspace] = useState<Workspace | null>(null);
  const [currentUser, setCurrentUser] = useState<User | null>(null);
  const [showNewWsModal, setShowNewWsModal] = useState(false);

  // Fetch workspaces list
  const loadWorkspaces = useCallback(async (selectId?: string) => {
    try {
      const res = await listWorkspaces();
      let list = res.workspaces || [];

      // Fallback: If no workspaces exist, create a default one
      if (list.length === 0) {
        const me = await getMe();
        const defaultWS = await createWorkspace(
          'Default Workspace',
          buildWorkspaceWorkDir(me.id, 'Default Workspace', 'default'),
        );
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

  // Detect session expiry so the Settings button can flag re-login.
  // Distinguish loading from auth-error (401) — a stale session surfaces a
  // re-login prompt instead of a silent dead click (PR #779 review P3-7).
  useEffect(() => {
    const ctrl = new AbortController();
    getMe(ctrl.signal)
      .then((u) => {
        if (ctrl.signal.aborted) return;
        setCurrentUser(u);
        setAuthError(false);
      })
      .catch((err) => {
        if (ctrl.signal.aborted) return;
        // Only surface auth-expired for real 401/403; transient network/5xx
        // failures are not solvable by re-login (PR #783 review P2).
        if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
          setAuthError(true);
        }
      });
    return () => { ctrl.abort(); };
  }, []);

  const handleSwitchWorkspace = (ws: Workspace) => {
    setActiveWorkspace(ws);
    localStorage.setItem('hotplex_active_workspace_id', ws.id);
    setSessionMetrics(null); // Reset metrics on workspace switch
    setWsDropdownOpen(false);
  };

  const handleLogout = async () => {
    clearHotplexSessionStorage();
    try {
      await logout();
    } catch {
      // Ignore error and redirect
    }
    window.location.replace('/login');
  };

  // Workspace handshake rejected (access denied / not found / disabled) —
  // clear the stale selection and reload. loadWorkspaces() re-derives the
  // active workspace as list[0] (only owned workspaces are returned), which
  // changes the workspaceId prop → useSessions refetches → activeSessionId
  // changes → ChatInterface remounts and reconnects with a valid workspace.
  const handleWorkspaceError = useCallback(() => {
    localStorage.removeItem('hotplex_active_workspace_id');
    loadWorkspaces();
  }, [loadWorkspaces]);

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
  const handleModalConfirm = useCallback(async (title: string, wt: string) => {
    setShowNewModal(false);
    await createNewSession(title, wt);
  }, [createNewSession]);

  // Handle "New Chat" button
  const handleCreateNew = useCallback(() => {
    setShowNewModal(true);
  }, []);

  const workerLabel = WORKER_DISPLAY[activeSession?.worker_type ?? defaultWorkerType] ?? activeSession?.worker_type ?? defaultWorkerType;

  return (
    <div className="flex h-screen flex-col overflow-hidden bg-[var(--bg-base)]">
      {/* Top bar: workspace switcher + brand + actions (P3.1 redesign — replaces the 72px nav) */}
      <header className="h-14 flex items-center px-3 gap-2 border-b border-[var(--border-subtle)] bg-[rgba(15,15,18,0.6)] backdrop-blur-xl flex-shrink-0 z-30">
        {/* Sidebar collapse toggle */}
        <button
          onClick={() => setSidebarOpen(!sidebarOpen)}
          className="p-2 text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)] rounded-lg transition-all active:scale-95"
          title={sidebarOpen ? 'Collapse sidebar' : 'Expand sidebar'}
        >
          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
          </svg>
        </button>

        {/* Workspace dropdown */}
        <div className="relative flex-shrink-0">
          <button
            onClick={() => setWsDropdownOpen((v) => !v)}
            className="flex items-center gap-2 pl-1.5 pr-2 py-1.5 rounded-lg bg-[var(--bg-surface)] hover:bg-[var(--bg-hover)] border border-[var(--border-subtle)] hover:border-[var(--border-default)] text-sm font-bold text-[var(--text-primary)] transition-all"
          >
            <span className="w-6 h-6 rounded-md bg-[var(--accent-gold)] text-black flex items-center justify-center text-[10px] font-black uppercase tracking-wider">
              {(activeWorkspace?.name || '?').slice(0, 2)}
            </span>
            <span className="truncate max-w-[140px]">{activeWorkspace?.name || 'Workspace'}</span>
            <svg className={`w-4 h-4 text-[var(--text-muted)] transition-transform ${wsDropdownOpen ? 'rotate-180' : ''}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M19 9l-7 7-7-7" />
            </svg>
          </button>
          {wsDropdownOpen && (
            <>
              <div className="fixed inset-0 z-40" onClick={() => setWsDropdownOpen(false)} />
              <div className="absolute top-full left-0 mt-1 w-64 bg-[var(--bg-elevated)] border border-[var(--border-default)] rounded-xl shadow-xl py-1.5 z-50">
                <p className="px-3 py-1 text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-widest">Workspaces</p>
                {workspaces.map((ws) => {
                  const isActive = activeWorkspace?.id === ws.id;
                  return (
                    <button
                      key={ws.id}
                      onClick={() => handleSwitchWorkspace(ws)}
                      className={`w-full flex items-center gap-2.5 px-3 py-2 text-sm transition-colors ${isActive ? 'bg-[var(--accent-gold)]/10 text-[var(--accent-gold)]' : 'text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)]'}`}
                    >
                      <span className={`w-6 h-6 rounded-md flex items-center justify-center text-[10px] font-black uppercase flex-shrink-0 ${isActive ? 'bg-[var(--accent-gold)] text-black' : 'bg-[var(--bg-surface)] text-[var(--text-muted)] border border-[var(--border-subtle)]'}`}>
                        {ws.name.slice(0, 2)}
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="truncate font-medium block">{ws.name}</span>
                        {isActive && ws.work_dir && (
                          <span className="block text-[10px] font-mono text-[var(--text-faint)] truncate" title={ws.work_dir}>{ws.work_dir}</span>
                        )}
                      </span>
                    </button>
                  );
                })}
                <div className="border-t border-[var(--border-subtle)] my-1" />
                <button
                  type="button"
                  onClick={() => { setWsDropdownOpen(false); setShowNewWsModal(true); }}
                  className="w-full flex items-center gap-2.5 px-3 py-2 text-sm text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--accent-gold)] transition-colors"
                >
                  <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M12 4v16m8-8H4" />
                  </svg>
                  <span className="font-medium">New Workspace</span>
                </button>
              </div>
            </>
          )}
        </div>

        {/* Worker / connection status pill */}
        <div className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-full bg-[var(--bg-glass)] backdrop-blur-xl border border-[var(--border-default)] flex-shrink-0">
          <div className={`w-1.5 h-1.5 rounded-full ${sessionsLoading ? 'bg-[var(--accent-gold)] animate-pulse' : sessionError ? 'bg-[var(--accent-coral)]' : 'bg-[var(--accent-emerald)] shadow-[0_0_8px_var(--accent-emerald)]'}`} />
          <span className="text-[10px] font-bold text-[var(--text-secondary)] uppercase tracking-wider">
            {sessionsLoading ? 'PREPARING' : sessionError ? 'ERROR' : 'ACTIVE'} · {workerLabel}
          </span>
        </div>

        {/* Center brand */}
        <div className="flex-1 flex items-center justify-center gap-2 min-w-0">
          <BrandIcon size={18} />
          <span className="font-display font-bold text-sm text-[var(--text-primary)] tracking-tight hidden sm:inline">HotPlex</span>
        </div>

        {/* Right actions */}
        <div className="flex items-center gap-1.5 flex-shrink-0">
          {sessionMetrics && sessionMetrics.turnCount > 0 && (
            <MetricsBar session={sessionMetrics} />
          )}
          {sessionError && (
            <span className="text-[10px] font-bold text-[var(--accent-coral)] px-2 truncate max-w-[160px]" title={sessionError}>{sessionError}</span>
          )}
          <a
            href={`${httpBase()}/docs/`}
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-1.5 px-3 py-1.5 text-[var(--accent-gold)] bg-[var(--accent-gold)]/10 border border-[var(--accent-gold)]/20 hover:bg-[var(--accent-gold)] hover:text-black rounded-full transition-all font-bold text-[10px] uppercase tracking-wider"
            title="Documentation"
          >
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M12 6.253v13m0-13C10.832 5.477 9.246 5 7.5 5S4.168 5.477 3 6.253v13C4.168 18.477 5.754 18 7.5 18s3.332.477 4.5 1.253m0-13C13.168 5.477 14.754 5 16.5 5c1.747 0 3.332.477 4.5 1.253v13C19.832 18.477 18.247 18 16.5 18c-1.746 0-3.332.477-4.5 1.253" />
            </svg>
            <span className="hidden md:inline">Docs</span>
          </a>
          {/* Settings — full /settings page (Phase 3, moved out of modal). Red dot on session expiry (PR #779 review P3-7). */}
          <button
            onClick={() => {
              if (authError) {
                window.location.replace('/login');
                return;
              }
              router.push('/settings');
            }}
            className={`relative p-2 rounded-lg transition-all ${authError ? 'text-[var(--accent-coral)] hover:bg-[var(--accent-coral)]/10' : 'text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)]'}`}
            title={authError ? 'Session expired — please re-login to access Settings' : 'Settings'}
          >
            {authError && (
              <span className="absolute top-1 right-1 w-1.5 h-1.5 rounded-full bg-[var(--accent-coral)]" />
            )}
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.066 2.573c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.573 1.066c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.066-2.573c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
            </svg>
          </button>
          {/* Logout */}
          <button
            onClick={handleLogout}
            className="p-2 text-[var(--text-muted)] hover:text-[var(--accent-coral)] hover:bg-[var(--bg-hover)] rounded-lg transition-all"
            title="Log Out"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1" />
            </svg>
          </button>
        </div>
      </header>

      {/* Body: session sidebar + chat */}
      <div className="flex flex-1 overflow-hidden">
        <aside className={`transition-all duration-300 ease-in-out ${sidebarOpen ? 'w-[280px]' : 'w-0'} overflow-hidden flex-shrink-0 relative z-20`}>
          <SessionPanel
            sessions={sessions}
            activeSession={activeSession}
            isLoading={sessionsLoading}
            onSelect={selectSession}
            onCreate={handleCreateNew}
            onDelete={removeSession}
            currentUserRole={currentUser?.role}
          />
        </aside>

        <main className="flex-1 flex flex-col min-w-0 relative">
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
                onMetricsChange={setSessionMetrics}
                onSessionStateChange={(state) => activeSessionId && updateSessionState(activeSessionId, state)}
                workspaceId={activeWorkspace?.id}
                onWorkspaceError={handleWorkspaceError}
              />
            )}
          </div>
        </main>
      </div>

      {/* New Session Modal */}
      {showNewModal && (
        <NewSessionModal
          onConfirm={handleModalConfirm}
          onCancel={() => setShowNewModal(false)}
        />
      )}

      {/* New Workspace Modal */}
      {showNewWsModal && currentUser && (
        <NewWorkspaceModal
          uid={currentUser.id}
          onClose={() => setShowNewWsModal(false)}
          onCreated={(ws) => {
            handleSwitchWorkspace(ws);
            loadWorkspaces(ws.id);
          }}
        />
      )}
    </div>
  );
}

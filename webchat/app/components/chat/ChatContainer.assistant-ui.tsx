'use client';

import { useState, useCallback, useEffect } from 'react';
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
import { workerType as defaultWorkerType, workDir, httpBase, type ConnectionState } from '@/lib/config';
import type { SessionMetrics } from '@/lib/hooks/useMetrics';
import { useSkillsCache } from '@/lib/hooks/useSkillsCache';
import {
  listWorkspaces,
  createWorkspace,
  updateWorkspace,
  deleteWorkspace,
  type Workspace,
} from '@/lib/api/workspaces';
import {
  getMe,
  logout,
  adminListUsers,
  adminUpdateUserStatus,
  adminListInvitations,
  adminCreateInvitation,
  adminDeleteInvitation,
  type User,
  type Invitation,
} from '@/lib/api/auth';
import { AnimatePresence, motion } from 'framer-motion';

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

  // Auth & Workspaces State
  const [currentUser, setCurrentUser] = useState<User | null>(null);
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [activeWorkspace, setActiveWorkspace] = useState<Workspace | null>(null);
  const [workspacesLoading, setWorkspacesLoading] = useState(true);
  
  // Modals state
  const [showNewWorkspaceModal, setShowNewWorkspaceModal] = useState(false);
  const [newWorkspaceName, setNewWorkspaceName] = useState('');
  const [newWorkspaceDir, setNewWorkspaceDir] = useState('');
  const [newWorkspaceLoading, setNewWorkspaceLoading] = useState(false);
  const [newWorkspaceError, setNewWorkspaceError] = useState('');

  const [showSettingsModal, setShowSettingsModal] = useState(false);
  const [settingsTab, setSettingsTab] = useState<'general' | 'ai' | 'members' | 'profile'>('general');

  // Load User Info
  useEffect(() => {
    getMe()
      .then(setCurrentUser)
      .catch(() => {
        // Fallback or ignore since root page will redirect if not authenticated
      });
  }, []);

  // Fetch workspaces list
  const loadWorkspaces = useCallback(async (selectId?: string) => {
    try {
      setWorkspacesLoading(true);
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
    } finally {
      setWorkspacesLoading(false);
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

  const handleCreateWorkspaceSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newWorkspaceName.trim() || !newWorkspaceDir.trim()) return;

    setNewWorkspaceLoading(true);
    setNewWorkspaceError('');
    try {
      const ws = await createWorkspace(newWorkspaceName.trim(), newWorkspaceDir.trim());
      setNewWorkspaceName('');
      setNewWorkspaceDir('');
      setShowNewWorkspaceModal(false);
      await loadWorkspaces(ws.id);
    } catch (err: any) {
      setNewWorkspaceError(err.message || 'Failed to create workspace.');
    } finally {
      setNewWorkspaceLoading(false);
    }
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

            {/* Create Workspace Button */}
            <button
              onClick={() => setShowNewWorkspaceModal(true)}
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
            </button>
          </div>
        </div>

        {/* Bottom profile actions */}
        <div className="flex flex-col items-center gap-3 w-full">
          {/* Settings button */}
          <button
            onClick={() => {
              setSettingsTab('general');
              setShowSettingsModal(true);
            }}
            className="w-12 h-12 rounded-xl bg-[var(--bg-surface)] hover:bg-[var(--bg-hover)] border border-[var(--border-subtle)] hover:border-[var(--border-default)] flex items-center justify-center text-[var(--text-secondary)] hover:text-[var(--text-primary)] transition-all duration-300 group"
            title="Workspace Settings"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.066 2.573c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.573 1.066c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.066-2.573c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
            </svg>

            {/* Tooltip */}
            <div className="absolute left-[80px] bg-[var(--bg-elevated)] border border-[var(--border-default)] px-2.5 py-1.5 rounded-lg text-xs font-bold font-display text-[var(--text-primary)] whitespace-nowrap opacity-0 group-hover:opacity-100 pointer-events-none transition-all duration-200 z-50 shadow-md">
              Settings & Admin
            </div>
          </button>

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

      {/* 5. Create Workspace Modal */}
      {showNewWorkspaceModal && (
        <CreateWorkspaceDialog
          loading={newWorkspaceLoading}
          error={newWorkspaceError}
          onSubmit={handleCreateWorkspaceSubmit}
          name={newWorkspaceName}
          setName={setNewWorkspaceName}
          dir={newWorkspaceDir}
          setDir={setNewWorkspaceDir}
          onCancel={() => {
            setShowNewWorkspaceModal(false);
            setNewWorkspaceName('');
            setNewWorkspaceDir('');
            setNewWorkspaceError('');
          }}
        />
      )}

      {/* 6. Settings Modal */}
      {showSettingsModal && activeWorkspace && (
        <SettingsModal
          tab={settingsTab}
          setTab={setSettingsTab}
          workspace={activeWorkspace}
          currentUser={currentUser}
          onClose={() => setShowSettingsModal(false)}
          onWorkspaceUpdated={(ws) => {
            setActiveWorkspace(ws);
            loadWorkspaces(ws.id);
          }}
          onWorkspaceDeleted={() => {
            setShowSettingsModal(false);
            loadWorkspaces();
          }}
        />
      )}
    </div>
  );
}

// Sub-component: Create Workspace Dialog
function CreateWorkspaceDialog({
  onSubmit,
  name,
  setName,
  dir,
  setDir,
  loading,
  error,
  onCancel,
}: {
  onSubmit: (e: React.FormEvent) => void;
  name: string;
  setName: (v: string) => void;
  dir: string;
  setDir: (v: string) => void;
  loading: boolean;
  error: string;
  onCancel: () => void;
}) {
  return (
    <motion.div
      className="fixed inset-0 z-[300] flex items-center justify-center"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
    >
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onCancel} />
      <motion.div
        className="relative w-full max-w-md mx-4 rounded-xl border border-[var(--border-default)] bg-[var(--bg-surface)] p-6 shadow-2xl z-10"
        initial={{ opacity: 0, scale: 0.95, y: 15 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
      >
        <h3 className="font-display font-bold text-lg text-[var(--text-primary)] mb-1">
          Create Workspace
        </h3>
        <p className="text-xs text-[var(--text-muted)] mb-4 leading-relaxed">
          Workspaces organize your AI coding configurations and working directories.
        </p>

        {error && (
          <div className="mb-4 rounded-lg border border-[var(--accent-coral)]/30 bg-[var(--accent-coral)]/10 p-3 text-xs text-[var(--accent-coral)]">
            {error}
          </div>
        )}

        <form onSubmit={onSubmit} className="space-y-4">
          <div>
            <label className="mb-1 block text-[10px] font-bold uppercase tracking-wider text-[var(--text-muted)]">
              Workspace Name
            </label>
            <input
              type="text"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. My Next Project"
              className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3 py-2 text-sm text-[var(--text-primary)] outline-none focus:border-[var(--accent-gold)]/40 focus:ring-1 focus:ring-[var(--accent-gold)]/20"
            />
          </div>

          <div>
            <label className="mb-1 block text-[10px] font-bold uppercase tracking-wider text-[var(--text-muted)]">
              Workspace Root Directory
            </label>
            <input
              type="text"
              required
              value={dir}
              onChange={(e) => setDir(e.target.value)}
              placeholder="e.g. /Users/name/projects/my-app"
              className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3 py-2 text-sm text-[var(--text-primary)] outline-none focus:border-[var(--accent-gold)]/40 focus:ring-1 focus:ring-[var(--accent-gold)]/20 font-mono"
            />
          </div>

          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={onCancel}
              className="px-4 py-2 rounded-lg text-xs font-bold text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)]"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={loading}
              className="px-5 py-2 rounded-lg bg-[var(--accent-gold)] text-black text-xs font-bold hover:bg-[var(--accent-gold-bright)] shadow-md"
            >
              {loading ? 'Creating...' : 'Create'}
            </button>
          </div>
        </form>
      </motion.div>
    </motion.div>
  );
}

// Sub-component: Settings Modal with tabs
function SettingsModal({
  tab,
  setTab,
  workspace,
  currentUser,
  onClose,
  onWorkspaceUpdated,
  onWorkspaceDeleted,
}: {
  tab: 'general' | 'ai' | 'members' | 'profile';
  setTab: (t: 'general' | 'ai' | 'members' | 'profile') => void;
  workspace: Workspace;
  currentUser: User | null;
  onClose: () => void;
  onWorkspaceUpdated: (ws: Workspace) => void;
  onWorkspaceDeleted: () => void;
}) {
  const isAdmin = currentUser?.role === 'admin';

  // Workspace General edit states
  const [wsName, setWsName] = useState(workspace.name);
  const [wsDir, setWsDir] = useState(workspace.work_dir);
  const [savingGeneral, setSavingGeneral] = useState(false);
  const [generalError, setGeneralError] = useState('');
  const [confirmDeleteWS, setConfirmDeleteWS] = useState(false);

  // AI Configuration/Preference states
  const [workerPreference, setWorkerPreference] = useState(workspace.worker_preference || 'claude_code');
  const [overridesList, setOverridesList] = useState<{ key: string; val: string }[]>(() => {
    const obj = workspace.agent_config_overrides || {};
    return Object.entries(obj).map(([k, v]) => ({ key: k, val: v }));
  });
  const [savingAI, setSavingAI] = useState(false);
  const [aiError, setAIError] = useState('');

  // Admin users / invites states
  const [users, setUsers] = useState<User[]>([]);
  const [usersLoading, setUsersLoading] = useState(false);
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [invitesLoading, setInvitesLoading] = useState(false);
  const [inviteRole, setInviteRole] = useState<'user' | 'admin'>('user');
  const [inviteTTL, setInviteTTL] = useState(86400); // Default 24 hours in seconds
  const [generatedInviteCode, setGeneratedInviteCode] = useState('');
  const [creatingInvite, setCreatingInvite] = useState(false);

  // Sync state if active workspace changes under setting
  useEffect(() => {
    setWsName(workspace.name);
    setWsDir(workspace.work_dir);
    setWorkerPreference(workspace.worker_preference || 'claude_code');
    const obj = workspace.agent_config_overrides || {};
    setOverridesList(Object.entries(obj).map(([k, v]) => ({ key: k, val: v })));
    setConfirmDeleteWS(false);
    setGeneralError('');
    setAIError('');
  }, [workspace]);

  // Load Admin Data
  useEffect(() => {
    if (tab === 'members' && isAdmin) {
      const loadAdminData = async () => {
        setUsersLoading(true);
        setInvitesLoading(true);
        try {
          const uRes = await adminListUsers();
          setUsers(uRes.users || []);
        } catch (err) {
          console.error(err);
        } finally {
          setUsersLoading(false);
        }

        try {
          const iRes = await adminListInvitations();
          setInvitations(iRes.invitations || []);
        } catch (err) {
          console.error(err);
        } finally {
          setInvitesLoading(false);
        }
      };
      loadAdminData();
    }
  }, [tab, isAdmin]);

  // Workspace General Save
  const handleSaveGeneral = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!wsName.trim()) return;
    setSavingGeneral(true);
    setGeneralError('');
    try {
      const updated = await updateWorkspace(workspace.id, {
        name: wsName.trim(),
      });
      onWorkspaceUpdated(updated);
    } catch (err: any) {
      setGeneralError(err.message || 'Failed to update workspace.');
    } finally {
      setSavingGeneral(false);
    }
  };

  // Workspace Delete
  const handleDeleteWS = async () => {
    try {
      await deleteWorkspace(workspace.id);
      onWorkspaceDeleted();
    } catch (err: any) {
      setGeneralError(err.message || 'Failed to delete workspace.');
    }
  };

  // AI config save
  const handleSaveAI = async (e: React.FormEvent) => {
    e.preventDefault();
    setSavingAI(true);
    setAIError('');

    // Reconstruct overrides object
    const overridesObj: Record<string, string> = {};
    for (const item of overridesList) {
      if (item.key.trim()) {
        overridesObj[item.key.trim()] = item.val;
      }
    }

    try {
      const updated = await updateWorkspace(workspace.id, {
        workerPreference: workerPreference,
        agentConfigOverrides: overridesObj,
      });
      onWorkspaceUpdated(updated);
    } catch (err: any) {
      setAIError(err.message || 'Failed to save configuration.');
    } finally {
      setSavingAI(false);
    }
  };

  const handleAddOverride = () => {
    setOverridesList((prev) => [...prev, { key: '', val: '' }]);
  };

  const handleRemoveOverride = (idx: number) => {
    setOverridesList((prev) => prev.filter((_, i) => i !== idx));
  };

  const handleOverrideChange = (idx: number, field: 'key' | 'val', value: string) => {
    setOverridesList((prev) => {
      const copy = [...prev];
      copy[idx] = { ...copy[idx], [field]: value };
      return copy;
    });
  };

  // User toggle (Admin action)
  const handleToggleUserStatus = async (user: User) => {
    const newStatus = user.status === 'active' ? 'disabled' : 'active';
    try {
      await adminUpdateUserStatus(user.id, newStatus);
      setUsers((prev) =>
        prev.map((u) => (u.id === user.id ? { ...u, status: newStatus } : u))
      );
    } catch (err: any) {
      alert(err.message || 'Failed to update user status.');
    }
  };

  // Revoke invite (Admin action)
  const handleRevokeInvite = async (inviteId: string) => {
    try {
      await adminDeleteInvitation(inviteId);
      setInvitations((prev) => prev.filter((i) => i.id !== inviteId));
    } catch (err: any) {
      alert(err.message || 'Failed to revoke invitation.');
    }
  };

  // Create invite (Admin action)
  const handleCreateInviteSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setCreatingInvite(true);
    setGeneratedInviteCode('');
    try {
      const invite = await adminCreateInvitation(inviteRole, inviteTTL);
      setGeneratedInviteCode(invite.code);
      // reload invitations list
      const iRes = await adminListInvitations();
      setInvitations(iRes.invitations || []);
    } catch (err: any) {
      alert(err.message || 'Failed to create invitation.');
    } finally {
      setCreatingInvite(false);
    }
  };

  return (
    <motion.div
      className="fixed inset-0 z-[250] flex items-center justify-center"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
    >
      <div className="absolute inset-0 bg-black/65 backdrop-blur-sm" onClick={onClose} />
      
      <motion.div
        className="relative w-full max-w-4xl h-[600px] mx-4 rounded-xl border border-[var(--border-default)] bg-[var(--bg-surface)] flex overflow-hidden z-10 shadow-2xl"
        initial={{ opacity: 0, scale: 0.96, y: 15 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
      >
        {/* Settings Tab Sidebar (Left) */}
        <aside className="w-[220px] border-r border-[var(--border-subtle)] bg-[#0c0c0e] p-4 flex flex-col justify-between flex-shrink-0">
          <div className="space-y-6">
            <div>
              <h4 className="text-[10px] font-mono font-bold tracking-widest text-[var(--text-faint)] uppercase pl-3">
                Workspace
              </h4>
              <div className="mt-2.5 space-y-1">
                <button
                  onClick={() => setTab('general')}
                  className={`w-full text-left px-3 py-2.5 rounded-lg text-xs font-bold flex items-center gap-2 transition-all ${
                    tab === 'general'
                      ? 'bg-[var(--bg-elevated)] text-[var(--accent-gold)] border border-[var(--border-subtle)]'
                      : 'text-[var(--text-secondary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)]'
                  }`}
                >
                  General Settings
                </button>
                <button
                  onClick={() => setTab('ai')}
                  className={`w-full text-left px-3 py-2.5 rounded-lg text-xs font-bold flex items-center gap-2 transition-all ${
                    tab === 'ai'
                      ? 'bg-[var(--bg-elevated)] text-[var(--accent-gold)] border border-[var(--border-subtle)]'
                      : 'text-[var(--text-secondary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)]'
                  }`}
                >
                  AI Configurations
                </button>
              </div>
            </div>

            {isAdmin && (
              <div>
                <h4 className="text-[10px] font-mono font-bold tracking-widest text-[var(--text-faint)] uppercase pl-3">
                  Admin Panel
                </h4>
                <div className="mt-2.5 space-y-1">
                  <button
                    onClick={() => setTab('members')}
                    className={`w-full text-left px-3 py-2.5 rounded-lg text-xs font-bold flex items-center gap-2 transition-all ${
                      tab === 'members'
                        ? 'bg-[var(--bg-elevated)] text-[var(--accent-gold)] border border-[var(--border-subtle)]'
                        : 'text-[var(--text-secondary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)]'
                    }`}
                  >
                    Members & Invites
                  </button>
                </div>
              </div>
            )}
          </div>

          <div>
            <h4 className="text-[10px] font-mono font-bold tracking-widest text-[var(--text-faint)] uppercase pl-3 mb-2">
              User Profile
            </h4>
            <button
              onClick={() => setTab('profile')}
              className={`w-full text-left px-3 py-2.5 rounded-lg text-xs font-bold flex items-center gap-2 transition-all ${
                tab === 'profile'
                  ? 'bg-[var(--bg-elevated)] text-[var(--accent-gold)] border border-[var(--border-subtle)]'
                  : 'text-[var(--text-secondary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)]'
              }`}
            >
              My Profile
            </button>
          </div>
        </aside>

        {/* Settings Tab Content (Right) */}
        <section className="flex-1 flex flex-col h-full overflow-hidden bg-[var(--bg-surface)]">
          {/* Header */}
          <header className="px-6 py-4 border-b border-[var(--border-subtle)] flex items-center justify-between flex-shrink-0">
            <div>
              <h3 className="font-display font-black text-lg text-[var(--text-primary)]">
                {tab === 'general' && 'General Settings'}
                {tab === 'ai' && 'AI Configurations'}
                {tab === 'members' && 'Member & Invitation Management'}
                {tab === 'profile' && 'User Account Profile'}
              </h3>
              <p className="text-xs text-[var(--text-muted)] mt-0.5">
                {tab === 'general' && 'Modify workspace properties and directory parameters'}
                {tab === 'ai' && 'Customize preferred AI agent models and config directives'}
                {tab === 'members' && 'Manage registration keys and active team member access'}
                {tab === 'profile' && 'Review your workspace credential profile and parameters'}
              </p>
            </div>
            <button
              onClick={onClose}
              className="p-1.5 rounded-lg text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)] transition-all"
            >
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </header>

          {/* Content Body */}
          <div className="flex-1 overflow-y-auto p-6">
            {tab === 'general' && (
              <div className="space-y-6">
                {generalError && (
                  <div className="rounded-lg border border-[var(--accent-coral)]/30 bg-[var(--accent-coral)]/10 p-3 text-xs text-[var(--accent-coral)]">
                    {generalError}
                  </div>
                )}
                
                <form onSubmit={handleSaveGeneral} className="space-y-4 max-w-lg">
                  <div>
                    <label className="mb-1 block text-[10px] font-bold uppercase tracking-wider text-[var(--text-muted)]">
                      Workspace Name
                    </label>
                    <input
                      type="text"
                      required
                      value={wsName}
                      onChange={(e) => setWsName(e.target.value)}
                      className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3.5 py-2 text-sm text-[var(--text-primary)] outline-none focus:border-[var(--accent-gold)]/40 focus:ring-1 focus:ring-[var(--accent-gold)]/20"
                    />
                  </div>

                  <div>
                    <div className="flex items-center justify-between mb-1">
                      <label className="block text-[10px] font-bold uppercase tracking-wider text-[var(--text-muted)]">
                        Workspace Root Directory
                      </label>
                      <span className="text-[10px] text-[var(--text-faint)] font-bold italic">
                        Immutable after creation
                      </span>
                    </div>
                    <input
                      type="text"
                      disabled
                      value={wsDir}
                      className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] opacity-60 px-3.5 py-2 text-sm text-[var(--text-faint)] cursor-not-allowed outline-none font-mono"
                    />
                  </div>

                  <button
                    type="submit"
                    disabled={savingGeneral || !wsName.trim()}
                    className="rounded-lg bg-[var(--accent-gold)] px-4 py-2.5 text-xs font-bold uppercase tracking-wider text-black hover:bg-[var(--accent-gold-bright)] transition-all active:scale-[0.98] disabled:opacity-40 disabled:cursor-not-allowed"
                  >
                    {savingGeneral ? 'Saving...' : 'Save General Settings'}
                  </button>
                </form>

                <div className="border-t border-[var(--border-subtle)] pt-6 max-w-lg">
                  <h4 className="text-xs font-bold text-[var(--accent-coral)] uppercase tracking-wider mb-2">
                    Danger Zone
                  </h4>
                  <p className="text-xs text-[var(--text-muted)] mb-4">
                    Deleting the workspace is permanent. All associated conversations, history records, and configurations stored in this workspace will be deleted.
                  </p>
                  
                  {confirmDeleteWS ? (
                    <div className="flex items-center gap-3">
                      <button
                        onClick={handleDeleteWS}
                        className="px-4 py-2 rounded-lg bg-[var(--accent-coral)] text-white text-xs font-bold hover:bg-rose-600 transition-all active:scale-95"
                      >
                        Confirm Delete
                      </button>
                      <button
                        onClick={() => setConfirmDeleteWS(false)}
                        className="px-4 py-2 rounded-lg bg-[var(--bg-hover)] text-[var(--text-primary)] text-xs font-bold hover:bg-[var(--bg-elevated)] transition-all"
                      >
                        Cancel
                      </button>
                    </div>
                  ) : (
                    <button
                      onClick={() => setConfirmDeleteWS(true)}
                      className="px-4 py-2 rounded-lg border border-[var(--accent-coral)]/30 text-[var(--accent-coral)] hover:bg-[var(--accent-coral)]/10 text-xs font-bold transition-all"
                    >
                      Delete Workspace
                    </button>
                  )}
                </div>
              </div>
            )}

            {tab === 'ai' && (
              <div className="space-y-6">
                {aiError && (
                  <div className="rounded-lg border border-[var(--accent-coral)]/30 bg-[var(--accent-coral)]/10 p-3 text-xs text-[var(--accent-coral)]">
                    {aiError}
                  </div>
                )}

                <form onSubmit={handleSaveAI} className="space-y-6 max-w-2xl">
                  {/* Dropdown selector */}
                  <div className="max-w-md">
                    <label className="mb-2 block text-[10px] font-bold uppercase tracking-wider text-[var(--text-muted)]">
                      Preferred Coding Agent Engine
                    </label>
                    <select
                      value={workerPreference}
                      onChange={(e) => setWorkerPreference(e.target.value)}
                      className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3.5 py-2 text-sm text-[var(--text-primary)] outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)]/20"
                    >
                      <option value="claude_code">Claude Code (Anthropic stdio)</option>
                      <option value="opencode_server">OpenCode Server (NextGen SSE)</option>
                      <option value="codex_cli">Codex CLI (OpenAI daemon)</option>
                      <option value="acp">ACP (Universal Agent RPC)</option>
                    </select>
                  </div>

                  {/* Overrides list */}
                  <div>
                    <div className="flex items-center justify-between mb-3">
                      <label className="block text-[10px] font-bold uppercase tracking-wider text-[var(--text-muted)]">
                        Agent Configuration Directives
                      </label>
                      <button
                        type="button"
                        onClick={handleAddOverride}
                        className="px-2.5 py-1 rounded border border-[var(--accent-gold)]/30 text-[var(--accent-gold)] hover:bg-[var(--accent-gold)]/10 text-[10px] font-bold uppercase tracking-wider transition-all"
                      >
                        + Add Configuration
                      </button>
                    </div>

                    <div className="space-y-2.5 max-h-[200px] overflow-y-auto custom-scrollbar border border-[var(--border-subtle)]/50 rounded-lg p-3 bg-[var(--bg-elevated)]/30">
                      {overridesList.length === 0 ? (
                        <p className="text-xs text-[var(--text-faint)] text-center py-4">
                          No custom agent config directives override in this workspace.
                        </p>
                      ) : (
                        overridesList.map((override, idx) => (
                          <div key={idx} className="flex gap-2 items-center">
                            <input
                              type="text"
                              value={override.key}
                              onChange={(e) => handleOverrideChange(idx, 'key', e.target.value)}
                              placeholder="Config Key (e.g. system_prompt)"
                              className="flex-1 rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3 py-1.5 text-xs text-[var(--text-primary)] font-mono outline-none"
                            />
                            <span className="text-[var(--text-faint)]">:</span>
                            <input
                              type="text"
                              value={override.val}
                              onChange={(e) => handleOverrideChange(idx, 'val', e.target.value)}
                              placeholder="Override Value string"
                              className="flex-1 rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3 py-1.5 text-xs text-[var(--text-primary)] outline-none"
                            />
                            <button
                              type="button"
                              onClick={() => handleRemoveOverride(idx)}
                              className="p-1.5 rounded-lg hover:bg-[var(--accent-coral)]/10 text-[var(--text-faint)] hover:text-[var(--accent-coral)] transition-all"
                            >
                              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                              </svg>
                            </button>
                          </div>
                        ))
                      )}
                    </div>
                  </div>

                  <button
                    type="submit"
                    disabled={savingAI}
                    className="rounded-lg bg-[var(--accent-gold)] px-4 py-2.5 text-xs font-bold uppercase tracking-wider text-black hover:bg-[var(--accent-gold-bright)] transition-all active:scale-[0.98]"
                  >
                    {savingAI ? 'Saving...' : 'Save AI Configuration'}
                  </button>
                </form>
              </div>
            )}

            {tab === 'members' && isAdmin && (
              <div className="space-y-8">
                {/* 1. Member Status Management */}
                <div>
                  <h4 className="text-xs font-black uppercase tracking-wider text-[var(--text-muted)] mb-3">
                    Active Team Members
                  </h4>
                  {usersLoading ? (
                    <div className="text-xs text-[var(--text-faint)]">Loading users list...</div>
                  ) : (
                    <div className="border border-[var(--border-subtle)] rounded-lg overflow-hidden bg-[var(--bg-surface)]">
                      <table className="w-full text-left border-collapse text-xs">
                        <thead>
                          <tr className="bg-[var(--bg-elevated)] border-b border-[var(--border-subtle)] text-[var(--text-muted)] uppercase tracking-wider font-bold">
                            <th className="p-3">Username</th>
                            <th className="p-3">Role</th>
                            <th className="p-3">Status</th>
                            <th className="p-3 text-right">Actions</th>
                          </tr>
                        </thead>
                        <tbody>
                          {users.map((u) => (
                            <tr key={u.id} className="border-b border-[var(--border-subtle)] hover:bg-[var(--bg-hover)]">
                              <td className="p-3 font-semibold text-[var(--text-primary)]">{u.username}</td>
                              <td className="p-3 font-mono text-[var(--text-muted)] uppercase">{u.role}</td>
                              <td className="p-3">
                                <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] font-bold ${
                                  u.status === 'active' ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20' : 'bg-rose-500/10 text-rose-400 border border-rose-500/20'
                                }`}>
                                  <span className={`w-1 h-1 rounded-full ${u.status === 'active' ? 'bg-emerald-400' : 'bg-rose-400'}`} />
                                  {u.status}
                                </span>
                              </td>
                              <td className="p-3 text-right">
                                {u.id !== currentUser?.id && (
                                  <button
                                    onClick={() => handleToggleUserStatus(u)}
                                    className={`px-2 py-1 rounded text-[10px] font-bold uppercase tracking-wider transition-all border ${
                                      u.status === 'active'
                                        ? 'border-rose-500/30 text-rose-400 hover:bg-rose-500/10'
                                        : 'border-emerald-500/30 text-emerald-400 hover:bg-emerald-500/10'
                                    }`}
                                  >
                                    {u.status === 'active' ? 'Disable' : 'Enable'}
                                  </button>
                                )}
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  )}
                </div>

                {/* 2. Invitation Code Generator */}
                <div className="grid grid-cols-1 md:grid-cols-2 gap-6 border-t border-[var(--border-subtle)] pt-6">
                  <div>
                    <h4 className="text-xs font-black uppercase tracking-wider text-[var(--text-muted)] mb-3">
                      Generate Invitation Key
                    </h4>
                    <form onSubmit={handleCreateInviteSubmit} className="space-y-4">
                      <div>
                        <label className="mb-1 block text-[10px] font-bold uppercase tracking-wider text-[var(--text-faint)]">
                          Assign Member Role
                        </label>
                        <select
                          value={inviteRole}
                          onChange={(e) => setInviteRole(e.target.value as 'user' | 'admin')}
                          className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3 py-1.5 text-xs text-[var(--text-primary)] outline-none"
                        >
                          <option value="user">User (Normal Access)</option>
                          <option value="admin">Admin (Full System Privilege)</option>
                        </select>
                      </div>

                      <div>
                        <label className="mb-1 block text-[10px] font-bold uppercase tracking-wider text-[var(--text-faint)]">
                          Key Lifespan Expiry
                        </label>
                        <select
                          value={inviteTTL}
                          onChange={(e) => setInviteTTL(Number(e.target.value))}
                          className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3 py-1.5 text-xs text-[var(--text-primary)] outline-none"
                        >
                          <option value={3600}>1 Hour</option>
                          <option value={86400}>24 Hours (1 Day)</option>
                          <option value={604800}>7 Days</option>
                          <option value={2592000}>30 Days</option>
                        </select>
                      </div>

                      <button
                        type="submit"
                        disabled={creatingInvite}
                        className="w-full rounded-lg bg-[var(--accent-gold)] text-black py-2 text-xs font-bold uppercase tracking-widest hover:bg-[var(--accent-gold-bright)] transition-all shadow-md"
                      >
                        {creatingInvite ? 'Generating...' : 'Generate Invite'}
                      </button>
                    </form>

                    {generatedInviteCode && (
                      <div className="mt-4 rounded-lg bg-[var(--bg-elevated)] border border-[var(--accent-emerald)]/30 p-3">
                        <p className="text-[10px] text-emerald-400 font-bold uppercase tracking-wide mb-1">
                          Generated Registration Key:
                        </p>
                        <div className="flex gap-2 items-center">
                          <code className="flex-1 text-xs text-[var(--text-primary)] bg-[var(--bg-surface)] p-2 rounded border border-[var(--border-subtle)] select-all truncate font-mono">
                            {generatedInviteCode}
                          </code>
                          <button
                            onClick={() => {
                              navigator.clipboard.writeText(generatedInviteCode);
                              alert('Code copied to clipboard!');
                            }}
                            className="px-2.5 py-2 text-[10px] font-bold uppercase tracking-wider bg-[var(--accent-emerald)] text-black rounded hover:bg-emerald-300"
                          >
                            Copy
                          </button>
                        </div>
                      </div>
                    )}
                  </div>

                  <div>
                    <h4 className="text-xs font-black uppercase tracking-wider text-[var(--text-muted)] mb-3">
                      Pending Invitation Keys
                    </h4>
                    {invitesLoading ? (
                      <div className="text-xs text-[var(--text-faint)]">Loading invitations list...</div>
                    ) : (
                      <div className="max-h-[250px] overflow-y-auto custom-scrollbar border border-[var(--border-subtle)] rounded-lg p-2.5 bg-[var(--bg-elevated)]/20">
                        {invitations.length === 0 ? (
                          <p className="text-xs text-[var(--text-faint)] text-center py-6">
                            No active invitation keys pending.
                          </p>
                        ) : (
                          <div className="space-y-2.5">
                            {invitations.map((i) => (
                              <div key={i.id} className="flex items-center justify-between bg-[var(--bg-elevated)] border border-[var(--border-subtle)] p-2 rounded-lg text-xs font-mono">
                                <div className="min-w-0 flex-1 pr-3">
                                  <div className="text-[var(--text-primary)] font-bold truncate select-all">{i.code}</div>
                                  <div className="text-[9px] text-[var(--text-muted)] uppercase mt-0.5">
                                    Role: {i.role} · Expires: {new Date(i.expires_at * 1000).toLocaleDateString()}
                                  </div>
                                </div>
                                <button
                                  onClick={() => handleRevokeInvite(i.id)}
                                  className="text-[10px] font-bold text-[var(--accent-coral)] hover:underline flex-shrink-0"
                                >
                                  Revoke
                                </button>
                              </div>
                            ))}
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                </div>
              </div>
            )}

            {tab === 'profile' && currentUser && (
              <div className="space-y-6 max-w-md">
                <div className="rounded-xl border border-[var(--border-default)] bg-[var(--bg-elevated)] p-6 space-y-4">
                  <div className="flex items-center gap-4">
                    <div className="w-12 h-12 rounded-full bg-[var(--accent-gold)]/10 border border-[var(--accent-gold)] flex items-center justify-center font-display font-black text-xl text-[var(--accent-gold)] uppercase">
                      {currentUser.username.slice(0, 2)}
                    </div>
                    <div>
                      <h4 className="font-display font-bold text-sm text-[var(--text-primary)]">
                        {currentUser.display_name || currentUser.username}
                      </h4>
                      <p className="text-xs text-[var(--text-faint)] uppercase font-mono tracking-wider">
                        {currentUser.role}
                      </p>
                    </div>
                  </div>

                  <div className="border-t border-[var(--border-subtle)] pt-4 space-y-3.5 text-xs font-mono">
                    <div className="flex justify-between">
                      <span className="text-[var(--text-muted)] font-semibold uppercase">Account ID:</span>
                      <span className="text-[var(--text-primary)] select-all">{currentUser.id}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-[var(--text-muted)] font-semibold uppercase">Username:</span>
                      <span className="text-[var(--text-primary)]">{currentUser.username}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-[var(--text-muted)] font-semibold uppercase">Created At:</span>
                      <span className="text-[var(--text-primary)]">
                        {new Date(currentUser.created_at * 1000).toLocaleString()}
                      </span>
                    </div>
                    {currentUser.last_login_at && (
                      <div className="flex justify-between">
                        <span className="text-[var(--text-muted)] font-semibold uppercase">Last Login:</span>
                        <span className="text-[var(--text-primary)]">
                          {new Date(currentUser.last_login_at * 1000).toLocaleString()}
                        </span>
                      </div>
                    )}
                  </div>
                </div>
              </div>
            )}
          </div>
        </section>
      </motion.div>
    </motion.div>
  );
}

/**
 * Session management hook for HotPlex webchat.
 *
 * Lifecycle:
 * 1. Mount / workspace switch → listSessions → pick default session
 *    (most-recently-active by updated_at; or initialSessionId if given)
 * 2. No session in workspace → auto-create anchor 'main' session
 * 3. User selects session → calls onSelect(sessionId)
 * 4. User creates new → POST → calls onSelect(newId)
 * 5. User deletes → optimistically removes from list
 *
 * 选择策略见 lib/session-select.ts(pickDefaultSession)。
 */

'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import {
  listSessions,
  createSession,
  deleteSession,
  AuthError,
  ANCHOR_CLIENT_SESSION_ID,
  type SessionInfo,
} from '@/lib/api/sessions';
import { workerType as defaultWorkerType } from '@/lib/config';
import { newSessionId } from '@/lib/ai-sdk-transport/client/envelope';
import { pickDefaultSession } from '@/lib/session-select';
import { logger } from '@/lib/logger';

export interface UseSessionsOptions {
  /** Called when the active session changes (user selects or creates). */
  onSelect: (sessionId: string) => void;
  /** Initial session to restore (e.g., from URL or localStorage). */
  initialSessionId?: string | null;
  /** Active workspace ID (spec ⑥) */
  workspaceId?: string;
}

export interface UseSessionsReturn {
  sessions: SessionInfo[];
  activeSession: SessionInfo | null;
  isLoading: boolean;
  error: string | null;
  isOpen: boolean;
  openPanel: () => void;
  closePanel: () => void;
  selectSession: (session: SessionInfo) => void;
  createNewSession: (title: string, workerType?: string) => Promise<void>;
  removeSession: (id: string) => Promise<void>;
  refreshSessions: () => Promise<void>;
  handleSessionSelect: (id: string) => void;
  updateSessionState: (sessionId: string, state: string) => void;
}

export function useSessions({
  onSelect,
  initialSessionId,
  workspaceId,
}: UseSessionsOptions): UseSessionsReturn {
  const [sessions, setSessions] = useState<SessionInfo[]>([]);
  const [activeSession, setActiveSession] = useState<SessionInfo | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [isOpen, setIsOpen] = useState(false);

  const onSelectRef = useRef(onSelect);
  const initialRef = useRef(initialSessionId);
  // Keep refs in sync with latest props (read inside callbacks below).
  useEffect(() => {
    onSelectRef.current = onSelect;
    initialRef.current = initialSessionId;
  });

  const isCreating = useRef(false);
  const DEFAULT_WORKER_TYPE = defaultWorkerType;

  // refreshSessions fetches the workspace's sessions and selects the default.
  // signal: caller (the workspaceId effect) passes an AbortController so a
  // fast A→B workspace switch cancels the in-flight A response before it can
  // overwrite activeSession (race → "entered the wrong session").
  const refreshSessions = useCallback(async (signal?: AbortSignal) => {
    const aborted = () => signal?.aborted ?? false;
    try {
      setIsLoading(true);
      setError(null);
      const { sessions: list } = await listSessions(20, 0, workspaceId, signal);
      if (aborted()) return;
      // Defensive null-guard: backend store.List returns [] for empty (make),
      // but guard against null from older deployments so we never crash on filter.
      const filtered = (list ?? []).filter(s => s.state !== 'deleted');
      setSessions(filtered);
      if (aborted()) return;

      // Default-session policy: explicit initialSessionId (hit) > most-recent
      // (updated_at) > none. savedId no longer participates — restoring a
      // stale "last selected" id fought the "enter the most recent session on
      // switch" expectation and blocked anchor auto-create when stale.
      const pick = pickDefaultSession(filtered, initialRef.current);
      if (pick) {
        setActiveSession(pick);
        onSelectRef.current(pick.id);
        return;
      }

      // No usable session in this workspace → auto-create the anchor 'main'
      // session so the workspace is immediately usable (期望:无 session 时创建默认).
      // DeriveSessionKey(userID, wt, 'main', workspaceID, workDir) is idempotent,
      // so concurrent callers (e.g. NewWorkspaceForm pre-create) resolve to one key.
      if (!isCreating.current) {
        isCreating.current = true;
        try {
          const { session_id } = await createSession({
            clientSessionId: ANCHOR_CLIENT_SESSION_ID,
            workerType: DEFAULT_WORKER_TYPE,
            title: ANCHOR_CLIENT_SESSION_ID,
            workspaceId,
          }, signal);
          if (aborted()) return;
          const now = new Date().toISOString();
          const newSession: SessionInfo = {
            id: session_id,
            user_id: '',
            worker_type: DEFAULT_WORKER_TYPE,
            state: 'created',
            title: ANCHOR_CLIENT_SESSION_ID,
            work_dir: '',
            created_at: now,
            updated_at: now,
          };
          setSessions([newSession]);
          setActiveSession(newSession);
          onSelectRef.current(newSession.id);
        } finally {
          isCreating.current = false;
        }
      } else {
        // A creation is already in flight (concurrent refresh); leave active
        // null and let the next refresh pick up the created session.
        setActiveSession(null);
      }
    } catch (e) {
      if (aborted()) return; // superseded by a newer switch — ignore stale error
      setError(e instanceof AuthError ? e.message : (e instanceof Error ? e.message : 'Failed to load sessions'));
    } finally {
      if (!aborted()) setIsLoading(false);
    }
  }, [workspaceId, DEFAULT_WORKER_TYPE]);

  // Load sessions on mount and whenever the active workspace changes.
  // Clear activeSession synchronously on switch: otherwise the stale session
  // (bound to the previous workspace) is paired with the new workspaceId in
  // the WS init handshake, which the server rejects as "session workspace
  // mismatch" (internal/gateway/conn.go resolveSession). refreshSessions()
  // then repopulates activeSession from the new workspace's list.
  //
  // Skip while workspaceId is empty (ChatContainer's activeWorkspace is still
  // loading): listSessions(undefined) would not filter by workspace and could
  // briefly select a session from another workspace. The abort controller
  // cancels the previous in-flight refresh on a rapid switch.
  useEffect(() => {
    const ctrl = new AbortController();
    // eslint-disable-next-line react-hooks/set-state-in-effect -- reset selection before refetching on workspace switch
    setActiveSession(null);
    if (workspaceId) {
      refreshSessions(ctrl.signal);
    }
    return () => ctrl.abort();
  }, [workspaceId, refreshSessions]);

  const selectSession = useCallback((session: SessionInfo) => {
    setActiveSession(session);
    onSelectRef.current(session.id);
    setIsOpen(false);
  }, []);

  const createNewSession = useCallback(async (title: string, workerType?: string) => {
    const wt = workerType || DEFAULT_WORKER_TYPE;
    if (isCreating.current) return;
    isCreating.current = true;
    setIsLoading(true);
    try {
      const { session_id } = await createSession({
        clientSessionId: newSessionId(),
        workerType: wt,
        title: title || undefined,
        workspaceId,
      });
      const now = new Date().toISOString();
      const newSession: SessionInfo = {
        id: session_id,
        user_id: '',
        worker_type: wt,
        state: 'created',
        title,
        work_dir: '',
        created_at: now,
        updated_at: now,
      };
      setSessions(prev => [newSession, ...prev.filter(s => s.state !== 'deleted')]);
      setActiveSession(newSession);
      onSelectRef.current(session_id);
    } catch (e) {
      setError(e instanceof AuthError ? e.message : (e instanceof Error ? e.message : 'Failed to create session'));
    } finally {
      setIsLoading(false);
      isCreating.current = false;
    }
  }, [workspaceId, DEFAULT_WORKER_TYPE]);

  const removeSession = useCallback(async (id: string) => {
    // Optimistic remove
    setSessions((prev) => prev.filter((s) => s.id !== id));
    if (activeSession?.id === id) {
      setActiveSession(null);
    }

    try {
      await deleteSession(id);
    } catch (e) {
      logger.error('Sessions', 'Failed to delete session', { error: String(e) });
      setError(e instanceof AuthError ? e.message : (e instanceof Error ? e.message : 'Failed to delete session'));
      refreshSessions();
    }
  }, [activeSession, refreshSessions]);

  // Handle manual session selection
  const handleSessionSelect = useCallback((id: string) => {
    const session = sessions.find(s => s.id === id);
    if (session) {
      selectSession(session);
    }
  }, [sessions, selectSession]);

  const openPanel = useCallback(() => setIsOpen(true), []);
  const closePanel = useCallback(() => setIsOpen(false), []);

  const updateSessionState = useCallback((sessionId: string, state: string) => {
    setSessions(prev => prev.map(s => s.id === sessionId ? { ...s, state: state as SessionInfo['state'] } : s));
    setActiveSession(prev => prev?.id === sessionId ? { ...prev, state: state as SessionInfo['state'] } : prev);
  }, []);

  return {
    sessions,
    activeSession,
    isLoading,
    error,
    isOpen,
    openPanel,
    closePanel,
    refreshSessions,
    createNewSession,
    removeSession,
    selectSession,
    handleSessionSelect,
    updateSessionState,
  };
}

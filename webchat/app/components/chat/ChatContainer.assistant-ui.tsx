'use client';

import { useCallback, useState } from 'react';
import {
  AssistantRuntimeProvider,
  useExternalStoreRuntime,
} from '@assistant-ui/react';
import { useHotPlexRuntime } from '@/lib/adapters/hotplex-runtime-adapter';
import { Thread } from '@/components/assistant-ui/thread';
import { BrandIcon } from '@/components/icons';
import { SessionPanel } from './SessionPanel';

function ChatInterface({
  url,
  workerType,
  apiKey,
  sessionId,
}: {
  url: string;
  workerType: string;
  apiKey: string;
  sessionId: string | null;
}) {
  const runtime = useExternalStoreRuntime(
    useHotPlexRuntime({ url, workerType, apiKey, sessionId: sessionId ?? undefined })
  );

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <Thread />
    </AssistantRuntimeProvider>
  );
}

export default function ChatContainer() {
  const url = process.env.NEXT_PUBLIC_HOTPLEX_WS_URL || 'ws://localhost:8888/ws';
  const workerType = process.env.NEXT_PUBLIC_HOTPLEX_WORKER_TYPE || 'claude_code';
  const apiKey = process.env.NEXT_PUBLIC_HOTPLEX_API_KEY || 'dev';

  const [activeSessionId, setActiveSessionId] = useState<string | null>(null);
  const [sidebarOpen, setSidebarOpen] = useState(true);

  const handleSessionSelect = useCallback((sessionId: string) => {
    setActiveSessionId(sessionId);
  }, []);

  return (
    <div className="flex h-screen overflow-hidden bg-[var(--bg-base)]">
      {/* PC Sidebar */}
      <aside className={`transition-all duration-300 ease-in-out ${sidebarOpen ? 'w-[280px]' : 'w-0'} overflow-hidden flex-shrink-0 relative z-30`}>
        <SessionPanel
          onSessionSelect={handleSessionSelect}
          initialSessionId={activeSessionId}
        />
      </aside>

      {/* Main Content Area */}
      <main className="flex-1 flex flex-col min-w-0 relative">
        {/* Toggle / Header Area */}
        <header className="h-14 flex items-center px-6 border-b border-[var(--border-subtle)] bg-[var(--bg-surface)] flex-shrink-0 z-20">
          <div className="flex items-center gap-4 w-full">
            <button 
              onClick={() => setSidebarOpen(!sidebarOpen)}
              className="p-2 -ml-2 text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-elevated)] rounded-lg transition-all"
              title={sidebarOpen ? "Collapse sidebar" : "Expand sidebar"}
            >
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
              </svg>
            </button>
            
            <div className="flex items-center gap-3 flex-1">
               <div className="md:hidden">
                 <BrandIcon size={28} />
               </div>
               <div>
                  <h1 className="text-xs font-bold text-[var(--text-primary)] leading-none mb-0.5">HotPlex Agent</h1>
                  <p className="text-[9px] text-[var(--text-faint)] font-mono uppercase tracking-widest">Active • {workerType}</p>
               </div>
            </div>

            <div className="flex items-center gap-2">
               <div className="flex items-center gap-1.5 px-3 py-1.5 rounded-full bg-[var(--bg-elevated)] border border-[var(--border-subtle)]">
                  <div className="w-1.5 h-1.5 rounded-full bg-[var(--accent-emerald)] animate-pulse" />
                  <span className="text-[10px] font-bold text-[var(--text-secondary)]">GATEWAY ONLINE</span>
               </div>
            </div>
          </div>
        </header>

        {/* Chat Thread */}
        <div className="flex-1 relative overflow-hidden">
          <ChatInterface
            key={activeSessionId ?? '__new__'}
            url={url}
            workerType={workerType}
            apiKey={apiKey}
            sessionId={activeSessionId}
          />
        </div>
      </main>
    </div>
  );
}

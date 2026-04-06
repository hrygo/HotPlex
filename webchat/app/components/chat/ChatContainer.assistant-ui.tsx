'use client';

import {
  AssistantRuntimeProvider,
  useExternalStoreRuntime,
} from '@assistant-ui/react';
import { useHotPlexRuntime } from '@/lib/adapters/hotplex-runtime-adapter';
import { Thread } from '@/components/assistant-ui/thread';

/**
 * ChatContainer - Main chat interface using assistant-ui
 *
 * Uses assistant-ui's Runtime architecture with pre-built Thread component.
 */
export default function ChatContainer() {
  // Create HotPlex runtime adapter
  const runtime = useExternalStoreRuntime(
    useHotPlexRuntime({
      url: process.env.NEXT_PUBLIC_HOTPLEX_WS_URL || 'ws://localhost:8888/ws',
      workerType: process.env.NEXT_PUBLIC_HOTPLEX_WORKER_TYPE || 'claude_code',
      apiKey: process.env.NEXT_PUBLIC_HOTPLEX_API_KEY || 'dev',
    })
  );

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <div className="flex flex-col h-screen bg-gray-50">
        {/* Header */}
        <header className="bg-white border-b px-4 py-3 flex-shrink-0">
          <div className="max-w-3xl mx-auto">
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-indigo-500 to-purple-600 flex items-center justify-center">
                <svg className="w-6 h-6 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z" />
                </svg>
              </div>
              <div>
                <h1 className="text-lg font-semibold text-gray-900">HotPlex AI</h1>
                <p className="text-sm text-gray-500">assistant-ui • AEP v1</p>
              </div>
            </div>
          </div>
        </header>

        {/* Thread (pre-built component) */}
        <div className="flex-1 overflow-hidden">
          <Thread />
        </div>
      </div>
    </AssistantRuntimeProvider>
  );
}

'use client';

import dynamic from 'next/dynamic';

const ChatUI = dynamic(() => import('./ChatClient'), {
  ssr: false,
  loading: () => (
    <div className="flex flex-col h-screen bg-gray-50">
      <header className="bg-white border-b px-4 py-3">
        <h1 className="text-lg font-semibold text-gray-900">HotPlex Chat</h1>
        <p className="text-sm text-gray-500">AI SDK • AEP v1 Protocol</p>
      </header>
      <main className="flex-1 flex items-center justify-center">
        <p className="text-gray-400">Loading...</p>
      </main>
    </div>
  ),
});

export default function Page() {
  return <ChatUI />;
}

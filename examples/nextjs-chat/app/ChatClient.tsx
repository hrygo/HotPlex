'use client';

import { useChat } from '@ai-sdk/react';
import { useRef, useEffect } from 'react';

export default function Chat() {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const { messages, input, handleInputChange, handleSubmit, isLoading, error } = useChat({
    api: '/api/chat',
  }) as any;

  const messagesEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  return (
    <div className="flex flex-col h-screen bg-gray-50">
      <header className="bg-white border-b px-4 py-3">
        <h1 className="text-lg font-semibold text-gray-900">HotPlex Chat</h1>
        <p className="text-sm text-gray-500">AI SDK &bull; AEP v1 Protocol</p>
      </header>

      <main className="flex-1 overflow-y-auto p-4 space-y-4">
        {messages.map((message: { id: string; role: string; content: string | Array<{ type: string; text?: string }> }) => (
          <div
            key={message.id}
            className={`flex ${message.role === 'user' ? 'justify-end' : 'justify-start'}`}
          >
            <div
              className={`max-w-[70%] rounded-lg px-4 py-2 ${
                message.role === 'user' ? 'bg-blue-600 text-white' : 'bg-white border shadow-sm'
              }`}
            >
              <div className="text-xs font-medium mb-1 opacity-70">
                {message.role === 'user' ? 'You' : 'Assistant'}
              </div>
              <div className="prose prose-sm max-w-none">
                {typeof message.content === 'string' ? (
                  <p className="whitespace-pre-wrap">{message.content}</p>
                ) : (
                  message.content.map((part, i) => (
                    <p key={i} className="whitespace-pre-wrap">
                      {part.type === 'text' ? part.text : ''}
                    </p>
                  ))
                )}
              </div>
            </div>
          </div>
        ))}
        <div ref={messagesEndRef} />
      </main>

      {error && (
        <div className="px-4 py-2 bg-red-50 border-t border-red-200">
          <p className="text-sm text-red-600">{error.message}</p>
        </div>
      )}

      <footer className="bg-white border-t p-4">
        <form onSubmit={handleSubmit} className="flex gap-2">
          <input
            type="text"
            value={input ?? ''}
            onChange={handleInputChange}
            placeholder="Ask me anything..."
            disabled={isLoading}
            className="flex-1 px-4 py-2 border rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:bg-gray-100"
          />
          <button
            type="submit"
            disabled={isLoading || !(input ?? '').trim()}
            className="px-6 py-2 bg-blue-600 text-white rounded-lg font-medium hover:bg-blue-700 disabled:bg-gray-300"
          >
            {isLoading ? '...' : 'Send'}
          </button>
        </form>
      </footer>
    </div>
  );
}

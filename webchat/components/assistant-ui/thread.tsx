"use client";

import {
  ThreadPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  ActionBarPrimitive,
  ChainOfThoughtPrimitive,
  useAui,
  useAuiState,
} from "@assistant-ui/react";
import { MarkdownText } from "./MarkdownText";

// SuggestionState shape — matches @assistant-ui/core SuggestionState
interface SuggestionState {
  title: string;
  label: string;
  prompt: string;
}

/**
 * SuggestionItem - Renders a single suggestion as a clickable card.
 * Populates the composer input when clicked.
 */
function SuggestionItem({
  suggestion,
}: {
  suggestion: SuggestionState;
}) {
  const aui = useAui();
  const isRunning = useAuiState((s) => s.thread.isRunning);

  const handleClick = () => {
    if (isRunning) return;
    aui.composer().setText(suggestion.prompt);
  };

  return (
    <button
      type="button"
      onClick={handleClick}
      disabled={isRunning}
      className="w-full text-left px-4 py-3 rounded-xl border border-gray-200 bg-white hover:bg-gray-50 hover:border-indigo-300 transition-all duration-150 group disabled:opacity-50 disabled:cursor-not-allowed"
    >
      <div className="flex items-start gap-3">
        <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-indigo-100 to-purple-100 flex items-center justify-center flex-shrink-0 mt-0.5">
          <svg
            className="w-4 h-4 text-indigo-500"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z"
            />
          </svg>
        </div>
        <div className="min-w-0">
          <p className="text-sm font-medium text-gray-800 group-hover:text-indigo-700 transition-colors line-clamp-2">
            {suggestion.prompt}
          </p>
        </div>
      </div>
    </button>
  );
}

/**
 * ReasoningPart - Renders individual reasoning message parts.
 * Displayed inside the ChainOfThought accordion.
 */
function ReasoningPart({ text }: { text: string }) {
  if (!text) return null;
  return (
    <div className="text-gray-500 text-sm italic leading-relaxed">
      {text}
    </div>
  );
}

/**
 * ChainOfThoughtWrapper - Accordion container for reasoning/tool-call content.
 * Must be used inside MessagePrimitive.Parts where the chainOfThought scope is available.
 * The scope is provided by ChainOfThoughtByIndicesProvider which wraps this component
 * when the message has reasoning or tool-call parts.
 */
function ChainOfThoughtWrapper() {
  return (
    <ChainOfThoughtPrimitive.Root>
      <ChainOfThoughtPrimitive.AccordionTrigger className="flex items-center gap-1.5 text-xs text-gray-400 hover:text-gray-600 transition-colors cursor-pointer mb-1">
        <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z" />
        </svg>
        <span>思考过程</span>
        <svg className="w-3 h-3 transition-transform ui-open:rotate-180" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
        </svg>
      </ChainOfThoughtPrimitive.AccordionTrigger>
      <div className="pl-4 border-l-2 border-indigo-200">
        <ChainOfThoughtPrimitive.Parts />
      </div>
    </ChainOfThoughtPrimitive.Root>
  );
}

/**
 * Thread - Main conversation thread component
 *
 * Displays message list and composer using assistant-ui primitives.
 */
export function Thread() {
  return (
    <ThreadPrimitive.Root className="flex flex-col h-full relative">
      {/* Viewport (scrollable message area) */}
      <ThreadPrimitive.Viewport className="flex-1 overflow-y-auto px-4 py-4">
        {/* Message list with render function */}
        <div className="space-y-4 max-w-3xl mx-auto">
          {/* Welcome suggestions — only shown when there are no messages */}
          <ThreadPrimitive.Suggestions>
            {({ suggestion }: { suggestion: SuggestionState }) => (
              <SuggestionItem suggestion={suggestion} />
            )}
          </ThreadPrimitive.Suggestions>

          <ThreadPrimitive.Messages>
            {({ message }) => {
              if (message.role === "user") {
                return <UserMessage />;
              }
              return <AssistantMessage />;
            }}
          </ThreadPrimitive.Messages>
        </div>
      </ThreadPrimitive.Viewport>

      {/* Scroll to bottom button */}
      <ThreadPrimitive.ScrollToBottom className="absolute bottom-24 right-6 p-3 bg-white text-gray-600 rounded-full shadow-lg hover:bg-gray-100 transition-colors border">
        <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 14l-7 7m0 0l-7-7m7 7V3" />
        </svg>
      </ThreadPrimitive.ScrollToBottom>

      {/* Composer (sticky footer) */}
      <ThreadPrimitive.ViewportFooter className="border-t bg-white p-4">
        <div className="max-w-3xl mx-auto">
          <Composer />
        </div>
      </ThreadPrimitive.ViewportFooter>
    </ThreadPrimitive.Root>
  );
}

/**
 * UserMessage - Displays user messages with action bar
 */
function UserMessage() {
  return (
    <MessagePrimitive.Root className="group flex justify-end">
      <div className="max-w-xl">
        <div className="bg-indigo-600 text-white rounded-2xl rounded-br-md px-4 py-3">
          <MessagePrimitive.Parts />
        </div>

        {/* Action bar - visible on hover */}
        <ActionBarPrimitive.Root
          autohide="not-last"
          autohideFloat="always"
          className="flex items-center gap-1 mt-1 justify-end opacity-0 group-hover:opacity-100 transition-opacity"
        >
          <ActionBarPrimitive.Edit className="p-1.5 rounded-full hover:bg-gray-100 text-gray-500 transition-colors">
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
            </svg>
          </ActionBarPrimitive.Edit>
          <ActionBarPrimitive.Copy className="p-1.5 rounded-full hover:bg-gray-100 text-gray-500 transition-colors group/copy">
            <svg className="w-4 h-4 group-data-[copied]/copy:hidden" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
            </svg>
            <svg className="w-4 h-4 hidden group-data-[copied]/copy:block text-green-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
            </svg>
          </ActionBarPrimitive.Copy>
        </ActionBarPrimitive.Root>
      </div>
    </MessagePrimitive.Root>
  );
}

/**
 * AssistantMessage - Displays assistant messages with action bar.
 * The ChainOfThought accordion (reasoning display) is rendered inside
 * MessagePrimitive.Parts via the components prop — it requires the
 * chainOfThought scope which is only available within that context.
 */
function AssistantMessage() {
  return (
    <MessagePrimitive.Root className="group flex justify-start">
      <div className="max-w-xl">
        <div className="bg-white border rounded-2xl rounded-bl-md px-4 py-3 shadow-sm">
          {/* Avatar and header */}
          <div className="flex items-center gap-2 mb-2">
            <div className="w-7 h-7 rounded-full bg-gradient-to-br from-indigo-500 to-purple-600 flex items-center justify-center flex-shrink-0">
              <svg className="w-4 h-4 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z" />
              </svg>
            </div>
            <span className="text-sm font-medium text-gray-900">HotPlex AI</span>
          </div>

          {/* Content — ChainOfThought accordion is inside Parts (components.ChainOfThought) */}
          <div className="text-gray-700">
            <MessagePrimitive.Parts
              components={
                {
                  Text: MarkdownText,
                  ChainOfThought: ChainOfThoughtWrapper,
                } as const
              }
            />
          </div>
        </div>

        {/* Action bar - visible on hover */}
        <ActionBarPrimitive.Root
          autohide="not-last"
          autohideFloat="always"
          className="flex items-center gap-1 mt-1 opacity-0 group-hover:opacity-100 transition-opacity"
        >
          <ActionBarPrimitive.Copy className="p-1.5 rounded-full hover:bg-gray-100 text-gray-500 transition-colors group/copy">
            <svg className="w-4 h-4 group-data-[copied]/copy:hidden" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
            </svg>
            <svg className="w-4 h-4 hidden group-data-[copied]/copy:block text-green-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
            </svg>
          </ActionBarPrimitive.Copy>
          <ActionBarPrimitive.Reload>
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
          </ActionBarPrimitive.Reload>
          <ActionBarPrimitive.FeedbackPositive className="p-1.5 rounded-full hover:bg-gray-100 text-gray-500 transition-colors">
            👍
          </ActionBarPrimitive.FeedbackPositive>
          <ActionBarPrimitive.FeedbackNegative className="p-1.5 rounded-full hover:bg-gray-100 text-gray-500 transition-colors">
            👎
          </ActionBarPrimitive.FeedbackNegative>
        </ActionBarPrimitive.Root>
      </div>
    </MessagePrimitive.Root>
  );
}

/**
 * Composer - Input area for sending messages
 */
function Composer() {
  return (
    <ComposerPrimitive.Root className="flex items-end gap-3">
      <ComposerPrimitive.Input
        className="flex-1 w-full px-4 py-3 border rounded-xl resize-none focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
        rows={1}
        placeholder="Type your message..."
      />

      {/* Send button */}
      <ComposerPrimitive.Send className="flex-shrink-0 p-3 bg-indigo-600 text-white rounded-xl hover:bg-indigo-700 disabled:bg-gray-200 disabled:text-gray-400 disabled:cursor-not-allowed transition-colors">
        <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8" />
        </svg>
      </ComposerPrimitive.Send>

      {/* Cancel button (shown when running) */}
      <ComposerPrimitive.Cancel className="flex-shrink-0 p-3 bg-red-500 text-white rounded-xl hover:bg-red-600 transition-colors">
        <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
          <rect x="6" y="6" width="12" height="12" rx="2" />
        </svg>
      </ComposerPrimitive.Cancel>
    </ComposerPrimitive.Root>
  );
}

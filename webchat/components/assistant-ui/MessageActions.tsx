"use client";

import { CopyButton } from "./CopyButton";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function MessageActions({ message, isUser }: { message: any; isUser?: boolean }) {
  return (
    <div className={`message-action-bar ${isUser ? 'justify-end' : 'justify-start'}`}>
      <CopyButton message={message} />
    </div>
  );
}

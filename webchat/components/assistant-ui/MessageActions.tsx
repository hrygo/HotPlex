"use client";

import { CopyButton } from "./CopyButton";

 
export function MessageActions({ message, isUser }: { message: any; isUser?: boolean }) {
  return (
    <div className={`message-action-bar ${isUser ? 'justify-end' : 'justify-start'}`}>
      <CopyButton message={message} />
    </div>
  );
}

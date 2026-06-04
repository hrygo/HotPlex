"use client";

import { memo } from "react";
import { motion } from "framer-motion";
import { MessagePrimitive } from "@assistant-ui/react";
import { MessageActions } from "./MessageActions";
import { messageVariants } from "./thread-helpers";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const UserMessage = memo(function UserMessage({ message }: { message: any }) {
  return (
    <motion.div className="group flex items-start justify-end gap-4 mb-8" variants={messageVariants} initial="hidden" animate="visible">
      <div className="relative max-w-[85%] flex-1 flex flex-col items-end min-w-0 group/msg">
        <div className="msg-user-bubble relative w-fit p-3.5 rounded-[var(--radius-lg)] rounded-tr-[var(--radius-xs)] bg-[var(--bg-elevated)] border border-[var(--border-subtle)] shadow-sm">
          <MessagePrimitive.Parts>
            {({ part }) => {
              // eslint-disable-next-line @typescript-eslint/no-explicit-any
              const p = part as Record<string, any>;
              if (p?.type === 'text') return <div className="whitespace-pre-wrap break-normal text-[14px] leading-relaxed">{p.text}</div>;
              return null;
            }}
          </MessagePrimitive.Parts>
        </div>
        <MessageActions message={message} isUser />
      </div>
      <div className="flex-shrink-0">
        <div className="w-9 h-9 rounded-full bg-[var(--bg-elevated)] border border-[var(--border-subtle)] flex items-center justify-center shadow-sm">
          <svg className="w-5 h-5 text-[var(--text-muted)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
          </svg>
        </div>
      </div>
    </motion.div>
  );
}, (prev, next) => prev.message.id === next.message.id);

export { UserMessage };

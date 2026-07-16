"use client";

import { memo } from "react";
import { motion } from "framer-motion";
import { MessagePrimitive } from "@assistant-ui/react";
import { useAuiState } from "@assistant-ui/store";
import { MessageActions } from "./MessageActions";
import { messageVariants } from "./thread-helpers";

 
const UserMessage = memo(function UserMessage({ message }: { message: any }) {
  const isRunning = useAuiState((s) => s.thread.isRunning);
  const messages = useAuiState((s) => s.thread.messages);

  const userMessages = messages.filter((m) => m.role === "user");
  const isLatestUserMessage = userMessages[userMessages.length - 1]?.id === message.id;
  const isLiving = isRunning && isLatestUserMessage;

  return (
    <motion.div className="group flex items-start justify-end gap-4 mb-8" variants={messageVariants} initial="hidden" animate="visible">
      <div className="relative max-w-[85%] flex-1 flex flex-col items-end min-w-0 group/msg">
        <div className="msg-user-bubble relative w-fit p-3.5 rounded-[var(--radius-lg)] rounded-tr-[var(--radius-xs)] bg-[var(--bg-elevated)] border border-[var(--border-subtle)] shadow-sm">
          <MessagePrimitive.Parts>
            {({ part }) => {
              const p = part as Record<string, any>;
              if (p?.type === 'text') return <div className="whitespace-pre-wrap break-normal text-[14px] leading-relaxed">{p.text}</div>;
              return null;
            }}
          </MessagePrimitive.Parts>
        </div>
        <MessageActions message={message} isUser />
      </div>
      <div className="flex-shrink-0">
        <div className={`w-9 h-9 rounded-full flex items-center justify-center relative ${isLiving ? "animate-gradient-shift" : ""}`}
          style={{
            background: isLiving
              ? "linear-gradient(-45deg, #3b82f6, #8b5cf6, #ec4899)"
              : "linear-gradient(135deg, var(--accent-blue), #6366f1)",
            border: isLiving
              ? "1px solid rgba(59, 130, 246, 0.4)"
              : "1px solid var(--border-subtle)",
            boxShadow: isLiving
              ? "0 0 12px rgba(59, 130, 246, 0.35)"
              : "var(--shadow-sm)",
          }}
        >
          {isLiving && (
            <>
              <div className="absolute inset-0 rounded-full border border-transparent animate-user-avatar-ripple-1" />
              <div className="absolute inset-0 rounded-full border border-transparent animate-user-avatar-ripple-2" />
            </>
          )}
          <div className={`${isLiving ? "animate-user-avatar-breath" : ""}`}>
            <svg className="w-5 h-5 text-white drop-shadow-[0_1px_2px_rgba(0,0,0,0.2)]" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M17.982 18.725A7.488 7.488 0 0012 15.75a7.488 7.488 0 00-5.982 2.975m11.963 0a9 9 0 10-11.963 0m11.963 0A8.966 8.966 0 0112 21a8.966 8.966 0 01-5.982-2.275M15 9.75a3 3 0 11-6 0 3 3 0 016 0z" />
            </svg>
          </div>
        </div>
      </div>
    </motion.div>
  );
}, (prev, next) => prev.message.id === next.message.id);

export { UserMessage };

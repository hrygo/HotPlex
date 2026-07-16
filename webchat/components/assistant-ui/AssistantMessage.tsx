"use client";

import { memo, useCallback, useMemo, useState } from "react";
import { motion } from "framer-motion";
import { BrandIcon } from "@/components/icons";
import { getToolCategory } from "@/lib/tool-categories";
import { MarkdownText } from "./MarkdownText";
import { TerminalTool } from "./tools/TerminalTool";
import { FileDiffTool } from "./tools/FileDiffTool";
import { SearchTool } from "./tools/SearchTool";
import { CompactToolTab } from "./tools/CompactToolTab";
import { ContextUsageCard } from "./ContextUsageCard";
import { TurnSummaryCard } from "./TurnSummaryCard";
import { ListTool } from "./tools/ListTool";
import { TodoTool } from "./tools/TodoTool";
import { AgentTool } from "./tools/AgentTool";
import { MessageActions } from "./MessageActions";
import { ReasoningBlock } from "./ReasoningBlock";
import { getExt, messageVariants, extractCommand, extractFilePath, extractFileContent } from "./thread-helpers";
import { useTranslation } from "react-i18next";
import { PermissionApprovalCard } from "@/components/assistant-ui/tools/PermissionApprovalCard";
import { QuestionResponseCard } from "@/components/assistant-ui/tools/QuestionResponseCard";
import { ElicitationFormCard } from "@/components/assistant-ui/tools/ElicitationFormCard";

// ToolCallPart is isolated so that a streaming sibling (growing text/reasoning)
// does not re-render completed tool parts. Its memo comparator compares the
// material fields each tool type mutates, so a rebuilt content array (different
// element references, same values) still skips re-render.
type ToolCallPartProps = {
  toolName: string;
  args: Record<string, any>;
  result: any;
  partStatus?: any;
  toolCallId?: string;
  partIndex: number;
  isLastPart: boolean;
  isExpanded: boolean;
  onToggle: (key: string | number, defaultExpanded: boolean) => void;
  onInteractionRespond?: (toolCallId: string, response: any) => void;
};

const ToolCallPart = memo(function ToolCallPart({
  toolName, args, result, partStatus, toolCallId, partIndex, isLastPart, isExpanded, onToggle, onInteractionRespond,
}: ToolCallPartProps) {
  const { t } = useTranslation('chat');
  const isComplete = partStatus?.type === "complete" || partStatus?.type === "error";
  const key = toolCallId || partIndex;
  const toggle = () => onToggle(key, isLastPart);

  const category = getToolCategory(toolName);
  const status = isComplete ? (partStatus?.type === "error" ? "error" : "complete") : "running";

  // A completed tool renders compact unless the user has expanded it. An
  // in-progress tool always renders the full view.
  if (isComplete && !isExpanded) {
    return (
      <CompactToolTab
        key={key}
        toolName={toolName}
        summary={extractCommand(args) || extractFilePath(args) || t('text.action_default')}
        status={status === "running" ? "complete" : status as "complete" | "error"}
        onClick={toggle}
      />
    );
  }

  return (
    <motion.div
      key={key}
      initial={{ opacity: 0, x: -10 }}
      animate={{ opacity: 1, x: 0 }}
      className="relative mt-3 first:mt-0"
    >
      {(() => {
        switch (category) {
          case "terminal": return <TerminalTool command={extractCommand(args)} stdout={result?.stdout || (typeof result === 'string' ? result : '')} stderr={result?.stderr} status={status} onToggle={toggle} />;
          case "file": return <FileDiffTool toolName={toolName} filePath={extractFilePath(args)} content={extractFileContent(args, result)} status={status} onToggle={toggle} />;
          case "search": return <SearchTool toolName={toolName} query={args.query || args.pattern} results={result} status={status} onToggle={toggle} />;
          case "list": return <ListTool toolName={toolName} path={extractFilePath(args)} items={result} status={status} onToggle={toggle} />;
          case "todo": return <TodoTool todo={args.todo} todos={args.todos} status={status === "running" ? "running" : "complete"} onToggle={toggle} />;
          case "ai": return <AgentTool description={args.description} prompt={args.prompt} subagent_type={args.subagent_type} run_in_background={args.run_in_background} status={status === "running" ? "running" : "complete"} onToggle={toggle} />;
          case "permission": {
            const interaction = args?.interaction;
            const resolvedStatus = interaction?.status || (status === "error" ? "failed" : status === "complete" ? "resolved" : "pending");

            if (toolName === "question_request") {
              const onRespondQuestion = toolCallId && onInteractionRespond
                ? (answers: Record<string, string | string[]>) => onInteractionRespond(toolCallId, { type: "question", answers })
                : undefined;
              return (
                <QuestionResponseCard
                  toolName={toolName}
                  questions={args.questions}
                  status={resolvedStatus}
                  interactionState={interaction}
                  onRespond={onRespondQuestion}
                  onToggle={toggle}
                />
              );
            } else if (toolName === "elicitation") {
              const onRespondElicitation = toolCallId && onInteractionRespond
                ? (action: "accept" | "decline" | "cancel", content?: Record<string, any>) => onInteractionRespond(toolCallId, { type: "elicitation", action, content })
                : undefined;
              return (
                <ElicitationFormCard
                  toolName={toolName}
                  message={args.message}
                  mcpServerName={args.mcp_server_name}
                  url={args.url}
                  status={resolvedStatus}
                  interactionState={interaction}
                  onRespond={onRespondElicitation}
                  onToggle={toggle}
                />
              );
            } else {
              const onRespondPerm = toolCallId && onInteractionRespond
                ? (allowed: boolean, reason?: string) => onInteractionRespond(toolCallId, { type: "permission", allowed, reason })
                : undefined;
              return (
                <PermissionApprovalCard
                  toolName={toolName}
                  args={args.args}
                  description={args.description}
                  status={resolvedStatus}
                  interactionState={interaction}
                  onRespond={onRespondPerm}
                  onToggle={toggle}
                />
              );
            }
          }
          default: {
            const content = result || args;
            const summary = typeof content === 'string' ? content : JSON.stringify(content, null, 2);
            return (
              <div className="group/tool border border-[var(--border-subtle)] rounded-[var(--radius-md)] overflow-hidden bg-[var(--bg-elevated)] shadow-sm">
                <div
                  className="flex items-center justify-between px-3 py-2 bg-[var(--bg-surface)] border-b border-[var(--border-subtle)] cursor-pointer hover:bg-[var(--bg-hover)] transition-colors"
                  onClick={toggle}
                >
                  <div className="flex items-center gap-2">
                    <span className="text-[10px] font-bold text-[var(--accent-gold)] uppercase tracking-wider">{toolName}</span>
                    {status === "running" && <div className="w-2 h-2 rounded-full bg-[var(--accent-gold)] animate-pulse" />}
                  </div>
                  {status !== "running" && (
                    <div className="text-[var(--text-faint)] transform group-hover/tool:scale-110 transition-transform">
                      <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 15l7-7 7 7" />
                      </svg>
                    </div>
                  )}
                </div>
                <div className="p-3 font-mono text-[11px] leading-relaxed whitespace-pre-wrap max-h-[300px] overflow-y-auto custom-scrollbar">
                  {summary}
                </div>
              </div>
            );
          }
        }
      })()}
    </motion.div>
  );
}, (prev, next) =>
  prev.toolName === next.toolName &&
  prev.args === next.args &&
  prev.result === next.result &&
  prev.partStatus === next.partStatus &&
  prev.toolCallId === next.toolCallId &&
  prev.partIndex === next.partIndex &&
  prev.isLastPart === next.isLastPart &&
  prev.isExpanded === next.isExpanded &&
  prev.onToggle === next.onToggle &&
  prev.onInteractionRespond === next.onInteractionRespond
);

const AssistantMessage = memo(function AssistantMessage({ message, onInteractionRespond }: { message: any; onInteractionRespond?: (toolCallId: string, response: any) => void }) {
  const { t } = useTranslation('chat');
  const [expandedTools, setExpandedTools] = useState<Record<string, boolean>>({});
  const isError = message?.status === "error";
  const ext = getExt(message);
  const content = ext.content || [];
  const custom = ext.metadata?.custom;
  const isStreaming = ext.status?.type === "running" || custom?.progress === "thinking" || custom?.progress === "accepted";

  // Stable toggle so ToolCallPart memo is not defeated by a fresh closure each
  // render. The default-expanded state (the last part) is passed in by the caller.
  const toggleTool = useCallback((key: string | number, defaultExpanded: boolean) => {
    setExpandedTools(prev => {
      const currentVal = prev[key] !== undefined ? prev[key] : defaultExpanded;
      return { ...prev, [key]: !currentVal };
    });
  }, []);

  // Single reverse pass: reasoningActiveByIndex[i] = true iff part i is reasoning
  // AND the message is streaming AND no later text/tool-call part exists. Avoids
  // the O(n²) slice+some per reasoning part.
  const reasoningActiveByIndex = useMemo(() => {
    const isStreaming = ext.status?.type === "running";
    const result = new Array(content.length).fill(false);
    if (!isStreaming) return result;
    let sawLaterActive = false;
    for (let i = content.length - 1; i >= 0; i--) {
      const o = content[i] as Record<string, any>;
      if (!o) continue;
      if (o.type === "reasoning") {
        result[i] = !sawLaterActive;
      } else if (o.type === "text" || o.type === "tool-call") {
        sawLaterActive = true;
      }
    }
    return result;
  }, [content, ext.status?.type]);

  return (
    <motion.div className={`group msg-assistant flex items-start gap-4 mb-8 ${isError ? "border-l-2 border-[var(--accent-coral)] pl-3" : ""}`} variants={messageVariants} initial="hidden" animate="visible">
      <div className="flex-shrink-0 relative">
        {isStreaming && (
          <>
            <div className="absolute inset-0 rounded-[var(--radius-md)] bg-[var(--accent-gold)]/10 animate-avatar-ripple-1 -z-10" />
            <div className="absolute inset-0 rounded-[var(--radius-md)] bg-[var(--accent-gold)]/10 animate-avatar-ripple-2 -z-10" />
          </>
        )}
        <div className={`w-9 h-9 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border shadow-sm flex items-center justify-center relative overflow-hidden transition-all duration-500 ${isStreaming ? "border-[var(--accent-gold)]/40 shadow-[0_0_15px_rgba(251,191,36,0.25)] scale-[1.02]" : "border-[var(--border-subtle)]"}`}>
          {isStreaming ? (
            <div className="absolute inset-0 bg-gradient-to-br from-[var(--accent-gold)]/20 via-indigo-500/5 to-transparent animate-gradient-shift" />
          ) : (
            <div className="absolute inset-0 bg-gradient-to-br from-[var(--accent-gold)]/5 to-transparent" />
          )}
          {isStreaming && (
            <div className="absolute inset-[1px] rounded-[calc(var(--radius-md)-1px)] border border-white/5 bg-transparent" />
          )}
          <BrandIcon size={24} className={`transition-all duration-500 relative z-10 ${isStreaming ? "opacity-100 scale-105 filter drop-shadow-[0_0_4px_rgba(251,191,36,0.5)] animate-avatar-breath" : "opacity-80"}`} />
        </div>
      </div>

      <div className="flex flex-col flex-1 min-w-0">
        <div className="msg-assistant-body relative space-y-4">
          {content.map((part, partIndex) => {
            const p = part as Record<string, any>;
            if (!p || !p.type) return null;
            const isStreaming = ext.status?.type === "running";

            if (p.type === "reasoning") {
              return <ReasoningBlock key={partIndex} text={p.text || p.reasoning || ""} isStreaming={reasoningActiveByIndex[partIndex]} />;
            }
            if (p.type === "text") return <div key={partIndex} className={`prose-hotplex ${isStreaming ? "streaming-cursor" : ""}`}><MarkdownText text={p.text} /></div>;

            if (p.type === "tool-call") {
              const isLastPart = partIndex === content.length - 1;
              const key = p.toolCallId || partIndex;
              const isExpanded = expandedTools[key] !== undefined ? expandedTools[key] : isLastPart;
              return (
                <ToolCallPart
                  key={key}
                  toolName={p.toolName}
                  args={p.args ?? {}}
                  result={p.result}
                  partStatus={p.status}
                  toolCallId={p.toolCallId}
                  partIndex={partIndex}
                  isLastPart={isLastPart}
                  isExpanded={isExpanded}
                  onToggle={toggleTool}
                  onInteractionRespond={onInteractionRespond}
                />
              );
            }
            if (p.type === "tool-summary") {
              const names = p.toolNames || [];
              return (
                <div key={partIndex} className="flex items-center gap-2 px-3 py-1.5 mt-3 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-[11px] font-bold text-[var(--text-secondary)] w-fit shadow-sm">
                  <span className="text-[var(--accent-gold)] animate-pulse-subtle">🔧</span>
                  <span className="tracking-wide">{names.join(', ').toUpperCase()}</span>
                  {p.count > 1 && <span className="text-[var(--text-faint)] ml-1">×{p.count}</span>}
                </div>
              );
            }

            // Fallback for unknown parts that might have text (like custom reasoning types)
            if (p.text || p.reasoning || p.content) {
              const fallbackText = p.text || p.reasoning || (typeof p.content === 'string' ? p.content : JSON.stringify(p.content));
              if (fallbackText) {
                return <div key={partIndex} className="mt-3"><MarkdownText text={fallbackText} /></div>;
              }
            }

            return null;
          })}
          {custom?.contextUsage && (
            <ContextUsageCard data={custom.contextUsage} />
          )}
          {custom?.turnSummary && (
            <TurnSummaryCard data={custom.turnSummary} />
          )}
          {custom?.progress && (
            <div className="flex items-center gap-2 text-sm text-[var(--text-secondary)]">
              <span className="w-2 h-2 rounded-full bg-[var(--accent-gold)] animate-pulse" />
              <span>{t(`status.${custom.progress}`)}</span>
            </div>
          )}
        </div>
        <MessageActions message={message} />
        {isError && (
          <p className="mt-2 text-[11px] text-[var(--text-faint)]">{t('error.resend_hint')}</p>
        )}
      </div>
    </motion.div>
  );
}, (prev, next) => {
  if (prev.message.id !== next.message.id) return false;
  // A status transition must always re-render: the streaming cursor and part
  // status chips depend on ext.status, not just content. The previous check
  // only forced re-render while next is 'running', missing the running→complete
  // transition where text is unchanged but the cursor must disappear.
  const prevRunning = getExt(prev.message).status?.type === 'running';
  const nextRunning = getExt(next.message).status?.type === 'running';
  if (prevRunning || nextRunning) return false;
  if (prev.onInteractionRespond !== next.onInteractionRespond) return false;
  // Shallow-compare the content array element-wise. The previous check
  // (`prev.message.content === next.message.content`) compared array
  // references, but convertToThreadMessage rebuilds the array on every render,
  // so the reference always differs and the memo was effectively disabled —
  // every delta flush re-rendered every assistant message. Compare by value
  // instead, covering the fields each part type actually mutates:
  //  - text / reasoning: text
  //  - tool-call: toolName + args + toolCallId + result + status
  // Without args/toolName, a tool-call whose args are patched post-stream
  // (e.g. reconcile) wouldn't re-render. Without reasoning text, a reconcile
  // that rewrites completed reasoning content wouldn't update either.
  const pc = getExt(prev.message).content || [];
  const nc = getExt(next.message).content || [];
  if (pc.length !== nc.length) return false;
  for (let i = 0; i < pc.length; i++) {
    const a = pc[i] as Record<string, unknown>;
    const b = nc[i] as Record<string, unknown>;
    if (a?.type !== b?.type) return false;
    if (a?.type === 'text' || a?.type === 'reasoning') {
      if (a.text !== b.text) return false;
    } else if (a?.type === 'tool-call') {
      if (a.toolName !== b.toolName) return false;
      if (a.args !== b.args) return false;
      if (a.toolCallId !== b.toolCallId) return false;
      if (a.result !== b.result) return false;
      if (a.status !== b.status) return false;
    }
  }
  return true;
});

export { AssistantMessage };

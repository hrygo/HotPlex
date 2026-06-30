"use client";

import { memo, useState } from "react";
import { motion } from "framer-motion";
import { MessagePrimitive } from "@assistant-ui/react";
import { BrandIcon } from "@/components/icons";
import { getToolCategory } from "@/lib/tool-categories";
import { MarkdownText } from "./MarkdownText";
import { TerminalTool } from "./tools/TerminalTool";
import { FileDiffTool } from "./tools/FileDiffTool";
import { SearchTool } from "./tools/SearchTool";
import { PermissionCard } from "./tools/PermissionCard";
import { CompactToolTab } from "./tools/CompactToolTab";
import { ContextUsageCard } from "./ContextUsageCard";
import { TurnSummaryCard } from "./TurnSummaryCard";
import { ListTool } from "./tools/ListTool";
import { TodoTool } from "./tools/TodoTool";
import { AgentTool } from "./tools/AgentTool";
import { MessageActions } from "./MessageActions";
import { ReasoningBlock } from "./ReasoningBlock";
import { getExt, messageVariants, extractCommand, extractFilePath, extractFileContent } from "./thread-helpers";

 
const AssistantMessage = memo(function AssistantMessage({ message, onInteractionRespond }: { message: any; onInteractionRespond?: (toolCallId: string, allowed: boolean) => void }) {
  const [expandedTools, setExpandedTools] = useState<Record<string, boolean>>({});
  const ext = getExt(message);

  return (
    <motion.div className="group msg-assistant flex items-start gap-4 mb-8" variants={messageVariants} initial="hidden" animate="visible">
      <div className="flex-shrink-0">
        <div className="w-9 h-9 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-subtle)] shadow-sm flex items-center justify-center relative overflow-hidden">
          <div className="absolute inset-0 bg-gradient-to-br from-[var(--accent-gold)]/5 to-transparent" />
          <BrandIcon size={24} />
        </div>
      </div>

      <div className="flex flex-col flex-1 min-w-0">
        <div className="msg-assistant-body relative space-y-4">
          <MessagePrimitive.Parts>
            {({ part }) => {
               
              const p = part as Record<string, any>;
              if (!p || !p.type) return null;
              const isStreaming = ext.status?.type === "running";

              if (p.type === "reasoning") return <ReasoningBlock text={p.text || p.reasoning || ""} />;
              if (p.type === "text") return <div className={`prose-hotplex ${isStreaming ? "streaming-cursor" : ""}`}><MarkdownText text={p.text} /></div>;

              if (p.type === "tool-call") {
                const parts = ext.content || [];
                const partIndex = parts.indexOf(p);
                const isLastPart = partIndex === parts.length - 1;
                const isComplete = p.status?.type === "complete" || p.status?.type === "error";
                const isExpanded = !!expandedTools[p.toolCallId || partIndex];

                const isCompacted = !isLastPart && isComplete && !isExpanded;
                const toggle = () => setExpandedTools(prev => ({ ...prev, [p.toolCallId || partIndex]: !prev[p.toolCallId || partIndex] }));

                const category = getToolCategory(p.toolName);
                const args = p.args ?? {};
                const status = isComplete ? (p.status?.type === "error" ? "error" : "complete") : "running";

                if (isCompacted) {
                  return (
                    <CompactToolTab
                      toolName={p.toolName}
                      summary={extractCommand(args) || extractFilePath(args) || "Action..."}
                      status={status === "running" ? "complete" : status as "complete" | "error"}
                      onClick={toggle}
                    />
                  );
                }

                return (
                  <motion.div
                    initial={{ opacity: 0, x: -10 }}
                    animate={{ opacity: 1, x: 0 }}
                    className="relative mt-3 first:mt-0"
                  >
                    {(() => {
                      switch (category) {
                        case "terminal": return <TerminalTool command={extractCommand(args)} stdout={p.result?.stdout || (typeof p.result === 'string' ? p.result : '')} stderr={p.result?.stderr} status={status} onToggle={!isLastPart ? toggle : undefined} />;
                        case "file": return <FileDiffTool toolName={p.toolName} filePath={extractFilePath(args)} content={extractFileContent(args, p.result)} status={status} onToggle={!isLastPart ? toggle : undefined} />;
                        case "search": return <SearchTool toolName={p.toolName} query={args.query || args.pattern} results={p.result} status={status} onToggle={!isLastPart ? toggle : undefined} />;
                        case "list": return <ListTool toolName={p.toolName} path={extractFilePath(args)} items={p.result} status={status} onToggle={!isLastPart ? toggle : undefined} />;
                        case "todo": return <TodoTool todo={args.todo} todos={args.todos} status={status === "running" ? "running" : "complete"} onToggle={!isLastPart ? toggle : undefined} />;
                        case "ai": return <AgentTool description={args.description} prompt={args.prompt} subagent_type={args.subagent_type} run_in_background={args.run_in_background} status={status === "running" ? "running" : "complete"} onToggle={!isLastPart ? toggle : undefined} />;
                        case "permission": return <PermissionCard toolName={p.toolName} args={args} status={status === "error" ? "complete" : status as "running" | "complete"} onRespond={p.toolCallId && onInteractionRespond ? (allowed: boolean) => onInteractionRespond(p.toolCallId, allowed) : undefined} onToggle={!isLastPart ? toggle : undefined} />;
                        default: {
                          const content = p.result || args;
                          const summary = typeof content === 'string' ? content : JSON.stringify(content, null, 2);
                          return (
                            <div className="group/tool border border-[var(--border-subtle)] rounded-[var(--radius-md)] overflow-hidden bg-[var(--bg-elevated)] shadow-sm">
                              <div className="flex items-center justify-between px-3 py-2 bg-[var(--bg-surface)] border-b border-[var(--border-subtle)]">
                                <span className="text-[10px] font-bold text-[var(--accent-gold)] uppercase tracking-wider">{p.toolName}</span>
                                {status === "running" && <div className="w-2 h-2 rounded-full bg-[var(--accent-gold)] animate-pulse" />}
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
              }
              if (p.type === "tool-summary") {
                const names = p.toolNames || [];
                return (
                  <div className="flex items-center gap-2 px-3 py-1.5 mt-3 rounded-[var(--radius-sm)] bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-[11px] font-bold text-[var(--text-secondary)] w-fit shadow-sm">
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
                  return <div className="mt-3"><MarkdownText text={fallbackText} /></div>;
                }
              }

              return null;
            }}
          </MessagePrimitive.Parts>
          {ext.metadata?.contextUsage && (
            <ContextUsageCard data={ext.metadata.contextUsage} />
          )}
          {ext.metadata?.turnSummary && (
            <TurnSummaryCard data={ext.metadata.turnSummary} />
          )}
        </div>
        <MessageActions message={message} />
      </div>
    </motion.div>
  );
}, (prev, next) => {
  if (prev.message.id !== next.message.id) return false;
  if (getExt(next.message).status?.type === 'running') return false;
  if (prev.onInteractionRespond !== next.onInteractionRespond) return false;
  return prev.message.content === next.message.content;
});

export { AssistantMessage };

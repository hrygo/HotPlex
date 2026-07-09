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
import { useTranslation } from "react-i18next";
import { PermissionApprovalCard } from "@/components/assistant-ui/tools/PermissionApprovalCard";
import { QuestionResponseCard } from "@/components/assistant-ui/tools/QuestionResponseCard";
import { ElicitationFormCard } from "@/components/assistant-ui/tools/ElicitationFormCard";

 
const AssistantMessage = memo(function AssistantMessage({ message, onInteractionRespond }: { message: any; onInteractionRespond?: (toolCallId: string, response: any) => void }) {
  const { t } = useTranslation('chat');
  const [expandedTools, setExpandedTools] = useState<Record<string, boolean>>({});
  const isError = message?.status === "error";
  const ext = getExt(message);

  return (
    <motion.div className={`group msg-assistant flex items-start gap-4 mb-8 ${isError ? "border-l-2 border-[var(--accent-coral)] pl-3" : ""}`} variants={messageVariants} initial="hidden" animate="visible">
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

              if (p.type === "reasoning") return <ReasoningBlock text={p.text || p.reasoning || ""} isStreaming={isStreaming} />;
              if (p.type === "text") return <div className={`prose-hotplex ${isStreaming ? "streaming-cursor" : ""}`}><MarkdownText text={p.text} /></div>;

              if (p.type === "tool-call") {
                const parts = ext.content || [];
                const partIndex = parts.indexOf(p);
                const isLastPart = partIndex === parts.length - 1;
                const isComplete = p.status?.type === "complete" || p.status?.type === "error";
                const isExpanded = expandedTools[p.toolCallId || partIndex] !== undefined
                  ? expandedTools[p.toolCallId || partIndex]
                  : isLastPart;

                const isCompacted = isComplete && !isExpanded;
                const toggle = () => setExpandedTools(prev => {
                  const key = p.toolCallId || partIndex;
                  const currentVal = prev[key] !== undefined ? prev[key] : isLastPart;
                  return { ...prev, [key]: !currentVal };
                });

                const category = getToolCategory(p.toolName);
                const args = p.args ?? {};
                const status = isComplete ? (p.status?.type === "error" ? "error" : "complete") : "running";

                if (isCompacted) {
                  return (
                    <CompactToolTab
                      toolName={p.toolName}
                      summary={extractCommand(args) || extractFilePath(args) || t('text.action_default')}
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
                        case "terminal": return <TerminalTool command={extractCommand(args)} stdout={p.result?.stdout || (typeof p.result === 'string' ? p.result : '')} stderr={p.result?.stderr} status={status} onToggle={toggle} />;
                        case "file": return <FileDiffTool toolName={p.toolName} filePath={extractFilePath(args)} content={extractFileContent(args, p.result)} status={status} onToggle={toggle} />;
                        case "search": return <SearchTool toolName={p.toolName} query={args.query || args.pattern} results={p.result} status={status} onToggle={toggle} />;
                        case "list": return <ListTool toolName={p.toolName} path={extractFilePath(args)} items={p.result} status={status} onToggle={toggle} />;
                        case "todo": return <TodoTool todo={args.todo} todos={args.todos} status={status === "running" ? "running" : "complete"} onToggle={toggle} />;
                        case "ai": return <AgentTool description={args.description} prompt={args.prompt} subagent_type={args.subagent_type} run_in_background={args.run_in_background} status={status === "running" ? "running" : "complete"} onToggle={toggle} />;
                        case "permission": {
                          const interaction = p.args?.interaction;
                          const resolvedStatus = interaction?.status || (status === "error" ? "failed" : status === "complete" ? "resolved" : "pending");

                          if (p.toolName === "question_request") {
                            const onRespondQuestion = p.toolCallId && onInteractionRespond
                              ? (answers: Record<string, string>) => onInteractionRespond(p.toolCallId, { type: "question", answers })
                              : undefined;
                            return (
                              <QuestionResponseCard
                                toolName={p.toolName}
                                questions={args.questions}
                                status={resolvedStatus}
                                interactionState={interaction}
                                onRespond={onRespondQuestion}
                                onToggle={toggle}
                              />
                            );
                          } else if (p.toolName === "elicitation") {
                            const onRespondElicitation = p.toolCallId && onInteractionRespond
                              ? (action: "accept" | "decline" | "cancel", content?: Record<string, any>) => onInteractionRespond(p.toolCallId, { type: "elicitation", action, content })
                              : undefined;
                            return (
                              <ElicitationFormCard
                                toolName={p.toolName}
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
                            const onRespondPerm = p.toolCallId && onInteractionRespond
                              ? (allowed: boolean, reason?: string) => onInteractionRespond(p.toolCallId, { type: "permission", allowed, reason })
                              : undefined;
                            return (
                              <PermissionApprovalCard
                                toolName={p.toolName}
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
                  <div className="flex items-center gap-2 px-3 py-1.5 mt-3 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-[11px] font-bold text-[var(--text-secondary)] w-fit shadow-sm">
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

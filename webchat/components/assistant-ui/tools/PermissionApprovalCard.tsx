"use client";

import React, { useState } from "react";
import { motion } from "framer-motion";
import { useTranslation } from "react-i18next";
import type { InteractionState, InteractionStatus } from "@/lib/adapters/hotplex-runtime-adapter";
import { useInteractionTimeout } from "@/hooks/useInteractionTimeout";

interface ParsedPermissionArgs {
  command?: string;
  action?: string;
  Action?: string;
  target?: string;
  Target?: string;
  reason?: string;
  Reason?: string;
  directories?: string[];
  patterns?: string[];
  toolAction?: string;
  toolSummary?: string;
  [key: string]: any;
}

function parsePermissionArgs(args: any): ParsedPermissionArgs | null {
  if (!args) return null;
  if (Array.isArray(args)) {
    if (args.length === 1 && typeof args[0] === 'string') {
      try {
        const parsed = JSON.parse(args[0]);
        if (parsed && typeof parsed === 'object') {
          return parsed;
        }
      } catch (e) {
        // Not JSON
      }
    }
  } else if (typeof args === 'string') {
    try {
      const parsed = JSON.parse(args);
      if (parsed && typeof parsed === 'object') {
        return parsed;
      }
    } catch (e) {
      // Not JSON
    }
  } else if (typeof args === 'object') {
    return args;
  }
  return null;
}

interface PermissionApprovalCardProps {
  toolName: string;
  args?: Record<string, any>;
  description?: string;
  status: InteractionStatus;
  interactionState?: InteractionState;
  onRespond?: (approved: boolean, reason?: string) => void;
  onToggle?: () => void;
}

export function PermissionApprovalCard({
  toolName,
  args,
  description,
  status: initialStatus,
  interactionState,
  onRespond,
  onToggle,
}: PermissionApprovalCardProps) {
  const { t } = useTranslation("chat");
  const { timeLeft, activeStatus } = useInteractionTimeout(initialStatus, interactionState?.expiresAt);
  const [reason, setReason] = useState("");
  const isInteractive = activeStatus === "pending" || activeStatus === "failed";

  const parsed = parsePermissionArgs(args);

  const title = t("tool.interaction.permission.title", { defaultValue: "Tool Execution Approval" });
  
  const defaultDesc = t("tool.interaction.permission.description", { defaultValue: "Agent requests permission to execute the following tool" });
  let descText = description || defaultDesc;
  if (parsed && (!description || description === defaultDesc)) {
    if (parsed.toolAction) {
      descText = parsed.toolAction;
    } else if (parsed.toolSummary) {
      descText = parsed.toolSummary;
    } else if (parsed.reason || parsed.Reason) {
      descText = parsed.reason || parsed.Reason || defaultDesc;
    }
  }

  const formatTime = (seconds: number) => {
    const mins = Math.floor(seconds / 60);
    const secs = seconds % 60;
    return `${mins}:${secs.toString().padStart(2, "0")}`;
  };

  return (
    <motion.div
      className="rounded-[var(--radius-md)] overflow-hidden border border-[rgba(251,191,36,0.2)] my-4 shadow-[var(--shadow-md)] bg-[var(--bg-surface)]"
      initial={{ opacity: 0, scale: 0.98 }}
      animate={{ opacity: 1, scale: 1 }}
      transition={{ type: "spring", stiffness: 260, damping: 20 }}
    >
      {/* Header */}
      <div
        className={`flex items-center gap-2 px-4 py-3 bg-[rgba(251,191,36,0.06)] border-b border-[rgba(251,191,36,0.12)] ${
          onToggle ? "cursor-pointer hover:bg-[rgba(251,191,36,0.1)] transition-colors" : ""
        }`}
        onClick={onToggle}
      >
        <div className="w-7 h-7 rounded-[var(--radius-sm)] bg-[rgba(251,191,36,0.1)] flex items-center justify-center">
          <svg className="w-4 h-4 text-[var(--accent-gold)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
          </svg>
        </div>
        <span className="text-[11px] font-display font-bold text-[var(--accent-gold)] uppercase tracking-wider">
          {title}
        </span>
        
        {timeLeft !== null && timeLeft > 0 && activeStatus === "pending" && (
          <span className="ml-2 px-1.5 py-0.5 text-[10px] font-mono rounded bg-[rgba(251,191,36,0.1)] text-[var(--accent-gold)]">
            {formatTime(timeLeft)}
          </span>
        )}

        {onToggle && (
          <div className="ml-auto text-[var(--text-faint)]">
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15l7-7 7 7" />
            </svg>
          </div>
        )}
      </div>

      {/* Body */}
      <div className="px-4 py-3">
        <div className="mb-2 px-3 py-1.5 rounded-[var(--radius-sm)] bg-[var(--bg-base)] border border-[var(--border-subtle)]">
          <span className="font-mono text-[12px] text-[var(--accent-emerald)] select-none mr-1.5">$</span>
          <span className="font-mono text-[12px] text-[var(--text-primary)]">{toolName}</span>
        </div>
        {descText && (
          <p className="text-sm text-[var(--text-secondary)] leading-relaxed mb-3">{descText}</p>
        )}

        {args && Object.keys(args).length > 0 && (() => {
          if (parsed) {
            const displayAction = parsed.action || parsed.Action;
            const displayTarget = parsed.target || parsed.Target;
            const displayReason = parsed.reason || parsed.Reason;
            
            const renderedKeys = new Set([
              'command', 'action', 'Action', 'target', 'Target', 'reason', 'Reason',
              'directories', 'patterns', 'toolAction', 'toolSummary'
            ]);
            const extraKeys = Object.keys(parsed).filter(
              k => !renderedKeys.has(k) && parsed[k] !== undefined && parsed[k] !== null
            );

            return (
              <div className="mt-2 text-xs space-y-3 font-sans text-[var(--text-secondary)]">
                {parsed.command && (
                  <div className="space-y-1">
                    <div className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">
                      {t("tool.interaction.permission.command_to_execute", { defaultValue: "Command to Execute" })}
                    </div>
                    <div className="font-mono bg-[var(--bg-base)] border border-[var(--border-subtle)] rounded p-2.5 whitespace-pre-wrap break-all text-[var(--text-primary)]">
                      <span className="text-[var(--accent-emerald)] select-none mr-1.5">$</span>
                      {parsed.command}
                    </div>
                  </div>
                )}
                
                {displayAction && (
                  <div className="flex items-center gap-2">
                    <span className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">
                      {t("tool.interaction.permission.action", { defaultValue: "Action" })}:
                    </span>
                    <span className="font-mono bg-[var(--bg-elevated)] px-1.5 py-0.5 rounded border border-[var(--border-subtle)] text-[var(--text-primary)] font-bold">
                      {displayAction}
                    </span>
                  </div>
                )}

                {displayTarget && (
                  <div className="space-y-1">
                    <div className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">
                      {t("tool.interaction.permission.target", { defaultValue: "Target" })}
                    </div>
                    <div className="font-mono bg-[var(--bg-base)] border border-[var(--border-subtle)] rounded p-2.5 whitespace-pre-wrap break-all text-[var(--text-primary)]">
                      {displayTarget}
                    </div>
                  </div>
                )}

                {displayReason && (
                  <div className="space-y-1">
                    <div className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">
                      {t("tool.interaction.permission.reason", { defaultValue: "Reason" })}
                    </div>
                    <p className="text-sm text-[var(--text-primary)] italic">&quot;{displayReason}&quot;</p>
                  </div>
                )}

                {parsed.directories && Array.isArray(parsed.directories) && parsed.directories.length > 0 && (
                  <div className="space-y-1">
                    <div className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">
                      {t("tool.interaction.permission.allowed_directories", { defaultValue: "Allowed Directories" })}
                    </div>
                    <div className="flex flex-wrap gap-1">
                      {parsed.directories.map((dir, idx) => (
                        <span key={idx} className="font-mono text-[11px] bg-[var(--bg-base)] px-1.5 py-0.5 rounded border border-[var(--border-subtle)] text-[var(--text-primary)] break-all">
                          {dir}
                        </span>
                      ))}
                    </div>
                  </div>
                )}

                {parsed.patterns && Array.isArray(parsed.patterns) && parsed.patterns.length > 0 && (
                  <div className="space-y-1">
                    <div className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider">
                      {t("tool.interaction.permission.allowed_patterns", { defaultValue: "Allowed Patterns" })}
                    </div>
                    <div className="flex flex-wrap gap-1">
                      {parsed.patterns.map((pat, idx) => (
                        <span key={idx} className="font-mono text-[11px] bg-[var(--bg-base)] px-1.5 py-0.5 rounded border border-[var(--border-subtle)] text-[var(--text-primary)] break-all">
                          {pat}
                        </span>
                      ))}
                    </div>
                  </div>
                )}

                {extraKeys.length > 0 && (
                  <div className="space-y-1 mt-2 border-t border-[var(--border-subtle)] pt-2">
                    <div className="text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1">
                      {t("tool.interaction.permission.additional_parameters", { defaultValue: "Additional Parameters" })}
                    </div>
                    <div className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5 font-mono text-[11px]">
                      {extraKeys.map(k => (
                        <React.Fragment key={k}>
                          <span className="text-[var(--text-faint)]">{k}:</span>
                          <span className="text-[var(--text-primary)] break-all">
                            {typeof parsed[k] === 'object' ? JSON.stringify(parsed[k]) : String(parsed[k])}
                          </span>
                        </React.Fragment>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            );
          }

          return (
            <div className="mt-2 text-xs font-mono bg-[var(--bg-base)] border border-[var(--border-subtle)] rounded p-2.5 max-h-[160px] overflow-y-auto custom-scrollbar whitespace-pre-wrap text-[var(--text-primary)]">
              {JSON.stringify(args, null, 2)}
            </div>
          );
        })()}
      </div>

      {/* Action Buttons */}
      {(activeStatus === "pending" || activeStatus === "failed") && (
        <div className="flex flex-col gap-2 px-4 py-3 border-t border-[var(--border-subtle)] bg-[var(--bg-surface)]">
          {activeStatus === "failed" && interactionState?.error && (
            <div className="text-xs text-[var(--accent-coral)] mb-2 px-3 py-2 rounded bg-[rgba(239,68,68,0.06)] border border-[rgba(239,68,68,0.15)] leading-normal">
              {t("tool.interaction.permission.failed", { error: interactionState.error, defaultValue: `Submission failed: ${interactionState.error}` })}
            </div>
          )}
          <div className="mb-2">
            <input
              type="text"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder={t("tool.interaction.permission.reason_placeholder", { defaultValue: "Provide feedback/reason (optional)..." })}
              className="w-full text-xs font-mono px-3 py-2 rounded border border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] transition-colors disabled:opacity-75"
              disabled={!isInteractive}
            />
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => onRespond?.(true, reason)}
              className="flex-1 py-2 rounded-[var(--radius-sm)] bg-[var(--accent-emerald)] text-black font-bold text-xs transition-all hover:opacity-90 active:scale-[0.98]"
            >
              {t("tool.interaction.permission.approve", { defaultValue: "Approve" })}
            </button>
            <button
              type="button"
              onClick={() => onRespond?.(false, reason)}
              className="flex-1 py-2 rounded-[var(--radius-sm)] bg-[var(--accent-coral)] text-white font-bold text-xs transition-all hover:opacity-90 active:scale-[0.98]"
            >
              {t("tool.interaction.permission.reject", { defaultValue: "Reject" })}
            </button>
          </div>
        </div>
      )}

      {activeStatus === "submitting" && (
        <div className="flex items-center justify-center gap-2 px-4 py-3 border-t border-[var(--border-subtle)] bg-[var(--bg-surface)] text-xs text-[var(--text-secondary)]">
          <div className="w-3.5 h-3.5 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
          <span>{t("tool.interaction.permission.submitting", { defaultValue: "Submitting approval..." })}</span>
        </div>
      )}

      {activeStatus === "resolved" && (
        <div className="border-t border-[var(--border-subtle)] bg-[rgba(16,185,129,0.04)]">
          <div className="flex items-center gap-2 px-4 py-2.5">
            <svg className="w-4 h-4 text-[var(--accent-emerald)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
            </svg>
            <span className="text-xs font-mono font-medium text-[var(--accent-emerald)]">
              {t("tool.interaction.permission.approved", { defaultValue: "Approved" })}
            </span>
          </div>
          {interactionState?.response?.reason && (
            <div className="px-4 pb-2.5 text-xs text-[var(--text-secondary)] font-mono pl-10 leading-normal border-t border-[rgba(16,185,129,0.08)] pt-1.5">
              <span className="text-[var(--text-faint)]">Feedback:</span> <span className="text-[var(--text-primary)]">{interactionState.response.reason}</span>
            </div>
          )}
        </div>
      )}

      {activeStatus === "rejected" && (
        <div className="border-t border-[var(--border-subtle)] bg-[rgba(239,68,68,0.04)]">
          <div className="flex items-center gap-2 px-4 py-2.5">
            <svg className="w-4 h-4 text-[var(--accent-coral)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M6 18L18 6M6 6l12 12" />
            </svg>
            <span className="text-xs font-mono font-medium text-[var(--accent-coral)]">
              {t("tool.interaction.permission.rejected", { defaultValue: "Rejected" })}
            </span>
          </div>
          {interactionState?.response?.reason && (
            <div className="px-4 pb-2.5 text-xs text-[var(--text-secondary)] font-mono pl-10 leading-normal border-t border-[rgba(239,68,68,0.08)] pt-1.5">
              <span className="text-[var(--text-faint)]">Reason:</span> <span className="text-[var(--text-primary)]">{interactionState.response.reason}</span>
            </div>
          )}
        </div>
      )}

      {activeStatus === "expired" && (
        <div className="flex items-center gap-2 px-4 py-2.5 border-t border-[var(--border-subtle)] bg-[var(--bg-base)]">
          <svg className="w-4 h-4 text-[var(--text-faint)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <span className="text-xs font-mono text-[var(--text-faint)]">
            {t("tool.interaction.permission.expired", { defaultValue: "Expired" })}
          </span>
        </div>
      )}
    </motion.div>
  );
}

"use client";

import { motion } from "framer-motion";
import { useTranslation } from "react-i18next";
import type { InteractionState, InteractionStatus } from "@/lib/adapters/hotplex-runtime-adapter";
import { useInteractionTimeout } from "@/hooks/useInteractionTimeout";

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

  const title = t("tool.interaction.permission.title", { defaultValue: "Tool Execution Approval" });
  const descText = description || t("tool.interaction.permission.description", { defaultValue: "Agent requests permission to execute the following tool" });

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

        {args && Object.keys(args).length > 0 && (
          <div className="mt-2 text-xs font-mono bg-[var(--bg-base)] border border-[var(--border-subtle)] rounded p-2.5 max-h-[160px] overflow-y-auto custom-scrollbar whitespace-pre-wrap text-[var(--text-primary)]">
            {JSON.stringify(args, null, 2)}
          </div>
        )}
      </div>

      {/* Action Buttons */}
      {(activeStatus === "pending" || activeStatus === "failed") && (
        <div className="flex flex-col gap-2 px-4 py-3 border-t border-[var(--border-subtle)] bg-[var(--bg-surface)]">
          {activeStatus === "failed" && interactionState?.error && (
            <div className="text-xs text-[var(--accent-coral)] mb-2 px-3 py-2 rounded bg-[rgba(239,68,68,0.06)] border border-[rgba(239,68,68,0.15)] leading-normal">
              {t("tool.interaction.permission.failed", { error: interactionState.error, defaultValue: `Submission failed: ${interactionState.error}` })}
            </div>
          )}
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => onRespond?.(true)}
              className="flex-1 py-2 rounded-[var(--radius-sm)] bg-[var(--accent-emerald)] text-black font-bold text-xs transition-all hover:opacity-90 active:scale-[0.98]"
            >
              {t("tool.interaction.permission.approve", { defaultValue: "Approve" })}
            </button>
            <button
              type="button"
              onClick={() => onRespond?.(false)}
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
        <div className="flex items-center gap-2 px-4 py-2.5 border-t border-[var(--border-subtle)] bg-[rgba(16,185,129,0.04)]">
          <svg className="w-4 h-4 text-[var(--accent-emerald)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
          </svg>
          <span className="text-xs font-mono font-medium text-[var(--accent-emerald)]">
            {t("tool.interaction.permission.approved", { defaultValue: "Approved" })}
          </span>
        </div>
      )}

      {activeStatus === "rejected" && (
        <div className="flex items-center gap-2 px-4 py-2.5 border-t border-[var(--border-subtle)] bg-[rgba(239,68,68,0.04)]">
          <svg className="w-4 h-4 text-[var(--accent-coral)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M6 18L18 6M6 6l12 12" />
          </svg>
          <span className="text-xs font-mono font-medium text-[var(--accent-coral)]">
            {t("tool.interaction.permission.rejected", { defaultValue: "Rejected" })}
          </span>
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

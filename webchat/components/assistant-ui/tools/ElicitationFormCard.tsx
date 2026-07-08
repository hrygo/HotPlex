"use client";

import { useState } from "react";
import { motion } from "framer-motion";
import { useTranslation } from "react-i18next";
import type { InteractionState, InteractionStatus } from "@/lib/adapters/hotplex-runtime-adapter";
import { useInteractionTimeout } from "@/hooks/useInteractionTimeout";

interface ElicitationFormCardProps {
  toolName: string;
  message: string;
  mcpServerName: string;
  url?: string;
  requestedSchema?: {
    type?: string;
    properties?: Record<
      string,
      {
        type?: string;
        description?: string;
        enum?: string[];
      }
    >;
    required?: string[];
  };
  status: InteractionStatus;
  interactionState?: InteractionState;
  onRespond?: (action: "accept" | "decline" | "cancel", content?: Record<string, any>) => void;
  onToggle?: () => void;
}

export function ElicitationFormCard({
  toolName,
  message,
  mcpServerName,
  url,
  requestedSchema,
  status: initialStatus,
  interactionState,
  onRespond,
  onToggle,
}: ElicitationFormCardProps) {
  const { t } = useTranslation("chat");
  const { timeLeft, activeStatus } = useInteractionTimeout(initialStatus, interactionState?.expiresAt);

  // Local state for schema form fields
  const [formValues, setFormValues] = useState<Record<string, any>>({});

  const isInteractive = activeStatus === "pending" || activeStatus === "failed";

  const handleInputChange = (key: string, value: any) => {
    if (!isInteractive) return;
    setFormValues((prev) => ({ ...prev, [key]: value }));
  };

  const handleAction = (action: "accept" | "decline" | "cancel") => {
    if (!isInteractive) return;
    if (action === "accept") {
      onRespond?.(action, requestedSchema ? formValues : undefined);
    } else {
      onRespond?.(action);
    }
  };

  const title = t("tool.interaction.elicitation.title", { defaultValue: "Interactive Request" });
  const descText = t("tool.interaction.elicitation.description", { defaultValue: "Input request from MCP server" });

  const formatTime = (seconds: number) => {
    const mins = Math.floor(seconds / 60);
    const secs = seconds % 60;
    return `${mins}:${secs.toString().padStart(2, "0")}`;
  };

  const schemaProperties = requestedSchema?.properties || {};
  const hasSchema = Object.keys(schemaProperties).length > 0;

  return (
    <motion.div
      className="rounded-[var(--radius-md)] overflow-hidden border border-[rgba(139,92,246,0.2)] my-4 shadow-[var(--shadow-md)] bg-[var(--bg-surface)]"
      initial={{ opacity: 0, scale: 0.98 }}
      animate={{ opacity: 1, scale: 1 }}
      transition={{ type: "spring", stiffness: 260, damping: 20 }}
    >
      {/* Header */}
      <div
        className={`flex items-center gap-2 px-4 py-3 bg-[rgba(139,92,246,0.06)] border-b border-[rgba(139,92,246,0.12)] ${
          onToggle ? "cursor-pointer hover:bg-[rgba(139,92,246,0.1)] transition-colors" : ""
        }`}
        onClick={onToggle}
      >
        <div className="w-7 h-7 rounded-[var(--radius-sm)] bg-[rgba(139,92,246,0.1)] flex items-center justify-center">
          <svg className="w-4 h-4 text-[var(--accent-gold)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
          </svg>
        </div>
        <span className="text-[11px] font-display font-bold text-[var(--accent-gold)] uppercase tracking-wider">
          {mcpServerName ? `${mcpServerName} - ${title}` : title}
        </span>
        
        {timeLeft !== null && timeLeft > 0 && activeStatus === "pending" && (
          <span className="ml-2 px-1.5 py-0.5 text-[10px] font-mono rounded bg-[rgba(139,92,246,0.1)] text-[var(--accent-gold)]">
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
      <div className="px-4 py-3 space-y-4">
        {descText && (
          <p className="text-sm text-[var(--text-secondary)] leading-relaxed">{message || descText}</p>
        )}

        {/* URL Link */}
        {url && (
          <div className="pt-1">
            <a
              href={url}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-2 px-4 py-2 rounded-[var(--radius-sm)] bg-[rgba(139,92,246,0.1)] text-[rgba(139,92,246,1)] hover:bg-[rgba(139,92,246,0.15)] font-bold text-xs transition-colors border border-[rgba(139,92,246,0.2)]"
            >
              <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" />
              </svg>
              {t("tool.interaction.elicitation.open_url", { defaultValue: "Open Form URL" })}
            </a>
          </div>
        )}

        {/* Dynamic Form Schema */}
        {hasSchema && (
          <div className="space-y-3 p-3 rounded-[var(--radius-sm)] bg-[var(--bg-base)] border border-[var(--border-subtle)]">
            {Object.entries(schemaProperties).map(([key, prop]) => {
              const label = key;
              const type = prop.type || "string";
              const isRequired = requestedSchema?.required?.includes(key);

              return (
                <div key={key} className="space-y-1">
                  <label className="text-xs font-bold text-[var(--text-secondary)]">
                    {label}
                    {isRequired && <span className="text-[var(--accent-coral)] ml-0.5">*</span>}
                  </label>
                  
                  {prop.enum && prop.enum.length > 0 ? (
                    <select
                      value={formValues[key] || ""}
                      disabled={!isInteractive}
                      onChange={(e) => handleInputChange(key, e.target.value)}
                      className="w-full text-xs font-mono p-2 rounded border border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] transition-colors disabled:opacity-75 disabled:cursor-not-allowed"
                    >
                      <option value="">Select option...</option>
                      {prop.enum.map((opt) => (
                        <option key={opt} value={opt}>
                          {opt}
                        </option>
                      ))}
                    </select>
                  ) : type === "boolean" ? (
                    <label className="flex items-center gap-2.5 py-1 text-xs cursor-pointer select-none">
                      <input
                        type="checkbox"
                        checked={!!formValues[key]}
                        disabled={!isInteractive}
                        onChange={(e) => handleInputChange(key, e.target.checked)}
                        className="hidden"
                      />
                      <div className={`w-4 h-4 flex items-center justify-center border rounded transition-all ${
                        formValues[key]
                          ? "border-[var(--accent-gold)] bg-[var(--accent-gold)] text-black"
                          : "border-[var(--text-faint)]"
                      }`}>
                        {formValues[key] && (
                          <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={3} d="M5 13l4 4L19 7" />
                          </svg>
                        )}
                      </div>
                      <span className="text-[var(--text-primary)] font-medium">{prop.description || "Enable"}</span>
                    </label>
                  ) : (
                    <input
                      type="text"
                      value={formValues[key] || ""}
                      disabled={!isInteractive}
                      onChange={(e) => handleInputChange(key, e.target.value)}
                      placeholder={prop.description || `Enter ${label}...`}
                      className="w-full text-xs font-mono p-2 rounded border border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] transition-colors disabled:opacity-75 disabled:cursor-not-allowed"
                    />
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Action Buttons */}
      {(activeStatus === "pending" || activeStatus === "failed") && (
        <div className="flex flex-col gap-2 px-4 py-3 border-t border-[var(--border-subtle)] bg-[var(--bg-surface)]">
          {activeStatus === "failed" && interactionState?.error && (
            <div className="text-xs text-[var(--accent-coral)] mb-2 px-3 py-2 rounded bg-[rgba(239,68,68,0.06)] border border-[rgba(239,68,68,0.15)] leading-normal">
              {t("tool.interaction.elicitation.failed", { error: interactionState.error, defaultValue: `Submission failed: ${interactionState.error}` })}
            </div>
          )}
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => handleAction("accept")}
              className="flex-grow py-2 rounded-[var(--radius-sm)] bg-[var(--accent-emerald)] text-black font-bold text-xs transition-all hover:opacity-90 active:scale-[0.98]"
            >
              {t("tool.interaction.elicitation.accept", { defaultValue: "Accept" })}
            </button>
            <button
              type="button"
              onClick={() => handleAction("decline")}
              className="flex-grow py-2 rounded-[var(--radius-sm)] bg-[var(--accent-coral)] text-white font-bold text-xs transition-all hover:opacity-90 active:scale-[0.98]"
            >
              {t("tool.interaction.elicitation.decline", { defaultValue: "Decline" })}
            </button>
            <button
              type="button"
              onClick={() => handleAction("cancel")}
              className="py-2 px-4 rounded-[var(--radius-sm)] bg-[var(--bg-base)] text-[var(--text-secondary)] font-bold text-xs transition-all hover:opacity-90 active:scale-[0.98] border border-[var(--border-subtle)]"
            >
              {t("tool.interaction.elicitation.cancel", { defaultValue: "Cancel" })}
            </button>
          </div>
        </div>
      )}

      {activeStatus === "submitting" && (
        <div className="flex items-center justify-center gap-2 px-4 py-3 border-t border-[var(--border-subtle)] bg-[var(--bg-surface)] text-xs text-[var(--text-secondary)]">
          <div className="w-3.5 h-3.5 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
          <span>{t("tool.interaction.elicitation.submitting", { defaultValue: "Submitting response..." })}</span>
        </div>
      )}

      {activeStatus === "resolved" && (
        <div className="border-t border-[var(--border-subtle)] bg-[rgba(16,185,129,0.04)]">
          <div className="flex items-center gap-2 px-4 py-2.5">
            <svg className="w-4 h-4 text-[var(--accent-emerald)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
            </svg>
            <span className="text-xs font-mono font-medium text-[var(--accent-emerald)]">
              {t("tool.interaction.elicitation.accepted", { defaultValue: "Request Accepted" })}
            </span>
          </div>

          {interactionState?.response?.content && Object.keys(interactionState.response.content).length > 0 && (
            <div className="px-4 pb-3 space-y-1 text-xs text-[var(--text-secondary)] font-mono pl-10 border-t border-[rgba(16,185,129,0.08)] pt-2 leading-relaxed">
              {Object.entries(interactionState.response.content).map(([k, v]) => (
                <div key={k}>
                  <span className="text-[var(--text-faint)]">{k}:</span> <span className="text-[var(--text-primary)]">{String(v)}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {activeStatus === "rejected" && (
        <div className="flex items-center gap-2 px-4 py-2.5 border-t border-[var(--border-subtle)] bg-[rgba(239,68,68,0.04)]">
          <svg className="w-4 h-4 text-[var(--accent-coral)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M6 18L18 6M6 6l12 12" />
          </svg>
          <span className="text-xs font-mono font-medium text-[var(--accent-coral)]">
            {t("tool.interaction.elicitation.declined", { defaultValue: "Request Declined" })}
          </span>
        </div>
      )}

      {activeStatus === "expired" && (
        <div className="flex items-center gap-2 px-4 py-2.5 border-t border-[var(--border-subtle)] bg-[var(--bg-base)]">
          <svg className="w-4 h-4 text-[var(--text-faint)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <span className="text-xs font-mono text-[var(--text-faint)]">
            {t("tool.interaction.elicitation.expired", { defaultValue: "Request Expired" })}
          </span>
        </div>
      )}
    </motion.div>
  );
}

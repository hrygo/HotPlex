"use client";

import { useState } from "react";
import { motion } from "framer-motion";
import { useTranslation } from "react-i18next";
import type { InteractionState, InteractionStatus } from "@/lib/adapters/hotplex-runtime-adapter";
import { useInteractionTimeout } from "@/hooks/useInteractionTimeout";

interface QuestionItem {
  id: string;
  header?: string;
  question: string;
  options?: Array<{ value?: string; label: string }>;
  is_multi_select?: boolean;
  multiSelect?: boolean;
}

interface QuestionResponseCardProps {
  toolName: string;
  questions?: QuestionItem[];
  status: InteractionStatus;
  interactionState?: InteractionState;
  onRespond?: (answers: Record<string, string>) => void;
  onToggle?: () => void;
}

export function QuestionResponseCard({
  toolName,
  questions = [],
  status: initialStatus,
  interactionState,
  onRespond,
  onToggle,
}: QuestionResponseCardProps) {
  const { t } = useTranslation("chat");
  const { timeLeft, activeStatus } = useInteractionTimeout(initialStatus, interactionState?.expiresAt);

  // Local state for answers: questionId -> value (or comma-separated values for multi-select)
  const [answers, setAnswers] = useState<Record<string, string>>({});
  // Local state for multi-select checkboxes: questionId -> Set of selected values
  const [selectedMulti, setSelectedMulti] = useState<Record<string, Set<string>>>({});
  // Local state for custom text inputs: questionId -> string
  const [textAnswers, setTextAnswers] = useState<Record<string, string>>({});

  const isInteractive = activeStatus === "pending" || activeStatus === "failed";

  const handleOptionSelect = (qId: string, val: string, isMulti: boolean) => {
    if (!isInteractive) return;

    if (isMulti) {
      const currentSet = new Set(selectedMulti[qId] || []);
      if (currentSet.has(val)) {
        currentSet.delete(val);
      } else {
        currentSet.add(val);
      }
      setSelectedMulti((prev) => ({ ...prev, [qId]: currentSet }));
      setAnswers((prev) => ({ ...prev, [qId]: Array.from(currentSet).join(", ") }));
    } else {
      setAnswers((prev) => ({ ...prev, [qId]: val }));
    }
  };

  const handleTextChange = (qId: string, val: string) => {
    if (!isInteractive) return;
    setTextAnswers((prev) => ({ ...prev, [qId]: val }));
    setAnswers((prev) => ({ ...prev, [qId]: val }));
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!isInteractive) return;

    // Validate that all questions have some answer
    const finalAnswers: Record<string, string> = {};
    for (const q of questions) {
      const qKey = q.id || q.question;
      const ans = answers[qKey] || textAnswers[qKey] || "";
      finalAnswers[qKey] = ans;
    }

    onRespond?.(finalAnswers);
  };

  const title = t("tool.interaction.question.title", { defaultValue: "Input Request" });
  const descText = t("tool.interaction.question.description", { defaultValue: "Agent requires your input on the following questions" });

  const formatTime = (seconds: number) => {
    const mins = Math.floor(seconds / 60);
    const secs = seconds % 60;
    return `${mins}:${secs.toString().padStart(2, "0")}`;
  };

  return (
    <motion.div
      className="rounded-[var(--radius-md)] overflow-hidden border border-[rgba(245,158,11,0.2)] my-4 shadow-[var(--shadow-md)] bg-[var(--bg-surface)]"
      initial={{ opacity: 0, scale: 0.98 }}
      animate={{ opacity: 1, scale: 1 }}
      transition={{ type: "spring", stiffness: 260, damping: 20 }}
    >
      {/* Header */}
      <div
        className={`flex items-center gap-2 px-4 py-3 bg-[rgba(245,158,11,0.06)] border-b border-[rgba(245,158,11,0.12)] ${
          onToggle ? "cursor-pointer hover:bg-[rgba(245,158,11,0.1)] transition-colors" : ""
        }`}
        onClick={onToggle}
      >
        <div className="w-7 h-7 rounded-[var(--radius-sm)] bg-[rgba(245,158,11,0.1)] flex items-center justify-center">
          <svg className="w-4 h-4 text-[var(--accent-gold)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8.228 9c.549-1.165 2.03-2 3.772-2 2.21 0 4 1.343 4 3 0 1.4-1.278 2.575-3.006 2.907-.542.104-.994.54-.994 1.093m0 3h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
        </div>
        <span className="text-[11px] font-display font-bold text-[var(--accent-gold)] uppercase tracking-wider">
          {title}
        </span>
        
        {timeLeft !== null && timeLeft > 0 && activeStatus === "pending" && (
          <span className="ml-2 px-1.5 py-0.5 text-[10px] font-mono rounded bg-[rgba(245,158,11,0.1)] text-[var(--accent-gold)]">
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
      <form onSubmit={handleSubmit}>
        <div className="px-4 py-3 space-y-4">
          {descText && (
            <p className="text-sm text-[var(--text-secondary)] leading-relaxed">{descText}</p>
          )}

          {questions.map((q, idx) => {
            const qKey = q.id || q.question;
            const isMulti = !!(q.is_multi_select || q.multiSelect);
            const hasOptions = q.options && q.options.length > 0;
            const currentAnswer = answers[qKey] || "";

            return (
              <div key={qKey || idx} className="space-y-2.5 p-3 rounded-[var(--radius-sm)] bg-[var(--bg-base)] border border-[var(--border-subtle)]">
                <div className="flex items-start gap-2">
                  <span className="font-mono text-xs text-[var(--text-faint)] mt-0.5">{idx + 1}.</span>
                  <div>
                    <h4 className="text-sm font-semibold text-[var(--text-primary)] leading-snug">
                      {q.header || q.question}
                      {isMulti && (
                        <span className="text-xs font-normal text-[var(--text-faint)] ml-1">
                          {t("tool.interaction.question.multi_select_hint", { defaultValue: " (Multiple choice)" })}
                        </span>
                      )}
                    </h4>
                    {q.header && q.question && (
                      <p className="text-xs text-[var(--text-secondary)] mt-1">{q.question}</p>
                    )}
                  </div>
                </div>

                {hasOptions ? (
                  <div className="space-y-1.5 pl-5">
                    {q.options!.map((opt, oIdx) => {
                      const optVal = opt.value !== undefined ? opt.value : opt.label;
                      const isSelected = isMulti
                        ? !!(selectedMulti[qKey]?.has(optVal))
                        : currentAnswer === optVal;

                      return (
                        <label
                          key={oIdx}
                          className={`flex items-center gap-2.5 px-3 py-2 rounded border text-xs cursor-pointer select-none transition-all ${
                            isSelected
                              ? "bg-[rgba(245,158,11,0.06)] border-[var(--accent-gold)] text-[var(--text-primary)]"
                              : "border-[var(--border-subtle)] text-[var(--text-secondary)] hover:bg-[var(--bg-elevated)]"
                          } ${!isInteractive ? "opacity-75 cursor-not-allowed" : ""}`}
                        >
                          <input
                            type={isMulti ? "checkbox" : "radio"}
                            name={qKey}
                            value={optVal}
                            checked={isSelected}
                            disabled={!isInteractive}
                            onChange={() => handleOptionSelect(qKey, optVal, isMulti)}
                            className="hidden"
                          />
                          <div className={`w-3.5 h-3.5 flex items-center justify-center border transition-all ${
                            isMulti ? "rounded-[3px]" : "rounded-full"
                          } ${
                            isSelected
                              ? "border-[var(--accent-gold)] bg-[var(--accent-gold)] text-black"
                              : "border-[var(--text-faint)]"
                          }`}>
                            {isSelected && (
                              <svg className="w-2.5 h-2.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={3} d="M5 13l4 4L19 7" />
                              </svg>
                            )}
                          </div>
                          <span className="font-medium leading-none">{opt.label}</span>
                        </label>
                      );
                    })}
                  </div>
                ) : (
                  <div className="pl-5">
                    <textarea
                      rows={3}
                      value={textAnswers[qKey] || ""}
                      disabled={!isInteractive}
                      onChange={(e) => handleTextChange(qKey, e.target.value)}
                      placeholder={t("tool.interaction.question.custom_placeholder", { defaultValue: "Type custom answer..." })}
                      className="w-full text-xs font-mono p-2.5 rounded border border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] transition-colors disabled:opacity-75 disabled:cursor-not-allowed resize-none"
                    />
                  </div>
                )}
              </div>
            );
          })}
        </div>

        {/* Action Buttons */}
        {(activeStatus === "pending" || activeStatus === "failed") && (
          <div className="flex flex-col gap-2 px-4 py-3 border-t border-[var(--border-subtle)] bg-[var(--bg-surface)]">
            {activeStatus === "failed" && interactionState?.error && (
              <div className="text-xs text-[var(--accent-coral)] mb-2 px-3 py-2 rounded bg-[rgba(239,68,68,0.06)] border border-[rgba(239,68,68,0.15)] leading-normal">
                {t("tool.interaction.question.failed", { error: interactionState.error, defaultValue: `Submission failed: ${interactionState.error}` })}
              </div>
            )}
            <button
              type="submit"
              className="w-full py-2 rounded-[var(--radius-sm)] bg-[var(--accent-gold)] text-black font-bold text-xs transition-all hover:opacity-90 active:scale-[0.98]"
            >
              {t("tool.interaction.question.submit", { defaultValue: "Submit Answer" })}
            </button>
          </div>
        )}

        {activeStatus === "submitting" && (
          <div className="flex items-center justify-center gap-2 px-4 py-3 border-t border-[var(--border-subtle)] bg-[var(--bg-surface)] text-xs text-[var(--text-secondary)]">
            <div className="w-3.5 h-3.5 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
            <span>{t("tool.interaction.question.submitting", { defaultValue: "Submitting answers..." })}</span>
          </div>
        )}

        {activeStatus === "resolved" && (
          <div className="border-t border-[var(--border-subtle)] bg-[rgba(16,185,129,0.04)]">
            <div className="flex items-center gap-2 px-4 py-2.5">
              <svg className="w-4 h-4 text-[var(--accent-emerald)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
              </svg>
              <span className="text-xs font-mono font-medium text-[var(--accent-emerald)]">
                {t("tool.interaction.question.answered", { defaultValue: "Answered" })}
              </span>
            </div>
            
            {interactionState?.response?.answers && (
              <div className="px-4 pb-3 space-y-1 text-xs text-[var(--text-secondary)] font-mono pl-10 border-t border-[rgba(16,185,129,0.08)] pt-2 leading-relaxed">
                {Object.entries(interactionState.response.answers).map(([qId, val]) => {
                  const q = questions.find((qi) => (qi.id || qi.question) === qId);
                  const qLabel = q?.header || q?.question || qId;
                  return (
                    <div key={qId}>
                      <span className="text-[var(--text-faint)]">{qLabel}:</span> <span className="text-[var(--text-primary)]">{val as string}</span>
                    </div>
                  );
                })}
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
              {t("tool.interaction.question.expired", { defaultValue: "Question Expired" })}
            </span>
          </div>
        )}
      </form>
    </motion.div>
  );
}

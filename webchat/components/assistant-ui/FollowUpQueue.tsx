"use client";

import { useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { useTranslation } from "react-i18next";

import type {
  FollowUpQueueControls,
  FollowUpQueueErrorKind,
  FollowUpQueueItem,
} from "@/lib/adapters/follow-up-queue";

interface FollowUpQueueProps {
  queue: FollowUpQueueControls;
  isStopping?: boolean;
}

const errorKey = {
  unknown: "follow_up.error.unknown",
  connection: "follow_up.error.connection",
  busy: "follow_up.error.busy",
  send: "follow_up.error.send",
  stop: "follow_up.error.stop",
} as const satisfies Record<FollowUpQueueErrorKind, string>;

export function FollowUpQueue({ queue, isStopping }: FollowUpQueueProps) {
  const { t } = useTranslation("chat");
  const [activeItemId, setActiveItemId] = useState<string | null>(null);
  const [editingItemId, setEditingItemId] = useState<string | null>(null);
  const [draftText, setDraftText] = useState("");

  if (queue.items.length === 0) return null;

  const activeItem = queue.items.find((item) => item.id === activeItemId);

  const handleEditClick = (item: FollowUpQueueItem) => {
    setDraftText(item.text);
    setEditingItemId(item.id);
  };

  const handleSaveEdit = (item: FollowUpQueueItem) => {
    if (!draftText.trim()) return;
    queue.updateText(item.id, draftText);
    setEditingItemId(null);
  };

  return (
    <div className="relative w-full pointer-events-auto">
      {/* Detail Popover Card */}
      <AnimatePresence>
        {activeItem && (
          <motion.div
            initial={{ opacity: 0, y: 10, scale: 0.95 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 10, scale: 0.95 }}
            className="absolute bottom-full right-0 z-30 mb-2.5 max-w-sm w-96 rounded-2xl border border-[var(--border-subtle)] bg-[var(--bg-glass)] backdrop-blur-xl p-4 shadow-xl flex flex-col gap-3 pointer-events-auto"
          >
            <div className="flex items-center justify-between">
              <span className="font-mono text-[9px] font-bold uppercase tracking-[0.14em] text-[var(--text-faint)]">
                {t(`follow_up.status.${activeItem.status}`)}
              </span>
              <button
                type="button"
                onClick={() => {
                  setActiveItemId(null);
                  setEditingItemId(null);
                }}
                className="text-[var(--text-faint)] hover:text-[var(--text-primary)] text-xs p-1"
              >
                ✕
              </button>
            </div>

            {editingItemId === activeItem.id ? (
              <div className="space-y-2">
                <textarea
                  value={draftText}
                  onChange={(e) => setDraftText(e.target.value)}
                  rows={3}
                  className="w-full resize-y rounded-lg border border-[var(--accent-gold)]/40 bg-[var(--bg-base)] px-3 py-2 text-xs leading-relaxed text-[var(--text-primary)] outline-none focus:ring-2 focus:ring-[var(--accent-gold)]/25"
                />
                <div className="flex justify-end gap-2">
                  <button
                    type="button"
                    onClick={() => setEditingItemId(null)}
                    className="rounded-md px-2.5 py-1 text-[10px] font-medium text-[var(--text-muted)] hover:bg-[var(--bg-hover)]"
                  >
                    {t("follow_up.action.cancel")}
                  </button>
                  <button
                    type="button"
                    onClick={() => handleSaveEdit(activeItem)}
                    disabled={!draftText.trim()}
                    className="rounded-md bg-[var(--accent-gold)] px-2.5 py-1 text-[10px] font-bold text-black disabled:opacity-40"
                  >
                    {t("follow_up.action.save")}
                  </button>
                </div>
              </div>
            ) : (
              <p className="whitespace-pre-wrap break-words text-xs leading-relaxed text-[var(--text-primary)] max-h-48 overflow-y-auto">
                {activeItem.text}
              </p>
            )}

            {activeItem.status === "failed" && activeItem.errorKind && (
              <p role="alert" className="flex items-center gap-1.5 text-[10px] leading-relaxed text-[var(--accent-coral)]">
                <span>⚠️</span>
                {t(errorKey[activeItem.errorKind])}
              </p>
            )}

            {editingItemId !== activeItem.id && (
              <div className="flex items-center gap-2 pt-2 border-t border-[var(--border-subtle)]">
                <button
                  type="button"
                  onClick={() => handleEditClick(activeItem)}
                  disabled={activeItem.status === "sending"}
                  className="rounded border border-[var(--border-subtle)] px-2.5 py-1 text-[10px] text-[var(--text-muted)] hover:border-[var(--text-faint)] hover:text-[var(--text-primary)] disabled:opacity-40"
                >
                  {t("follow_up.action.edit")}
                </button>
                <button
                  type="button"
                  onClick={() => {
                    queue.remove(activeItem.id);
                    setActiveItemId(null);
                  }}
                  disabled={activeItem.status === "sending"}
                  className="rounded border border-[var(--border-subtle)] px-2.5 py-1 text-[10px] text-[var(--text-muted)] hover:border-[var(--accent-coral)]/40 hover:text-[var(--accent-coral)] disabled:opacity-40"
                >
                  {t("follow_up.action.delete")}
                </button>
                {activeItem.status === "failed" && (
                  <button
                    type="button"
                    onClick={() => {
                      queue.retry(activeItem.id);
                      setActiveItemId(null);
                    }}
                    className="rounded border border-[var(--accent-gold)]/35 bg-[var(--accent-gold)]/10 px-2.5 py-1 text-[10px] font-semibold text-[var(--accent-gold)] hover:bg-[var(--accent-gold)]/15"
                  >
                    {t("follow_up.action.retry")}
                  </button>
                )}
                <button
                  type="button"
                  onClick={() => {
                    void queue.sendNow(activeItem.id);
                    setActiveItemId(null);
                  }}
                  disabled={activeItem.status === "sending" || isStopping}
                  className="ml-auto rounded bg-[var(--accent-coral)]/12 px-2.5 py-1 text-[10px] font-semibold text-[var(--accent-coral)] hover:bg-[var(--accent-coral)]/20 disabled:opacity-40"
                >
                  {t("follow_up.action.send_now")}
                </button>
              </div>
            )}
          </motion.div>
        )}
      </AnimatePresence>

      {/* Horizontal Pills Row */}
      <div className="flex items-center justify-end gap-1.5 overflow-x-auto no-scrollbar py-1 w-full">
        <AnimatePresence initial={false} mode="popLayout">
          {queue.items.map((item) => {
            const sending = item.status === "sending";
            const failed = item.status === "failed";
            return (
              <motion.button
                key={item.id}
                layout
                initial={{ opacity: 0, scale: 0.9, x: -10 }}
                animate={{ opacity: 1, scale: 1, x: 0 }}
                exit={{ opacity: 0, scale: 0.9, x: 10 }}
                type="button"
                onClick={() => {
                  setActiveItemId(item.id);
                  setEditingItemId(null);
                }}
                className={`flex items-center gap-1.5 px-3 py-1.5 rounded-full border shadow-sm text-[11px] font-medium transition-all cursor-pointer whitespace-nowrap hover:scale-[1.02] active:scale-[0.98] ${
                  failed
                    ? "bg-[var(--accent-coral)]/8 border-[var(--accent-coral)]/30 text-[var(--accent-coral)]"
                    : sending
                      ? "bg-[var(--accent-gold)]/8 border-[var(--accent-gold)]/30 text-[var(--accent-gold)]"
                      : "bg-[var(--bg-elevated)] border-[var(--border-subtle)] text-[var(--text-muted)] hover:border-[var(--text-faint)] hover:text-[var(--text-primary)]"
                }`}
              >
                {/* Status Indicator Icon */}
                {sending ? (
                  <svg className="h-3 w-3 animate-spin" fill="none" viewBox="0 0 24 24">
                    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                    <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                  </svg>
                ) : failed ? (
                  <span>⚠️</span>
                ) : (
                  <span className="h-1.5 w-1.5 rounded-full bg-[var(--text-faint)]" />
                )}

                <span className="max-w-[120px] truncate font-medium">
                  {item.text}
                </span>

                {/* Quick Delete 'x' Button */}
                {!sending && (
                  <span
                    role="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      queue.remove(item.id);
                      if (activeItemId === item.id) {
                        setActiveItemId(null);
                        setEditingItemId(null);
                      }
                    }}
                    className="ml-0.5 text-[10px] text-[var(--text-faint)] hover:text-[var(--text-primary)] p-0.5 rounded-full hover:bg-[var(--bg-hover)]"
                  >
                    ✕
                  </span>
                )}
              </motion.button>
            );
          })}
        </AnimatePresence>
      </div>
    </div>
  );
}

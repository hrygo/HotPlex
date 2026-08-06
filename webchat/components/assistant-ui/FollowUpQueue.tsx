"use client";

import { useEffect, useRef, useState } from "react";
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
  const prevFailedIdsRef = useRef<ReadonlySet<string>>(new Set());

  // Auto-open the popover only when an item *transitions* into the failed
  // state while nothing else is open, mirroring the pre-redesign panel
  // contract where a failed item's status and retry affordance were always
  // visible. Triggering on the transition (previous failed-set vs current
  // failed-set) keeps ✕ close end-state-reaching with any number of failed
  // items, and a new failure episode on the same item (failed → retried →
  // failed again) surfaces again.
  useEffect(() => {
    const currentFailedIds = new Set(
      queue.items
        .filter((item) => item.status === "failed")
        .map((item) => item.id),
    );
    const newlyFailedId = [...currentFailedIds].find(
      (id) => !prevFailedIdsRef.current.has(id),
    );
    prevFailedIdsRef.current = currentFailedIds;
    if (newlyFailedId === undefined || activeItemId !== null) return;
    setActiveItemId(newlyFailedId);
    setEditingItemId(null);
  }, [queue.items, activeItemId]);

  if (queue.items.length === 0) return null;

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
    <div
      role="region"
      aria-label={t("follow_up.aria.panel")}
      className="relative w-full pointer-events-auto"
    >
      {/* Horizontal Pills Row */}
      <div
        role="list"
        className="flex flex-wrap items-center justify-end gap-1.5 py-1 w-full"
      >
        <AnimatePresence initial={false} mode="popLayout">
          {queue.items.map((item, index) => {
            const position = index + 1;
            const sending = item.status === "sending";
            const failed = item.status === "failed";
            const expanded = activeItemId === item.id;
            return (
              <motion.div
                key={item.id}
                layout
                initial={{ opacity: 0, scale: 0.9, x: -10 }}
                animate={{ opacity: 1, scale: 1, x: 0 }}
                exit={{ opacity: 0, scale: 0.9, x: 10 }}
                role="listitem"
                aria-busy={sending}
                className="flex items-center gap-1.5"
              >
                {/* Expand / Collapse Pill */}
                <button
                  type="button"
                  onClick={() => {
                    setActiveItemId((current) =>
                      current === item.id ? null : item.id,
                    );
                    setEditingItemId(null);
                  }}
                  aria-expanded={expanded}
                  aria-label={t(
                    expanded
                      ? "follow_up.aria.collapse"
                      : "follow_up.aria.expand",
                    { position },
                  )}
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
                    <svg
                      className="h-3 w-3 animate-spin"
                      fill="none"
                      viewBox="0 0 24 24"
                    >
                      <circle
                        className="opacity-25"
                        cx="12"
                        cy="12"
                        r="10"
                        stroke="currentColor"
                        strokeWidth="4"
                      />
                      <path
                        className="opacity-75"
                        fill="currentColor"
                        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"
                      />
                    </svg>
                  ) : failed ? (
                    <span>⚠️</span>
                  ) : (
                    <span className="h-1.5 w-1.5 rounded-full bg-[var(--text-faint)]" />
                  )}

                  <span className="max-w-[120px] truncate font-medium">
                    {item.text}
                  </span>
                </button>

                {/* Quick Delete 'x' Button — sibling of the expand button */}
                {!sending && !expanded && (
                  <button
                    type="button"
                    onClick={() => {
                      queue.remove(item.id);
                      if (activeItemId === item.id) {
                        setActiveItemId(null);
                        setEditingItemId(null);
                      }
                    }}
                    aria-label={t("follow_up.aria.delete", { position })}
                    className="ml-0.5 text-[10px] text-[var(--text-faint)] hover:text-[var(--text-primary)] p-0.5 rounded-full hover:bg-[var(--bg-hover)]"
                  >
                    ✕
                  </button>
                )}

                {/* Detail Popover Card — inside the active listitem */}
                <AnimatePresence>
                  {expanded && (
                    <motion.div
                      initial={{ opacity: 0, y: 10, scale: 0.95 }}
                      animate={{ opacity: 1, y: 0, scale: 1 }}
                      exit={{ opacity: 0, y: 10, scale: 0.95 }}
                      className="absolute bottom-full right-0 z-30 mb-2.5 max-w-sm w-96 rounded-2xl border border-[var(--border-subtle)] bg-[var(--bg-glass)] backdrop-blur-xl p-4 shadow-xl flex flex-col gap-3 pointer-events-auto"
                    >
                      <div className="flex items-center justify-between">
                        <span className="font-mono text-[9px] font-bold uppercase tracking-[0.14em] text-[var(--text-faint)]">
                          {t(`follow_up.status.${item.status}`)}
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

                      {editingItemId === item.id ? (
                        <div className="space-y-2">
                          <textarea
                            value={draftText}
                            onChange={(e) => setDraftText(e.target.value)}
                            rows={3}
                            aria-label={t("follow_up.aria.edit_input", {
                              position,
                            })}
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
                              onClick={() => handleSaveEdit(item)}
                              disabled={!draftText.trim()}
                              className="rounded-md bg-[var(--accent-gold)] px-2.5 py-1 text-[10px] font-bold text-black disabled:opacity-40"
                            >
                              {t("follow_up.action.save")}
                            </button>
                          </div>
                        </div>
                      ) : (
                        <p className="whitespace-pre-wrap break-words text-xs leading-relaxed text-[var(--text-primary)] max-h-48 overflow-y-auto">
                          {item.text}
                        </p>
                      )}

                      {item.status === "failed" && item.errorKind && (
                        <p
                          role="alert"
                          className="flex items-center gap-1.5 text-[10px] leading-relaxed text-[var(--accent-coral)]"
                        >
                          <span>⚠️</span>
                          {t(errorKey[item.errorKind])}
                        </p>
                      )}

                      {editingItemId !== item.id && (
                        <div className="flex items-center gap-2 pt-2 border-t border-[var(--border-subtle)]">
                          <button
                            type="button"
                            onClick={() => handleEditClick(item)}
                            disabled={sending}
                            aria-label={t("follow_up.aria.edit", { position })}
                            className="rounded border border-[var(--border-subtle)] px-2.5 py-1 text-[10px] text-[var(--text-muted)] hover:border-[var(--text-faint)] hover:text-[var(--text-primary)] disabled:opacity-40"
                          >
                            {t("follow_up.action.edit")}
                          </button>
                          <button
                            type="button"
                            onClick={() => {
                              queue.remove(item.id);
                              setActiveItemId(null);
                              setEditingItemId(null);
                            }}
                            disabled={sending}
                            aria-label={t("follow_up.aria.delete", {
                              position,
                            })}
                            className="rounded border border-[var(--border-subtle)] px-2.5 py-1 text-[10px] text-[var(--text-muted)] hover:border-[var(--accent-coral)]/40 hover:text-[var(--accent-coral)] disabled:opacity-40"
                          >
                            {t("follow_up.action.delete")}
                          </button>
                          {failed && (
                            <button
                              type="button"
                              onClick={() => {
                                queue.retry(item.id);
                                setActiveItemId(null);
                                setEditingItemId(null);
                              }}
                              aria-label={t("follow_up.aria.retry", {
                                position,
                              })}
                              className="rounded border border-[var(--accent-gold)]/35 bg-[var(--accent-gold)]/10 px-2.5 py-1 text-[10px] font-semibold text-[var(--accent-gold)] hover:bg-[var(--accent-gold)]/15"
                            >
                              {t("follow_up.action.retry")}
                            </button>
                          )}
                          <button
                            type="button"
                            onClick={() => {
                              // Keep the popover open so the failure state
                              // (if any) stays visible.
                              void queue.sendNow(item.id);
                            }}
                            disabled={sending || isStopping}
                            aria-label={t("follow_up.aria.send_now", {
                              position,
                            })}
                            className="ml-auto rounded bg-[var(--accent-coral)]/12 px-2.5 py-1 text-[10px] font-semibold text-[var(--accent-coral)] hover:bg-[var(--accent-coral)]/20 disabled:opacity-40"
                          >
                            {t("follow_up.action.send_now")}
                          </button>
                        </div>
                      )}
                    </motion.div>
                  )}
                </AnimatePresence>
              </motion.div>
            );
          })}
        </AnimatePresence>
      </div>
    </div>
  );
}

"use client";

import { useCallback, useState } from "react";
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

function QueueItem({
  item,
  index,
  queue,
  isStopping,
}: {
  item: FollowUpQueueItem;
  index: number;
  queue: FollowUpQueueControls;
  isStopping?: boolean;
}) {
  const { t } = useTranslation("chat");
  const [expanded, setExpanded] = useState(false);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(item.text);
  const sending = item.status === "sending";

  const cancelEdit = useCallback(() => {
    setDraft(item.text);
    setEditing(false);
  }, [item.text]);

  const saveEdit = useCallback(() => {
    if (!draft.trim() || !queue.updateText(item.id, draft)) return;
    setEditing(false);
  }, [draft, item.id, queue]);

  return (
    <motion.li
      layout
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, x: 16 }}
      className={`rounded-xl border bg-[var(--bg-elevated)]/95 px-3 py-2.5 shadow-sm ${
        item.status === "failed"
          ? "border-[var(--accent-coral)]/55"
          : sending
            ? "border-[var(--accent-gold)]/55"
            : "border-[var(--border-subtle)]"
      }`}
      aria-busy={sending}
    >
      <div className="flex items-start gap-2.5">
        <span
          className="mt-0.5 flex h-6 min-w-6 items-center justify-center rounded-full border border-[var(--accent-gold)]/30 bg-[var(--accent-gold)]/10 px-1 font-mono text-[10px] font-bold text-[var(--accent-gold)]"
          aria-label={t("follow_up.aria.position", { position: index + 1 })}
        >
          {String(index + 1).padStart(2, "0")}
        </span>

        <div className="min-w-0 flex-1">
          <div className="mb-1 flex items-center justify-between gap-2">
            <span className="font-mono text-[9px] font-bold uppercase tracking-[0.14em] text-[var(--text-faint)]">
              {t(`follow_up.status.${item.status}`)}
            </span>
            {!editing && (
              <button
                type="button"
                onClick={() => setExpanded((value) => !value)}
                className="rounded px-1.5 py-0.5 text-[10px] text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] focus:outline-none focus:ring-1 focus:ring-[var(--accent-gold)]/50"
                aria-expanded={expanded}
                aria-label={t(
                  expanded
                    ? "follow_up.aria.collapse"
                    : "follow_up.aria.expand",
                  { position: index + 1 },
                )}
              >
                {t(expanded ? "follow_up.action.collapse" : "follow_up.action.expand")}
              </button>
            )}
          </div>

          {editing ? (
            <div className="space-y-2">
              <textarea
                value={draft}
                onChange={(event) => setDraft(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Escape") cancelEdit();
                }}
                rows={3}
                autoFocus
                className="w-full resize-y rounded-lg border border-[var(--accent-gold)]/40 bg-[var(--bg-base)] px-3 py-2 text-xs leading-relaxed text-[var(--text-primary)] outline-none focus:ring-2 focus:ring-[var(--accent-gold)]/25"
                aria-label={t("follow_up.aria.edit_input", { position: index + 1 })}
              />
              <div className="flex justify-end gap-2">
                <button
                  type="button"
                  onClick={cancelEdit}
                  className="rounded-md px-2.5 py-1 text-[10px] font-medium text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] focus:outline-none focus:ring-1 focus:ring-[var(--accent-gold)]/50"
                >
                  {t("follow_up.action.cancel")}
                </button>
                <button
                  type="button"
                  onClick={saveEdit}
                  disabled={!draft.trim()}
                  className="rounded-md bg-[var(--accent-gold)] px-2.5 py-1 text-[10px] font-bold text-black transition-opacity disabled:cursor-not-allowed disabled:opacity-40 focus:outline-none focus:ring-2 focus:ring-[var(--accent-gold)]/40"
                >
                  {t("follow_up.action.save")}
                </button>
              </div>
            </div>
          ) : (
            <p
              className={`whitespace-pre-wrap break-words text-xs leading-relaxed text-[var(--text-primary)] ${
                expanded ? "" : "line-clamp-2"
              }`}
            >
              {item.text}
            </p>
          )}

          {item.status === "failed" && item.errorKind && (
            <p
              role="alert"
              className="mt-2 flex items-center gap-1.5 text-[10px] leading-relaxed text-[var(--accent-coral)]"
            >
              <span aria-hidden="true">!</span>
              {t(errorKey[item.errorKind])}
            </p>
          )}

          {!editing && (
            <div className="mt-2 flex flex-wrap items-center gap-1.5">
              <button
                type="button"
                onClick={() => {
                  setDraft(item.text);
                  setEditing(true);
                }}
                disabled={sending}
                className="rounded-md border border-[var(--border-subtle)] px-2 py-1 text-[10px] text-[var(--text-muted)] hover:border-[var(--text-faint)] hover:text-[var(--text-primary)] disabled:cursor-not-allowed disabled:opacity-40 focus:outline-none focus:ring-1 focus:ring-[var(--accent-gold)]/50"
                aria-label={t("follow_up.aria.edit", { position: index + 1 })}
              >
                {t("follow_up.action.edit")}
              </button>
              <button
                type="button"
                onClick={() => queue.remove(item.id)}
                disabled={sending}
                className="rounded-md border border-[var(--border-subtle)] px-2 py-1 text-[10px] text-[var(--text-muted)] hover:border-[var(--accent-coral)]/40 hover:text-[var(--accent-coral)] disabled:cursor-not-allowed disabled:opacity-40 focus:outline-none focus:ring-1 focus:ring-[var(--accent-coral)]/50"
                aria-label={t("follow_up.aria.delete", { position: index + 1 })}
              >
                {t("follow_up.action.delete")}
              </button>
              {item.status === "failed" && (
                <button
                  type="button"
                  onClick={() => queue.retry(item.id)}
                  className="rounded-md border border-[var(--accent-gold)]/35 bg-[var(--accent-gold)]/10 px-2 py-1 text-[10px] font-semibold text-[var(--accent-gold)] hover:bg-[var(--accent-gold)]/15 focus:outline-none focus:ring-1 focus:ring-[var(--accent-gold)]/50"
                  aria-label={t("follow_up.aria.retry", { position: index + 1 })}
                >
                  {t("follow_up.action.retry")}
                </button>
              )}
              <button
                type="button"
                onClick={() => void queue.sendNow(item.id)}
                disabled={sending || isStopping}
                className="ml-auto rounded-md bg-[var(--accent-coral)]/12 px-2 py-1 text-[10px] font-semibold text-[var(--accent-coral)] hover:bg-[var(--accent-coral)]/20 disabled:cursor-not-allowed disabled:opacity-40 focus:outline-none focus:ring-1 focus:ring-[var(--accent-coral)]/50"
                aria-label={t("follow_up.aria.send_now", { position: index + 1 })}
              >
                {t("follow_up.action.send_now")}
              </button>
            </div>
          )}
        </div>
      </div>
    </motion.li>
  );
}

export function FollowUpQueue({ queue, isStopping }: FollowUpQueueProps) {
  const { t } = useTranslation("chat");
  if (queue.items.length === 0) return null;

  return (
    <section
      className="pointer-events-auto mx-auto mb-3 w-full max-w-3xl overflow-hidden rounded-2xl border border-[var(--accent-gold)]/20 bg-[var(--bg-glass)] shadow-[var(--shadow-sm)] backdrop-blur-xl"
      aria-label={t("follow_up.aria.panel")}
    >
      <header className="flex items-center justify-between border-b border-[var(--border-subtle)] px-3.5 py-2">
        <div className="flex items-center gap-2">
          <span className="h-1.5 w-1.5 rounded-full bg-[var(--accent-gold)] shadow-[0_0_10px_var(--accent-gold)]" />
          <h2 className="font-mono text-[10px] font-bold uppercase tracking-[0.16em] text-[var(--text-muted)]">
            {t("follow_up.title")}
          </h2>
        </div>
        <span
          className="rounded-full border border-[var(--border-subtle)] bg-[var(--bg-elevated)] px-2 py-0.5 font-mono text-[9px] text-[var(--text-faint)]"
          aria-live="polite"
        >
          {t("follow_up.count", { count: queue.items.length })}
        </span>
      </header>
      <ol className="max-h-64 space-y-2 overflow-y-auto p-2.5">
        <AnimatePresence initial={false} mode="popLayout">
          {queue.items.map((item, index) => (
            <QueueItem
              key={item.id}
              item={item}
              index={index}
              queue={queue}
              isStopping={isStopping}
            />
          ))}
        </AnimatePresence>
      </ol>
    </section>
  );
}

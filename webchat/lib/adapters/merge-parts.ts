/**
 * Pure helpers for merging streaming content into message parts.
 *
 * Extracted from hotplex-runtime-adapter.ts so the merge logic is unit-testable
 * without React/jsdom. Every function here is pure: (input parts, content) →
 * new parts array, no side effects.
 */

import type {
    MessagePart,
    ReasoningPart,
    TextPart,
} from "@/lib/types/message-parts";

/**
 * Append delta text to the last text part of `parts`, or push a new text part
 * if the last part isn't text (or parts is empty). Returns a new array — never
 * mutates the input.
 *
 * This is the core of streaming text accumulation: consecutive message.delta
 * events are stitched into a single text run.
 */
export function appendTextDelta(
    parts: MessagePart[],
    content: string,
): MessagePart[] {
    if (!content) return parts;
    const next = [...parts];
    const last = next[next.length - 1];
    if (last && last.type === "text") {
        next[next.length - 1] = {
            type: "text",
            text: (last as TextPart).text + content,
        };
    } else {
        next.push({ type: "text", text: content });
    }
    return next;
}

/**
 * Append reasoning text to the last reasoning part, or push a new reasoning
 * part. Mirrors appendTextDelta for thinking/reasoning streams.
 */
export function appendReasoningDelta(
    parts: MessagePart[],
    content: string,
): MessagePart[] {
    if (!content) return parts;
    const next = [...parts];
    const last = next[next.length - 1];
    if (last && last.type === "reasoning") {
        next[next.length - 1] = {
            type: "reasoning",
            text: (last as ReasoningPart).text + content,
        };
    } else {
        next.push({ type: "reasoning", text: content });
    }
    return next;
}

/**
 * Concatenate the text of all text parts in order. Used to compare streamed
 * length against the authoritative event-store content when reconciling
 * dropped deltas.
 */
export function concatTextParts(parts: MessagePart[]): string {
    return parts
        .filter((p): p is TextPart => p.type === "text")
        .map((p) => p.text)
        .join("");
}

/**
 * Minimal shape of a stored event — only the fields this function reads.
 * Avoids importing the full StoredEvent type (which carries auth/network
 * concerns) into this pure module.
 */
interface StoredEventLike {
    seq: number;
    type: string;
    data: { content?: string };
}

/**
 * Collect the `message` events belonging to the latest turn from a page of
 * stored events. The event store returns events in **ASC seq order** (DESC
 * queries are reversed before return — see eventstore query_shared.go), so we
 * walk newest→oldest (backwards) to find the most recent `done`, then gather
 * the `message` events that precede it within the same turn, stopping at the
 * previous turn's `done`. Returns the events in ASC seq order so the caller
 * can concatenate `.data.content` directly.
 *
 * Returns an empty array if there is no `done` or no preceding `message` in
 * the latest turn (e.g. the page only contains partial history).
 */
export function collectLastTurnMessages(
    events: StoredEventLike[],
    maxMessages = 20,
): StoredEventLike[] {
    let seenDone = false;
    const collected: StoredEventLike[] = [];
    for (let i = events.length - 1; i >= 0; i--) {
        const ev = events[i];
        if (!seenDone) {
            if (ev.type === "done") seenDone = true;
            continue;
        }
        // Hit the previous turn's done → stop, don't cross turns.
        if (ev.type === "done") break;
        if (ev.type === "message") {
            collected.push(ev);
            if (collected.length >= maxMessages) break;
        }
    }
    if (!seenDone || collected.length === 0) return [];
    // collected is in DESC seq order (we walked backwards); sort to ASC so the
    // caller concatenates content in the original emit order.
    return collected.sort((a, b) => a.seq - b.seq);
}

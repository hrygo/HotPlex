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
    ToolCallPart,
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
 * Upsert a tool-call part by `toolCallId`. If a part with the same id exists,
 * merge `updates` into it (shallow); otherwise push a new part. Used by both
 * handleToolCall (new/updated call) and handleToolResult (result patch).
 *
 * De-dupes repeated tool-call events for the same id (#331 region) and applies
 * result patches without losing the original args/toolName.
 */
export function upsertToolCallPart(
    parts: MessagePart[],
    updates: Partial<ToolCallPart> & Pick<ToolCallPart, "toolCallId">,
): MessagePart[] {
    const next = [...parts];
    const idx = next.findIndex(
        (p): p is ToolCallPart =>
            p.type === "tool-call" &&
            (p as ToolCallPart).toolCallId === updates.toolCallId,
    );
    if (idx !== -1) {
        const existing = next[idx] as ToolCallPart;
        next[idx] = { ...existing, ...updates };
    } else {
        next.push({
            type: "tool-call",
            toolName: updates.toolName ?? "",
            args: updates.args ?? {},
            ...updates,
        } as ToolCallPart);
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

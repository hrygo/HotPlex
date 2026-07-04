import { describe, it, expect } from "vitest";
import {
    appendTextDelta,
    appendReasoningDelta,
    concatTextParts,
    collectLastTurnMessages,
} from "@/lib/adapters/merge-parts";
import type { MessagePart } from "@/lib/types/message-parts";

describe("appendTextDelta", () => {
    it("appends to the last text part when it is text", () => {
        const parts: MessagePart[] = [{ type: "text", text: "hello" }];
        const result = appendTextDelta(parts, " world");
        expect(result).toEqual([{ type: "text", text: "hello world" }]);
    });

    it("concatenates multiple deltas into a single text run", () => {
        let parts: MessagePart[] = [{ type: "text", text: "a" }];
        parts = appendTextDelta(parts, "b");
        parts = appendTextDelta(parts, "c");
        expect(parts).toEqual([{ type: "text", text: "abc" }]);
    });

    it("pushes a new text part when the last part is not text", () => {
        const parts: MessagePart[] = [
            { type: "reasoning", text: "thinking" },
        ];
        const result = appendTextDelta(parts, "answer");
        expect(result).toEqual([
            { type: "reasoning", text: "thinking" },
            { type: "text", text: "answer" },
        ]);
    });

    it("pushes a new text part when parts is empty", () => {
        const result = appendTextDelta([], "first");
        expect(result).toEqual([{ type: "text", text: "first" }]);
    });

    it("returns parts unchanged when content is empty", () => {
        const parts: MessagePart[] = [{ type: "text", text: "x" }];
        expect(appendTextDelta(parts, "")).toBe(parts);
    });

    it("does not mutate the input array", () => {
        const parts: MessagePart[] = [{ type: "text", text: "x" }];
        const result = appendTextDelta(parts, "y");
        // input untouched
        expect(parts).toEqual([{ type: "text", text: "x" }]);
        // output is a new reference
        expect(result).not.toBe(parts);
    });
});

describe("appendReasoningDelta", () => {
    it("appends to the last reasoning part", () => {
        const parts: MessagePart[] = [{ type: "reasoning", text: "hmm" }];
        const result = appendReasoningDelta(parts, " more");
        expect(result).toEqual([{ type: "reasoning", text: "hmm more" }]);
    });

    it("pushes a new reasoning part when the last part is text", () => {
        const parts: MessagePart[] = [{ type: "text", text: "answer" }];
        const result = appendReasoningDelta(parts, "rethink");
        expect(result).toEqual([
            { type: "text", text: "answer" },
            { type: "reasoning", text: "rethink" },
        ]);
    });

    it("returns parts unchanged when content is empty", () => {
        const parts: MessagePart[] = [{ type: "reasoning", text: "x" }];
        expect(appendReasoningDelta(parts, "")).toBe(parts);
    });
});

describe("concatTextParts", () => {
    it("concatenates all text parts in order", () => {
        const parts: MessagePart[] = [
            { type: "text", text: "a" },
            { type: "reasoning", text: "ignore" },
            { type: "text", text: "b" },
        ];
        expect(concatTextParts(parts)).toBe("ab");
    });

    it("returns empty string when there are no text parts", () => {
        const parts: MessagePart[] = [{ type: "reasoning", text: "x" }];
        expect(concatTextParts(parts)).toBe("");
    });

    it("returns empty string for empty parts", () => {
        expect(concatTextParts([])).toBe("");
    });
});

describe("collectLastTurnMessages", () => {
    // Helper: build an event in the shape returned by the events API (ASC order).
    const mk = (seq: number, type: string, content?: string) => ({
        seq,
        type,
        data: content !== undefined ? { content } : {},
    });

    it("collects messages after the most recent done, in ASC seq order", () => {
        // The store returns ASC: [msg1, msg2, done] — newest is at the end.
        const events = [
            mk(1, "message", "Hello "),
            mk(2, "message", "World"),
            mk(3, "done"),
        ];
        const result = collectLastTurnMessages(events);
        expect(result.map((e) => e.seq)).toEqual([1, 2]);
        // Concatenating content reproduces the original text.
        expect(result.map((e) => e.data.content).join("")).toBe("Hello World");
    });

    it("does not cross turn boundaries (stops at the previous done)", () => {
        // Two turns, both ASC. Latest turn = seq 5,6,7. Previous turn = 1,2,3.
        const events = [
            mk(1, "message", "T1a"),
            mk(2, "message", "T1b"),
            mk(3, "done"),
            mk(5, "message", "T2a"),
            mk(6, "message", "T2b"),
            mk(7, "done"),
        ];
        const result = collectLastTurnMessages(events);
        expect(result.map((e) => e.seq)).toEqual([5, 6]);
        expect(result.map((e) => e.data.content).join("")).toBe("T2aT2b");
    });

    it("returns empty when there is no done event", () => {
        const events = [mk(1, "message", "a"), mk(2, "message", "b")];
        expect(collectLastTurnMessages(events)).toEqual([]);
    });

    it("returns empty when done has no preceding message in the page", () => {
        const events = [mk(1, "done")];
        expect(collectLastTurnMessages(events)).toEqual([]);
    });

    it("ignores non-message, non-done events between done and messages", () => {
        const events = [
            mk(1, "message", "A"),
            mk(2, "tool.call"),
            mk(3, "message", "B"),
            mk(4, "done"),
        ];
        const result = collectLastTurnMessages(events);
        expect(result.map((e) => e.seq)).toEqual([1, 3]);
    });

    it("regression: handles ASC order correctly (the original bug)", () => {
        // Before the fix, the loop iterated forward over ASC-ordered events,
        // passing all messages (seenDone=false) then hitting done → no messages
        // ever collected → reconcile was a no-op. Verify it now works.
        const events = [
            mk(10, "message", "first chunk"),
            mk(20, "message", " second chunk"),
            mk(30, "done"),
        ];
        const result = collectLastTurnMessages(events);
        expect(result).toHaveLength(2);
        expect(result.map((e) => e.data.content).join("")).toBe(
            "first chunk second chunk",
        );
    });

    it("respects maxMessages cap", () => {
        const events = [
            ...Array.from({ length: 10 }, (_, i) =>
                mk(i + 1, "message", `m${i} `),
            ),
            mk(11, "done"),
        ];
        const result = collectLastTurnMessages(events, 5);
        expect(result).toHaveLength(5);
        // Still in ASC order.
        expect(result[0].seq).toBeLessThan(result[4].seq);
    });
});

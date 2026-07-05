import { describe, it, expect } from "vitest";
import {
    appendTextDelta,
    appendReasoningDelta,
    concatTextParts,
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

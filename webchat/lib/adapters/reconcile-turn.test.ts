import { describe, expect, it } from "vitest";

import type { ConversationRecord } from "@/lib/api/sessions";
import type { HotPlexMessage } from "@/lib/types/message";
import {
    patchAuthoritativeAssistantContent,
    selectAuthoritativeAssistantContent,
} from "./reconcile-turn";

function record(
    seq: number,
    role: "user" | "assistant",
    content: string,
    turnNum: number,
): ConversationRecord {
    return {
        id: seq,
        session_id: "session-1",
        generation: 1,
        turn_num: turnNum,
        seq,
        role,
        content,
        platform: "webchat",
        user_id: "user-1",
        model: "test",
        success: true,
        source: "normal",
        tools: null,
        tool_call_count: 0,
        tokens_in: 0,
        tokens_input: 0,
        tokens_cache_write: 0,
        tokens_cache_read: 0,
        tokens_out: 0,
        duration_ms: 0,
        cost_usd: 0,
        created_at: seq,
    };
}

describe("turn reconciliation", () => {
    const records = [
        record(10, "user", "first", 1),
        record(11, "assistant", "first answer", 1),
        record(20, "user", "second", 2),
        record(21, "assistant", "second answer", 2),
    ];

    it("selects the assistant bounded by the completed terminal sequence", () => {
        expect(
            selectAuthoritativeAssistantContent(records, { terminalSeq: 15 }),
        ).toBe("first answer");
    });

    it("waits when the current turn user exists but its assistant has not flushed", () => {
        const partiallyFlushed = [
            record(10, "user", "first", 1),
            record(11, "assistant", "first answer", 1),
            record(20, "user", "second", 2),
        ];

        expect(
            selectAuthoritativeAssistantContent(partiallyFlushed, {
                terminalSeq: 25,
            }),
        ).toBeNull();
    });

    it("selects the assistant paired with the reconnecting input turn", () => {
        expect(
            selectAuthoritativeAssistantContent(records, {
                inputContent: "first",
            }),
        ).toBe("first answer");
    });

    it("patches the captured assistant without touching the next pending turn", () => {
        const messages: HotPlexMessage[] = [
            {
                id: "assistant-first",
                role: "assistant",
                parts: [{ type: "text", text: "first" }],
                createdAt: new Date(1),
                status: "complete",
            },
            {
                id: "assistant-second",
                role: "assistant",
                parts: [],
                createdAt: new Date(2),
                status: "streaming",
            },
        ];

        const patched = patchAuthoritativeAssistantContent(
            messages,
            "assistant-first",
            "first answer",
        );

        expect(patched[0]?.parts).toEqual([
            { type: "text", text: "first answer" },
        ]);
        expect(patched[1]).toBe(messages[1]);
        expect(patched[1]?.parts).toEqual([]);
    });

    it("appends a new assistant message if the target id is not found", () => {
        const messages: HotPlexMessage[] = [
            {
                id: "user-1",
                role: "user",
                parts: [{ type: "text", text: "hello" }],
                createdAt: new Date(1),
                status: "complete",
            },
        ];

        const patched = patchAuthoritativeAssistantContent(
            messages,
            "assistant-target",
            "hello back",
        );

        expect(patched).toHaveLength(2);
        expect(patched[0]).toBe(messages[0]);
        expect(patched[1]?.id).toBe("assistant-target");
        expect(patched[1]?.role).toBe("assistant");
        expect(patched[1]?.parts).toEqual([{ type: "text", text: "hello back" }]);
        expect(patched[1]?.status).toBe("complete");
    });
});

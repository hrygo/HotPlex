import { describe, expect, it } from "vitest";

import type { HotPlexMessage } from "@/lib/types/message";
import type { MessagePart } from "@/lib/types/message-parts";

import { expireInteractions } from "./terminal-interactions";

type InteractionKind = "permission" | "question" | "elicitation";
type InteractionStatus =
    | "pending"
    | "submitting"
    | "resolved"
    | "rejected"
    | "expired"
    | "failed";

const interactionPart = (
    id: string,
    kind: InteractionKind,
    status: InteractionStatus,
    extra: Record<string, unknown> = {},
): MessagePart => ({
    type: "tool-call",
    toolName: `${kind}_request`,
    toolCallId: id,
    args: {
        description: `Keep this text for ${id}`,
        payload: { id, kind },
        interaction: {
            kind,
            requestId: id,
            status,
            createdAt: 1,
            ...extra,
        },
    },
    status: { type: "running" },
});

const assistantMessage = (
    id: string,
    part: MessagePart,
): HotPlexMessage => ({
    id,
    role: "assistant",
    parts: [{ type: "text", text: "Keep this assistant text" }, part],
    createdAt: new Date(0),
    status: "streaming",
});

describe("expireInteractions", () => {
    it.each([
        ["permission", "pending"],
        ["question", "submitting"],
        ["elicitation", "failed"],
    ] as const)(
        "expires a %s interaction in %s state and completes its tool call",
        (kind, status) => {
            const messages = [
                assistantMessage(
                    `${kind}-message`,
                    interactionPart(`${kind}-request`, kind, status),
                ),
            ];

            const result = expireInteractions(
                messages,
                new Set([`${kind}-request`]),
            );
            const part = result[0].parts[1];

            expect(part).toMatchObject({
                type: "tool-call",
                toolCallId: `${kind}-request`,
                status: { type: "complete" },
                args: {
                    description: `Keep this text for ${kind}-request`,
                    payload: { id: `${kind}-request`, kind },
                    interaction: {
                        kind,
                        requestId: `${kind}-request`,
                        status: "expired",
                    },
                },
            });
        },
    );

    it("preserves resolved, rejected, and already expired interactions", () => {
        const messages = [
            assistantMessage(
                "resolved-message",
                interactionPart("resolved", "question", "resolved", {
                    response: { answer: "yes" },
                }),
            ),
            assistantMessage(
                "rejected-message",
                interactionPart("rejected", "permission", "rejected", {
                    error: "denied",
                }),
            ),
            assistantMessage(
                "expired-message",
                interactionPart("expired", "elicitation", "expired", {
                    error: "session ended",
                }),
            ),
        ];
        const before = structuredClone(messages);

        const result = expireInteractions(
            messages,
            new Set(["resolved", "rejected", "expired"]),
        );

        expect(result).toEqual(before);
        expect(result[0].parts[1]).toBe(messages[0].parts[1]);
        expect(result[0].parts[1]).toMatchObject({
            args: { interaction: { response: { answer: "yes" } } },
        });
    });

    it("expires only selected interactions without mutating messages or text parts", () => {
        const messages = [
            assistantMessage(
                "target-message",
                interactionPart("target", "question", "pending"),
            ),
            assistantMessage(
                "other-message",
                interactionPart("other", "permission", "pending"),
            ),
            {
                id: "user-message",
                role: "user" as const,
                parts: [{ type: "text" as const, text: "Keep user text" }],
                createdAt: new Date(0),
            },
        ];
        const before = structuredClone(messages);

        const result = expireInteractions(messages, new Set(["target"]));

        expect(messages).toEqual(before);
        expect(result).not.toBe(messages);
        expect(result[0]).not.toBe(messages[0]);
        expect(result[0].parts[0]).toEqual({
            type: "text",
            text: "Keep this assistant text",
        });
        expect(result[0].parts[1]).toMatchObject({
            toolCallId: "target",
            status: { type: "complete" },
            args: { interaction: { status: "expired" } },
        });
        expect(result[1]).toBe(messages[1]);
        expect(result[2]).toBe(messages[2]);
    });
});

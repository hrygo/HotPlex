import { describe, expect, it } from "vitest";

import {
    completeAssistantParts,
    convertToThreadMessage,
    isGatewayCommandAck,
    reconcileMessagesByClientMessageId,
} from "./hotplex-runtime-adapter";
import type { TurnSummaryPart } from "@/lib/types/message-parts";
import type { HotPlexMessage } from "@/lib/types/message";

describe("completeAssistantParts", () => {
    it("retains streamed content and appends the turn summary", () => {
        const stats: TurnSummaryPart["data"] = {
            turn_count: 1,
            tool_call_count: 0,
            duration: "1s",
            duration_seconds: 1,
            total_input_tok: 12,
            total_output_tok: 4,
            context_fill: 0,
            context_window: 100,
            context_pct: 0,
            total_cost_usd: 0,
            model_name: "test",
            turn_duration_ms: 1000,
            turn_input_tok: 12,
            turn_output_tok: 4,
            turn_cost_usd: 0,
            tool_names: null,
            work_dir: "/tmp",
            git_branch: "main",
        };

        expect(
            completeAssistantParts(
                [{ type: "text", text: "answer" }],
                stats,
            ),
        ).toEqual([
            { type: "text", text: "answer" },
            { type: "turn-summary", data: stats },
        ]);
    });
});

describe("convertToThreadMessage", () => {
    it("preserves local progress in assistant-ui custom metadata", () => {
        const message = convertToThreadMessage({
            id: "assistant-local-1",
            role: "assistant",
            parts: [],
            createdAt: new Date(0),
            status: "streaming",
            progress: "thinking",
        }) as { metadata?: { custom?: { progress?: string } } };

        expect(message.metadata?.custom?.progress).toBe("thinking");
    });

    it("keeps a skills result in custom metadata for the structured card", () => {
        const message = convertToThreadMessage({
            id: "assistant-skills-1",
            role: "assistant",
            parts: [
                {
                    type: "skill-list",
                    skills: [
                        {
                            name: "api-designer",
                            description: "Design APIs",
                            source: "global",
                            status: "discoverable",
                        },
                    ],
                },
            ],
            createdAt: new Date(0),
            status: "complete",
        }) as { metadata?: { custom?: { skillsList?: Array<{ name: string }> } } };

        expect(message.metadata?.custom?.skillsList).toEqual([
            expect.objectContaining({ name: "api-designer" }),
        ]);
    });
});

describe("isGatewayCommandAck", () => {
    it("recognizes a delivered ack with the cmd- execution id prefix", () => {
        expect(
            isGatewayCommandAck({
                execution_id: "cmd-01J8X2",
                status: "delivered",
            }),
        ).toBe(true);
    });

    it("does not treat a real worker turn ack as a command ack", () => {
        expect(
            isGatewayCommandAck({
                execution_id: "exec-01J8X2",
                status: "delivered",
            }),
        ).toBe(false);
    });

    it("only treats the delivered status as terminal for commands", () => {
        expect(
            isGatewayCommandAck({
                execution_id: "cmd-01J8X2",
                status: "accepted",
            }),
        ).toBe(false);
        expect(
            isGatewayCommandAck({
                execution_id: "cmd-01J8X2",
                status: "unknown",
            }),
        ).toBe(false);
    });
});

describe("reconcileMessagesByClientMessageId", () => {
    it("reconciles optimistic and durable user turns by client_message_id", () => {
        const optimistic = {
            id: "user-local-1",
            role: "user",
            clientMessageId: "cm_1",
            parts: [{ type: "text", text: "same content" }],
            createdAt: new Date(1),
            status: "complete",
            deliveryStatus: "unknown",
        } as HotPlexMessage;
        const durable = {
            id: "turn:42:user",
            role: "user",
            clientMessageId: "cm_1",
            parts: [{ type: "text", text: "same content" }],
            createdAt: new Date(2),
            status: "complete",
            deliveryStatus: "delivered",
        } as HotPlexMessage;

        const merged = reconcileMessagesByClientMessageId(
            [optimistic],
            [durable],
        );
        expect(merged.filter((message) => message.role === "user")).toHaveLength(1);
        expect(merged[0]?.id).toBe("turn:42:user");
    });
});

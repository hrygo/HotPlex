import { describe, expect, it } from "vitest";

import {
    convertToThreadMessage,
    isGatewayCommandAck,
    reconcileMessagesByClientMessageId,
} from "./hotplex-runtime-adapter";
import type { HotPlexMessage } from "@/lib/types/message";

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

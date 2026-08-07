import { describe, expect, it } from "vitest";

import {
    convertToThreadMessage,
    isGatewayCommandAck,
} from "./hotplex-runtime-adapter";

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

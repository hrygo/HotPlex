import { describe, expect, it } from "vitest";

import { convertToThreadMessage } from "./hotplex-runtime-adapter";

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

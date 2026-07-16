import { describe, expect, it } from "vitest";
import type { HotPlexMessage } from "@/lib/types/message";
import {
    completeStreamingAssistant,
    createPendingAssistantMessage,
    isVisibleAdapterMessage,
    matchesActiveInput,
    removePendingAssistant,
    updatePendingAssistant,
} from "./pending-assistant";

const pending = () => createPendingAssistantMessage("assistant-local-1", new Date(0));

describe("pending assistant state", () => {
    it("creates one local streaming placeholder in the thinking stage", () => {
        const message = pending();

        expect(message).toMatchObject({
            id: "assistant-local-1",
            role: "assistant",
            parts: [],
            status: "streaming",
            progress: "thinking",
        });
    });

    it("keeps an empty local placeholder visible to the thread adapter", () => {
        expect(isVisibleAdapterMessage(pending())).toBe(true);
    });

    it("adopts the placeholder without changing its ID for delta-first output", () => {
        const messages = updatePendingAssistant([pending()], "assistant-local-1", (message) => ({
            ...message,
            progress: undefined,
            parts: [{ type: "text", text: "hello" }],
        }));

        expect(messages).toHaveLength(1);
        expect(messages[0]).toMatchObject({
            id: "assistant-local-1",
            status: "streaming",
            progress: undefined,
            parts: [{ type: "text", text: "hello" }],
        });
    });

    it("transitions a delivered acknowledgement and ignores output-before-ack", () => {
        const accepted = updatePendingAssistant([pending()], "assistant-local-1", (message) => ({
            ...message,
            progress: "accepted",
        }));
        const output: HotPlexMessage[] = [{
            ...accepted[0],
            progress: undefined,
            parts: [{ type: "reasoning", text: "working" }],
        }];
        const lateAck = updatePendingAssistant(output, null, (message) => ({
            ...message,
            progress: "accepted",
        }));

        expect(accepted[0].progress).toBe("accepted");
        expect(lateAck[0].progress).toBeUndefined();
        expect(lateAck[0].parts).toEqual([{ type: "reasoning", text: "working" }]);
    });

    it("accepts only acknowledgements for the active client message", () => {
        expect(matchesActiveInput("evt-current", "evt-current")).toBe(true);
        expect(matchesActiveInput("evt-current", "evt-replayed")).toBe(false);
        expect(matchesActiveInput(null, "evt-current")).toBe(false);
    });

    it("clears the placeholder on reconnect or unknown delivery", () => {
        expect(removePendingAssistant([pending()], "assistant-local-1")).toEqual([]);
    });

    it("finalizes an adopted streaming message on a terminal transport outcome", () => {
        const adopted: HotPlexMessage = {
            ...pending(),
            progress: undefined,
            parts: [{ type: "text", text: "partial response" }],
        };

        expect(completeStreamingAssistant([adopted], adopted.id)).toMatchObject([
            { id: adopted.id, status: "complete", progress: undefined },
        ]);
    });
});

import { describe, expect, it } from "vitest";

import { shouldSkipAssistantMessageRender } from "@/components/assistant-ui/assistant-message-memo";

const message = (turnSummary?: Record<string, unknown>) => ({
  id: "assistant-1",
  content: [{ type: "text", text: "done" }],
  metadata: { custom: turnSummary ? { turnSummary } : {} },
  status: { type: "complete" },
});

describe("shouldSkipAssistantMessageRender", () => {
  it("re-renders when done adds a Turn Summary to an already complete message", () => {
    const summary = {
      turn_input_tok: 1200,
      turn_output_tok: 300,
    };

    expect(
      shouldSkipAssistantMessageRender(
        { message: message() },
        { message: message(summary) },
      ),
    ).toBe(false);
  });

  it("skips equal completed messages", () => {
    const summary = {
      turn_input_tok: 1200,
      turn_output_tok: 300,
    };
    const stable = message(summary);

    expect(
      shouldSkipAssistantMessageRender(
        { message: stable },
        { message: stable },
      ),
    ).toBe(true);
  });
});

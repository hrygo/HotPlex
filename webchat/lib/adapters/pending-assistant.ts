import type { HotPlexMessage } from "@/lib/types/message";

export function createPendingAssistantMessage(
    id: string,
    createdAt: Date,
): HotPlexMessage {
    return {
        id,
        role: "assistant",
        parts: [],
        createdAt,
        status: "streaming",
        progress: "thinking",
    };
}

// Empty parts identify a local pending placeholder. It must stay visible until
// the first Worker event replaces it with content.
export function isVisibleAdapterMessage(
    message: Pick<HotPlexMessage, "parts">,
): boolean {
    return message.parts.length === 0 ||
        !message.parts.every(
            (part) =>
                part.type === "context-usage" || part.type === "turn-summary",
        );
}

export function matchesActiveInput(
    activeClientMessageID: string | null,
    acknowledgedClientMessageID: string,
): boolean {
    return activeClientMessageID !== null &&
        activeClientMessageID === acknowledgedClientMessageID;
}

export function updatePendingAssistant(
    messages: HotPlexMessage[],
    id: string | null,
    update: (message: HotPlexMessage) => HotPlexMessage,
): HotPlexMessage[] {
    if (!id) return messages;
    const index = messages.findIndex(
        (message) =>
            message.id === id &&
            message.role === "assistant" &&
            message.status === "streaming",
    );
    if (index === -1) return messages;
    const next = [...messages];
    next[index] = update(next[index]);
    return next;
}

export function removePendingAssistant(
    messages: HotPlexMessage[],
    id: string | null,
): HotPlexMessage[] {
    if (!id) return messages;
    return messages.filter(
        (message) =>
            !(
                message.id === id &&
                message.role === "assistant" &&
                message.status === "streaming"
            ),
    );
}

export function completeStreamingAssistant(
    messages: HotPlexMessage[],
    id: string | null,
): HotPlexMessage[] {
    if (!id) return messages;
    const index = messages.findIndex(
        (message) =>
            message.id === id &&
            message.role === "assistant" &&
            message.status === "streaming",
    );
    if (index === -1) return messages;
    const next = [...messages];
    next[index] = {
        ...next[index],
        progress: undefined,
        status: "complete",
    };
    return next;
}

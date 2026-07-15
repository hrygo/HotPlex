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

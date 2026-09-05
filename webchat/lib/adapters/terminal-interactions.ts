import type { HotPlexMessage } from "@/lib/types/message";

type InteractionCard = {
    status: string;
    [key: string]: unknown;
};

function isExpirableStatus(status: unknown): boolean {
    return status === "pending" || status === "submitting" || status === "failed";
}

function getInteractionCard(args: unknown): InteractionCard | undefined {
    if (typeof args !== "object" || args === null) return undefined;
    const interaction = (args as { interaction?: unknown }).interaction;
    if (typeof interaction !== "object" || interaction === null) return undefined;
    const status = (interaction as { status?: unknown }).status;
    return typeof status === "string"
        ? (interaction as InteractionCard)
        : undefined;
}

/**
 * Close selected interaction cards when a turn/session reaches a terminal
 * outcome. The caller supplies the IDs that belong to that outcome so cards
 * from another live request remain actionable.
 */
export function expireInteractions(
    messages: readonly HotPlexMessage[],
    interactionIDs: ReadonlySet<string>,
): HotPlexMessage[] {
    let changed = false;
    const next = messages.map((message) => {
        if (message.role !== "assistant") return message;

        let messageChanged = false;
        const parts = message.parts.map((part) => {
            if (
                part.type !== "tool-call" ||
                !interactionIDs.has(part.toolCallId)
            ) {
                return part;
            }

            const interaction = getInteractionCard(part.args);
            if (!interaction || !isExpirableStatus(interaction.status)) {
                return part;
            }

            messageChanged = true;
            return {
                ...part,
                args: {
                    ...part.args,
                    interaction: {
                        ...interaction,
                        status: "expired" as const,
                    },
                },
                status: { type: "complete" as const },
            };
        });

        if (!messageChanged) return message;
        changed = true;
        return { ...message, parts };
    });

    // Always provide a fresh message-list container. Individual untouched
    // messages and parts retain their identity for efficient rendering.
    return changed ? next : [...messages];
}

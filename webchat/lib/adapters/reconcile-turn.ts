import type { ConversationRecord } from "@/lib/api/sessions";
import { concatTextParts } from "@/lib/adapters/merge-parts";
import type { HotPlexMessage } from "@/lib/types/message";
import type { MessagePart } from "@/lib/types/message-parts";

export type TurnReconcileCriteria =
    | { terminalSeq: number; inputContent?: never }
    | { terminalSeq?: never; inputContent: string };

export function selectAuthoritativeAssistantContent(
    records: readonly ConversationRecord[],
    criteria: TurnReconcileCriteria,
): string | null {
    let targetRecord: ConversationRecord | undefined;
    if (criteria.terminalSeq !== undefined) {
        const userRecord = records
            .filter(
                (record) =>
                    record.role === "user" &&
                    record.seq <= criteria.terminalSeq,
            )
            .sort((left, right) => right.seq - left.seq)[0];
        if (userRecord) {
            targetRecord = records
                .filter(
                    (record) =>
                        record.role === "assistant" &&
                        record.content &&
                        record.generation === userRecord.generation &&
                        record.turn_num === userRecord.turn_num &&
                        record.seq <= criteria.terminalSeq,
                )
                .sort((left, right) => right.seq - left.seq)[0];
        }
    } else {
        const userRecord = records
            .filter(
                (record) =>
                    record.role === "user" &&
                    record.content === criteria.inputContent,
            )
            .sort((left, right) => right.seq - left.seq)[0];
        if (userRecord) {
            targetRecord = records
                .filter(
                    (record) =>
                        record.role === "assistant" &&
                        record.content &&
                        record.generation === userRecord.generation &&
                        record.turn_num === userRecord.turn_num,
                )
                .sort((left, right) => right.seq - left.seq)[0];
        }
    }
    return targetRecord?.content ?? null;
}

export function patchAuthoritativeAssistantContent(
    messages: readonly HotPlexMessage[],
    targetAssistantId: string,
    fullText: string,
): HotPlexMessage[] {
    const targetIndex = messages.findIndex(
        (message) =>
            message.id === targetAssistantId && message.role === "assistant",
    );
    if (targetIndex === -1) {
        return [
            ...messages,
            {
                id: targetAssistantId,
                role: "assistant",
                parts: [{ type: "text", text: fullText }],
                createdAt: new Date(),
                status: "complete",
            },
        ];
    }

    const target = messages[targetIndex];
    const currentText = concatTextParts(target.parts);
    if (currentText.length > 0 && !fullText.startsWith(currentText)) {
        return messages as HotPlexMessage[];
    }
    if (fullText.length <= currentText.length) {
        return messages as HotPlexMessage[];
    }

    const firstTextIndex = target.parts.findIndex((part) => part.type === "text");
    const nonTextParts: MessagePart[] = target.parts.filter(
        (part) => part.type !== "text",
    );
    const parts = [...nonTextParts];
    parts.splice(firstTextIndex === -1 ? 0 : firstTextIndex, 0, {
        type: "text",
        text: fullText,
    });
    const next = [...messages];
    next[targetIndex] = { ...target, parts };
    return next;
}

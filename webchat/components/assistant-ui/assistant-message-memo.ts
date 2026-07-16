import { getExt } from "./thread-helpers";

type AssistantMessageProps = {
  message: unknown;
  onInteractionRespond?: unknown;
};

// shouldSkipAssistantMessageRender is the memo comparator for AssistantMessage.
// Summary and context cards are carried in metadata rather than content, so
// they must participate in equality checks after a message reaches complete.
export function shouldSkipAssistantMessageRender(
  prev: AssistantMessageProps,
  next: AssistantMessageProps,
): boolean {
  if ((prev.message as { id?: unknown })?.id !== (next.message as { id?: unknown })?.id) return false;

  const prevExt = getExt(prev.message);
  const nextExt = getExt(next.message);
  const prevRunning = prevExt.status?.type === "running";
  const nextRunning = nextExt.status?.type === "running";
  if (prevRunning || nextRunning) return false;
  if (prev.onInteractionRespond !== next.onInteractionRespond) return false;

  const prevCustom = prevExt.metadata?.custom;
  const nextCustom = nextExt.metadata?.custom;
  if (
    prevCustom?.contextUsage !== nextCustom?.contextUsage ||
    prevCustom?.turnSummary !== nextCustom?.turnSummary ||
    prevCustom?.progress !== nextCustom?.progress
  ) {
    return false;
  }

  const prevContent = prevExt.content || [];
  const nextContent = nextExt.content || [];
  if (prevContent.length !== nextContent.length) return false;
  for (let i = 0; i < prevContent.length; i += 1) {
    const a = prevContent[i] as Record<string, unknown>;
    const b = nextContent[i] as Record<string, unknown>;
    if (a?.type !== b?.type) return false;
    if (a?.type === "text" || a?.type === "reasoning") {
      if (a.text !== b.text) return false;
    } else if (a?.type === "tool-call") {
      if (a.toolName !== b.toolName) return false;
      if (a.args !== b.args) return false;
      if (a.toolCallId !== b.toolCallId) return false;
      if (a.result !== b.result) return false;
      if (a.status !== b.status) return false;
    }
  }
  return true;
}

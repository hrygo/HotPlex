import type { ContextUsageData, TurnSessionStats } from "@/lib/ai-sdk-transport/client/types";

// assistant-ui ThreadMessage doesn't expose status/metadata in public types.
// Centralize the extension access here to avoid scattered as-any casts.
export interface ThreadMessageExtension {
  status?: { type: string };
  metadata?: {
    contextUsage?: ContextUsageData;
    turnSummary?: TurnSessionStats;
  };
  content: unknown[];
}

export function getExt(msg: unknown): ThreadMessageExtension {
  return msg as ThreadMessageExtension;
}

export const messageVariants = {
  hidden: { opacity: 0, y: 10 },
  visible: { opacity: 1, y: 0, transition: { type: "spring" as const, stiffness: 300, damping: 24 } },
};

 
export function extractCommand(args: any) { return args?.command || args?.Command || ""; }
 
export function extractFilePath(args: any) { return args?.file_path || args?.path || args?.target_file; }
 
export function extractFileContent(args: any, result: any) { return args?.content || args?.code || (typeof result === "string" ? result : ""); }

export interface Suggestion {
  title: string;
  label: string;
  prompt: string;
}

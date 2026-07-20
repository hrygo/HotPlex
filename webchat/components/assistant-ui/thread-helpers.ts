import type { ContextUsageData, TurnSessionStats } from "@/lib/ai-sdk-transport/client/types";

// assistant-ui ThreadMessage doesn't expose status/metadata in public types.
// Centralize the extension access here to avoid scattered as-any casts.
export interface ThreadMessageExtension {
  status?: { type: string };
  metadata?: {
    custom?: {
      contextUsage?: ContextUsageData;
      turnSummary?: TurnSessionStats;
      progress?: 'thinking' | 'accepted';
    };
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
 
export function extractFilePath(args: any) { 
  return args?.file_path || args?.filePath || args?.filepath || args?.path || args?.target_file || args?.TargetFile || args?.targetFile || args?.AbsolutePath || args?.absolutePath || args?.file || args?.filename || args?.fileName || ""; 
}
 
export function extractFileContent(args: any, result: any) { 
  return args?.content || args?.code || args?.ReplacementContent || args?.CodeContent || args?.replacementContent || args?.codeContent || args?.replacement || args?.text || (typeof result === "string" ? result : ""); 
}

export interface Suggestion {
  title: string;
  label: string;
  prompt: string;
}

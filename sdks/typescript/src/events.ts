export type EventType =
  | 'thinking'
  | 'answer'
  | 'tool_use'
  | 'tool_result'
  | 'session_stats'
  | 'error'
  | 'danger_block';

export interface EventMeta {
  tool_name?: string;
  tool_id?: string;
  status?: string;
  duration_ms?: number;
  total_duration_ms?: number;
  input_summary?: string;
  output_summary?: string;
}

export interface Event {
  type: EventType;
  data?: unknown;
  meta?: EventMeta;
  timestamp: Date;
}

export interface SessionStats {
  session_id: string;
  start_time: number;
  end_time: number;
  total_duration_ms: number;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  tool_call_count: number;
  tools_used: string[];
  files_modified: number;
  file_paths: string[];
  model_used: string;
  total_cost_usd: number;
  is_error: boolean;
  error_message?: string;
}

export function parseEvent(data: Record<string, unknown>): Event {
  const eventType = (data.type as EventType) || 'answer';
  const eventData = data.data;
  
  const metaData = (data.meta as Record<string, unknown>) || {};
  const meta: EventMeta = {
    tool_name: metaData.tool_name as string | undefined,
    tool_id: metaData.tool_id as string | undefined,
    status: metaData.status as string | undefined,
    duration_ms: metaData.duration_ms as number | undefined,
    total_duration_ms: metaData.total_duration_ms as number | undefined,
    input_summary: metaData.input_summary as string | undefined,
    output_summary: metaData.output_summary as string | undefined,
  };

  return {
    type: eventType,
    data: eventData,
    meta,
    timestamp: new Date(),
  };
}

export function parseSessionStats(data: Record<string, unknown>): SessionStats {
  return {
    session_id: (data.session_id as string) || '',
    start_time: (data.start_time as number) || 0,
    end_time: (data.end_time as number) || 0,
    total_duration_ms: (data.total_duration_ms as number) || 0,
    input_tokens: (data.input_tokens as number) || 0,
    output_tokens: (data.output_tokens as number) || 0,
    total_tokens: (data.total_tokens as number) || 0,
    tool_call_count: (data.tool_call_count as number) || 0,
    tools_used: (data.tools_used as string[]) || [],
    files_modified: (data.files_modified as number) || 0,
    file_paths: (data.file_paths as string[]) || [],
    model_used: (data.model_used as string) || '',
    total_cost_usd: (data.total_cost_usd as number) || 0,
    is_error: (data.is_error as boolean) || false,
    error_message: data.error_message as string | undefined,
  };
}

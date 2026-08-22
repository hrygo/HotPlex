/**
 * Unified HotPlexMessage type — single source of truth.
 *
 * Generic over parts to allow both narrow (history) and wide (live) usage:
 * - History: HotPlexMessage<TextPart | ToolSummaryPart>
 * - Live:    HotPlexMessage<MessagePart> (default)
 */

import type { MessagePart } from './message-parts';

export type MessageDeliveryStatus = 'pending' | 'delivered' | 'unknown' | 'failed';

export interface HotPlexMessage<P extends MessagePart = MessagePart> {
  id: string;
  role: 'user' | 'assistant' | 'system';
  parts: P[];
  createdAt: Date;
  status?: 'streaming' | 'complete' | 'error';
  /** Stable identity assigned at client ingress and echoed by durable history. */
  clientMessageId?: string;
  /** Delivery outcome for optimistic user turns; unknown is intentionally retained. */
  deliveryStatus?: MessageDeliveryStatus;
  /**
   * Local-only staged feedback shown before the worker emits visible output.
   * It is deliberately not persisted or sent over AEP.
   */
  progress?: 'thinking' | 'accepted';
}

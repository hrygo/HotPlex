/**
 * AI SDK v4 ChatTransport adapter for HotPlex Worker Gateway.
 *
 * This module provides utilities for converting AEP v1 events
 * to AI SDK's data stream format.
 *
 * Architecture:
 * 1. Browser connects to Next.js API route via HTTP
 * 2. API route connects to HotPlex gateway via WebSocket
 * 3. API route streams AEP events back as AI SDK data stream format
 */

export { createAepStream, createDataStreamWriter } from './stream-controller.js';
export { mapAepToDataStream, mapErrorToDataStream } from './chunk-mapper.js';

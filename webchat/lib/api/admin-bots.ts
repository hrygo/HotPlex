/**
 * Admin Bot API client.
 *
 * CRUD operations for bot configurations and agent config files,
 * backed by the admin fetch wrapper (Bearer token auth).
 */

import { adminFetch } from './admin-client';
import type { BotConfigEntry, AgentConfigFile } from '@/lib/types/admin';

// ---------------------------------------------------------------------------
// Bot CRUD
// ---------------------------------------------------------------------------

export function listBots(): Promise<BotConfigEntry[]> {
  return adminFetch<BotConfigEntry[]>('/admin/bots/config');
}

export function getBot(name: string): Promise<BotConfigEntry> {
  return adminFetch<BotConfigEntry>(`/admin/bots/${encodeURIComponent(name)}/config`);
}

export function createBot(attrs: Partial<BotConfigEntry>): Promise<BotConfigEntry> {
  return adminFetch<BotConfigEntry>('/admin/bots', {
    method: 'POST',
    body: JSON.stringify(attrs),
  });
}

export function updateBot(name: string, updates: Record<string, unknown>): Promise<void> {
  return adminFetch<void>(`/admin/bots/${encodeURIComponent(name)}`, {
    method: 'PATCH',
    body: JSON.stringify(updates),
  });
}

export function deleteBot(name: string): Promise<void> {
  return adminFetch<void>(`/admin/bots/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  });
}

// ---------------------------------------------------------------------------
// Agent Config Files
// ---------------------------------------------------------------------------

export function getAgentFile(name: string, file: string): Promise<AgentConfigFile> {
  return adminFetch<AgentConfigFile>(
    `/admin/bots/${encodeURIComponent(name)}/config/${encodeURIComponent(file)}`,
  );
}

export function writeAgentFile(
  name: string,
  file: string,
  content: string,
): Promise<AgentConfigFile> {
  return adminFetch<AgentConfigFile>(
    `/admin/bots/${encodeURIComponent(name)}/config/${encodeURIComponent(file)}`,
    {
      method: 'PUT',
      body: JSON.stringify({ content }),
    },
  );
}

// ---------------------------------------------------------------------------
// System Prompt Preview
// ---------------------------------------------------------------------------

export function previewSystemPrompt(name: string): Promise<{ preview: string }> {
  return adminFetch<{ preview: string }>(`/admin/bots/${encodeURIComponent(name)}/preview`);
}

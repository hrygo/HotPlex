'use client';

import { useState, useCallback, useRef, useEffect } from 'react';
import type { SkillEntry } from '@/lib/ai-sdk-transport/client/types';

const STORAGE_KEY_PREFIX = 'hotplex:skills';
const CACHE_TTL_MS = 24 * 60 * 60 * 1000; // 24 hours

interface CachedSkills {
  ts: number;
  skills: SkillEntry[];
}

function storageKey(sessionId: string) {
  return `${STORAGE_KEY_PREFIX}:${sessionId}`;
}

function loadFromStorage(sessionId: string): SkillEntry[] {
  try {
    const raw = localStorage.getItem(storageKey(sessionId));
    if (!raw) return [];
    const cached: CachedSkills = JSON.parse(raw);
    if (Date.now() - cached.ts > CACHE_TTL_MS) {
      localStorage.removeItem(storageKey(sessionId));
      return [];
    }
    return cached.skills ?? [];
  } catch {
    return [];
  }
}

function saveToStorage(sessionId: string, skills: SkillEntry[]) {
  try {
    const cached: CachedSkills = { ts: Date.now(), skills };
    localStorage.setItem(storageKey(sessionId), JSON.stringify(cached));
  } catch {
    // localStorage full or unavailable — in-memory only
  }
}

export function useSkillsCache(sessionId: string | null) {
  const [skills, setSkills] = useState<SkillEntry[]>(() =>
    sessionId ? loadFromStorage(sessionId) : [],
  );
  const currentIdRef = useRef(sessionId);
  currentIdRef.current = sessionId;

  // Reload from storage when session changes
  useEffect(() => {
    setSkills(sessionId ? loadFromStorage(sessionId) : []);
  }, [sessionId]);

  const mergeSkills = useCallback((incoming: SkillEntry[]) => {
    setSkills(prev => {
      const existing = new Map(prev.map(s => [s.name, s]));
      for (const skill of incoming) {
        const cur = existing.get(skill.name);
        // Don't let stub entries (empty description) overwrite richer data
        if (!cur || cur.description === '' || skill.description !== '') {
          existing.set(skill.name, skill);
        }
      }
      const merged = Array.from(existing.values());
      if (currentIdRef.current) saveToStorage(currentIdRef.current, merged);
      return merged;
    });
  }, []);

  const clearSkills = useCallback(() => {
    setSkills([]);
    if (currentIdRef.current) {
      try { localStorage.removeItem(storageKey(currentIdRef.current)); } catch {}
    }
  }, []);

  return { skills, mergeSkills, clearSkills };
}

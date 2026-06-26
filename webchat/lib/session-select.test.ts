// Tests for pickDefaultSession — the session-selection policy when switching
// workspace (or reloading). Keen-eyed: these assert the FIX for the reported
// "切换 workspace 未能直接进入默认 session" bug, where the old logic restored a
// stale localStorage savedId instead of the most-recently-active session.
//
// Run: node --test --experimental-strip-types lib/session-select.test.ts

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { pickDefaultSession } from './session-select.ts';

// Minimal session shape — pickDefaultSession only reads id + updated_at.
const mk = (id: string, updated_at: string) => ({ id, updated_at });

test('empty session list returns null (caller auto-creates anchor)', () => {
  assert.equal(pickDefaultSession([]), null);
});

test('single session is returned as-is', () => {
  const only = mk('s1', '2026-06-01T00:00:00Z');
  assert.equal(pickDefaultSession([only])?.id, 's1');
});

test('picks the most-recently-active session by updated_at regardless of list order', () => {
  // 列表顺序:旧在前、新在后 —— 旧逻辑会取 list[0],修复后必须取最新者。
  const older = mk('older', '2026-01-01T00:00:00Z');
  const newer = mk('newer', '2026-06-01T00:00:00Z');
  assert.equal(pickDefaultSession([older, newer])?.id, 'newer');
  // 反过来排也一样
  assert.equal(pickDefaultSession([newer, older])?.id, 'newer');
});

test('regression: does NOT prefer a stale "savedId" — selection is purely most-recent', () => {
  // 模拟根因 2:切回 workspace,savedId 曾指向旧 session,但存在更新会话。
  // pickDefaultSession 的签名故意不接受 savedId —— 选择必须只看 updated_at。
  const savedButOld = mk('saved-old', '2026-01-01T00:00:00Z');
  const fresh = mk('fresh', '2026-06-01T00:00:00Z');
  assert.equal(pickDefaultSession([savedButOld, fresh])?.id, 'fresh');
});

test('explicit initialSessionId wins when it exists in the list', () => {
  const a = mk('a', '2026-06-01T00:00:00Z'); // 更新
  const b = mk('b', '2026-01-01T00:00:00Z'); // 更旧
  // 即使 b 不是最新,显式指定且命中时优先返回
  assert.equal(pickDefaultSession([a, b], 'b')?.id, 'b');
});

test('initialSessionId not present in list falls back to most-recent', () => {
  const a = mk('a', '2026-06-01T00:00:00Z');
  const b = mk('b', '2026-01-01T00:00:00Z');
  assert.equal(pickDefaultSession([a, b], 'gone')?.id, 'a');
});

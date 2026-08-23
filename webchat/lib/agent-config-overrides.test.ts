import { describe, expect, it } from 'vitest';

import { prepareAgentConfigOverrides } from './agent-config-overrides';

describe('AgentConfig canonical overrides', () => {
  it('keeps only the five canonical keys', () => {
    expect(prepareAgentConfigOverrides({
      'SOUL.md': 'persona',
      'AGENTS.md': 'rules',
      'TOOLS.md': 'canonical',
      'USER.md': 'user context',
      'MEMORY.md': 'memory',
      'SKILLS.md': 'legacy',
      'README.md': 'ignored',
    })).toEqual({
      'SOUL.md': 'persona',
      'AGENTS.md': 'rules',
      'TOOLS.md': 'canonical',
      'USER.md': 'user context',
      'MEMORY.md': 'memory',
    });
  });

  it('preserves an empty canonical value as an explicit clear', () => {
    expect(prepareAgentConfigOverrides({ 'TOOLS.md': '', 'README.md': 'ignored' })).toEqual({
      'TOOLS.md': '',
    });
  });
});

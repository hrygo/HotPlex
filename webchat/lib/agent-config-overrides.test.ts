import { describe, expect, it } from 'vitest';

import {
  normalizeLegacyToolsOverride,
  prepareAgentConfigOverrides,
} from './agent-config-overrides';

describe('AgentConfig override migration', () => {
  it('normalizes a legacy-only tools override to TOOLS.md', () => {
    const result = normalizeLegacyToolsOverride({
      'SKILLS.md': 'legacy tool guidance',
      'SOUL.md': 'persona',
    });

    expect(result.conflict).toBe(false);
    expect(result.overrides).toEqual({
      'TOOLS.md': 'legacy tool guidance',
      'SOUL.md': 'persona',
    });
  });

  it('preserves both basenames so the server can report a conflict', () => {
    const input = {
      'TOOLS.md': 'canonical',
      'SKILLS.md': 'legacy',
    };
    const result = normalizeLegacyToolsOverride(input);

    expect(result.conflict).toBe(true);
    expect(result.overrides).toEqual(input);
  });

  it('preserves an empty canonical value as an explicit clear', () => {
    expect(prepareAgentConfigOverrides({ 'TOOLS.md': '', 'README.md': 'ignored' })).toEqual({
      'TOOLS.md': '',
    });
  });
});

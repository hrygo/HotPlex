export type AgentConfigOverrides = Record<string, string>;

const CANONICAL_FILES = ['SOUL.md', 'AGENTS.md', 'TOOLS.md', 'USER.md', 'MEMORY.md'] as const;
const LEGACY_TOOLS_FILE = 'SKILLS.md';

function hasOwn(map: AgentConfigOverrides, key: string): boolean {
  return Object.prototype.hasOwnProperty.call(map, key);
}

export function normalizeLegacyToolsOverride(input: AgentConfigOverrides): {
  overrides: AgentConfigOverrides;
  conflict: boolean;
} {
  const overrides = { ...input };
  const hasTools = hasOwn(overrides, 'TOOLS.md');
  const hasLegacy = hasOwn(overrides, LEGACY_TOOLS_FILE);

  if (hasTools && hasLegacy) {
    return { overrides, conflict: true };
  }
  if (hasLegacy) {
    overrides['TOOLS.md'] = overrides[LEGACY_TOOLS_FILE];
    delete overrides[LEGACY_TOOLS_FILE];
  }
  return { overrides, conflict: false };
}

// Keep present-empty values: they explicitly clear an inherited slot. Unknown
// keys are excluded, while the legacy key is retained only so a pre-existing
// TOOLS.md/SKILLS.md collision reaches server-side conflict validation.
export function prepareAgentConfigOverrides(input: AgentConfigOverrides): AgentConfigOverrides {
  const out: AgentConfigOverrides = {};
  for (const file of CANONICAL_FILES) {
    if (hasOwn(input, file)) out[file] = input[file];
  }
  if (hasOwn(input, LEGACY_TOOLS_FILE)) {
    out[LEGACY_TOOLS_FILE] = input[LEGACY_TOOLS_FILE];
  }
  return out;
}

export function agentConfigOverridesEqual(a: AgentConfigOverrides, b: AgentConfigOverrides): boolean {
  const left = prepareAgentConfigOverrides(a);
  const right = prepareAgentConfigOverrides(b);
  const leftKeys = Object.keys(left);
  const rightKeys = Object.keys(right);
  return leftKeys.length === rightKeys.length && leftKeys.every((key) => left[key] === right[key]);
}

export function hasAgentConfigOverride(overrides: AgentConfigOverrides, file: string): boolean {
  return hasOwn(overrides, file);
}

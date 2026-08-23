export type AgentConfigOverrides = Record<string, string>;

const CANONICAL_FILES = ['SOUL.md', 'AGENTS.md', 'TOOLS.md', 'USER.md', 'MEMORY.md'] as const;

function hasOwn(map: AgentConfigOverrides, key: string): boolean {
  return Object.prototype.hasOwnProperty.call(map, key);
}

// Keep present-empty values: they explicitly clear an inherited slot. Unknown
// keys are excluded.
export function prepareAgentConfigOverrides(input: AgentConfigOverrides): AgentConfigOverrides {
  const out: AgentConfigOverrides = {};
  for (const file of CANONICAL_FILES) {
    if (hasOwn(input, file)) out[file] = input[file];
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

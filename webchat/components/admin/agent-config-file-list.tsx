'use client';

export interface ConfigFileDef {
  key: string;
  file: string;
  label: string;
  description: string;
}

// Keys are the 5-file whitelist enforced by agentconfig.ValidateOverrides.
// The DB override map is keyed by filename (SOUL.md / AGENTS.md / ...),
// so `def.file` is the map key and `def.key` is the UI-internal short id.
export const CONFIG_FILES: ConfigFileDef[] = [
  { key: 'soul', file: 'SOUL.md', label: 'Soul', description: 'Persona & identity' },
  { key: 'agents', file: 'AGENTS.md', label: 'Agents', description: 'Behavior rules' },
  { key: 'skills', file: 'SKILLS.md', label: 'Skills', description: 'Capabilities' },
  { key: 'user', file: 'USER.md', label: 'User', description: 'User preferences' },
  { key: 'memory', file: 'MEMORY.md', label: 'Memory', description: 'Persistent context' },
];

export function AgentConfigFileList({
  activeKey,
  overrides,
  onSelect,
}: {
  activeKey: string;
  overrides: Record<string, string>;
  onSelect: (key: string) => void;
}) {
  return (
    <div className="w-48 flex-shrink-0 space-y-1">
      {CONFIG_FILES.map((def) => {
        const isActive = activeKey === def.key;
        const hasOverride = !!(overrides[def.file] && overrides[def.file].trim());
        return (
          <button
            key={def.key}
            onClick={() => onSelect(def.key)}
            className={`w-full text-left px-3 py-2.5 rounded-xl transition-all text-sm border ${
              isActive
                ? 'bg-[var(--bg-active)] border-[var(--border-active)] text-[var(--accent-gold)]'
                : 'hover:bg-[var(--bg-hover)] text-[var(--text-secondary)] border-transparent'
            }`}
          >
            <div className="flex items-center justify-between">
              <span className="font-semibold">{def.label}</span>
              <span
                className={`w-1.5 h-1.5 rounded-full ${hasOverride ? 'bg-[var(--accent-gold)]' : 'bg-[var(--border-subtle)]'}`}
                title={hasOverride ? 'Overridden' : 'Inherits team default'}
              />
            </div>
            <span className="text-[10px] text-[var(--text-faint)] block mt-0.5">{def.description}</span>
          </button>
        );
      })}
    </div>
  );
}

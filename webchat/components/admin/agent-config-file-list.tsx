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
    <div className="w-full flex flex-wrap gap-1.5 border-b border-[var(--border-subtle)] pb-3">
      {CONFIG_FILES.map((def) => {
        const isActive = activeKey === def.key;
        const hasOverride = !!(overrides[def.file] && overrides[def.file].trim());
        return (
          <button
            key={def.key}
            onClick={() => onSelect(def.key)}
            title={`${def.label}: ${def.description}`}
            className={`flex items-center gap-2 px-3.5 py-2 rounded-lg transition-all text-xs font-bold border cursor-pointer select-none ${
              isActive
                ? 'bg-[var(--bg-active)] border-[rgba(251,191,36,0.15)] text-[var(--accent-gold)]'
                : 'bg-transparent border-transparent hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text-primary)]'
            }`}
          >
            <span>{def.file}</span>
            <span
              className={`w-1.5 h-1.5 rounded-full ${hasOverride ? 'bg-[var(--accent-gold)]' : 'bg-[var(--border-subtle)]'}`}
              title={hasOverride ? 'Overridden' : 'Inherits default'}
            />
          </button>
        );
      })}
    </div>
  );
}

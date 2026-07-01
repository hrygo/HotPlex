import { useTranslation } from 'react-i18next';

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
  const { t } = useTranslation();

  const getFileLabel = (key: string) => {
    switch (key) {
      case 'soul': return t('admin:bots.config_files.soul.label', { defaultValue: 'Soul' });
      case 'agents': return t('admin:bots.config_files.agents.label', { defaultValue: 'Agents' });
      case 'skills': return t('admin:bots.config_files.skills.label', { defaultValue: 'Skills' });
      case 'user': return t('admin:bots.config_files.user.label', { defaultValue: 'User' });
      case 'memory': return t('admin:bots.config_files.memory.label', { defaultValue: 'Memory' });
      default: return key;
    }
  };

  const getFileDescription = (key: string) => {
    switch (key) {
      case 'soul': return t('admin:bots.config_files.soul.description', { defaultValue: 'Persona & identity' });
      case 'agents': return t('admin:bots.config_files.agents.description', { defaultValue: 'Behavior rules' });
      case 'skills': return t('admin:bots.config_files.skills.description', { defaultValue: 'Capabilities' });
      case 'user': return t('admin:bots.config_files.user.description', { defaultValue: 'User preferences' });
      case 'memory': return t('admin:bots.config_files.memory.description', { defaultValue: 'Persistent context' });
      default: return '';
    }
  };

  return (
    <div className="w-full flex flex-wrap gap-1.5 border-b border-[var(--border-subtle)] pb-3">
      {CONFIG_FILES.map((def) => {
        const isActive = activeKey === def.key;
        const hasOverride = !!(overrides[def.file] && overrides[def.file].trim());
        const fileLabel = getFileLabel(def.key);
        const fileDesc = getFileDescription(def.key);
        return (
          <button
            key={def.key}
            onClick={() => onSelect(def.key)}
            title={`${fileLabel}: ${fileDesc}`}
            className={`flex items-center gap-2 px-3.5 py-2 rounded-lg transition-all text-xs font-bold border cursor-pointer select-none ${
              isActive
                ? 'bg-[var(--bg-active)] border-[rgba(251,191,36,0.15)] text-[var(--accent-gold)]'
                : 'bg-transparent border-transparent hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text-primary)]'
            }`}
          >
            <span>{def.file}</span>
            <span
              className={`w-1.5 h-1.5 rounded-full ${hasOverride ? 'bg-[var(--accent-gold)]' : 'bg-[var(--border-subtle)]'}`}
              title={hasOverride ? t('admin:bots.editor.overridden', { defaultValue: 'Overridden' }) : t('admin:bots.editor.inherits_default', { defaultValue: 'Inherits default' })}
            />
          </button>
        );
      })}
    </div>
  );
}

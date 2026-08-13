'use client';

import { useState } from 'react';
import { createWorkspace, type Workspace } from '@/lib/api/workspaces';
import { createSession, ANCHOR_CLIENT_SESSION_ID } from '@/lib/api/sessions';
import { workerType } from '@/lib/config';
import {
  buildWorkspaceWorkDir,
  workspaceSandboxPrefix,
} from '@/lib/utils/workspace-path';
import { useTranslation } from 'react-i18next';

interface NewWorkspaceFormProps {
  // 服务端 workspace 沙箱根（List 响应 workspace_root，绝对路径）。缺失（旧后端）
  // 时表单禁用提交并提示刷新 —— 不发送错误路径（spec Root-HotplexHome §5.2.1）。
  workspaceRoot: string;
  // permission_mode 是 admin-only 字段：admin 显示下拉，非 admin 不渲染也不发送。
  isAdmin: boolean;
  onCreated: (ws: Workspace) => void;
  onCancel?: () => void;
}

const inputClass =
  'w-full rounded-[var(--radius-sm)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] px-3 py-2 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-faint)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors font-mono';

const labelClass =
  'block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5';

const PERMISSION_MODE_OPTIONS: { value: string }[] = [
  { value: 'workspace' },
  { value: 'auto-edit' },
  { value: 'bypass' },
  { value: 'read-only' },
];

export function NewWorkspaceForm({ workspaceRoot, isAdmin, onCreated, onCancel }: NewWorkspaceFormProps) {
  const { t } = useTranslation(['chat', 'common']);
  const [name, setName] = useState('');
  const [subdir, setSubdir] = useState('');
  const [permMode, setPermMode] = useState('workspace');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const rootKnown = workspaceRoot.trim().length > 0;
  const preview = !rootKnown
    ? ''
    : name.trim()
      ? buildWorkspaceWorkDir(workspaceRoot, name, subdir)
      : `${workspaceSandboxPrefix(workspaceRoot)}…`;

  async function submit() {
    if (!name.trim() || !rootKnown || submitting) return;
    setSubmitting(true);
    setError(null);
    try {
      const ws = await createWorkspace(
        name.trim(),
        buildWorkspaceWorkDir(workspaceRoot, name, subdir),
        isAdmin ? permMode : undefined,
      );
      // Pre-create the anchor session so the new workspace is immediately
      // usable on switch (listSessions returns it, no empty-state window).
      // Best-effort + idempotent: useSessions retries on switch, and
      // DeriveSessionKey maps (clientSessionId, ws, workDir) to one session.
      try {
        await createSession({
          clientSessionId: ANCHOR_CLIENT_SESSION_ID,
          workerType,
          title: ANCHOR_CLIENT_SESSION_ID,
          workspaceId: ws.id,
        });
      } catch {
        // Swallow — useSessions will (re)create the anchor session on switch.
      }
      onCreated(ws);
      setName('');
      setSubdir('');
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
      className="space-y-3"
    >
      <div>
        <label className={labelClass}>{t('chat:label.workspace_name')}</label>
        <input
          className={inputClass}
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t('chat:placeholder.workspace_name')}
          disabled={submitting || !rootKnown}
          autoFocus
        />
      </div>
      <div>
        <label className={labelClass}>{t('chat:label.directory_optional')}</label>
        <input
          className={inputClass}
          value={subdir}
          onChange={(e) => setSubdir(e.target.value)}
          placeholder={t('chat:placeholder.directory_optional')}
          disabled={submitting || !rootKnown}
        />
      </div>
      {isAdmin && (
        <div>
          <label className={labelClass}>{t('chat:settings.label.permission_mode')}</label>
          <select
            className={inputClass}
            value={permMode}
            onChange={(e) => setPermMode(e.target.value)}
            disabled={submitting || !rootKnown}
          >
            {PERMISSION_MODE_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {t(('chat:settings.permission.' + opt.value) as any)}
              </option>
            ))}
          </select>
        </div>
      )}
      <div className="text-[10px] font-mono text-[var(--text-faint)] break-all">
        {t('chat:text.path')}: {preview || t('chat:error.workspace_root_missing')}
      </div>
      {error && <div className="text-xs text-[var(--accent-coral)]">{error}</div>}
      <div className="flex items-center gap-2">
        <button
          type="submit"
          className="px-3 py-1.5 rounded-[var(--radius-sm)] bg-[var(--accent-gold)] text-black text-sm font-bold transition-opacity hover:opacity-90 disabled:opacity-50"
          disabled={!name.trim() || !rootKnown || submitting}
        >
          {submitting ? t('common:action.creating') : t('chat:action.create_workspace')}
        </button>
        {onCancel && (
          <button
            type="button"
            onClick={onCancel}
            className="px-3 py-1.5 rounded-[var(--radius-sm)] text-sm text-[var(--text-muted)] hover:text-[var(--text-primary)] disabled:opacity-50"
            disabled={submitting}
          >
            {t('common:action.cancel')}
          </button>
        )}
      </div>
    </form>
  );
}

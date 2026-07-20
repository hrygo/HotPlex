'use client';

import { useState, useEffect, useRef, useCallback } from 'react';
import type { Workspace } from '@/lib/api/workspaces';
import { listSkills, installWorkspaceSkill, deleteWorkspaceSkill, type Skill } from '@/lib/api/skills';
import { TabPanel } from './tab-panel';
import { useTranslation } from 'react-i18next';

// Badge color helper — managed (writable) vs external (read-only) provenance.
function Badge({ kind, label }: { kind: 'managed' | 'external'; label: string }) {
  if (kind === 'managed') {
    return (
      <span className="rounded-full bg-[rgba(16,185,129,0.12)] px-2 py-0.5 text-[10px] font-medium text-[rgb(16,185,129)]">
        {label}
      </span>
    );
  }
  return (
    <span className="rounded-full bg-[var(--bg-hover)] px-2 py-0.5 text-[10px] font-medium text-[var(--text-muted)]">
      {label}
    </span>
  );
}

interface SkillsTabProps {
  workspace: Workspace;
}

export function SkillsTab({ workspace }: SkillsTabProps) {
  const { t } = useTranslation(['chat', 'common']);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Status message for inline notifications (success, error, warning)
  const [statusMsg, setStatusMsg] = useState<{ type: 'success' | 'error' | 'warning'; text: string } | null>(null);
  const statusTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Modals state
  const [showUpload, setShowUpload] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);

  // Action states
  const [uploading, setUploading] = useState(false);
  const [actionLoading, setActionLoading] = useState<string | null>(null);

  // Upload dialog form states
  const [file, setFile] = useState<File | null>(null);
  const [replace, setReplace] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const abortRef = useRef<AbortController | null>(null);
  const isMounted = useRef(false);

  const showStatus = (type: 'success' | 'error' | 'warning', text: string) => {
    if (!isMounted.current) return;
    setStatusMsg({ type, text });
    if (statusTimer.current) clearTimeout(statusTimer.current);
    statusTimer.current = setTimeout(() => {
      if (isMounted.current) setStatusMsg(null);
    }, 5000);
  };

  const load = useCallback(async () => {
    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;

    if (isMounted.current) {
      setLoading(true);
      setError(null);
    }

    try {
      const res = await listSkills(ctrl.signal);
      if (ctrl.signal.aborted || !isMounted.current) return;
      setSkills(res.skills || []);
    } catch (err) {
      if (ctrl.signal.aborted || !isMounted.current) return;
      setError(err instanceof Error ? err.message : 'Load failed');
    } finally {
      if (!ctrl.signal.aborted && isMounted.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    isMounted.current = true;
    load();
    return () => {
      isMounted.current = false;
      abortRef.current?.abort();
      if (statusTimer.current) clearTimeout(statusTimer.current);
    };
  }, [load]);

  const onPickFile = (f: File | null) => {
    if (f && !/\.zip$/i.test(f.name)) {
      showStatus('error', t('settings.skills.error.not_zip', { defaultValue: 'Only .zip files are accepted' }));
      return;
    }
    setFile(f);
  };

  const handleUpload = async () => {
    if (!file) {
      showStatus('error', t('settings.skills.error.no_file', { defaultValue: 'Please select a zip file' }));
      return;
    }
    try {
      setUploading(true);
      const res = await installWorkspaceSkill(workspace.id, file, replace);
      await load();
      if (!isMounted.current) return;
      setShowUpload(false);
      setFile(null);
      setReplace(false);
      if (fileInputRef.current) fileInputRef.current.value = '';

      if (res.warning) {
        showStatus(
          'warning',
          t('settings.skills.toast.installed_warning', { warning: res.warning, defaultValue: `Installed — ${res.warning}` })
        );
      } else {
        showStatus('success', t('settings.skills.toast.installed', { defaultValue: 'Skill installed successfully' }));
      }
    } catch (err) {
      if (!isMounted.current) return;
      const status = (err as any).status;
      if (status === 409) {
        showStatus(
          'error',
          t('settings.skills.error.already_exists', { defaultValue: 'Skill already exists — enable "Replace existing" to overwrite' })
        );
      } else {
        showStatus(
          'error',
          err instanceof Error ? err.message : t('settings.skills.error.install_failed', { defaultValue: 'Install failed' })
        );
      }
    } finally {
      if (isMounted.current) setUploading(false);
    }
  };

  const handleDelete = async (name: string) => {
    try {
      setActionLoading(name);
      await deleteWorkspaceSkill(workspace.id, name);
      await load();
      if (isMounted.current) {
        showStatus('success', t('settings.skills.toast.deleted', { defaultValue: 'Skill deleted successfully' }));
      }
    } catch (err) {
      if (isMounted.current) {
        showStatus(
          'error',
          err instanceof Error ? err.message : t('settings.skills.error.delete_failed', { defaultValue: 'Delete failed' })
        );
      }
    } finally {
      if (isMounted.current) {
        setActionLoading(null);
        setDeleteTarget(null);
      }
    }
  };

  const closeUpload = () => {
    setShowUpload(false);
    setFile(null);
    setReplace(false);
    if (fileInputRef.current) fileInputRef.current.value = '';
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <div className="w-6 h-6 border-2 border-[var(--accent-gold)] stroke-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex gap-2 px-4 py-3 rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] text-xs text-[var(--accent-coral)] font-bold items-center justify-center">
        <svg className="w-4 h-4 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
        </svg>
        <span>{t('common:status.error')}: {error}</span>
      </div>
    );
  }

  return (
    <TabPanel>
      {/* Top Banner Status Notification */}
      {statusMsg && (
        <div className={`flex gap-2 px-4 py-3 rounded-[var(--radius-md)] border text-xs font-bold items-start animate-fade-in-up ${
          statusMsg.type === 'success'
            ? 'bg-[rgba(52,211,153,0.08)] border-[rgba(52,211,153,0.15)] text-[var(--accent-emerald)]'
            : statusMsg.type === 'warning'
            ? 'bg-[rgba(251,191,36,0.08)] border-[rgba(251,191,36,0.15)] text-[var(--accent-gold)]'
            : 'bg-[rgba(244,63,94,0.08)] border-[rgba(244,63,94,0.15)] text-[var(--accent-coral)]'
        }`}>
          {statusMsg.type === 'success' && (
            <svg className="w-4 h-4 shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
            </svg>
          )}
          {(statusMsg.type === 'warning' || statusMsg.type === 'error') && (
            <svg className="w-4 h-4 shrink-0 mt-0.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
            </svg>
          )}
          <span className="break-words">{statusMsg.text}</span>
        </div>
      )}

      {/* Header and Upload Action */}
      <div className="flex items-center justify-between mb-3">
        <h3 className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest">
          {t('settings.skills.title.list', { defaultValue: 'Installed Skills' })}
        </h3>
        <button
          type="button"
          onClick={() => setShowUpload(true)}
          className="rounded-lg bg-[var(--accent-gold)] px-3 py-1.5 text-xs font-medium text-[var(--bg-surface)] hover:opacity-90 cursor-pointer shadow-sm active:scale-[0.98] transition-all"
        >
          {t('settings.skills.action.upload', { defaultValue: 'Upload Skill' })}
        </button>
      </div>

      {/* Skill List */}
      <div className="space-y-2">
        {skills.map((s) => {
          // A skill is deletable if it is project-scoped (workspace) and managed.
          const isDeletable = s.source === 'project' && s.managed;

          return (
            <div
              key={`${s.source}/${s.name}`}
              className="flex items-center justify-between rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-3.5 hover:border-[var(--border-default)] transition-colors"
            >
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="truncate text-sm font-bold text-[var(--text-primary)]">{s.name}</span>
                  <Badge
                    kind={s.managed ? 'managed' : 'external'}
                    label={s.managed ? t('settings.skills.label.managed') : t('settings.skills.label.external')}
                  />
                  <span className="text-[9px] font-mono font-bold uppercase px-1.5 py-0.5 rounded bg-[var(--bg-elevated)] text-[var(--text-muted)] border border-[var(--border-subtle)]">
                    {s.source === 'project' ? t('chat:settings.label.active_workspace') : t('chat:settings.group.personal')}
                  </span>
                </div>
                <p className="mt-1 text-xs text-[var(--text-muted)] line-clamp-2 leading-relaxed">{s.description}</p>
              </div>
              {isDeletable && (
                <button
                  type="button"
                  disabled={actionLoading === s.name}
                  onClick={() => setDeleteTarget(s.name)}
                  className="ml-3 shrink-0 rounded-md border border-[var(--border-subtle)] px-2.5 py-1.5 text-[10px] font-bold text-[var(--accent-coral)] hover:bg-[rgba(244,63,94,0.08)] hover:border-[var(--accent-coral)]/30 disabled:opacity-50 transition-all cursor-pointer active:scale-[0.98]"
                >
                  {t('settings.skills.action.delete', { defaultValue: 'Delete' })}
                </button>
              )}
            </div>
          );
        })}
        {skills.length === 0 && (
          <div className="flex flex-col items-center justify-center py-16 text-center border border-dashed border-[var(--border-subtle)] rounded-lg bg-[var(--bg-elevated)]/10">
            <p className="text-sm font-medium text-[var(--text-secondary)] mb-1">
              {t('settings.skills.empty.title', { defaultValue: 'No skills installed in this workspace' })}
            </p>
            <p className="text-xs text-[var(--text-faint)] max-w-sm">
              {t('settings.skills.empty.desc', { defaultValue: 'Upload a skill .zip to install it into this workspace\'s skills directory.' })}
            </p>
          </div>
        )}
      </div>

      {/* Upload Dialog Modal Overlay */}
      {showUpload && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-md transition-all duration-300" onClick={closeUpload}>
          <div
            className="relative w-full max-w-md border border-[var(--border-default)] bg-[var(--bg-glass)] backdrop-blur-xl p-6 rounded-[var(--radius-lg)] shadow-[var(--shadow-lg)] transform scale-100 transition-all duration-300 hover:border-[var(--accent-gold)]/20"
            onClick={(e) => e.stopPropagation()}
          >
            {/* Glow highlight */}
            <div className="absolute -inset-px -z-10 rounded-[var(--radius-lg)] opacity-10 blur-sm bg-[var(--accent-gold)]" />

            <h2 className="text-sm font-semibold text-[var(--text-primary)]">
              {t('settings.skills.dialog.upload_title', { defaultValue: 'Upload Workspace Skill' })}
            </h2>
            <p className="mt-2 text-xs text-[var(--text-muted)] leading-relaxed">
              {t('settings.skills.dialog.upload_desc', { defaultValue: 'Select a .zip whose root has SKILL.md (or a single top-level dir containing it). Aligned with agentskills.io.' })}
            </p>
            <div className="mt-4">
              <input
                ref={fileInputRef}
                type="file"
                accept=".zip"
                onChange={(e) => onPickFile(e.target.files?.[0] ?? null)}
                className="block w-full text-xs text-[var(--text-muted)] file:mr-3 file:rounded-md file:border-0 file:bg-[var(--accent-gold)] file:px-3 file:py-1.5 file:text-xs file:font-medium file:text-[var(--bg-surface)] hover:file:opacity-90 file:cursor-pointer"
              />
              {file && <p className="mt-2 text-xs text-[var(--text-secondary)] font-mono">{file.name}</p>}
            </div>
            <label className="mt-4 flex items-center gap-2 text-xs text-[var(--text-secondary)] cursor-pointer select-none">
              <input
                type="checkbox"
                checked={replace}
                onChange={(e) => setReplace(e.target.checked)}
                className="rounded border-[var(--border-default)] bg-[var(--bg-elevated)] text-[var(--accent-gold)] focus:ring-[var(--accent-gold)]/20"
              />
              {t('settings.skills.dialog.replace', { defaultValue: 'Replace existing same-name skill' })}
            </label>
            <div className="mt-6 flex justify-end gap-3">
              <button
                type="button"
                onClick={closeUpload}
                className="px-4 py-2 text-xs font-medium rounded-[var(--radius-md)] border border-[var(--border-default)] bg-transparent text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] transition-all cursor-pointer"
              >
                {t('common:action.cancel', { defaultValue: 'Cancel' })}
              </button>
              <button
                type="button"
                onClick={handleUpload}
                disabled={uploading || !file}
                className="px-4 py-2 text-xs font-semibold rounded-[var(--radius-md)] bg-[var(--accent-gold)] text-[var(--text-contrast)] hover:bg-[var(--accent-gold-bright)] transition-all disabled:opacity-40 disabled:cursor-not-allowed shadow-sm"
              >
                {uploading
                  ? t('settings.skills.dialog.uploading', { defaultValue: 'Uploading…' })
                  : t('settings.skills.action.install', { defaultValue: 'Install' })}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Delete Confirmation Modal Overlay */}
      {deleteTarget && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-md transition-all duration-300" onClick={() => setDeleteTarget(null)}>
          <div
            className="relative w-full max-w-md border border-[var(--border-default)] bg-[var(--bg-glass)] backdrop-blur-xl p-6 rounded-[var(--radius-lg)] shadow-[var(--shadow-lg)] transform scale-100 transition-all duration-300 hover:border-[var(--accent-coral)]/30"
            onClick={(e) => e.stopPropagation()}
          >
            {/* Glow highlight */}
            <div className="absolute -inset-px -z-10 rounded-[var(--radius-lg)] opacity-10 blur-sm bg-[var(--accent-coral)]" />

            <div className="flex items-start gap-4">
              {/* Warning Indicator */}
              <div className="flex-shrink-0 w-10 h-10 rounded-full flex items-center justify-center bg-white/5 border border-white/10">
                <svg className="w-5 h-5 text-[var(--accent-coral)]" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                </svg>
              </div>

              <div className="flex-1 min-w-0">
                <h3 className="text-base font-display font-bold text-[var(--text-primary)]">
                  {t('settings.skills.confirm.delete_title', { defaultValue: 'Delete Workspace Skill' })}
                </h3>
                <p className="mt-2 text-sm text-[var(--text-secondary)] leading-relaxed">
                  {t('settings.skills.confirm.delete_body', { name: deleteTarget, defaultValue: `Delete skill "${deleteTarget}"? This removes it from the workspace skills directory and cannot be undone.` })}
                </p>
              </div>
            </div>

            {/* Action Buttons */}
            <div className="mt-6 flex justify-end gap-3">
              <button
                type="button"
                onClick={() => setDeleteTarget(null)}
                className="px-4 py-2 text-xs font-medium rounded-[var(--radius-md)] border border-[var(--border-default)] bg-transparent text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] transition-all cursor-pointer"
              >
                {t('common:action.cancel', { defaultValue: 'Cancel' })}
              </button>
              <button
                type="button"
                onClick={() => handleDelete(deleteTarget)}
                className="px-4 py-2 text-xs font-semibold rounded-[var(--radius-md)] bg-[var(--accent-coral)] text-white hover:bg-[var(--accent-coral)]/90 transition-all shadow-sm cursor-pointer"
              >
                {t('common:action.delete', { defaultValue: 'Delete' })}
              </button>
            </div>
          </div>
        </div>
      )}
    </TabPanel>
  );
}

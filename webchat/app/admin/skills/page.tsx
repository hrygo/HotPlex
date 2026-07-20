'use client';

import { useRef, useState } from 'react';
import { useAdminUI } from '@/context/admin-ui-context';
import { listAdminSkills, installAdminSkill, deleteAdminSkill } from '@/lib/api/admin-skills';
import type { AdminSkill } from '@/lib/api/admin-skills';
import { useResource } from '@/hooks/use-resource';
import { LoadingState, ErrorState, EmptyState } from '@/components/admin/resource-states';
import { useTranslation } from 'react-i18next';

// Badge color helper — managed (writable) vs external (read-only) provenance.
function Badge({ kind }: { kind: 'managed' | 'external' }) {
  const { t } = useTranslation();
  if (kind === 'managed') {
    return (
      <span className="rounded-full bg-[rgba(16,185,129,0.12)] px-2 py-0.5 text-[10px] font-medium text-[rgb(16,185,129)]">
        {t('admin:skills.label.managed', { defaultValue: 'Managed' })}
      </span>
    );
  }
  return (
    <span className="rounded-full bg-[var(--bg-hover)] px-2 py-0.5 text-[10px] font-medium text-[var(--text-muted)]">
      {t('admin:skills.label.external', { defaultValue: 'External (read-only)' })}
    </span>
  );
}

export default function AdminSkillsPage() {
  const { t } = useTranslation();
  const { showToast, confirm } = useAdminUI();
  const { data, loading, error, reload } = useResource<{ skills: AdminSkill[]; total: number }>(
    async () => listAdminSkills(),
    [],
  );
  const skills = data?.skills ?? [];

  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [showUpload, setShowUpload] = useState(false);
  const [file, setFile] = useState<File | null>(null);
  const [replace, setReplace] = useState(false);
  const [uploading, setUploading] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const onPickFile = (f: File | null) => {
    if (f && !/\.zip$/i.test(f.name)) {
      showToast(t('admin:skills.error.not_zip', { defaultValue: 'Only .zip files are accepted' }), 'error');
      return;
    }
    setFile(f);
  };

  const handleUpload = async () => {
    if (!file) {
      showToast(t('admin:skills.error.no_file', { defaultValue: 'Please select a zip file' }), 'error');
      return;
    }
    try {
      setUploading(true);
      const res = await installAdminSkill(file, replace);
      await reload();
      setShowUpload(false);
      setFile(null);
      setReplace(false);
      if (fileInputRef.current) fileInputRef.current.value = '';
      // warning 非空 = 跨 scope 遮蔽（spec §3.3 B6）：安装成功但需 UI 提示。
      const msg = res.warning
        ? t('admin:skills.toast.installed_warning', { warning: res.warning, defaultValue: `Installed — ${res.warning}` })
        : t('admin:skills.toast.installed', { defaultValue: 'Skill installed' });
      showToast(msg, 'success');
    } catch (err) {
      const status = (err as any).status;
      if (status === 409) {
        showToast(
          t('admin:skills.error.already_exists', { defaultValue: 'Skill already exists — enable "Replace existing" to overwrite' }),
          'error',
        );
      } else {
        showToast(err instanceof Error ? err.message : t('admin:skills.error.install_failed', { defaultValue: 'Install failed' }), 'error');
      }
    } finally {
      setUploading(false);
    }
  };

  const handleDelete = async (name: string) => {
    const ok = await confirm(
      t('admin:skills.confirm.delete_title', { defaultValue: 'Delete Skill' }),
      t('admin:skills.confirm.delete_body', { name, defaultValue: `Delete skill "${name}"? This removes it from the global skills directory and cannot be undone.` }),
      {
        confirmLabel: t('common:action.delete', { defaultValue: 'Delete' }),
        cancelLabel: t('common:action.cancel', { defaultValue: 'Cancel' }),
        destructive: true,
      },
    );
    if (!ok) return;
    try {
      setActionLoading(name);
      await deleteAdminSkill(name);
      await reload();
      showToast(t('admin:skills.toast.deleted', { defaultValue: 'Skill deleted' }), 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('admin:skills.error.delete_failed', { defaultValue: 'Delete failed' }), 'error');
    } finally {
      setActionLoading(null);
    }
  };

  const closeUpload = () => {
    setShowUpload(false);
    setFile(null);
    setReplace(false);
    if (fileInputRef.current) fileInputRef.current.value = '';
  };

  return (
    <div className="mx-auto max-w-4xl space-y-6 p-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-semibold text-[var(--text-primary)]">
            {t('admin:skills.title', { defaultValue: 'Skills' })}
          </h1>
          <p className="mt-1 text-xs text-[var(--text-muted)]">
            {t('admin:skills.subtitle', { defaultValue: 'Manage global skills under ~/.agents/skills. External skills (.claude/.hotplex) are read-only.' })}
          </p>
        </div>
        <button
          onClick={() => setShowUpload(true)}
          className="rounded-lg bg-[var(--accent-gold)] px-3 py-1.5 text-xs font-medium text-[var(--bg-surface)] hover:opacity-90"
        >
          {t('admin:skills.action.upload', { defaultValue: 'Upload Skill' })}
        </button>
      </div>

      {loading && <LoadingState />}
      {error && <ErrorState message={error} onRetry={reload} />}
      {!loading && !error && skills.length === 0 && (
        <EmptyState
          title={t('admin:skills.empty.title', { defaultValue: 'No skills installed' })}
          description={t('admin:skills.empty.desc', { defaultValue: 'Upload a skill .zip to install it into the global skills directory.' })}
        />
      )}

      {/* List */}
      {!loading && !error && skills.length > 0 && (
        <div className="space-y-2">
          {skills.map((s) => (
            <div
              key={`${s.source}/${s.name}`}
              className="flex items-center justify-between rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-3"
            >
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="truncate text-sm font-medium text-[var(--text-primary)]">{s.name}</span>
                  <Badge kind={s.managed ? 'managed' : 'external'} />
                  <span className="text-[10px] uppercase text-[var(--text-faint)]">{s.source}</span>
                </div>
                <p className="mt-0.5 truncate text-xs text-[var(--text-muted)]">{s.description}</p>
              </div>
              {s.managed && (
                <button
                  disabled={actionLoading === s.name}
                  onClick={() => handleDelete(s.name)}
                  className="ml-3 shrink-0 rounded-md border border-[var(--border-subtle)] px-2 py-1 text-xs text-[var(--accent-coral)] hover:bg-[rgba(244,63,94,0.08)] disabled:opacity-50"
                >
                  {t('common:action.delete', { defaultValue: 'Delete' })}
                </button>
              )}
            </div>
          ))}
        </div>
      )}

      {/* Upload dialog */}
      {showUpload && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={closeUpload}>
          <div
            className="w-full max-w-md rounded-xl bg-[var(--bg-surface)] p-5 shadow-xl"
            onClick={(e) => e.stopPropagation()}
          >
            <h2 className="text-sm font-semibold text-[var(--text-primary)]">
              {t('admin:skills.dialog.upload_title', { defaultValue: 'Upload Skill' })}
            </h2>
            <p className="mt-1 text-xs text-[var(--text-muted)]">
              {t('admin:skills.dialog.upload_desc', { defaultValue: 'Select a .zip whose root has SKILL.md (or a single top-level dir containing it). Aligned with agentskills.io.' })}
            </p>
            <div className="mt-4">
              <input
                ref={fileInputRef}
                type="file"
                accept=".zip"
                onChange={(e) => onPickFile(e.target.files?.[0] ?? null)}
                className="block w-full text-xs text-[var(--text-muted)] file:mr-3 file:rounded-md file:border-0 file:bg-[var(--accent-gold)] file:px-3 file:py-1.5 file:text-xs file:font-medium file:text-[var(--bg-surface)] hover:file:opacity-90"
              />
              {file && <p className="mt-2 text-xs text-[var(--text-secondary)]">{file.name}</p>}
            </div>
            <label className="mt-3 flex items-center gap-2 text-xs text-[var(--text-secondary)]">
              <input type="checkbox" checked={replace} onChange={(e) => setReplace(e.target.checked)} />
              {t('admin:skills.dialog.replace', { defaultValue: 'Replace existing same-name skill' })}
            </label>
            <div className="mt-5 flex justify-end gap-2">
              <button onClick={closeUpload} className="rounded-md px-3 py-1.5 text-xs text-[var(--text-muted)] hover:bg-[var(--bg-hover)]">
                {t('common:action.cancel', { defaultValue: 'Cancel' })}
              </button>
              <button
                onClick={handleUpload}
                disabled={uploading || !file}
                className="rounded-md bg-[var(--accent-gold)] px-3 py-1.5 text-xs font-medium text-[var(--bg-surface)] hover:opacity-90 disabled:opacity-50"
              >
                {uploading
                  ? t('admin:skills.dialog.uploading', { defaultValue: 'Uploading…' })
                  : t('admin:skills.action.install', { defaultValue: 'Install' })}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

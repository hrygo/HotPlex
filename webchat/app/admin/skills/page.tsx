'use client';

import { useRef, useState, useMemo } from 'react';
import { useAdminUI } from '@/context/admin-ui-context';
import {
  listAdminSkills,
  installAdminSkill,
  deleteAdminSkill,
  getAdminSkill,
  updateAdminSkill,
  createAdminSkillText,
} from '@/lib/api/admin-skills';
import type { AdminSkill, AdminSkillDetail } from '@/lib/api/admin-skills';
import { useResource } from '@/hooks/use-resource';
import { LoadingState, ErrorState, EmptyState } from '@/components/admin/resource-states';
import { useTranslation } from 'react-i18next';

function Badge({ kind }: { kind: 'managed' | 'external' }) {
  const { t } = useTranslation();
  if (kind === 'managed') {
    return (
      <span className="inline-flex items-center rounded-full bg-[rgba(16,185,129,0.1)] border border-[rgba(16,185,129,0.25)] px-2 py-0.5 text-[10px] font-mono font-bold uppercase tracking-wider text-[rgb(16,185,129)]">
        {t('admin:skills.label.managed', { defaultValue: 'Managed' })}
      </span>
    );
  }
  return (
    <span className="inline-flex items-center rounded-full bg-[var(--bg-hover)] border border-[var(--border-subtle)] px-2 py-0.5 text-[10px] font-mono font-bold uppercase tracking-wider text-[var(--text-muted)]">
      {t('admin:skills.label.external', { defaultValue: 'External (read-only)' })}
    </span>
  );
}

const SKILL_TEMPLATES = {
  basic: `---
name: my-new-skill
description: Concise description of what this skill does
---

# My New Skill

Write your skill prompt and instructions here.
`,
  cli: `---
name: cli-helper-skill
description: Guidelines for executing CLI commands and analyzing terminal output
---

# CLI Tool Skill

## Workflow
1. Run the target shell command or CLI tool.
2. Inspect stdout/stderr for status and error codes.
3. Present structured findings and next steps.
`,
  api: `---
name: api-integration-skill
description: SOP for querying external REST APIs and formatting JSON responses
---

# API Integration Skill

## Implementation SOP
1. Prepare request headers and authentication.
2. Query target endpoint and check HTTP response status.
3. Extract key JSON fields and format output clearly.
`,
};

export default function AdminSkillsPage() {
  const { t } = useTranslation();
  const { showToast, confirm } = useAdminUI();

  // Search & Pagination State
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);

  // Fetch Skills List with dependencies
  const { data, loading, error, reload } = useResource(
    async () => listAdminSkills({ page, page_size: pageSize, search }),
    [page, pageSize, search],
  );

  const skills = useMemo(() => data?.skills ?? [], [data?.skills]);
  const total = data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  // Action / Dialog States
  const [actionLoading, setActionLoading] = useState<string | null>(null);

  // Upload Zip Modal
  const [showUpload, setShowUpload] = useState(false);
  const [file, setFile] = useState<File | null>(null);
  const [replace, setReplace] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  // Create Text Modal
  const [showCreateText, setShowCreateText] = useState(false);
  const [newSkillName, setNewSkillName] = useState('');
  const [newSkillBody, setNewSkillBody] = useState(SKILL_TEMPLATES.basic);
  const [creatingText, setCreatingText] = useState(false);

  // Name validation regex: lowercase alphanumeric + hyphens
  const isNameValid = useMemo(() => {
    if (!newSkillName.trim()) return false;
    return /^[a-z0-9-]+$/.test(newSkillName.trim());
  }, [newSkillName]);

  // Detail / Edit Drawer
  const [detailModal, setDetailModal] = useState<AdminSkillDetail | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [isEditing, setIsEditing] = useState(false);
  const [editBody, setEditBody] = useState('');
  const [savingEdit, setSavingEdit] = useState(false);
  const [activeTab, setActiveTab] = useState<'body' | 'files'>('body');

  const onPickFile = (f: File | null) => {
    setUploadError(null);
    if (f && !/\.zip$/i.test(f.name)) {
      setUploadError(t('admin:skills.error.not_zip', { defaultValue: 'Only .zip files are accepted' }));
      setFile(null);
      return;
    }
    setFile(f);
  };

  const closeUpload = () => {
    setShowUpload(false);
    setFile(null);
    setReplace(false);
    setUploadError(null);
    if (fileInputRef.current) fileInputRef.current.value = '';
  };

  const handleUpload = async () => {
    if (!file) {
      setUploadError(t('admin:skills.error.no_file', { defaultValue: 'Please select a zip file' }));
      return;
    }
    setUploadError(null);
    try {
      setUploading(true);
      const res = await installAdminSkill(file, replace);
      await reload();
      closeUpload();
      const msg = res.warning
        ? t('admin:skills.toast.installed_warning', { warning: res.warning, defaultValue: `Installed — ${res.warning}` })
        : t('admin:skills.toast.installed', { defaultValue: 'Skill installed' });
      showToast(msg, 'success');
    } catch (err) {
      if (err instanceof Error) {
        const msg = err.message || '';
        const extMatch = msg.match(/\.([a-zA-Z0-9]+)/);
        const ext = extMatch ? `.${extMatch[1].toLowerCase()}` : '';

        if (msg.includes('SKILL_FILE_TYPE_BLOCKED') || msg.includes('blocked file type') || msg.includes('file type blocked')) {
          setUploadError(
            ext
              ? t('admin:skills.error.blocked_ext', { ext, defaultValue: `Zip contains unsupported file type (${ext}). Only .md, .json, .yaml, .txt, .png, .jpg, .py, .sh are allowed.` })
              : t('admin:skills.error.file_type_blocked', { defaultValue: 'Zip contains blocked file types (e.g. .pptx, .exe, .docx). Only text and asset files are allowed.' })
          );
        } else if (msg.includes('SKILL_INVALID_ZIP') || msg.includes('invalid zip') || msg.includes('invalid, corrupt, or oversized zip')) {
          setUploadError(t('admin:skills.error.invalid_zip', { defaultValue: 'Invalid or corrupted zip archive. Please verify the archive.' }));
        } else if (msg.includes('SKILL_INVALID_FORMAT') || msg.includes('invalid format') || msg.includes('no SKILL.md found') || msg.includes('missing frontmatter')) {
          setUploadError(t('admin:skills.error.invalid_format', { defaultValue: 'Invalid skill format. Root must contain a valid SKILL.md with YAML frontmatter.' }));
        } else if (msg.includes('SKILL_ALREADY_EXISTS') || msg.includes('already exists')) {
          setUploadError(t('admin:skills.error.already_exists', { defaultValue: 'Skill already exists — enable "Replace existing" to overwrite.' }));
        } else {
          const cleaned = msg.replace(/\\x[0-9a-fA-F]{2}/g, '').replace(/^skill:\s*/i, '').trim();
          setUploadError(cleaned || t('admin:skills.error.install_failed', { defaultValue: 'Install failed' }));
        }
      } else {
        setUploadError(t('admin:skills.error.install_failed', { defaultValue: 'Install failed' }));
      }
    } finally {
      setUploading(false);
    }
  };

  const handleCreateText = async () => {
    if (!newSkillName.trim()) {
      showToast(t('admin:skills.error.name_required', { defaultValue: 'Please enter a skill name' }), 'error');
      return;
    }
    if (!isNameValid) {
      showToast('Skill name can only contain lowercase letters, numbers, and hyphens (e.g. my-skill)', 'error');
      return;
    }
    try {
      setCreatingText(true);
      await createAdminSkillText(newSkillName.trim(), newSkillBody, replace);
      await reload();
      setShowCreateText(false);
      setNewSkillName('');
      setNewSkillBody(SKILL_TEMPLATES.basic);
      setReplace(false);
      showToast(t('admin:skills.toast.created', { defaultValue: 'Skill created' }), 'success');
    } catch (err) {
      const status = (err as any).status;
      if (status === 409) {
        showToast(
          t('admin:skills.error.already_exists', { defaultValue: 'Skill already exists — enable "Replace existing" to overwrite' }),
          'error',
        );
      } else {
        showToast(err instanceof Error ? err.message : t('admin:skills.error.install_failed', { defaultValue: 'Create failed' }), 'error');
      }
    } finally {
      setCreatingText(false);
    }
  };

  const handleOpenDetail = async (skill: AdminSkill) => {
    try {
      setDetailLoading(true);
      const detail = await getAdminSkill(skill.name);
      setDetailModal(detail);
      setEditBody(detail.body);
      setIsEditing(false);
      setActiveTab('body');
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Failed to load skill details', 'error');
    } finally {
      setDetailLoading(false);
    }
  };

  const handleSaveEdit = async () => {
    if (!detailModal) return;
    try {
      setSavingEdit(true);
      const updated = await updateAdminSkill(detailModal.name, editBody);
      setDetailModal(updated);
      setIsEditing(false);
      await reload();
      showToast(t('admin:skills.toast.updated', { defaultValue: 'Skill updated' }), 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('admin:skills.detail_drawer.save_failed', { defaultValue: 'Failed to update skill' }), 'error');
    } finally {
      setSavingEdit(false);
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
      if (detailModal?.name === name) {
        setDetailModal(null);
      }
      await reload();
      showToast(t('admin:skills.toast.deleted', { defaultValue: 'Skill deleted' }), 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : t('admin:skills.error.delete_failed', { defaultValue: 'Delete failed' }), 'error');
    } finally {
      setActionLoading(null);
    }
  };

  const closeCreateText = () => {
    setShowCreateText(false);
    setNewSkillName('');
    setNewSkillBody(SKILL_TEMPLATES.basic);
    setReplace(false);
  };

  return (
    <div className="relative min-h-screen bg-[var(--bg-base)] px-6 py-8">
      {/* Background ambient gradient glow */}
      <div className="pointer-events-none fixed inset-0 z-0 bg-mesh opacity-30" />
      <div className="pointer-events-none fixed inset-0 z-0 noise-overlay" />

      <div className="relative z-10 max-w-6xl mx-auto">
        {/* Header */}
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 mb-8">
          <div>
            <h1 className="text-2xl font-display font-bold tracking-tight text-[var(--text-primary)]">
              {t('admin:skills.title', { defaultValue: 'Skills' })}
            </h1>
            <p className="text-xs text-[var(--text-muted)] mt-1">
              {t('admin:skills.subtitle', { defaultValue: 'Manage global skills under ~/.agents/skills. External skills (.claude/.hotplex) are read-only.' })}
            </p>
          </div>

          <div className="flex items-center gap-2 shrink-0">
            <button
              type="button"
              onClick={reload}
              disabled={loading}
              title={t('common:action.refresh', { defaultValue: 'Refresh' })}
              className="inline-flex items-center justify-center w-9 h-9 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] transition-all active:scale-95 disabled:opacity-40 shadow-[var(--shadow-sm)]"
            >
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
                className={loading ? 'animate-spin' : ''}
              >
                <path d="M21 2v6h-6" />
                <path d="M3 12a9 9 0 0 1 15-6.7L21 8" />
                <path d="M3 22v-6h6" />
                <path d="M21 12a9 9 0 0 1-15 6.7L3 16" />
              </svg>
            </button>
            <button
              type="button"
              onClick={() => setShowCreateText(true)}
              className="inline-flex items-center gap-1.5 px-3.5 py-2 rounded-[var(--radius-sm)] text-xs font-bold border border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[var(--text-primary)] hover:bg-[var(--bg-hover)] transition-all active:scale-95 shadow-[var(--shadow-sm)]"
            >
              + {t('admin:skills.action.create_text', { defaultValue: 'New Skill' })}
            </button>
            <button
              type="button"
              onClick={() => setShowUpload(true)}
              className="inline-flex items-center gap-1.5 px-3.5 py-2 rounded-[var(--radius-sm)] text-xs font-bold bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-all active:scale-95 shadow-[var(--shadow-sm)]"
            >
              {t('admin:skills.action.upload', { defaultValue: 'Upload Skill' })}
            </button>
          </div>
        </div>

        {/* Toolbar: search + count */}
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 mb-6 bg-[var(--bg-glass)] border border-[var(--border-subtle)] rounded-[var(--radius-md)] p-3 backdrop-blur-md shadow-[var(--shadow-sm)]">
          <div className="relative w-full sm:w-64">
            <svg
              className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-[var(--text-faint)] pointer-events-none"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
            <input
              type="text"
              value={search}
              onChange={(e) => {
                setSearch(e.target.value);
                setPage(1);
              }}
              placeholder={t('admin:skills.search_placeholder', { defaultValue: 'Search skill name or description…' })}
              className="w-full pl-8 pr-7 py-1.5 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-base)] text-xs text-[var(--text-primary)] placeholder:text-[var(--text-faint)] outline-none transition-all focus:border-[var(--accent-gold)]/40"
            />
            {search && (
              <button
                type="button"
                onClick={() => {
                  setSearch('');
                  setPage(1);
                }}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-[var(--text-faint)] hover:text-[var(--text-primary)] transition-colors p-0.5"
              >
                <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" className="w-3.5 h-3.5">
                  <path d="M6.28 5.22a.75.75 0 0 0-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 1 0 1.06 1.06L10 11.06l3.72 3.72a.75.75 0 1 0 1.06-1.06L11.06 10l3.72-3.72a.75.75 0 0 0-1.06-1.06L10 8.94 6.28 5.22Z" />
                </svg>
              </button>
            )}
          </div>

          {!loading && !error && (
            <span className="text-[11px] font-mono text-[var(--text-faint)] px-2 py-0.5 rounded-full bg-[var(--bg-hover)] self-start sm:self-auto">
              {total}
            </span>
          )}
        </div>

        {/* List Content Panel */}
        {loading && <LoadingState />}
        {error && <ErrorState message={error} onRetry={reload} />}
        {!loading && !error && skills.length === 0 && (
          <EmptyState
            title={t('admin:skills.empty.title', { defaultValue: 'No skills installed' })}
            description={t('admin:skills.empty.desc', { defaultValue: 'Upload a skill .zip or create a text skill.' })}
          />
        )}

        {!loading && !error && skills.length > 0 && (
          <div className="rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-surface)] overflow-hidden shadow-sm">
            <div className="divide-y divide-[var(--border-subtle)]">
              {skills.map((s) => (
                <div
                  key={`${s.source}/${s.name}`}
                  className="flex items-center justify-between px-4 py-2.5 transition-colors hover:bg-[var(--bg-hover)]"
                >
                  <div
                    className="min-w-0 flex-1 cursor-pointer pr-4"
                    onClick={() => handleOpenDetail(s)}
                  >
                    <div className="flex items-center gap-2 mb-0.5">
                      <span className="truncate text-xs font-bold text-[var(--text-primary)] hover:text-[var(--accent-gold)] transition-colors">
                        {s.name}
                      </span>
                      <Badge kind={s.managed ? 'managed' : 'external'} />
                      <span className="text-[10px] font-mono font-bold uppercase tracking-wider text-[var(--text-faint)] bg-[var(--bg-hover)] px-1.5 py-0.5 rounded border border-[var(--border-subtle)]">
                        {s.source}
                      </span>
                    </div>
                    <p className="line-clamp-1 text-xs text-[var(--text-muted)] leading-relaxed">{s.description}</p>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <button
                      onClick={() => handleOpenDetail(s)}
                      className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] px-2.5 py-1 text-xs font-semibold text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] transition-colors"
                    >
                      {t('admin:skills.action.view', { defaultValue: 'View' })}
                    </button>
                    {s.managed && (
                      <button
                        disabled={actionLoading === s.name}
                        onClick={() => handleDelete(s.name)}
                        className="rounded-[var(--radius-sm)] border border-[rgba(244,63,94,0.2)] px-2.5 py-1 text-xs font-semibold text-[var(--accent-coral)] hover:bg-[rgba(244,63,94,0.08)] transition-colors disabled:opacity-50"
                      >
                        {t('common:action.delete', { defaultValue: 'Delete' })}
                      </button>
                    )}
                  </div>
                </div>
              ))}
            </div>

            {/* Pagination Footer in Panel */}
            <div className="flex items-center justify-between border-t border-[var(--border-subtle)] px-4 py-2.5 bg-[var(--bg-surface)] text-xs text-[var(--text-muted)]">
              <span className="font-mono text-[11px]">
                {t('admin:skills.pagination.page_info', {
                  page,
                  totalPages,
                  total,
                  defaultValue: `Page ${page} of ${totalPages} (Total ${total})`,
                })}
              </span>
              <div className="flex items-center gap-3">
                <select
                  value={pageSize}
                  onChange={(e) => {
                    setPageSize(Number(e.target.value));
                    setPage(1);
                  }}
                  className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-hover)] px-2 py-1 text-[11px] font-mono text-[var(--text-secondary)] focus:outline-none cursor-pointer"
                >
                  <option value={5}>5 / page</option>
                  <option value={10}>10 / page</option>
                  <option value={20}>20 / page</option>
                </select>
                <div className="flex items-center gap-1.5">
                  <button
                    disabled={page <= 1}
                    onClick={() => setPage((p) => Math.max(1, p - 1))}
                    className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] px-2.5 py-1 text-[11px] font-semibold text-[var(--text-primary)] hover:bg-[var(--bg-hover)] disabled:opacity-40 transition-colors"
                  >
                    {t('admin:skills.pagination.prev', { defaultValue: 'Previous' })}
                  </button>
                  <button
                    disabled={page >= totalPages}
                    onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                    className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] px-2.5 py-1 text-[11px] font-semibold text-[var(--text-primary)] hover:bg-[var(--bg-hover)] disabled:opacity-40 transition-colors"
                  >
                    {t('admin:skills.pagination.next', { defaultValue: 'Next' })}
                  </button>
                </div>
              </div>
            </div>
          </div>
        )}

        {/* Upload Zip Dialog */}
        {showUpload && (
          <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4" onClick={closeUpload}>
            <div
              className="w-full max-w-lg sm:max-w-xl max-h-[85vh] flex flex-col rounded-xl bg-[var(--bg-surface)] p-6 shadow-2xl border border-[var(--border-subtle)] transition-all"
              onClick={(e) => e.stopPropagation()}
            >
              <div className="flex items-start justify-between border-b border-[var(--border-subtle)] pb-4">
                <div>
                  <h2 className="text-base font-display font-bold text-[var(--text-primary)]">
                    {t('admin:skills.dialog.upload_title', { defaultValue: 'Upload Skill' })}
                  </h2>
                  <p className="mt-1 text-xs text-[var(--text-muted)] leading-relaxed">
                    {t('admin:skills.dialog.upload_desc', { defaultValue: 'Select a .zip whose root has SKILL.md (or a single top-level dir containing it).' })}
                  </p>
                </div>
                <button
                  onClick={closeUpload}
                  className="rounded-full p-1.5 text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] transition-all"
                >
                  ✕
                </button>
              </div>

              <div className="mt-5 space-y-4 flex-1 overflow-y-auto pr-0.5">
                <div>
                  <input
                    ref={fileInputRef}
                    type="file"
                    accept=".zip"
                    onChange={(e) => onPickFile(e.target.files?.[0] ?? null)}
                    className="block w-full text-xs text-[var(--text-muted)] file:mr-3 file:rounded-md file:border-0 file:bg-[var(--accent-gold)] file:px-3.5 file:py-2 file:text-xs file:font-bold file:text-black hover:file:bg-[var(--accent-gold-bright)] transition-colors cursor-pointer"
                  />
                  {file && <p className="mt-2 text-xs font-mono text-[var(--text-secondary)]">{file.name}</p>}
                </div>
                <label className="flex items-center gap-2 text-xs font-medium text-[var(--text-secondary)] cursor-pointer">
                  <input type="checkbox" checked={replace} onChange={(e) => setReplace(e.target.checked)} className="rounded accent-[var(--accent-gold)]" />
                  {t('admin:skills.dialog.replace', { defaultValue: 'Replace existing same-name skill' })}
                </label>

                {uploadError && (
                  <div role="alert" className="mt-3 flex items-start gap-2.5 rounded-lg border border-[rgba(244,63,94,0.3)] bg-[rgba(244,63,94,0.08)] p-3 text-xs font-medium text-[var(--accent-coral)] animate-in fade-in duration-200">
                    <span className="shrink-0 text-base leading-none">⚠️</span>
                    <div className="flex-1 leading-relaxed">{uploadError}</div>
                  </div>
                )}
              </div>

              <div className="mt-6 flex justify-end gap-2 border-t border-[var(--border-subtle)] pt-4">
                <button onClick={closeUpload} className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] px-4 py-1.5 text-xs font-semibold text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] transition-colors">
                  {t('common:action.cancel', { defaultValue: 'Cancel' })}
                </button>
                <button
                  onClick={handleUpload}
                  disabled={uploading || !file}
                  className="rounded-[var(--radius-sm)] bg-[var(--accent-gold)] px-4 py-1.5 text-xs font-bold uppercase tracking-wider text-black hover:bg-[var(--accent-gold-bright)] active:scale-95 transition-all disabled:opacity-50"
                >
                  {uploading
                    ? t('admin:skills.dialog.uploading', { defaultValue: 'Uploading…' })
                    : t('admin:skills.action.install', { defaultValue: 'Install' })}
                </button>
              </div>
            </div>
          </div>
        )}

        {/* Create Text Skill Dialog */}
        {showCreateText && (
          <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4 sm:p-6" onClick={closeCreateText}>
            <div
              className="w-full max-w-xl sm:max-w-3xl lg:max-w-4xl max-h-[90vh] sm:h-[82vh] flex flex-col rounded-xl bg-[var(--bg-surface)] p-6 shadow-2xl border border-[var(--border-subtle)] transition-all"
              onClick={(e) => e.stopPropagation()}
            >
              {/* Modal Header */}
              <div className="flex items-start justify-between border-b border-[var(--border-subtle)] pb-4 shrink-0">
                <div>
                  <h2 className="text-lg font-display font-bold text-[var(--text-primary)]">
                    {t('admin:skills.create_dialog.title', { defaultValue: 'Create Text Skill' })}
                  </h2>
                  <p className="mt-1 text-xs text-[var(--text-muted)] leading-relaxed">
                    {t('admin:skills.create_dialog.desc', { defaultValue: 'Create a new SKILL.md in global skills directory.' })}
                  </p>
                </div>
                <button
                  onClick={closeCreateText}
                  className="rounded-full p-1.5 text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] transition-all"
                >
                  ✕
                </button>
              </div>

              {/* Form Body */}
              <div className="mt-4 space-y-4 flex-1 overflow-y-auto pr-1 flex flex-col min-h-0">
                {/* Skill Name Input */}
                <div className="shrink-0">
                  <div className="flex items-center justify-between mb-1">
                    <label className="block text-xs font-semibold text-[var(--text-secondary)]">
                      {t('admin:skills.create_dialog.name_label', { defaultValue: 'Skill Name' })}
                    </label>
                    <span className="text-[10px] font-mono text-[var(--text-faint)]">lowercase, hyphens & numbers</span>
                  </div>
                  <input
                    type="text"
                    value={newSkillName}
                    onChange={(e) => setNewSkillName(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, '-'))}
                    placeholder="e.g. my-custom-skill"
                    className={`w-full rounded-[var(--radius-sm)] border bg-[var(--bg-hover)] px-3.5 py-2 text-xs font-mono text-[var(--text-primary)] caret-[var(--accent-gold)] selection:bg-[rgba(245,158,11,0.25)] selection:text-[var(--text-primary)] hover:border-[var(--border-medium)] hover:bg-[var(--bg-surface)] focus:bg-[var(--bg-surface)] focus:outline-none transition-all duration-200 ${
                      newSkillName && !isNameValid
                        ? 'border-[var(--accent-coral)] focus:border-[var(--accent-coral)] focus:ring-2 focus:ring-[rgba(244,63,94,0.2)]'
                        : 'border-[var(--border-subtle)] focus:border-[var(--accent-gold)] focus:ring-2 focus:ring-[rgba(245,158,11,0.2)]'
                    }`}
                  />
                  {newSkillName && !isNameValid && (
                    <p className="mt-1 text-[11px] text-[var(--accent-coral)]">
                      Skill name must be lowercase alphanumeric with hyphens (e.g. my-custom-skill)
                    </p>
                  )}
                </div>

                {/* Preset Templates Picker */}
                <div className="shrink-0">
                  <div className="flex items-center justify-between mb-1.5">
                    <label className="block text-xs font-semibold text-[var(--text-secondary)]">
                      Quick Templates
                    </label>
                    <span className="text-[10px] font-mono text-[var(--text-faint)]">Click to load boilerplate</span>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <button
                      type="button"
                      onClick={() => setNewSkillBody(SKILL_TEMPLATES.basic)}
                      className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-hover)] px-3 py-1.5 text-xs font-semibold text-[var(--text-secondary)] hover:border-[var(--accent-gold)] hover:text-[var(--accent-gold)] hover:bg-[var(--bg-surface)] transition-all active:scale-95"
                    >
                      ✨ Basic Skill
                    </button>
                    <button
                      type="button"
                      onClick={() => setNewSkillBody(SKILL_TEMPLATES.cli)}
                      className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-hover)] px-3 py-1.5 text-xs font-semibold text-[var(--text-secondary)] hover:border-[var(--accent-gold)] hover:text-[var(--accent-gold)] hover:bg-[var(--bg-surface)] transition-all active:scale-95"
                    >
                      💻 CLI Tool Skill
                    </button>
                    <button
                      type="button"
                      onClick={() => setNewSkillBody(SKILL_TEMPLATES.api)}
                      className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--bg-hover)] px-3 py-1.5 text-xs font-semibold text-[var(--text-secondary)] hover:border-[var(--accent-gold)] hover:text-[var(--accent-gold)] hover:bg-[var(--bg-surface)] transition-all active:scale-95"
                    >
                      🔌 API Integration
                    </button>
                  </div>
                </div>

                {/* SKILL.md Body Textarea */}
                <div className="flex-1 flex flex-col min-h-[240px] sm:min-h-[300px]">
                  <div className="flex items-center justify-between mb-1 shrink-0">
                    <label className="block text-xs font-semibold text-[var(--text-secondary)]">
                      {t('admin:skills.create_dialog.content_label', { defaultValue: 'SKILL.md Content' })}
                    </label>
                    <span className="text-[10px] font-mono text-[var(--text-faint)]">
                      {newSkillBody.split('\n').length} lines · {newSkillBody.length} chars
                    </span>
                  </div>
                  <textarea
                    value={newSkillBody}
                    onChange={(e) => setNewSkillBody(e.target.value)}
                    className="w-full flex-1 min-h-[220px] rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-hover)] p-4 text-xs font-mono text-[var(--text-primary)] caret-[var(--accent-gold)] selection:bg-[rgba(245,158,11,0.25)] selection:text-[var(--text-primary)] hover:border-[var(--border-medium)] hover:bg-[var(--bg-surface)] focus:border-[var(--accent-gold)] focus:ring-2 focus:ring-[rgba(245,158,11,0.2)] focus:bg-[var(--bg-surface)] focus:outline-none transition-all duration-200 leading-relaxed cursor-text"
                  />
                </div>

                {/* Replace Checkbox */}
                <div className="shrink-0 pt-1">
                  <label className="flex items-center gap-2 text-xs font-medium text-[var(--text-secondary)] cursor-pointer">
                    <input type="checkbox" checked={replace} onChange={(e) => setReplace(e.target.checked)} className="rounded accent-[var(--accent-gold)]" />
                    {t('admin:skills.dialog.replace', { defaultValue: 'Replace existing same-name skill' })}
                  </label>
                </div>
              </div>

              {/* Modal Footer */}
              <div className="mt-4 flex justify-end gap-2 border-t border-[var(--border-subtle)] pt-4 shrink-0">
                <button
                  onClick={closeCreateText}
                  className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] px-4 py-1.5 text-xs font-semibold text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] active:scale-95 transition-all"
                >
                  {t('common:action.cancel', { defaultValue: 'Cancel' })}
                </button>
                <button
                  onClick={handleCreateText}
                  disabled={creatingText || !newSkillName.trim() || !isNameValid}
                  className="inline-flex items-center gap-1.5 rounded-[var(--radius-sm)] bg-[var(--accent-gold)] px-4 py-1.5 text-xs font-bold uppercase tracking-wider text-black hover:bg-[var(--accent-gold-bright)] active:scale-95 transition-all disabled:opacity-50"
                >
                  {creatingText ? (
                    <>
                      <svg className="animate-spin h-3.5 w-3.5 text-black" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                        <circle className="opacity-25" cx="12" cy="12" r="10" strokeWidth="4" />
                        <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                      </svg>
                      <span>{t('admin:skills.create_dialog.creating', { defaultValue: 'Creating…' })}</span>
                    </>
                  ) : (
                    <span>{t('admin:skills.action.create_text', { defaultValue: 'Create Skill' })}</span>
                  )}
                </button>
              </div>
            </div>
          </div>
        )}

        {/* Detail / Edit Drawer Modal */}
        {(detailModal || detailLoading) && (
          <div
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4 sm:p-6"
            onClick={() => setDetailModal(null)}
          >
            <div
              className="flex max-h-[90vh] sm:h-[85vh] w-full max-w-2xl sm:max-w-4xl lg:max-w-5xl flex-col rounded-xl bg-[var(--bg-surface)] p-6 shadow-2xl border border-[var(--border-subtle)] transition-all"
              onClick={(e) => e.stopPropagation()}
            >
              {detailLoading ? (
                <LoadingState />
              ) : detailModal ? (
                <>
                  {/* Header */}
                  <div className="flex items-start justify-between border-b border-[var(--border-subtle)] pb-4">
                    <div className="min-w-0 flex-1 pr-4">
                      <div className="flex flex-wrap items-center gap-2 mb-1">
                        <h2 className="text-lg font-display font-bold text-[var(--text-primary)]">{detailModal.name}</h2>
                        <Badge kind={detailModal.managed ? 'managed' : 'external'} />
                        <span className="text-[10px] font-mono font-bold uppercase tracking-wider text-[var(--text-faint)] bg-[var(--bg-hover)] px-2 py-0.5 rounded border border-[var(--border-subtle)]">
                          {detailModal.source}
                        </span>
                      </div>
                      <p className="text-xs text-[var(--text-muted)] leading-relaxed">{detailModal.description}</p>
                    </div>

                    <div className="flex shrink-0 items-center gap-2">
                      <button
                        type="button"
                        onClick={async () => {
                          try {
                            await navigator.clipboard.writeText(detailModal.body);
                            showToast(t('common:toast.copied', { defaultValue: 'Copied to clipboard' }), 'success');
                          } catch {
                            showToast('Failed to copy', 'error');
                          }
                        }}
                        className="inline-flex items-center gap-1.5 rounded-[var(--radius-sm)] border border-[var(--border-subtle)] px-2.5 py-1 text-xs font-semibold text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] transition-all active:scale-95"
                        title="Copy SKILL.md content"
                      >
                        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                          <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
                          <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
                        </svg>
                        <span>{t('common:action.copy', { defaultValue: 'Copy' })}</span>
                      </button>

                      {detailModal.managed && (
                        <button
                          onClick={() => setIsEditing(!isEditing)}
                          className={`inline-flex items-center gap-1.5 rounded-[var(--radius-sm)] border px-2.5 py-1 text-xs font-semibold transition-all active:scale-95 ${
                            isEditing
                              ? 'border-[var(--accent-gold)] bg-[rgba(245,158,11,0.12)] text-[var(--accent-gold)]'
                              : 'border-[var(--border-subtle)] text-[var(--text-primary)] hover:bg-[var(--bg-hover)]'
                          }`}
                        >
                          {isEditing ? (
                            <>
                              <span>👁</span>
                              <span>{t('admin:skills.action.view', { defaultValue: 'View' })}</span>
                            </>
                          ) : (
                            <>
                              <span>✏️</span>
                              <span>{t('admin:skills.action.edit', { defaultValue: 'Edit' })}</span>
                            </>
                          )}
                        </button>
                      )}

                      <button
                        onClick={() => setDetailModal(null)}
                        className="rounded-full p-1.5 text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] transition-all"
                      >
                        ✕
                      </button>
                    </div>
                  </div>

                  {/* Tabs */}
                  {!isEditing && (
                    <div className="flex gap-6 border-b border-[var(--border-subtle)] text-xs font-semibold pt-3">
                      <button
                        onClick={() => setActiveTab('body')}
                        className={`pb-2.5 border-b-2 transition-all ${
                          activeTab === 'body'
                            ? 'border-[var(--accent-gold)] text-[var(--accent-gold)] font-bold'
                            : 'border-transparent text-[var(--text-muted)] hover:text-[var(--text-primary)]'
                        }`}
                      >
                        {t('admin:skills.detail_drawer.body_tab', { defaultValue: 'SKILL.md Content' })}
                      </button>
                      <button
                        onClick={() => setActiveTab('files')}
                        className={`pb-2.5 border-b-2 transition-all ${
                          activeTab === 'files'
                            ? 'border-[var(--accent-gold)] text-[var(--accent-gold)] font-bold'
                            : 'border-transparent text-[var(--text-muted)] hover:text-[var(--text-primary)]'
                        }`}
                      >
                        {t('admin:skills.detail_drawer.files_tab', { defaultValue: 'Files List' })} ({detailModal.files?.length ?? 0})
                      </button>
                    </div>
                  )}

                  {/* Body Content / Edit Form */}
                  <div className="flex-1 overflow-y-auto py-4 min-h-[300px]">
                    {isEditing ? (
                      <div className="h-full flex flex-col space-y-2">
                        <div className="flex items-center justify-between">
                          <label className="text-xs font-semibold text-[var(--text-secondary)]">
                            {t('admin:skills.detail_drawer.edit_title', { defaultValue: 'Edit SKILL.md' })}
                          </label>
                          <span className="text-[10px] font-mono text-[var(--text-faint)]">
                            {editBody.split('\n').length} lines · {editBody.length} chars
                          </span>
                        </div>
                        <textarea
                          value={editBody}
                          onChange={(e) => setEditBody(e.target.value)}
                          className="flex-1 min-h-[360px] w-full rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-hover)] p-4 text-xs font-mono text-[var(--text-primary)] caret-[var(--accent-gold)] selection:bg-[rgba(245,158,11,0.25)] selection:text-[var(--text-primary)] hover:border-[var(--border-medium)] hover:bg-[var(--bg-surface)] focus:border-[var(--accent-gold)] focus:ring-2 focus:ring-[rgba(245,158,11,0.2)] focus:bg-[var(--bg-surface)] focus:outline-none transition-all duration-200 leading-relaxed cursor-text"
                        />
                      </div>
                    ) : activeTab === 'body' ? (
                      <div className="space-y-4">
                        {/* Prompt Body Document */}
                        <div className="rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-hover)] p-4 overflow-x-auto">
                          <pre className="whitespace-pre-wrap text-xs font-mono text-[var(--text-primary)] leading-relaxed">
                            {detailModal.body}
                          </pre>
                        </div>
                      </div>
                    ) : (
                      <div className="space-y-2">
                        {detailModal.files && detailModal.files.length > 0 ? (
                          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                            {detailModal.files.map((f) => (
                              <div
                                key={f}
                                className="flex items-center gap-2.5 rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-hover)] px-3.5 py-2.5 text-xs font-mono text-[var(--text-secondary)] hover:border-[var(--border-medium)] transition-colors"
                              >
                                <span className="text-base">📄</span>
                                <span className="truncate">{f}</span>
                              </div>
                            ))}
                          </div>
                        ) : (
                          <p className="text-xs text-[var(--text-muted)] py-4 text-center">No extra files included</p>
                        )}
                      </div>
                    )}
                  </div>

                  {/* Footer */}
                  <div className="flex items-center justify-between border-t border-[var(--border-subtle)] pt-4 text-xs">
                    {detailModal.managed ? (
                      <button
                        onClick={() => handleDelete(detailModal.name)}
                        className="rounded-[var(--radius-sm)] border border-[rgba(244,63,94,0.2)] bg-[rgba(244,63,94,0.05)] px-3.5 py-1.5 text-xs font-semibold text-[var(--accent-coral)] hover:bg-[rgba(244,63,94,0.15)] hover:border-[var(--accent-coral)] active:scale-95 transition-all"
                      >
                        {t('common:action.delete', { defaultValue: 'Delete Skill' })}
                      </button>
                    ) : (
                      <span className="text-[11px] text-[var(--text-faint)] font-mono font-bold uppercase tracking-wider">External skill (Read-Only)</span>
                    )}

                    <div className="flex items-center gap-2">
                      {isEditing && (
                        <button
                          onClick={handleSaveEdit}
                          disabled={savingEdit}
                          className="inline-flex items-center gap-1.5 rounded-[var(--radius-sm)] bg-[var(--accent-gold)] px-4 py-1.5 text-xs font-bold uppercase tracking-wider text-black hover:bg-[var(--accent-gold-bright)] active:scale-95 transition-all disabled:opacity-50"
                        >
                          {savingEdit ? (
                            <>
                              <svg className="animate-spin h-3.5 w-3.5 text-black" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                                <circle className="opacity-25" cx="12" cy="12" r="10" strokeWidth="4" />
                                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                              </svg>
                              <span>{t('admin:skills.detail_drawer.saving', { defaultValue: 'Saving…' })}</span>
                            </>
                          ) : (
                            <span>{t('admin:skills.action.save', { defaultValue: 'Save Changes' })}</span>
                          )}
                        </button>
                      )}
                      <button
                        onClick={() => setDetailModal(null)}
                        className="rounded-[var(--radius-sm)] border border-[var(--border-subtle)] px-3.5 py-1.5 text-xs font-semibold text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)] active:scale-95 transition-all"
                      >
                        {t('admin:skills.action.close', { defaultValue: 'Close' })}
                      </button>
                    </div>
                  </div>
                </>
              ) : null}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}



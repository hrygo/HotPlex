'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { createWorkspace } from '@/lib/api/workspaces';

interface FormState {
  name: string;
  work_dir: string;
}

interface FieldError {
  field: string;
  message: string;
}

const NAME_RE = /^[a-zA-Z0-9-]+$/;

const inputClass =
  'w-full rounded-[var(--radius-sm)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] px-3 py-2 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-faint)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors font-mono';

const labelClass =
  'block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5';

export default function NewWorkspacePage() {
  const router = useRouter();
  const [form, setForm] = useState<FormState>({ name: '', work_dir: '' });
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<FieldError[]>([]);
  const [touched, setTouched] = useState<Set<string>>(new Set());

  function set<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((prev) => ({ ...prev, [key]: value }));
    setTouched((prev) => new Set(prev).add(key));
    setFieldErrors((prev) => prev.filter((e) => e.field !== key));
  }

  function getFieldError(field: string): string | undefined {
    if (!touched.has(field)) return undefined;
    return fieldErrors.find((e) => e.field === field)?.message;
  }

  function validate(): FieldError[] {
    const errors: FieldError[] = [];
    if (!form.name.trim()) {
      errors.push({ field: 'name', message: 'Workspace name is required.' });
    } else if (!NAME_RE.test(form.name.trim())) {
      errors.push({ field: 'name', message: 'Only letters, numbers, and hyphens.' });
    }
    if (!form.work_dir.trim()) {
      errors.push({ field: 'work_dir', message: 'Work directory is required.' });
    }
    return errors;
  }

  const handleSubmit = () => {
    setError(null);
    setTouched(new Set(['name', 'work_dir']));

    const errors = validate();
    setFieldErrors(errors);
    if (errors.length > 0) return;

    setSubmitting(true);
    createWorkspace(form.name.trim(), form.work_dir.trim())
      .then(() => {
        router.push('/admin/workspaces');
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : 'Failed to create workspace');
      })
      .finally(() => {
        setSubmitting(false);
      });
  }

  function fieldBorder(field: string): string {
    return getFieldError(field) ? 'border-[var(--accent-coral)]' : '';
  }

  return (
    <div className="min-h-screen bg-[var(--bg-base)] p-6">
      <div className="max-w-2xl mx-auto px-6 py-8">
        {/* Breadcrumb */}
        <div className="flex items-center gap-2 mb-6 text-xs text-[var(--text-faint)]">
          <Link
            href="/admin/workspaces"
            className="hover:text-[var(--text-secondary)] transition-colors flex items-center gap-1"
          >
            <svg width="12" height="12" viewBox="0 0 16 16" fill="none">
              <path d="M10 3L5 8l5 5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
            Workspaces
          </Link>
          <span className="text-[var(--border-subtle)]">/</span>
          <span className="text-[var(--text-secondary)]">New Workspace</span>
        </div>

        <h1 className="text-xl font-display font-bold text-[var(--text-primary)] mb-8">Create Workspace</h1>

        {/* Global error */}
        {error && (
          <div className="rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] p-4 mb-6">
            <p className="text-sm text-[var(--accent-coral)]">{error}</p>
          </div>
        )}

        <form onSubmit={(e) => { e.preventDefault(); handleSubmit(); }} className="space-y-8">
          {/* Basic Info */}
          <section className="space-y-4 pb-8 border-b border-[var(--border-subtle)]">
            <h2 className="text-xs font-semibold text-[var(--text-faint)] uppercase tracking-wider">Basic Info</h2>

            <div>
              <label htmlFor="name" className={labelClass}>Workspace Name *</label>
              <input
                id="name"
                type="text"
                placeholder="my-workspace"
                value={form.name}
                onChange={(e) => set('name', e.target.value)}
                className={`${inputClass} ${fieldBorder('name')}`}
              />
              {getFieldError('name') ? (
                <p className="mt-1 text-[11px] text-[var(--accent-coral)]">{getFieldError('name')}</p>
              ) : (
                <p className="mt-1 text-[11px] text-[var(--text-faint)]">Letters, numbers, and hyphens only.</p>
              )}
            </div>

            <div>
              <label htmlFor="work_dir" className={labelClass}>Work Directory *</label>
              <input
                id="work_dir"
                type="text"
                placeholder="/home/user/workspace"
                value={form.work_dir}
                onChange={(e) => set('work_dir', e.target.value)}
                className={`${inputClass} ${fieldBorder('work_dir')}`}
              />
              {getFieldError('work_dir') ? (
                <p className="mt-1 text-[11px] text-[var(--accent-coral)]">{getFieldError('work_dir')}</p>
              ) : (
                <p className="mt-1 text-[11px] text-[var(--text-faint)]">Immutable after creation. Scopes the worker&apos;s working directory.</p>
              )}
            </div>
          </section>

          {/* Actions */}
          <div className="flex items-center justify-end gap-3 pt-2">
            <Link
              href="/admin/workspaces"
              className="px-4 py-2 rounded-[var(--radius-sm)] text-xs font-semibold text-[var(--text-faint)] hover:text-[var(--text-secondary)] transition-colors"
            >
              Cancel
            </Link>
            <button
              type="submit"
              disabled={submitting}
              className="inline-flex items-center gap-1.5 px-4 py-2 rounded-[var(--radius-sm)] text-xs font-bold uppercase tracking-wider bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {submitting && (
                <div className="w-3 h-3 border-2 border-black border-t-transparent rounded-full animate-spin" />
              )}
              {submitting ? 'Creating...' : 'Create Workspace'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

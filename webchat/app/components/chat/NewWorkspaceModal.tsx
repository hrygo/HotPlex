'use client';

import { NewWorkspaceForm } from './NewWorkspaceForm';
import type { Workspace } from '@/lib/api/workspaces';

interface NewWorkspaceModalProps {
  uid: string;
  onClose: () => void;
  onCreated: (ws: Workspace) => void;
}

export function NewWorkspaceModal({ uid, onClose, onCreated }: NewWorkspaceModalProps) {
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md rounded-xl border border-[var(--border-default)] bg-[var(--bg-elevated)] p-6 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-sm font-bold text-[var(--text-primary)]">New Workspace</h2>
          <button
            onClick={onClose}
            className="text-[var(--text-muted)] hover:text-[var(--text-primary)]"
            aria-label="Close"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>
        <NewWorkspaceForm
          uid={uid}
          onCreated={(ws) => {
            onCreated(ws);
            onClose();
          }}
          onCancel={onClose}
        />
      </div>
    </div>
  );
}

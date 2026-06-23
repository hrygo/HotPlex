'use client';

import { useState } from 'react';
import { motion } from 'framer-motion';
import type { Workspace } from '@/lib/api/workspaces';
import type { User } from '@/lib/api/auth';
import { GeneralTab } from './general-tab';
import { AIConfigTab } from './ai-config-tab';
import { ProfileTab } from './profile-tab';
import { MembersTab } from './members-tab';

interface SettingsModalProps {
  open: boolean;
  onClose: () => void;
  workspace: Workspace | null;
  currentUser: User | null;
  onWorkspaceUpdated?: (ws: Workspace) => void;
}

type TabId = 'general' | 'ai' | 'profile' | 'members';

export function SettingsModal({ open, onClose, workspace, currentUser, onWorkspaceUpdated }: SettingsModalProps) {
  const [activeTab, setActiveTab] = useState<TabId>('general');

  if (!open) return null;

  const tabs: { id: TabId; label: string }[] = [
    { id: 'general', label: 'General' },
    { id: 'ai', label: 'AI Config' },
    { id: 'profile', label: 'Profile' },
    ...(currentUser?.role === 'admin' ? [{ id: 'members' as TabId, label: 'Members' }] : []),
  ];

  return (
    <motion.div
      className="fixed inset-0 z-[300] flex items-center justify-center"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
    >
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onClose} />

      <motion.div
        className="relative w-full max-w-2xl mx-4 rounded-[var(--radius-xl)] border border-[var(--border-default)] bg-[var(--bg-surface)] backdrop-blur-2xl shadow-[0_32px_64px_rgba(0,0,0,0.5)] flex flex-col max-h-[85vh]"
        initial={{ opacity: 0, scale: 0.95, y: 20 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        transition={{ type: 'spring' as const, stiffness: 300, damping: 28 }}
      >
        <div className="px-6 pt-6 pb-3 flex items-center justify-between flex-shrink-0">
          <div>
            <h2 className="text-lg font-display font-bold text-[var(--text-primary)]">Settings</h2>
            <p className="text-xs text-[var(--text-muted)] mt-0.5 truncate">{workspace?.name ?? 'Workspace'}</p>
          </div>
          <button
            onClick={onClose}
            className="p-1.5 text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)] rounded-lg transition-all"
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <div className="px-6 border-b border-[var(--border-subtle)] flex gap-1 flex-shrink-0">
          {tabs.map((tab) => (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id)}
              className={`px-4 py-2 text-sm font-bold transition-all border-b-2 -mb-px ${
                activeTab === tab.id
                  ? 'text-[var(--accent-gold)] border-[var(--accent-gold)]'
                  : 'text-[var(--text-muted)] border-transparent hover:text-[var(--text-primary)]'
              }`}
            >
              {tab.label}
            </button>
          ))}
        </div>

        <div className="flex-1 overflow-y-auto px-6 py-5">
          {activeTab === 'general' && workspace && (
            <GeneralTab workspace={workspace} onUpdated={onWorkspaceUpdated} />
          )}
          {activeTab === 'ai' && workspace && (
            <AIConfigTab workspace={workspace} onUpdated={onWorkspaceUpdated} />
          )}
          {activeTab === 'profile' && currentUser && <ProfileTab user={currentUser} />}
          {activeTab === 'members' && currentUser?.role === 'admin' && <MembersTab />}
        </div>
      </motion.div>
    </motion.div>
  );
}

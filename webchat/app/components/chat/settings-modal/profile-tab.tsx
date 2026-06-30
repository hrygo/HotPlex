'use client';

import { useState, useRef, useEffect } from 'react';
import type { User } from '@/lib/api/auth';
import { TabPanel } from './tab-panel';

interface ProfileTabProps {
  user: User;
}

export function ProfileTab({ user }: ProfileTabProps) {
  const [copied, setCopied] = useState(false);
  const copyTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => () => {
    if (copyTimer.current) clearTimeout(copyTimer.current);
  }, []);

  const handleCopyUserId = () => {
    navigator.clipboard.writeText(user.id);
    setCopied(true);
    if (copyTimer.current) clearTimeout(copyTimer.current);
    copyTimer.current = setTimeout(() => setCopied(false), 1500);
  };

  const getInitials = (name: string) => {
    return name.slice(0, 2).toUpperCase();
  };

  const formattedCreated = user.created_at
    ? new Date(user.created_at * 1000).toLocaleString()
    : '—';
  const formattedLogin = user.last_login_at
    ? new Date(user.last_login_at * 1000).toLocaleString()
    : '—';

  return (
    <TabPanel>
      {/* Profile Header Card */}
      <div className="flex flex-col sm:flex-row items-center gap-4 p-5 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-elevated)]/40">
        {/* Large Gradient Avatar */}
        <div className="w-14 h-14 rounded-full bg-gradient-to-tr from-[var(--accent-gold)] to-[rgba(251,191,36,0.3)] flex items-center justify-center text-black font-display font-black text-lg shadow-sm border border-[var(--border-default)]">
          {getInitials(user.display_name || user.username)}
        </div>
        
        {/* User Identity Details */}
        <div className="flex-1 text-center sm:text-left min-w-0">
          <div className="flex flex-col sm:flex-row sm:items-center gap-2">
            <h2 className="text-base font-bold text-[var(--text-primary)] truncate">
              {user.display_name || user.username}
            </h2>
            <span className={`inline-block mx-auto sm:mx-0 px-2 py-0.5 rounded text-[9px] font-bold uppercase tracking-wider ${
              user.role === 'admin' 
                ? 'bg-[var(--accent-gold)]/10 text-[var(--accent-gold)] border border-[var(--accent-gold)]/20' 
                : 'bg-[var(--bg-hover)] text-[var(--text-muted)] border border-[var(--border-subtle)]'
            }`}>
              {user.role}
            </span>
          </div>
          <p className="text-xs text-[var(--text-muted)] mt-0.5">@{user.username}</p>
        </div>

        {/* Status Indicator */}
        <div className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-[var(--bg-elevated)] border border-[var(--border-subtle)]">
          <span className={`w-1.5 h-1.5 rounded-full ${user.status === 'active' ? 'bg-[var(--accent-emerald)]' : 'bg-[var(--accent-coral)]'} animate-pulse`} />
          <span className="text-[10px] font-mono font-bold uppercase tracking-wider text-[var(--text-secondary)]">
            {user.status}
          </span>
        </div>
      </div>

      {/* Profile Details List */}
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        {/* User ID Detail Card */}
        <div className="p-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-elevated)]/20 flex flex-col justify-between min-h-[80px]">
          <span className="text-[9px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest block mb-1">
            User ID
          </span>
          <div className="flex items-center justify-between gap-2 mt-1">
            <span className="text-xs font-mono text-[var(--text-muted)] truncate select-all">{user.id}</span>
            <button
              onClick={handleCopyUserId}
              title="Copy User ID"
              className="p-1 rounded bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)] transition-all cursor-pointer"
            >
              {copied ? (
                <svg className="w-3.5 h-3.5 text-[var(--accent-emerald)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
                </svg>
              ) : (
                <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 5H6a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2v-1M8 5a2 2 0 002 2h2a2 2 0 002-2M8 5a2 2 0 002 2h2a2 2 0 002-2M8 5a2 2 0 012-2h2a2 2 0 012 2m0 0h2a2 2 0 012 2v3m2 4H10m0 0l3-3m-3 3l3 3" />
                </svg>
              )}
            </button>
          </div>
        </div>

        {/* Display Name Card */}
        <div className="p-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-elevated)]/20 flex flex-col justify-between min-h-[80px]">
          <span className="text-[9px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest block mb-1">
            Display Name
          </span>
          <span className="text-xs font-bold text-[var(--text-primary)] mt-1 truncate">
            {user.display_name || 'Not configured'}
          </span>
        </div>

        {/* Account Created Card */}
        <div className="p-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-elevated)]/20 flex flex-col justify-between min-h-[80px]">
          <span className="text-[9px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest block mb-1">
            Created Time
          </span>
          <span className="text-xs font-bold text-[var(--text-primary)] mt-1 truncate">
            {formattedCreated}
          </span>
        </div>

        {/* Last Login Card */}
        <div className="p-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-elevated)]/20 flex flex-col justify-between min-h-[80px]">
          <span className="text-[9px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest block mb-1">
            Last Login
          </span>
          <span className="text-xs font-bold text-[var(--text-primary)] mt-1 truncate">
            {formattedLogin}
          </span>
        </div>
      </div>
    </TabPanel>
  );
}

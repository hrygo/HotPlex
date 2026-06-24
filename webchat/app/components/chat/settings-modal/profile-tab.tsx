'use client';

import type { User } from '@/lib/api/auth';

interface ProfileTabProps {
  user: User;
}

export function ProfileTab({ user }: ProfileTabProps) {
  const rows: { label: string; value: string; mono?: boolean }[] = [
    { label: 'Username', value: user.username },
    { label: 'Display Name', value: user.display_name || '—' },
    { label: 'Role', value: user.role },
    { label: 'Status', value: user.status },
    { label: 'User ID', value: user.id, mono: true },
    ...(user.created_at ? [{ label: 'Created', value: new Date(user.created_at * 1000).toLocaleString() }] : []),
    ...(user.last_login_at ? [{ label: 'Last Login', value: new Date(user.last_login_at * 1000).toLocaleString() }] : []),
  ];

  return (
    <div className="space-y-1">
      {rows.map((row) => (
        <div key={row.label} className="flex justify-between items-center py-2.5 border-b border-[var(--border-subtle)] last:border-0 gap-4">
          <span className="text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest flex-shrink-0">{row.label}</span>
          <span className={`text-sm text-right break-all ${row.mono ? 'font-mono text-[var(--text-muted)]' : 'text-[var(--text-primary)]'}`}>
            {row.value}
          </span>
        </div>
      ))}
    </div>
  );
}

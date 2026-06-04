'use client';

import { useEffect } from 'react';
import { usePathname, useRouter } from 'next/navigation';
import { useAdminAuth } from '@/hooks/use-admin-auth';
import { AdminNav } from '@/components/admin/admin-nav';

export function AdminShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const { state, logout } = useAdminAuth();
  const isLoginPage = pathname === '/admin/login';

  // Unauthenticated and not on login page -> redirect
  useEffect(() => {
    if (state === 'unauthenticated' && !isLoginPage) {
      router.replace('/admin/login');
    }
  }, [state, isLoginPage, router]);

  if (state === 'unauthenticated' && !isLoginPage) {
    return null;
  }

  // Checking auth state -> show spinner
  if (state === 'checking') {
    return (
      <div className="flex h-screen items-center justify-center bg-[var(--bg-base)]">
        <div className="h-8 w-8 animate-spin rounded-full border-2 border-[var(--border-default)] border-t-[var(--accent-gold)]" />
      </div>
    );
  }

  // Login page -- render children only, no shell
  if (isLoginPage) {
    return <>{children}</>;
  }

  // Authenticated -- render shell with nav + content
  return (
    <div className="flex h-screen overflow-hidden bg-[var(--bg-base)]">
      <AdminNav onLogout={logout} />
      <main className="flex-1 overflow-y-auto">
        {children}
      </main>
    </div>
  );
}

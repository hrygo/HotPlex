'use client';

import { useEffect, useState } from 'react';
import { usePathname, useRouter } from 'next/navigation';
import { useAdminAuth } from '@/hooks/use-admin-auth';
import { AdminNav } from '@/components/admin/admin-nav';
import { getMe } from '@/lib/api/auth';

// Auth channel resolved by probing the chat cookie session (issue #788 A1+A2):
// - 'cookie-admin'      : chat session present and role==admin → embedded
//                         scenario, no admin token needed (cookie channel)
// - 'non-admin'         : chat session present but role!=admin → bounce to /,
//                         never render the login page (closes the privilege
//                         escalation surface via /admin/login)
// - 'no-cookie-session' : no chat session → standalone admin-token channel
//                         (remote gateway operations fallback)
// - 'checking'          : cookie probe in flight
type Channel = 'checking' | 'cookie-admin' | 'non-admin' | 'no-cookie-session';

function CheckingSpinner() {
  return (
    <div className="flex h-screen items-center justify-center bg-[var(--bg-base)]">
      <div className="h-8 w-8 animate-spin rounded-full border-2 border-[var(--border-default)] border-t-[var(--accent-gold)]" />
    </div>
  );
}

function AdminLayout({
  children,
  onLogout,
}: {
  children: React.ReactNode;
  onLogout: () => void;
}) {
  return (
    <div className="flex h-screen overflow-hidden bg-[var(--bg-base)]">
      <AdminNav onLogout={onLogout} />
      <main className="flex-1 overflow-y-auto">{children}</main>
    </div>
  );
}

export function AdminShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const { state: tokenState, logout: tokenLogout } = useAdminAuth();
  const [channel, setChannel] = useState<Channel>('checking');
  const isLoginPage = pathname === '/admin/login';

  useEffect(() => {
    let cancelled = false;
    getMe()
      .then((u) => {
        if (cancelled) return;
        setChannel(u.role === 'admin' ? 'cookie-admin' : 'non-admin');
      })
      .catch(() => {
        if (cancelled) return;
        setChannel('no-cookie-session');
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Non-admin (has a chat session but not admin) — never expose the admin
  // surface, including /admin/login. Redirect to the chat root.
  useEffect(() => {
    if (channel === 'non-admin') {
      router.replace('/');
    }
  }, [channel, router]);

  if (channel === 'non-admin') {
    return null;
  }

  // Cookie-admin: authenticated via chat session. The login page is meaningless
  // here (no token to enter), so redirect to /admin if visited directly.
  if (channel === 'cookie-admin') {
    if (isLoginPage) {
      router.replace('/admin');
      return null;
    }
    // "Logout" in the embedded scenario = leave the admin console, return to chat.
    return <AdminLayout onLogout={() => router.replace('/')}>{children}</AdminLayout>;
  }

  // No chat session: standalone admin-token channel (remote operations).
  if (channel === 'no-cookie-session') {
    if (tokenState === 'unauthenticated' && !isLoginPage) {
      router.replace('/admin/login');
      return null;
    }
    if (tokenState === 'checking') {
      return <CheckingSpinner />;
    }
    if (isLoginPage) {
      return <>{children}</>;
    }
    return (
      <AdminLayout
        onLogout={() => {
          tokenLogout();
          router.replace('/admin/login');
        }}
      >
        {children}
      </AdminLayout>
    );
  }

  // channel === 'checking'
  return <CheckingSpinner />;
}

'use client';

import { Suspense, useEffect, useState } from 'react';
import { useRouter, useSearchParams } from 'next/navigation';
import dynamic from 'next/dynamic';
import { BrandIcon } from '@/components/icons';
import { getMe } from '@/lib/api/auth';

const ChatUI = dynamic(() => import('./components/chat/ChatContainer.assistant-ui'), {
  ssr: false,
  loading: () => <LoadingScreen text="Initialising..." />,
});

function LoadingScreen({ text }: { text: string }) {
  return (
    <div className="flex flex-col h-screen bg-[var(--bg-base)]">
      <header className="app-header bg-[rgba(15,15,18,0.6)] backdrop-blur-xl">
        <div className="header-inner">
          <div className="flex items-center gap-3">
            <BrandIcon size={42} />
            <div>
              <h1 className="text-sm font-display font-bold text-[var(--text-primary)]">HotPlex AI</h1>
              <p className="text-[10px] font-mono text-[var(--text-faint)] uppercase tracking-widest">{text}</p>
            </div>
          </div>
        </div>
      </header>
      <div className="flex-1 flex items-center justify-center relative overflow-hidden">
        <div className="bg-mesh opacity-50" />
        <div className="flex flex-col items-center gap-6 relative z-10">
          <div className="relative">
            <div className="absolute inset-0 bg-[var(--accent-gold)] opacity-15 blur-2xl rounded-full animate-pulse" />
            <BrandIcon size={64} className="relative z-10" />
          </div>
          <div className="flex items-center gap-3 px-4 py-2 rounded-full glass-dark">
            <div className="w-4 h-4 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
            <span className="text-xs font-bold tracking-tight text-[var(--text-secondary)]">CONNECTING TO GATEWAY</span>
          </div>
        </div>
      </div>
    </div>
  );
}

function InnerPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const authError = searchParams.get('auth_error');
  const [checking, setChecking] = useState(true);

  useEffect(() => {
    if (authError) {
      router.replace(`/login?auth_error=${authError}`);
      return;
    }

    const checkAuth = async () => {
      try {
        await getMe();
        setChecking(false);
      } catch {
        router.replace('/login');
      }
    };

    checkAuth();
  }, [router, authError]);

  if (checking) {
    return <LoadingScreen text="Verifying authentication..." />;
  }

  return <ChatUI />;
}

export default function Page() {
  return (
    <Suspense fallback={<LoadingScreen text="Loading app..." />}>
      <InnerPage />
    </Suspense>
  );
}

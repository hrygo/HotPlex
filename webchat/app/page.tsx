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

function OnboardingWelcome({ onClose }: { onClose: () => void }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4">
      <div className="w-full max-w-lg rounded-xl border border-[var(--border-default)] bg-[var(--bg-surface)] p-7 shadow-[var(--shadow-lg)] animate-fade-in-up">
        <div className="mb-4 flex items-center gap-3">
          <div className="flex h-11 w-11 items-center justify-center rounded-lg bg-[var(--accent-gold)]/10">
            <BrandIcon size={32} />
          </div>
          <div>
            <h2 className="font-display text-lg font-black text-[var(--text-primary)]">欢迎来到 HotPlex</h2>
            <p className="text-[11px] text-[var(--text-muted)] uppercase tracking-wider">几步开始你的第一次对话</p>
          </div>
        </div>
        <ol className="space-y-2.5 text-sm text-[var(--text-secondary)]">
          <li><span className="font-bold text-[var(--accent-gold)]">1.</span> 在设置中创建你的第一个 workspace(工作目录)</li>
          <li><span className="font-bold text-[var(--accent-gold)]">2.</span> 选择 worker 类型(claude_code / codex 等)</li>
          <li><span className="font-bold text-[var(--accent-gold)]">3.</span> 在对话框输入任务,开始编码</li>
        </ol>
        <button
          type="button"
          onClick={onClose}
          className="mt-6 w-full rounded-lg bg-[var(--accent-gold)] px-4 py-2.5 text-xs font-bold uppercase tracking-widest text-black hover:bg-[var(--accent-gold-bright)]"
        >
          开始使用
        </button>
      </div>
    </div>
  );
}

function InnerPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const authError = searchParams.get('auth_error');
  const [checking, setChecking] = useState(true);
  const [showOnboarding, setShowOnboarding] = useState(false);

  useEffect(() => {
    if (authError) {
      router.replace(`/login?auth_error=${authError}`);
      return;
    }

    const checkAuth = async () => {
      try {
        await getMe();
        setChecking(false);
        try {
          if (localStorage.getItem('hotplex.onboarding') === '1') {
            setShowOnboarding(true);
            localStorage.removeItem('hotplex.onboarding');
          }
        } catch {}
      } catch {
        router.replace('/login');
      }
    };

    checkAuth();
  }, [router, authError]);

  if (checking) {
    return <LoadingScreen text="Verifying authentication..." />;
  }

  return (
    <>
      <ChatUI />
      {showOnboarding && (
        <OnboardingWelcome onClose={() => setShowOnboarding(false)} />
      )}
    </>
  );
}

export default function Page() {
  return (
    <Suspense fallback={<LoadingScreen text="Loading app..." />}>
      <InnerPage />
    </Suspense>
  );
}

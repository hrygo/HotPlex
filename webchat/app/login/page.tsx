'use client';

import { Suspense, useEffect, useState } from 'react';
import { useRouter, useSearchParams } from 'next/navigation';
import { BrandIcon } from '@/components/icons';
import { httpBase } from '@/lib/config';
import { login, acceptInvite, getOAuthProviders, getMe, type OAuthProvider } from '@/lib/api/auth';
import { AnimatePresence, motion } from 'framer-motion';

function mapAuthError(code: string | null): string | null {
  if (!code) return null;
  switch (code) {
    case 'STATE_EXPIRED':
      return '登录状态已过期，请重新登录。';
    case 'PROVIDER_MISMATCH':
      return '第三方登录服务商不匹配。';
    case 'CSRF_DETECTED':
      return '检测到跨站请求伪造(CSRF)，请确保浏览器启用了 Cookie 并重试。';
    case 'STATE_INVALID':
      return '登录状态无效，请重新登录。';
    case 'USER_DISABLED':
      return '此账号已被管理员禁用，请联系系统管理员。';
    case 'USER_CREATE_FAILED':
      return '从单点登录(SSO)创建用户账号失败。';
    case 'CODE_EXCHANGE_FAILED':
      return 'SSO 授权码交换失败。';
    case 'ID_TOKEN_INVALID':
      return 'SSO 凭证令牌验证失败。';
    case 'IDP_ERROR':
      return '第三方登录服务商返回错误。';
    case 'UNAUTHORIZED':
      return '会话未授权，请先登录。';
    default:
      return `认证错误: ${code}`;
  }
}

function InnerLoginPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const authErrorParam = searchParams.get('auth_error');

  const [activeTab, setActiveTab] = useState<'login' | 'register'>('login');
  
  // Login fields
  const [loginUsername, setLoginUsername] = useState('');
  const [loginPassword, setLoginPassword] = useState('');
  
  // Register fields
  const [inviteCode, setInviteCode] = useState('');
  const [registerUsername, setRegisterUsername] = useState('');
  const [registerPassword, setRegisterPassword] = useState('');

  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(mapAuthError(authErrorParam) || '');
  const [providers, setProviders] = useState<OAuthProvider[]>([]);

  // Fetch OAuth Providers
  useEffect(() => {
    const fetchProviders = async () => {
      try {
        const list = await getOAuthProviders();
        setProviders(list || []);
      } catch {
        // Degrade gracefully if endpoint fails
        setProviders([]);
      }
    };
    fetchProviders();
  }, []);

  // Pre-check if already logged in, redirect to "/"
  useEffect(() => {
    const checkUser = async () => {
      try {
        const user = await getMe();
        if (user && user.id) {
          router.replace('/');
        }
      } catch {
        // Not logged in, stay on page
      }
    };
    checkUser();
  }, [router]);

  const handleLoginSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!loginUsername.trim() || !loginPassword) return;

    setLoading(true);
    setError('');
    try {
      await login(loginUsername.trim(), loginPassword);
      router.push('/');
    } catch (err: any) {
      setError(err.message || '登录失败，请检查用户名或密码。');
    } finally {
      setLoading(false);
    }
  };

  const handleRegisterSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!inviteCode.trim() || !registerUsername.trim() || !registerPassword) return;

    setLoading(true);
    setError('');
    try {
      await acceptInvite(inviteCode.trim(), registerUsername.trim(), registerPassword);
      router.push('/');
    } catch (err: any) {
      setError(err.message || '注册失败，请检查邀请码和表单项。');
    } finally {
      setLoading(false);
    }
  };

  const handleOAuthLogin = (providerName: string) => {
    setLoading(true);
    window.location.href = `${httpBase()}/api/auth/oauth/${providerName}/login`;
  };

  return (
    <div className="relative flex min-h-screen items-center justify-center bg-[var(--bg-base)] px-4">
      {/* Background aesthetics */}
      <div className="bg-mesh opacity-50" />
      <div className="noise-overlay" />

      <div className="w-full max-w-md animate-fade-in-up z-10">
        <div className="rounded-xl border border-[var(--border-default)] bg-[var(--bg-surface)] p-8 shadow-[var(--shadow-lg)] backdrop-blur-xl">
          {/* Header */}
          <div className="mb-6 text-center">
            <div className="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-xl bg-[var(--accent-gold)]/10 relative">
              <div className="absolute inset-0 bg-[var(--accent-gold)] opacity-10 blur-xl rounded-full" />
              <BrandIcon size={48} className="relative z-10 animate-float" />
            </div>
            <h1 className="font-display text-2xl font-black tracking-tight text-[var(--text-primary)]">
              HotPlex <span className="text-gradient-gold">WebChat</span>
            </h1>
            <p className="mt-1.5 text-xs text-[var(--text-muted)] tracking-wide uppercase">
              Multitenant AI Coding Interface
            </p>
          </div>

          {/* Tab Switcher */}
          <div className="mb-6 grid grid-cols-2 gap-1 rounded-lg bg-[var(--bg-elevated)] p-1 border border-[var(--border-subtle)]">
            <button
              onClick={() => {
                setActiveTab('login');
                setError('');
              }}
              className={`relative rounded-md py-2.5 text-xs font-bold uppercase tracking-wider transition-all duration-300 ${
                activeTab === 'login'
                  ? 'bg-[var(--bg-surface)] text-[var(--text-primary)] shadow-sm border border-[var(--border-subtle)]'
                  : 'text-[var(--text-muted)] hover:text-[var(--text-primary)]'
              }`}
            >
              Sign In
            </button>
            <button
              onClick={() => {
                setActiveTab('register');
                setError('');
              }}
              className={`relative rounded-md py-2.5 text-xs font-bold uppercase tracking-wider transition-all duration-300 ${
                activeTab === 'register'
                  ? 'bg-[var(--bg-surface)] text-[var(--text-primary)] shadow-sm border border-[var(--border-subtle)]'
                  : 'text-[var(--text-muted)] hover:text-[var(--text-primary)]'
              }`}
            >
              Accept Invite
            </button>
          </div>

          {/* Errors */}
          <AnimatePresence mode="wait">
            {error && (
              <motion.div
                initial={{ opacity: 0, y: -8 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, y: -8 }}
                className="mb-4 rounded-lg border border-[var(--accent-coral)]/30 bg-[var(--accent-coral)]/10 p-3 text-xs text-[var(--accent-coral)] leading-relaxed"
              >
                {error}
              </motion.div>
            )}
          </AnimatePresence>

          {/* Forms */}
          {activeTab === 'login' ? (
            <form onSubmit={handleLoginSubmit} className="space-y-4">
              <div>
                <label className="mb-1.5 block text-[10px] font-bold uppercase tracking-wider text-[var(--text-muted)]">
                  Username
                </label>
                <input
                  type="text"
                  required
                  value={loginUsername}
                  onChange={(e) => setLoginUsername(e.target.value)}
                  placeholder="Enter username"
                  className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3.5 py-2.5 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-faint)] outline-none transition-all focus:border-[var(--accent-gold)]/40 focus:ring-1 focus:ring-[var(--accent-gold)]/20"
                />
              </div>

              <div>
                <label className="mb-1.5 block text-[10px] font-bold uppercase tracking-wider text-[var(--text-muted)]">
                  Password
                </label>
                <input
                  type="password"
                  required
                  value={loginPassword}
                  onChange={(e) => setLoginPassword(e.target.value)}
                  placeholder="Enter password"
                  className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3.5 py-2.5 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-faint)] outline-none transition-all focus:border-[var(--accent-gold)]/40 focus:ring-1 focus:ring-[var(--accent-gold)]/20"
                />
              </div>

              <button
                type="submit"
                disabled={loading}
                className="w-full rounded-lg bg-[var(--accent-gold)] px-4 py-2.5 text-xs font-bold uppercase tracking-widest text-black transition-all hover:bg-[var(--accent-gold-bright)] hover:scale-[1.01] active:scale-[0.99] disabled:cursor-not-allowed disabled:opacity-30 shadow-[var(--shadow-glow)]"
              >
                {loading ? 'Processing...' : 'Sign In'}
              </button>
            </form>
          ) : (
            <form onSubmit={handleRegisterSubmit} className="space-y-4">
              <div>
                <label className="mb-1.5 block text-[10px] font-bold uppercase tracking-wider text-[var(--text-muted)]">
                  Invitation Code
                </label>
                <input
                  type="text"
                  required
                  value={inviteCode}
                  onChange={(e) => setInviteCode(e.target.value)}
                  placeholder="Paste invitation code"
                  className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3.5 py-2.5 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-faint)] outline-none transition-all focus:border-[var(--accent-gold)]/40 focus:ring-1 focus:ring-[var(--accent-gold)]/20 font-mono"
                />
              </div>

              <div>
                <label className="mb-1.5 block text-[10px] font-bold uppercase tracking-wider text-[var(--text-muted)]">
                  New Username
                </label>
                <input
                  type="text"
                  required
                  value={registerUsername}
                  onChange={(e) => setRegisterUsername(e.target.value)}
                  placeholder="Choose username"
                  className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3.5 py-2.5 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-faint)] outline-none transition-all focus:border-[var(--accent-gold)]/40 focus:ring-1 focus:ring-[var(--accent-gold)]/20"
                />
              </div>

              <div>
                <label className="mb-1.5 block text-[10px] font-bold uppercase tracking-wider text-[var(--text-muted)]">
                  New Password
                </label>
                <input
                  type="password"
                  required
                  value={registerPassword}
                  onChange={(e) => setRegisterPassword(e.target.value)}
                  placeholder="Choose password"
                  className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] px-3.5 py-2.5 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-faint)] outline-none transition-all focus:border-[var(--accent-gold)]/40 focus:ring-1 focus:ring-[var(--accent-gold)]/20"
                />
              </div>

              <button
                type="submit"
                disabled={loading}
                className="w-full rounded-lg bg-[var(--accent-gold)] px-4 py-2.5 text-xs font-bold uppercase tracking-widest text-black transition-all hover:bg-[var(--accent-gold-bright)] hover:scale-[1.01] active:scale-[0.99] disabled:cursor-not-allowed disabled:opacity-30 shadow-[var(--shadow-glow)]"
              >
                {loading ? 'Processing...' : 'Register & Join'}
              </button>
            </form>
          )}

          {/* OIDC Providers */}
          {providers.length > 0 && (
            <div className="mt-6 pt-6 border-t border-[var(--border-subtle)]">
              <p className="mb-3.5 text-center text-[10px] font-bold uppercase tracking-widest text-[var(--text-faint)]">
                Or sign in with
              </p>
              <div className="grid grid-cols-1 gap-2">
                {providers.map((p) => (
                  <button
                    key={p.name}
                    onClick={() => handleOAuthLogin(p.name)}
                    disabled={loading}
                    className="flex items-center justify-center gap-2.5 rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] hover:bg-[var(--bg-hover)] px-4 py-2.5 text-xs font-bold text-[var(--text-primary)] transition-all hover:border-[var(--border-bright)] hover:scale-[1.01]"
                  >
                    {/* SSO Icon */}
                    <svg
                      xmlns="http://www.w3.org/2000/svg"
                      fill="none"
                      viewBox="0 0 24 24"
                      strokeWidth={1.8}
                      stroke="var(--accent-gold)"
                      className="h-4 w-4"
                    >
                      <path
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        d="M15.75 5.25a3 3 0 0 1 3 3m3 0a6 6 0 0 1-7.029 5.912c-.563-.097-1.159.026-1.563.43L10.5 17.25H8.25v2.25H6v2.25H2.25v-2.818c0-.597.237-1.17.659-1.591l6.499-6.499c.404-.404.527-1 .43-1.563A6 6 0 1 1 21.75 8.25Z"
                      />
                    </svg>
                    Continue with {p.display_name}
                  </button>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

export default function LoginPage() {
  return (
    <Suspense fallback={
      <div className="flex min-h-screen items-center justify-center bg-[var(--bg-base)]">
        <div className="flex flex-col items-center gap-3">
          <div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
          <span className="text-xs text-[var(--text-muted)] tracking-wider">Loading...</span>
        </div>
      </div>
    }>
      <InnerLoginPage />
    </Suspense>
  );
}

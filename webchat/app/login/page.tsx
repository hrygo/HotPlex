'use client';

import { Suspense, useEffect, useState } from 'react';
import { useRouter, useSearchParams } from 'next/navigation';
import { BrandIcon } from '@/components/icons';
import { httpBase } from '@/lib/config';
import { login, acceptInvite, getOAuthProviders, getMe, getBootstrapStatus, type OAuthProvider } from '@/lib/api/auth';
import { AnimatePresence, motion } from 'framer-motion';

function mapAuthError(code: string | null): string | null {
  if (!code) return null;
  switch (code) {
    // 登录凭证
    case 'INVALID_CREDENTIALS':
      return '用户名或密码错误,请重试。';
    case 'USER_DISABLED':
      return '此账号已被管理员禁用,请联系系统管理员。';
    case 'NO_IDP':
      return '登录服务未就绪,请稍后再试或联系管理员。';
    // 注册 — 用户名/密码
    case 'INVALID_USERNAME':
      return '用户名格式无效:需 3-64 字符,仅 [a-zA-Z0-9_.-],且不能以 apikey: 开头。';
    case 'INVALID_PASSWORD':
      return '密码长度无效:需 8-72 字符。';
    case 'USERNAME_TAKEN':
      return '该用户名已被占用。若你刚用此邀请码注册,邀请码已消耗,请联系管理员重新发码后换名重试。';
    // 注册 — 邀请码
    case 'INVITATION_NOT_FOUND':
      return '邀请码不存在,请检查后重试。';
    case 'INVITATION_USED':
      return '邀请码已被使用(每个码仅一次)。请联系管理员重新发放。';
    case 'INVITATION_EXPIRED':
      return '邀请码已过期。请联系管理员重新发放。';
    // SSO
    case 'STATE_EXPIRED':
      return '登录状态已过期,请重新登录。';
    case 'PROVIDER_MISMATCH':
      return '第三方登录服务商不匹配。';
    case 'CSRF_DETECTED':
      return '检测到跨站请求伪造(CSRF),请确保浏览器启用了 Cookie 并重试。';
    case 'STATE_INVALID':
      return '登录状态无效,请重新登录。';
    case 'USER_CREATE_FAILED':
      return '从单点登录(SSO)创建用户账号失败。';
    case 'CODE_EXCHANGE_FAILED':
      return 'SSO 授权码交换失败。';
    case 'ID_TOKEN_INVALID':
      return 'SSO 凭证令牌验证失败。';
    case 'IDP_ERROR':
      return '第三方登录服务商返回错误。';
    case 'UNAUTHORIZED':
      return '会话未授权,请先登录。';
    case 'BAD_REQUEST':
      return '请求参数缺失,请填写完整后重试。';
    default:
      return `操作失败(${code}),请重试或联系管理员。`;
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
  const [bootstrapped, setBootstrapped] = useState<boolean | null>(null);

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

  // Detect bootstrap state: if no admin exists yet, show setup guide instead of the form.
  useEffect(() => {
    const check = async () => {
      try {
        setBootstrapped(await getBootstrapStatus());
      } catch {
        setBootstrapped(true); // degrade to normal login
      }
    };
    check();
  }, []);

  // ?invite=CODE → 预填邀请码并切到 Accept Invite tab
  useEffect(() => {
    const code = searchParams.get('invite');
    if (code) {
      setInviteCode(code);
      setActiveTab('register');
    }
  }, [searchParams]);

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
      const result = await login(loginUsername.trim(), loginPassword);
      if (result.first_login) {
        try { localStorage.setItem('hotplex.onboarding', '1'); } catch {}
      }
      router.push('/');
    } catch (err: any) {
      setError(mapAuthError(err.message) || err.message || '登录失败，请检查用户名或密码。');
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
      try { localStorage.setItem('hotplex.onboarding', '1'); } catch {}
      router.push('/');
    } catch (err: any) {
      setError(mapAuthError(err.message) || err.message || '注册失败，请检查邀请码和表单项。');
    } finally {
      setLoading(false);
    }
  };

  const handleOAuthLogin = (providerName: string) => {
    setLoading(true);
    window.location.href = `${httpBase()}/api/auth/oauth/${providerName}/login`;
  };

  if (bootstrapped === null) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-[var(--bg-base)]">
        <div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
      </div>
    );
  }

  if (bootstrapped === false) {
    const cmd = `hotplex admin create --username <name> --config configs/config-dev.yaml`;
    return (
      <div className="relative flex min-h-screen items-center justify-center bg-[var(--bg-base)] px-4">
        <div className="bg-mesh opacity-50" />
        <div className="noise-overlay" />
        <div className="w-full max-w-md animate-fade-in-up z-10 rounded-xl border border-[var(--border-default)] bg-[var(--bg-surface)] p-8 shadow-[var(--shadow-lg)] backdrop-blur-xl">
          <div className="mb-5 text-center">
            <div className="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-xl bg-[var(--accent-gold)]/10">
              <BrandIcon size={48} className="animate-float" />
            </div>
            <h1 className="font-display text-2xl font-black tracking-tight text-[var(--text-primary)]">
              初始化管理员账号
            </h1>
            <p className="mt-2 text-xs text-[var(--text-muted)] leading-relaxed">
              这是全新部署,还没有管理员账号。请在服务器上运行以下命令创建首个管理员,然后刷新此页。
            </p>
          </div>
          <div className="rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-elevated)] p-3">
            <div className="flex items-center justify-between gap-2">
              <code className="text-xs font-mono text-[var(--text-secondary)] break-all">{cmd}</code>
              <button
                type="button"
                onClick={() => navigator.clipboard?.writeText(cmd)}
                className="shrink-0 rounded-md border border-[var(--border-default)] bg-[var(--bg-surface)] px-2.5 py-1.5 text-[10px] font-bold uppercase tracking-wider text-[var(--text-muted)] hover:text-[var(--text-primary)]"
              >
                复制
              </button>
            </div>
          </div>
          <p className="mt-4 text-center text-[10px] text-[var(--text-faint)] leading-relaxed">
            密码交互式输入(≥8 字符)。用户名 [a-zA-Z0-9_.-],不可以 apikey: 开头。
          </p>
          <button
            type="button"
            onClick={() => window.location.reload()}
            className="mt-5 w-full rounded-lg bg-[var(--accent-gold)] px-4 py-2.5 text-xs font-bold uppercase tracking-widest text-black hover:bg-[var(--accent-gold-bright)]"
          >
            创建后刷新
          </button>
        </div>
      </div>
    );
  }

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

              <p className="pt-1 text-center text-[11px] text-[var(--text-muted)]">
                收到邀请码?{' '}
                <button
                  type="button"
                  onClick={() => { setActiveTab('register'); setError(''); }}
                  className="font-bold text-[var(--accent-gold)] hover:underline"
                >
                  立即注册 →
                </button>
              </p>
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
              <p className="-mt-2 mb-1 text-[10px] text-[var(--text-faint)]">
                邀请码由管理员发放。没有?请联系管理员获取。
              </p>

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

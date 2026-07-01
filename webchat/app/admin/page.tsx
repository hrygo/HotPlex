'use client';

import { useEffect, useState } from 'react';
import { listBots } from '@/lib/api/admin-bots';
import { listSessions } from '@/lib/api/admin-sessions';
import { listCronJobs } from '@/lib/api/admin-cron';
import { MetricCard } from '@/components/admin/metric-card';
import { adminFetch } from '@/lib/api/admin-client';
import { useAdminUI } from '@/context/admin-ui-context';
import { useTranslation } from 'react-i18next';

interface DashboardMetrics {
	botsTotal: number;
	botsConnected: number;
	botsDisconnected: number;
	sessionsTotal: number;
	sessionsActive: number;
	cronTotal: number;
	cronEnabled: number;
	gatewayOnline: boolean;
	dbPath: string;
	dbStatus: string;
	version: string;
}

export default function DashboardPage() {
	const { t } = useTranslation();
	const { showToast, confirm } = useAdminUI();

	const [metrics, setMetrics] = useState<DashboardMetrics>({
		botsTotal: 0,
		botsConnected: 0,
		botsDisconnected: 0,
		sessionsTotal: 0,
		sessionsActive: 0,
		cronTotal: 0,
		cronEnabled: 0,
		gatewayOnline: false,
		dbPath: '',
		dbStatus: '',
		version: '',
	});
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState<string | null>(null);

	// Uptime ticking state
	const [uptime, setUptime] = useState<number | null>(null);

	// Restart pipeline states
	const [isRestarting, setIsRestarting] = useState(false);
	const [restartStep, setRestartStep] = useState<'idle' | 'initiating' | 'offline' | 'polling' | 'completed' | 'failed'>('idle');
	const [pollingAttempts, setPollingAttempts] = useState(0);

	const fetchAllMetrics = async () => {
		try {
			setError(null);

			// Fetch health first
			let healthData: any = null;
			try {
				healthData = await adminFetch<any>('/admin/health');
			} catch (e) {
				console.warn('Health probe failed', e);  // TODO: replace with logger after logger import available in admin
			}

			const [botsRes, sessionsRes, cronRes] = await Promise.allSettled([
				listBots(),
				listSessions(1, 0),
				listCronJobs(),
			]);

			const m: DashboardMetrics = {
				botsTotal: 0,
				botsConnected: 0,
				botsDisconnected: 0,
				sessionsTotal: 0,
				sessionsActive: 0,
				cronTotal: 0,
				cronEnabled: 0,
				gatewayOnline: false,
				dbPath: healthData?.checks?.database?.path || 'sqlite.db',
				dbStatus: healthData?.checks?.database?.status || 'healthy',
				version: healthData?.version || 'v1.16.0',
			};

			if (botsRes.status === 'fulfilled') {
				const bots = botsRes.value;
				m.botsTotal = bots.length;
				m.botsConnected = bots.filter((b) => b.status === 'connected').length;
				m.botsDisconnected = bots.filter((b) => b.status !== 'connected').length;
				m.gatewayOnline = true;
			}

			if (sessionsRes.status === 'fulfilled') {
				const sessions = sessionsRes.value.sessions;
				m.sessionsTotal = sessions.length;
				m.sessionsActive = sessions.filter(
					(s) => s.state === 'active' || s.state === 'working',
				).length;
				m.gatewayOnline = true;
			}

			if (cronRes.status === 'fulfilled') {
				const jobs = cronRes.value;
				m.cronTotal = jobs.length;
				m.cronEnabled = jobs.filter((j) => j.enabled).length;
				m.gatewayOnline = true;
			}

			if (healthData?.checks?.gateway?.uptime_seconds !== undefined) {
				setUptime(healthData.checks.gateway.uptime_seconds);
			}

			// If every request failed, the gateway is unreachable.
			const allFailed =
				botsRes.status === 'rejected' &&
				sessionsRes.status === 'rejected' &&
				cronRes.status === 'rejected';

			if (allFailed) {
				const firstErr = botsRes.reason;
				setError(
					firstErr instanceof Error ? firstErr.message : t('admin:dashboard.error.unreachable', { defaultValue: 'Gateway unreachable' }),
				);
				m.gatewayOnline = false;
				setUptime(null);
			}

			setMetrics(m);
		} catch (err) {
			setError(err instanceof Error ? err.message : t('admin:dashboard.error.load_failed', { defaultValue: 'Failed to load dashboard' }));
			setUptime(null);
		} finally {
			setLoading(false);
		}
	};

	// Initial load
	useEffect(() => {
		// eslint-disable-next-line react-hooks/set-state-in-effect -- mount-time metrics fetch
		fetchAllMetrics();
	}, []);

	// Live ticking uptime
	useEffect(() => {
		if (uptime === null || !metrics.gatewayOnline || isRestarting) return;
		const timer = setInterval(() => {
			setUptime((prev) => (prev !== null ? prev + 1 : null));
		}, 1000);
		return () => clearInterval(timer);
	}, [uptime, metrics.gatewayOnline, isRestarting]);

	const formatUptime = (seconds: number | null): string => {
		if (seconds === null || seconds < 0) return t('admin:dashboard.offline', { defaultValue: 'Offline' });
		const d = Math.floor(seconds / (3600 * 24));
		const h = Math.floor((seconds % (3600 * 24)) / 3600);
		const m = Math.floor((seconds % 3600) / 60);
		const s = seconds % 60;

		const parts = [];
		if (d > 0) parts.push(`${d}${t('admin:dashboard.uptime.days', { defaultValue: 'd' })}`);
		if (h > 0) parts.push(`${h}${t('admin:dashboard.uptime.hours', { defaultValue: 'h' })}`);
		if (m > 0) parts.push(`${m}${t('admin:dashboard.uptime.minutes', { defaultValue: 'm' })}`);
		parts.push(`${s}${t('admin:dashboard.uptime.seconds', { defaultValue: 's' })}`);
		return parts.join(' ');
	};

	// Exponential backoff polling routine for health recovery. Uses adminFetch
	// so it works in both Bearer (remote ops) and cookie (embedded webchat)
	// channels — the gateway is briefly unreachable mid-restart, so fetch
	// failures just extend the backoff (issue #788 A2).
	const startRecoveryPolling = async () => {
		setRestartStep('polling');
		let delay = 500;
		const maxAttempts = 15;

		for (let attempt = 1; attempt <= maxAttempts; attempt++) {
			setPollingAttempts(attempt);
			try {
				const data = await adminFetch<{ status: string }>('/admin/health');
				if (data.status === 'healthy' || data.status === 'degraded') {
					setRestartStep('completed');
					showToast(t('admin:dashboard.success.restarted', { defaultValue: 'Gateway successfully restarted and recovered!' }), 'success');
					setTimeout(() => {
						setIsRestarting(false);
						setRestartStep('idle');
						fetchAllMetrics();
					}, 1500);
					return;
				}
			} catch (e) {
				// Gateway is currently down / starting up
			}

			await new Promise((r) => setTimeout(r, delay));
			delay = Math.min(delay * 1.5, 6000); // Backoff scaling factor
		}

		setRestartStep('failed');
		showToast(t('admin:dashboard.error.timeout', { defaultValue: 'Gateway restart polling timed out.' }), 'error');
		setTimeout(() => {
			setIsRestarting(false);
			setRestartStep('idle');
			fetchAllMetrics();
		}, 3000);
	};

	// Gateway trigger function
	const handleRestartGateway = async () => {
		const confirmed = await confirm(
			t('admin:dashboard.confirm.restart_title', { defaultValue: 'Restart HotPlex Gateway?' }),
			t('admin:dashboard.confirm.restart_body', { defaultValue: 'This will safely fork a detached process helper, flush existing HTTP connections, reload configuration, and reboot. Active WebSocket clients will temporarily disconnect.' }),
			{
				confirmLabel: t('admin:dashboard.action.restart', { defaultValue: 'Restart Gateway' }),
				cancelLabel: t('common:action.cancel', { defaultValue: 'Cancel' }),
				destructive: true,
			}
		);

		if (!confirmed) return;

		try {
			setIsRestarting(true);
			setRestartStep('initiating');

			// Fire restart API call
			await adminFetch<{ status: string }>('/admin/restart', {
				method: 'POST',
			});

			showToast(t('admin:dashboard.status.acknowledged', { defaultValue: 'Restart command acknowledged. Waiting for offline drop...' }), 'info');

			// Wait for gateway to terminate
			setRestartStep('offline');
			await new Promise((r) => setTimeout(r, 1200));

			// Start polling backoff recovery
			await startRecoveryPolling();

		} catch (err) {
			setIsRestarting(false);
			setRestartStep('idle');
			showToast(err instanceof Error ? err.message : t('admin:dashboard.error.restart_failed', { defaultValue: 'Restart request failed' }), 'error');
		}
	};

	return (
		<div className="min-h-screen bg-[var(--bg-base)] px-6 py-8">
			<div className="max-w-5xl mx-auto">
				{/* Header */}
				<div className="mb-8 flex flex-col md:flex-row md:items-center justify-between gap-4">
					<div>
						<h1 className="text-xl font-display font-bold text-[var(--text-primary)]">
							{t('admin:dashboard.title', { defaultValue: 'Dashboard' })}
						</h1>
						<p className="mt-1 text-sm text-[var(--text-muted)]">
							{t('admin:dashboard.subtitle', { defaultValue: 'Gateway overview and system status' })}
						</p>
					</div>

					{!isRestarting && (
						<button
							type="button"
							onClick={handleRestartGateway}
							disabled={loading || !metrics.gatewayOnline}
							className="px-4 py-2 text-xs font-semibold rounded-[var(--radius-md)] border border-[var(--accent-gold)]/40 bg-[var(--bg-glass)] text-[var(--accent-gold)] hover:bg-[var(--accent-gold)] hover:text-[var(--text-contrast)] disabled:opacity-50 disabled:cursor-not-allowed hover:shadow-[var(--shadow-glow)] transition-all duration-300"
						>
							{t('admin:dashboard.action.restart_gateway', { defaultValue: 'Restart Go Gateway' })}
						</button>
					)}
				</div>

				{/* Error banner */}
				{error && !isRestarting && (
					<div className="mb-6 rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] p-4">
						<p className="text-sm text-[var(--accent-coral)]">{error}</p>
					</div>
				)}

				{/* Premium Reboot Panel Overlay */}
				{isRestarting && (
					<div className="mb-8 rounded-[var(--radius-lg)] border border-[var(--border-active)] bg-[var(--bg-glass)] backdrop-blur-xl p-6 shadow-[var(--shadow-lg)] animate-fade-in-up">
						<div className="flex items-center justify-between border-b border-[var(--border-subtle)] pb-4 mb-4">
							<div className="flex items-center gap-3">
								<div className="w-2.5 h-2.5 rounded-full bg-[var(--accent-gold)] animate-pulse" />
								<h2 className="text-sm font-display font-bold text-[var(--text-primary)]">
									{t('admin:dashboard.lifecycle.title', { defaultValue: 'Gateway Reboot Lifecycle' })}
								</h2>
							</div>
							<span className="text-[10px] font-mono text-[var(--text-faint)]">
								{t('admin:dashboard.lifecycle.pgid_active', { defaultValue: 'PGID Restart Handler Active' })}
							</span>
						</div>

						<div className="grid grid-cols-1 md:grid-cols-4 gap-4 mt-6">
							{/* Step 1 */}
							<div className={`flex flex-col p-3 rounded-[var(--radius-md)] border transition-all ${
								restartStep === 'initiating'
									? 'border-[var(--accent-gold)] bg-white/5'
									: 'border-[var(--border-subtle)] opacity-60'
							}`}>
								<span className="text-[10px] uppercase font-bold text-[var(--text-faint)]">{t('admin:dashboard.lifecycle.step1', { defaultValue: 'Step 1' })}</span>
								<span className="text-xs font-semibold text-[var(--text-primary)] mt-1">{t('admin:dashboard.lifecycle.step1_title', { defaultValue: 'Initiating Handshake' })}</span>
								<p className="text-[10px] text-[var(--text-muted)] mt-1">{t('admin:dashboard.lifecycle.step1_desc', { defaultValue: 'Triggering POST /restart...' })}</p>
							</div>

							{/* Step 2 */}
							<div className={`flex flex-col p-3 rounded-[var(--radius-md)] border transition-all ${
								restartStep === 'offline'
									? 'border-[var(--accent-gold)] bg-white/5'
									: 'border-[var(--border-subtle)] opacity-60'
							}`}>
								<span className="text-[10px] uppercase font-bold text-[var(--text-faint)]">{t('admin:dashboard.lifecycle.step2', { defaultValue: 'Step 2' })}</span>
								<span className="text-xs font-semibold text-[var(--text-primary)] mt-1">{t('admin:dashboard.lifecycle.step2_title', { defaultValue: 'Connection Drop' })}</span>
								<p className="text-[10px] text-[var(--text-muted)] mt-1">{t('admin:dashboard.lifecycle.step2_desc', { defaultValue: 'Gracefully flushing sockets...' })}</p>
							</div>

							{/* Step 3 */}
							<div className={`flex flex-col p-3 rounded-[var(--radius-md)] border transition-all ${
								restartStep === 'polling'
									? 'border-[var(--accent-gold)] bg-white/5 animate-pulse'
									: 'border-[var(--border-subtle)] opacity-60'
							}`}>
								<span className="text-[10px] uppercase font-bold text-[var(--text-faint)]">{t('admin:dashboard.lifecycle.step3', { defaultValue: 'Step 3' })}</span>
								<span className="text-xs font-semibold text-[var(--text-primary)] mt-1">
									{t('admin:dashboard.lifecycle.step3_title', { defaultValue: 'Polling Health' })} {pollingAttempts > 0 && `(x${pollingAttempts})`}
								</span>
								<p className="text-[10px] text-[var(--text-muted)] mt-1">{t('admin:dashboard.lifecycle.step3_desc', { defaultValue: 'Backing off recovery checks...' })}</p>
							</div>

							{/* Step 4 */}
							<div className={`flex flex-col p-3 rounded-[var(--radius-md)] border transition-all ${
								restartStep === 'completed'
									? 'border-[var(--accent-emerald)] bg-white/5'
									: 'border-[var(--border-subtle)] opacity-60'
							}`}>
								<span className="text-[10px] uppercase font-bold text-[var(--text-faint)]">{t('admin:dashboard.lifecycle.step4', { defaultValue: 'Step 4' })}</span>
								<span className="text-xs font-semibold text-[var(--accent-emerald)] mt-1">{t('admin:dashboard.lifecycle.step4_title', { defaultValue: 'Gateway Online' })}</span>
								<p className="text-[10px] text-[var(--text-muted)] mt-1">{t('admin:dashboard.lifecycle.step4_desc', { defaultValue: 'Dashboard metrics synced.' })}</p>
							</div>
						</div>
					</div>
				)}

				{/* Gateway Control & System Details Section */}
				{!loading && !isRestarting && (
					<div className="mb-8 rounded-[var(--radius-lg)] border border-[var(--border-default)] bg-[var(--bg-glass)] backdrop-blur-xl p-6 shadow-[var(--shadow-md)]">
						<div className="flex flex-col lg:flex-row lg:items-center justify-between gap-6">
							{/* Live Ticking Uptime view */}
							<div className="flex flex-col gap-1">
								<span className="text-[10px] font-bold uppercase tracking-wider text-[var(--text-faint)]">
									{t('admin:dashboard.uptime.label', { defaultValue: 'Gateway Live Uptime' })}
								</span>
								<span className="text-4xl font-display font-bold text-[var(--text-primary)] tracking-tight tabular-nums">
									{metrics.gatewayOnline ? formatUptime(uptime) : t('admin:dashboard.offline', { defaultValue: 'Offline' })}
								</span>
								<span className="text-xs text-[var(--text-muted)] flex items-center gap-2 mt-1">
									<span className={`w-2 h-2 rounded-full inline-block ${
										metrics.gatewayOnline && metrics.dbStatus === 'healthy'
											? 'bg-[var(--accent-emerald)] shadow-[0_0_8px_var(--accent-emerald)] animate-pulse'
											: metrics.gatewayOnline
											? 'bg-[var(--accent-gold)] shadow-[0_0_8px_var(--accent-gold)]'
											: 'bg-[var(--accent-coral)]'
									}`} />
									{metrics.gatewayOnline
										? t('admin:dashboard.uptime.active', { version: metrics.version, defaultValue: `Active (Version ${metrics.version})` })
										: t('admin:dashboard.uptime.unreachable', { defaultValue: 'Unreachable' })}
								</span>
							</div>

							{/* Details table */}
							<div className="border-t lg:border-t-0 lg:border-l border-[var(--border-subtle)] pt-6 lg:pt-0 lg:pl-8 flex-1">
								<div className="grid grid-cols-1 sm:grid-cols-2 gap-4 text-xs">
									<div>
										<span className="text-[10px] uppercase tracking-wider font-bold text-[var(--text-faint)] block mb-1">
											{t('admin:dashboard.database.sqlite_label', { defaultValue: 'SQLite Database' })}
										</span>
										<span className="font-mono text-[var(--text-secondary)] break-all bg-white/5 px-2 py-1 rounded-[var(--radius-xs)] select-all block">
											{metrics.dbPath || 'sqlite.db'}
										</span>
									</div>

									<div>
										<span className="text-[10px] uppercase tracking-wider font-bold text-[var(--text-faint)] block mb-1">
											{t('admin:dashboard.database.status_label', { defaultValue: 'Database Store Status' })}
										</span>
										<span className={`font-semibold ${
											metrics.dbStatus === 'healthy'
												? 'text-[var(--accent-emerald)]'
												: 'text-[var(--accent-gold)]'
										}`}>
											{metrics.dbStatus === 'healthy' ? t('admin:dashboard.database.healthy', { defaultValue: 'Healthy (SQLite Core)' }) : t('admin:dashboard.database.degraded', { defaultValue: 'Degraded / Standby' })}
										</span>
									</div>
								</div>
							</div>
						</div>
					</div>
				)}

				{/* Loading overlay */}
				{loading && !isRestarting && (
					<div className="flex items-center justify-center py-24">
						<div className="flex flex-col items-center gap-3">
							<div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
							<span className="text-xs text-[var(--text-faint)]">
								{t('admin:dashboard.loading', { defaultValue: 'Loading dashboard...' })}
							</span>
						</div>
					</div>
				)}

				{/* Metric cards grid */}
				{!loading && (
					<div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
						{/* Bots card */}
						<MetricCard
							label={t('admin:dashboard.metrics.bots_label', { defaultValue: 'Active Agents / Bots' })}
							value={metrics.botsTotal}
							sub={t('admin:dashboard.metrics.bots_sub', { count: metrics.botsConnected, standby: metrics.botsDisconnected, defaultValue: `${metrics.botsConnected} connected, ${metrics.botsDisconnected} standby` })}
						/>

						{/* Sessions card */}
						<MetricCard
							label={t('admin:dashboard.metrics.sessions_label', { defaultValue: 'WebSocket Sessions' })}
							value={metrics.sessionsActive}
							sub={t('admin:dashboard.metrics.sessions_sub', { active: metrics.sessionsActive, total: metrics.sessionsTotal, defaultValue: `${metrics.sessionsActive} active of ${metrics.sessionsTotal} total` })}
						/>

						{/* Cron Jobs card */}
						<MetricCard
							label={t('admin:dashboard.metrics.cron_label', { defaultValue: 'Scheduled Jobs (Cron)' })}
							value={metrics.cronTotal}
							sub={t('admin:dashboard.metrics.cron_sub', { count: metrics.cronEnabled, defaultValue: `${metrics.cronEnabled} scheduler loops enabled` })}
						/>
					</div>
				)}
			</div>
		</div>
	);
}

'use client';

import { useEffect, useState, useRef } from 'react';
import { listBots } from '@/lib/api/admin-bots';
import { listSessions } from '@/lib/api/admin-sessions';
import { listCronJobs } from '@/lib/api/admin-cron';
import { MetricCard } from '@/components/admin/metric-card';
import { adminFetch, getStoredAdminConnection } from '@/lib/api/admin-client';
import { useAdminUI } from '@/context/admin-ui-context';

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
				console.warn('Health probe failed', e);
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
					firstErr instanceof Error ? firstErr.message : 'Gateway unreachable',
				);
				m.gatewayOnline = false;
				setUptime(null);
			}

			setMetrics(m);
		} catch (err) {
			setError(err instanceof Error ? err.message : 'Failed to load dashboard');
			setUptime(null);
		} finally {
			setLoading(false);
		}
	};

	// Initial load
	useEffect(() => {
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
		if (seconds === null || seconds < 0) return 'Offline';
		const d = Math.floor(seconds / (3600 * 24));
		const h = Math.floor((seconds % (3600 * 24)) / 3600);
		const m = Math.floor((seconds % 3600) / 60);
		const s = seconds % 60;

		const parts = [];
		if (d > 0) parts.push(`${d}d`);
		if (h > 0) parts.push(`${h}h`);
		if (m > 0) parts.push(`${m}m`);
		parts.push(`${s}s`);
		return parts.join(' ');
	};

	// Exponential backoff polling routine for health recovery
	const startRecoveryPolling = async () => {
		setRestartStep('polling');
		let delay = 500;
		const maxAttempts = 15;
		const conn = getStoredAdminConnection();

		if (!conn) {
			setRestartStep('failed');
			setIsRestarting(false);
			showToast('Restart aborted: admin connection key missing.', 'error');
			return;
		}

		for (let attempt = 1; attempt <= maxAttempts; attempt++) {
			setPollingAttempts(attempt);
			try {
				const res = await fetch(`${conn.url}/admin/health`, {
					headers: {
						'Authorization': `Bearer ${conn.token}`,
					},
				});

				if (res.ok) {
					const data = await res.json();
					if (data.status === 'healthy' || data.status === 'degraded') {
						setRestartStep('completed');
						showToast('Gateway successfully restarted and recovered!', 'success');
						setTimeout(() => {
							setIsRestarting(false);
							setRestartStep('idle');
							fetchAllMetrics();
						}, 1500);
						return;
					}
				}
			} catch (e) {
				// Gateway is currently down / starting up
			}

			await new Promise((r) => setTimeout(r, delay));
			delay = Math.min(delay * 1.5, 6000); // Backoff scaling factor
		}

		setRestartStep('failed');
		showToast('Gateway restart polling timed out.', 'error');
		setTimeout(() => {
			setIsRestarting(false);
			setRestartStep('idle');
			fetchAllMetrics();
		}, 3000);
	};

	// Gateway trigger function
	const handleRestartGateway = async () => {
		const confirmed = await confirm(
			'Restart HotPlex Gateway?',
			'This will safely fork a detached process helper, flush existing HTTP connections, reload configuration, and reboot. Active WebSocket clients will temporarily disconnect.',
			{
				confirmLabel: 'Restart Gateway',
				cancelLabel: 'Cancel',
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

			showToast('Restart command acknowledged. Waiting for offline drop...', 'info');

			// Wait for gateway to terminate
			setRestartStep('offline');
			await new Promise((r) => setTimeout(r, 1200));

			// Start polling backoff recovery
			await startRecoveryPolling();

		} catch (err) {
			setIsRestarting(false);
			setRestartStep('idle');
			showToast(err instanceof Error ? err.message : 'Restart request failed', 'error');
		}
	};

	return (
		<div className="min-h-screen bg-[var(--bg-base)] px-6 py-8">
			<div className="max-w-5xl mx-auto">
				{/* Header */}
				<div className="mb-8 flex flex-col md:flex-row md:items-center justify-between gap-4">
					<div>
						<h1 className="text-xl font-display font-bold text-[var(--text-primary)]">
							Dashboard
						</h1>
						<p className="mt-1 text-sm text-[var(--text-muted)]">
							Gateway overview and system status
						</p>
					</div>

					{!isRestarting && (
						<button
							onClick={handleRestartGateway}
							disabled={loading || !metrics.gatewayOnline}
							className="px-4 py-2 text-xs font-semibold rounded-[var(--radius-md)] border border-[var(--accent-gold)]/40 bg-[var(--bg-glass)] text-[var(--accent-gold)] hover:bg-[var(--accent-gold)] hover:text-[var(--text-contrast)] disabled:opacity-50 disabled:cursor-not-allowed hover:shadow-[var(--shadow-glow)] transition-all duration-300"
						>
							Restart Go Gateway
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
									Gateway Reboot Lifecycle
								</h2>
							</div>
							<span className="text-[10px] font-mono text-[var(--text-faint)]">
								PGID Restart Handler Active
							</span>
						</div>

						<div className="grid grid-cols-1 md:grid-cols-4 gap-4 mt-6">
							{/* Step 1 */}
							<div className={`flex flex-col p-3 rounded-[var(--radius-md)] border transition-all ${
								restartStep === 'initiating'
									? 'border-[var(--accent-gold)] bg-white/5'
									: 'border-[var(--border-subtle)] opacity-60'
							}`}>
								<span className="text-[10px] uppercase font-bold text-[var(--text-faint)]">Step 1</span>
								<span className="text-xs font-semibold text-[var(--text-primary)] mt-1">Initiating Handshake</span>
								<p className="text-[10px] text-[var(--text-muted)] mt-1">Triggering POST /restart...</p>
							</div>

							{/* Step 2 */}
							<div className={`flex flex-col p-3 rounded-[var(--radius-md)] border transition-all ${
								restartStep === 'offline'
									? 'border-[var(--accent-gold)] bg-white/5'
									: 'border-[var(--border-subtle)] opacity-60'
							}`}>
								<span className="text-[10px] uppercase font-bold text-[var(--text-faint)]">Step 2</span>
								<span className="text-xs font-semibold text-[var(--text-primary)] mt-1">Connection Drop</span>
								<p className="text-[10px] text-[var(--text-muted)] mt-1">Gracefully flushing sockets...</p>
							</div>

							{/* Step 3 */}
							<div className={`flex flex-col p-3 rounded-[var(--radius-md)] border transition-all ${
								restartStep === 'polling'
									? 'border-[var(--accent-gold)] bg-white/5 animate-pulse'
									: 'border-[var(--border-subtle)] opacity-60'
							}`}>
								<span className="text-[10px] uppercase font-bold text-[var(--text-faint)]">Step 3</span>
								<span className="text-xs font-semibold text-[var(--text-primary)] mt-1">
									Polling Health {pollingAttempts > 0 && `(x${pollingAttempts})`}
								</span>
								<p className="text-[10px] text-[var(--text-muted)] mt-1">Backing off recovery checks...</p>
							</div>

							{/* Step 4 */}
							<div className={`flex flex-col p-3 rounded-[var(--radius-md)] border transition-all ${
								restartStep === 'completed'
									? 'border-[var(--accent-emerald)] bg-white/5'
									: 'border-[var(--border-subtle)] opacity-60'
							}`}>
								<span className="text-[10px] uppercase font-bold text-[var(--text-faint)]">Step 4</span>
								<span className="text-xs font-semibold text-[var(--accent-emerald)] mt-1">Gateway Online</span>
								<p className="text-[10px] text-[var(--text-muted)] mt-1">Dashboard metrics synced.</p>
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
									Gateway Live Uptime
								</span>
								<span className="text-4xl font-display font-bold text-[var(--text-primary)] tracking-tight tabular-nums">
									{metrics.gatewayOnline ? formatUptime(uptime) : 'Offline'}
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
										? `Active (Version ${metrics.version})`
										: 'Unreachable'}
								</span>
							</div>

							{/* Details table */}
							<div className="border-t lg:border-t-0 lg:border-l border-[var(--border-subtle)] pt-6 lg:pt-0 lg:pl-8 flex-1">
								<div className="grid grid-cols-1 sm:grid-cols-2 gap-4 text-xs">
									<div>
										<span className="text-[10px] uppercase tracking-wider font-bold text-[var(--text-faint)] block mb-1">
											SQLite Database
										</span>
										<span className="font-mono text-[var(--text-secondary)] break-all bg-white/5 px-2 py-1 rounded select-all block">
											{metrics.dbPath || 'sqlite.db'}
										</span>
									</div>

									<div>
										<span className="text-[10px] uppercase tracking-wider font-bold text-[var(--text-faint)] block mb-1">
											Database Store Status
										</span>
										<span className={`font-semibold ${
											metrics.dbStatus === 'healthy'
												? 'text-[var(--accent-emerald)]'
												: 'text-[var(--accent-gold)]'
										}`}>
											{metrics.dbStatus === 'healthy' ? 'Healthy (SQLite Core)' : 'Degraded / Standby'}
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
								Loading dashboard...
							</span>
						</div>
					</div>
				)}

				{/* Metric cards grid */}
				{!loading && (
					<div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
						{/* Bots card */}
						<MetricCard
							label="Active Agents / Bots"
							value={metrics.botsTotal}
							sub={`${metrics.botsConnected} connected, ${metrics.botsDisconnected} standby`}
						/>

						{/* Sessions card */}
						<MetricCard
							label="WebSocket Sessions"
							value={metrics.sessionsActive}
							sub={`${metrics.sessionsActive} active of ${metrics.sessionsTotal} total`}
						/>

						{/* Cron Jobs card */}
						<MetricCard
							label="Scheduled Jobs (Cron)"
							value={metrics.cronTotal}
							sub={`${metrics.cronEnabled} scheduler loops enabled`}
						/>
					</div>
				)}
			</div>
		</div>
	);
}

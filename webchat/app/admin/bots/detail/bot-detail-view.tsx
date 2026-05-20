'use client';

import { useState, useEffect } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useSearchParams } from 'next/navigation';
import { getBot, deleteBot, updateBot } from '@/lib/api/admin-bots';
import { BotConfigEditor } from '@/components/admin/bot-config-editor';
import { SystemPromptPreview } from '@/components/admin/system-prompt-preview';
import { StatusBadge } from '@/components/admin/status-badge';
import type { BotConfigEntry } from '@/lib/types/admin';

type Policy = 'open' | 'allowlist' | 'disabled';
type WorkerType = 'claude_code' | 'open_code_server';

const selectClass =
	'w-full rounded-[var(--radius-sm)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] px-3 py-2 text-sm text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors appearance-none';

const inputClass =
	'w-full rounded-[var(--radius-sm)] bg-[var(--bg-surface)] border border-[var(--border-subtle)] px-3 py-2 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-faint)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-colors font-mono';

// ---------------------------------------------------------------------------
// TagInput — editable list of strings
// ---------------------------------------------------------------------------

function TagInput({
	label,
	value,
	onChange,
	placeholder,
}: {
	label: string;
	value: string[];
	onChange: (v: string[]) => void;
	placeholder?: string;
}) {
	const [input, setInput] = useState('');

	const add = () => {
		const trimmed = input.trim();
		if (trimmed && !value.includes(trimmed)) {
			onChange([...value, trimmed]);
		}
		setInput('');
	};

	const remove = (idx: number) => {
		onChange(value.filter((_, i) => i !== idx));
	};

	return (
		<div>
			<label className="block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5">
				{label}
			</label>
			<div className="flex flex-wrap gap-1.5 mb-2">
				{value.map((tag, i) => (
					<span
						key={i}
						className="inline-flex items-center gap-1 px-2 py-0.5 rounded-md text-xs font-mono bg-[var(--bg-hover)] text-[var(--text-secondary)] border border-[var(--border-subtle)]"
					>
						{tag}
						<button
							onClick={() => remove(i)}
							className="text-[var(--text-faint)] hover:text-[var(--accent-coral)] transition-colors"
						>
							<svg width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
								<line x1="1" y1="1" x2="9" y2="9" />
								<line x1="9" y1="1" x2="1" y2="9" />
							</svg>
						</button>
					</span>
				))}
			</div>
			<div className="flex gap-2">
				<input
					type="text"
					value={input}
					onChange={(e) => setInput(e.target.value)}
					onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); add(); } }}
					placeholder={placeholder || 'Add item...'}
					className={`${inputClass} flex-1`}
				/>
				<button
					onClick={add}
					disabled={!input.trim()}
					className="px-3 py-2 rounded-[var(--radius-sm)] text-xs font-semibold border border-[var(--border-subtle)] text-[var(--text-faint)] hover:text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] transition-colors disabled:opacity-40"
				>
					Add
				</button>
			</div>
		</div>
	);
}

// ---------------------------------------------------------------------------
// OverviewEditor
// ---------------------------------------------------------------------------

function OverviewEditor({
	bot,
	botName,
}: {
	bot: BotConfigEntry;
	botName: string;
}) {
	const cfg = bot.config;
	const [workerType, setWorkerType] = useState<WorkerType>((cfg?.worker_type as WorkerType) || 'claude_code');
	const [workDir, setWorkDir] = useState(cfg?.work_dir ?? '');
	const [saving, setSaving] = useState(false);
	const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

	const dirty =
		workerType !== ((cfg?.worker_type as WorkerType) || 'claude_code') ||
		workDir !== (cfg?.work_dir ?? '');

	const handleSave = async () => {
		setSaving(true);
		setMessage(null);
		try {
			await updateBot(botName, {
				worker_type: workerType,
				work_dir: workDir || undefined,
			});
			setMessage({ type: 'success', text: 'Updated. Restart gateway to apply.' });
			setTimeout(() => setMessage(null), 3000);
		} catch (err) {
			setMessage({ type: 'error', text: err instanceof Error ? err.message : 'Failed to update' });
		} finally {
			setSaving(false);
		}
	};

	return (
		<div>
			<div className="space-y-4">
				{/* Bot ID — read-only */}
				<div>
					<label className="block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5">
						Bot ID
					</label>
					<div className="px-4 py-3 rounded-xl bg-[var(--bg-surface)] border border-[var(--border-subtle)]">
						<p className="text-sm text-[var(--text-primary)] font-mono break-all">{bot.bot_id}</p>
					</div>
				</div>

				<div className="grid grid-cols-2 gap-3">
					<div>
						<label className="block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5">
							Worker Type
						</label>
						<select value={workerType} onChange={(e) => setWorkerType(e.target.value as WorkerType)} className={selectClass}>
							<option value="claude_code">claude_code</option>
							<option value="open_code_server">open_code_server</option>
						</select>
					</div>
					<div>
						<label className="block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5">
							Work Dir
						</label>
						<input
							type="text"
							value={workDir}
							onChange={(e) => setWorkDir(e.target.value)}
							placeholder="/home/user/workspace"
							className={inputClass}
						/>
					</div>
				</div>

				<div>
					<label className="block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5">
						Connected At
					</label>
					<div className="px-4 py-3 rounded-xl bg-[var(--bg-surface)] border border-[var(--border-subtle)]">
						<p className="text-sm text-[var(--text-primary)]">{bot.connected_at || '—'}</p>
					</div>
				</div>
			</div>

			{message && (
				<div
					className={`mt-4 px-4 py-3 rounded-[var(--radius-md)] text-xs ${
						message.type === 'success'
							? 'bg-[var(--accent-emerald-glow)] text-[var(--accent-emerald)]'
							: 'bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] text-[var(--accent-coral)]'
					}`}
				>
					{message.text}
				</div>
			)}

			<div className="flex items-center justify-between mt-6 pt-6 border-t border-[var(--border-subtle)]">
				<button
					onClick={handleSave}
					disabled={saving || !dirty}
					className="px-4 py-2 rounded-[var(--radius-sm)] text-xs font-bold uppercase tracking-wider bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
				>
					{saving ? 'Saving...' : 'Save Changes'}
				</button>
			</div>
		</div>
	);
}

// ---------------------------------------------------------------------------
// AccessEditor — full access control + STT/TTS editing
// ---------------------------------------------------------------------------

function AccessEditor({
	initial,
	botName,
}: {
	initial: BotConfigEntry['config'];
	botName: string;
}) {
	const [dmPolicy, setDmPolicy] = useState<Policy>((initial?.dm_policy as Policy) || 'open');
	const [groupPolicy, setGroupPolicy] = useState<Policy>((initial?.group_policy as Policy) || 'open');
	const [requireMention, setRequireMention] = useState(initial?.require_mention ?? false);
	const [allowFrom, setAllowFrom] = useState<string[]>(initial?.allow_from ?? []);
	const [allowDMFrom, setAllowDMFrom] = useState<string[]>(initial?.allow_dm_from ?? []);
	const [allowGroupFrom, setAllowGroupFrom] = useState<string[]>(initial?.allow_group_from ?? []);
	const [sttProvider, setSttProvider] = useState(initial?.stt?.provider ?? '');
	const [ttsProvider, setTtsProvider] = useState(initial?.tts?.provider ?? '');
	const [ttsVoice, setTtsVoice] = useState(initial?.tts?.voice ?? '');
	const [saving, setSaving] = useState(false);
	const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

	const dirty =
		dmPolicy !== ((initial?.dm_policy as Policy) || 'open') ||
		groupPolicy !== ((initial?.group_policy as Policy) || 'open') ||
		requireMention !== (initial?.require_mention ?? false) ||
		JSON.stringify(allowFrom) !== JSON.stringify(initial?.allow_from ?? []) ||
		JSON.stringify(allowDMFrom) !== JSON.stringify(initial?.allow_dm_from ?? []) ||
		JSON.stringify(allowGroupFrom) !== JSON.stringify(initial?.allow_group_from ?? []) ||
		sttProvider !== (initial?.stt?.provider ?? '') ||
		ttsProvider !== (initial?.tts?.provider ?? '') ||
		ttsVoice !== (initial?.tts?.voice ?? '');

	const handleSave = async () => {
		setSaving(true);
		setMessage(null);
		try {
			const updates: Record<string, unknown> = {
				dm_policy: dmPolicy,
				group_policy: groupPolicy,
				require_mention: requireMention,
				allow_from: allowFrom,
				allow_dm_from: allowDMFrom,
				allow_group_from: allowGroupFrom,
			};
			if (sttProvider) updates.stt = { provider: sttProvider };
			if (ttsProvider || ttsVoice) updates.tts = { provider: ttsProvider, voice: ttsVoice };

			await updateBot(botName, updates);
			setMessage({ type: 'success', text: 'Access control updated. Restart gateway to apply.' });
			setTimeout(() => setMessage(null), 3000);
		} catch (err) {
			setMessage({ type: 'error', text: err instanceof Error ? err.message : 'Failed to update' });
		} finally {
			setSaving(false);
		}
	};

	return (
		<div className="space-y-6">
			{/* Access Control */}
			<section>
				<h3 className="text-xs font-semibold text-[var(--text-faint)] uppercase tracking-wider mb-3">
					Access Control
				</h3>
				<div className="space-y-4">
					<div className="grid grid-cols-2 gap-3">
						<div>
							<label className="block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5">
								DM Policy
							</label>
							<select value={dmPolicy} onChange={(e) => setDmPolicy(e.target.value as Policy)} className={selectClass}>
								<option value="open">Open</option>
								<option value="allowlist">Allowlist</option>
								<option value="disabled">Disabled</option>
							</select>
						</div>
						<div>
							<label className="block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5">
								Group Policy
							</label>
							<select value={groupPolicy} onChange={(e) => setGroupPolicy(e.target.value as Policy)} className={selectClass}>
								<option value="open">Open</option>
								<option value="allowlist">Allowlist</option>
								<option value="disabled">Disabled</option>
							</select>
						</div>
					</div>

					<div className="flex items-center gap-3 px-1">
						<input
							id="require_mention"
							type="checkbox"
							checked={requireMention}
							onChange={(e) => setRequireMention(e.target.checked)}
							className="h-4 w-4 rounded border-[var(--border-subtle)] bg-[var(--bg-surface)] accent-[var(--accent-gold)]"
						/>
						<label htmlFor="require_mention" className="text-sm text-[var(--text-secondary)]">
							Require mention in group messages
						</label>
					</div>

					<TagInput
						label="Allow From"
						value={allowFrom}
						onChange={setAllowFrom}
						placeholder="User or channel ID..."
					/>
					<TagInput
						label="Allow DM From"
						value={allowDMFrom}
						onChange={setAllowDMFrom}
						placeholder="User ID..."
					/>
					<TagInput
						label="Allow Group From"
						value={allowGroupFrom}
						onChange={setAllowGroupFrom}
						placeholder="Channel or group ID..."
					/>
				</div>
			</section>

			{/* Voice */}
			<section>
				<h3 className="text-xs font-semibold text-[var(--text-faint)] uppercase tracking-wider mb-3">
					Voice (STT/TTS)
				</h3>
				<div className="grid grid-cols-3 gap-3">
					<div>
						<label className="block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5">
							STT Provider
						</label>
						<select value={sttProvider} onChange={(e) => setSttProvider(e.target.value)} className={selectClass}>
							<option value="">Default</option>
							<option value="local">Local</option>
							<option value="feishu">Feishu</option>
							<option value="feishu+local">Feishu + Local</option>
						</select>
					</div>
					<div>
						<label className="block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5">
							TTS Provider
						</label>
						<select value={ttsProvider} onChange={(e) => setTtsProvider(e.target.value)} className={selectClass}>
							<option value="">Default</option>
							<option value="edge">Edge</option>
							<option value="edge+moss">Edge + MOSS</option>
						</select>
					</div>
					<div>
						<label className="block text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-wider mb-1.5">
							TTS Voice
						</label>
						<input
							type="text"
							value={ttsVoice}
							onChange={(e) => setTtsVoice(e.target.value)}
							placeholder="e.g. zh-CN-XiaoxiaoNeural"
							className={inputClass}
						/>
					</div>
				</div>
			</section>

			{message && (
				<div
					className={`px-4 py-3 rounded-[var(--radius-md)] text-xs ${
						message.type === 'success'
							? 'bg-[var(--accent-emerald-glow)] text-[var(--accent-emerald)]'
							: 'bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] text-[var(--accent-coral)]'
					}`}
				>
					{message.text}
				</div>
			)}

			<div className="flex justify-end">
				<button
					onClick={handleSave}
					disabled={saving || !dirty}
					className="px-4 py-2 rounded-[var(--radius-sm)] text-xs font-bold uppercase tracking-wider bg-[var(--accent-gold)] text-black hover:bg-[var(--accent-gold-bright)] transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
				>
					{saving ? 'Saving...' : 'Save Changes'}
				</button>
			</div>
		</div>
	);
}

// ---------------------------------------------------------------------------
// DeleteButton
// ---------------------------------------------------------------------------

function DeleteButton({ botName }: { botName: string }) {
	const router = useRouter();
	const [confirming, setConfirming] = useState(false);
	const [deleting, setDeleting] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const handleDelete = async () => {
		setDeleting(true);
		setError(null);
		try {
			await deleteBot(botName);
			router.push('/admin/bots');
		} catch (err) {
			setError(err instanceof Error ? err.message : 'Failed to delete bot');
			setDeleting(false);
		}
	};

	if (confirming) {
		return (
			<div className="mt-6 pt-6 border-t border-[var(--border-subtle)]">
				<div className="rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.06)] border border-[rgba(244,63,94,0.12)] p-4">
					<p className="text-sm text-[var(--accent-coral)] font-medium mb-3">
						Delete &ldquo;{botName}&rdquo;? This action cannot be undone.
					</p>
					{error && <p className="text-xs text-[var(--accent-coral)] mb-3">{error}</p>}
					<div className="flex items-center gap-2">
						<button
							onClick={handleDelete}
							disabled={deleting}
							className="px-3 py-1.5 rounded-[var(--radius-sm)] text-xs font-bold bg-[var(--accent-coral)] text-white hover:opacity-90 transition-opacity disabled:opacity-50"
						>
							{deleting ? 'Deleting...' : 'Yes, Delete'}
						</button>
						<button
							onClick={() => { setConfirming(false); setError(null); }}
							className="px-3 py-1.5 rounded-[var(--radius-sm)] text-xs font-semibold text-[var(--text-faint)] hover:text-[var(--text-secondary)] transition-colors"
						>
							Cancel
						</button>
					</div>
				</div>
			</div>
		);
	}

	return (
		<div className="mt-6 pt-6 border-t border-[var(--border-subtle)]">
			<button
				onClick={() => setConfirming(true)}
				className="px-3 py-1.5 rounded-[var(--radius-sm)] text-xs font-semibold text-[var(--accent-coral)] border border-[rgba(244,63,94,0.2)] hover:bg-[rgba(244,63,94,0.06)] transition-colors"
			>
				Delete Bot
			</button>
		</div>
	);
}

// ---------------------------------------------------------------------------
// Main BotDetailView
// ---------------------------------------------------------------------------

type TabKey = 'overview' | 'config' | 'access';

const TABS: { key: TabKey; label: string }[] = [
	{ key: 'overview', label: 'Overview' },
	{ key: 'config', label: 'Config' },
	{ key: 'access', label: 'Access' },
];

export function BotDetailView() {
	const searchParams = useSearchParams();
	const name = searchParams.get('name') ?? '';

	const [bot, setBot] = useState<BotConfigEntry | null>(null);
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState<string | null>(null);
	const [activeTab, setActiveTab] = useState<TabKey>('overview');

	useEffect(() => {
		if (!name) return;
		let cancelled = false;
		setLoading(true);
		getBot(name)
			.then((data: BotConfigEntry) => {
				if (!cancelled) setBot(data);
			})
			.catch((err: unknown) => {
				if (!cancelled) setError(String(err));
			})
			.finally(() => {
				if (!cancelled) setLoading(false);
			});
		return () => { cancelled = true; };
	}, [name]);

	if (!name) {
		return (
			<div className="flex items-center justify-center min-h-[60vh]">
				<p className="text-sm text-[var(--text-faint)]">No bot name specified</p>
			</div>
		);
	}

	if (loading) {
		return (
			<div className="flex items-center justify-center min-h-[60vh]">
				<div className="flex flex-col items-center gap-3">
					<div className="w-6 h-6 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
					<span className="text-xs text-[var(--text-faint)]">Loading bot...</span>
				</div>
			</div>
		);
	}

	if (error || !bot) {
		return (
			<div className="flex flex-col items-center justify-center min-h-[60vh] gap-4">
				<div className="rounded-[var(--radius-md)] bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.15)] p-4">
					<p className="text-sm text-[var(--accent-coral)]">{error || 'Bot not found'}</p>
				</div>
				<Link
					href="/admin/bots"
					className="text-xs text-[var(--accent-gold)] hover:underline"
				>
					Back to Bots
				</Link>
			</div>
		);
	}

	return (
		<div className="max-w-5xl mx-auto px-6 py-8">
			{/* Breadcrumb */}
			<div className="flex items-center gap-2 mb-6 text-xs text-[var(--text-faint)]">
				<Link
					href="/admin/bots"
					className="hover:text-[var(--text-secondary)] transition-colors flex items-center gap-1"
				>
					<svg width="12" height="12" viewBox="0 0 16 16" fill="none">
						<path d="M10 3L5 8l5 5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
					</svg>
					Bots
				</Link>
				<span className="text-[var(--border-subtle)]">/</span>
				<span className="text-[var(--text-secondary)]">{name}</span>
			</div>

			{/* Header */}
			<div className="flex items-center gap-3 mb-6">
				<h1 className="text-xl font-display font-bold text-[var(--text-primary)]">{name}</h1>
				<span className="px-2 py-0.5 rounded-full text-[10px] font-mono font-semibold bg-[var(--bg-elevated)] text-[var(--text-secondary)] border border-[var(--border-subtle)] uppercase">
					{bot.platform}
				</span>
				<StatusBadge status={bot.status} />
				<SystemPromptPreview botName={name} />
			</div>

			{/* Tabs */}
			<div className="flex gap-1 mb-6 border-b border-[var(--border-subtle)]">
				{TABS.map((tab) => (
					<button
						key={tab.key}
						onClick={() => setActiveTab(tab.key)}
						className={`px-4 py-2.5 text-xs font-semibold transition-colors border-b-2 -mb-px ${
							activeTab === tab.key
								? 'border-[var(--accent-gold)] text-[var(--accent-gold)]'
								: 'border-transparent text-[var(--text-faint)] hover:text-[var(--text-secondary)]'
						}`}
					>
						{tab.label}
					</button>
				))}
			</div>

			{/* Tab content */}
			{activeTab === 'overview' && (
				<div>
					<OverviewEditor bot={bot} botName={name} />
					<DeleteButton botName={name} />
				</div>
			)}

			{activeTab === 'config' && <BotConfigEditor botName={name} />}

			{activeTab === 'access' && <AccessEditor initial={bot.config} botName={name} />}
		</div>
	);
}

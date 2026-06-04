'use client';

import React, { createContext, useContext, useState, useCallback } from 'react';

type ToastType = 'info' | 'success' | 'warning' | 'error';

interface Toast {
	id: string;
	message: string;
	type: ToastType;
}

interface ModalOptions {
	confirmLabel?: string;
	cancelLabel?: string;
	destructive?: boolean;
}

interface ModalState {
	isOpen: boolean;
	title: string;
	message: string;
	mode: 'alert' | 'confirm';
	type: ToastType;
	confirmLabel?: string;
	cancelLabel?: string;
	destructive?: boolean;
	resolve: (value: boolean) => void;
}

interface AdminUIContextType {
	showToast: (message: string, type: ToastType, duration?: number) => void;
	alert: (title: string, message: string, type?: ToastType) => Promise<void>;
	confirm: (title: string, message: string, options?: ModalOptions) => Promise<boolean>;
}

const AdminUIContext = createContext<AdminUIContextType | null>(null);

export function useAdminUI() {
	const context = useContext(AdminUIContext);
	if (!context) {
		throw new Error('useAdminUI must be used within an AdminUIProvider');
	}
	return context;
}

export function AdminUIProvider({ children }: { children: React.ReactNode }) {
	const [toasts, setToasts] = useState<Toast[]>([]);
	const [modal, setModal] = useState<ModalState | null>(null);

	const showToast = useCallback((message: string, type: ToastType, duration = 3000) => {
		const id = Math.random().toString(36).substring(2, 9);
		setToasts((prev) => [...prev, { id, message, type }]);

		setTimeout(() => {
			setToasts((prev) => prev.filter((t) => t.id !== id));
		}, duration);
	}, []);

	const alert = useCallback((title: string, message: string, type: ToastType = 'info'): Promise<void> => {
		return new Promise<void>((resolve) => {
			setModal({
				isOpen: true,
				title,
				message,
				mode: 'alert',
				type,
				confirmLabel: 'OK',
				resolve: () => {
					setModal(null);
					resolve();
				},
			});
		});
	}, []);

	const confirm = useCallback((
		title: string,
		message: string,
		options: ModalOptions = {}
	): Promise<boolean> => {
		return new Promise<boolean>((resolve) => {
			setModal({
				isOpen: true,
				title,
				message,
				mode: 'confirm',
				type: 'warning',
				confirmLabel: options.confirmLabel || 'Confirm',
				cancelLabel: options.cancelLabel || 'Cancel',
				destructive: options.destructive ?? false,
				resolve: (result) => {
					setModal(null);
					resolve(result);
				},
			});
		});
	}, []);

	return (
		<AdminUIContext.Provider value={{ showToast, alert, confirm }}>
			{children}

			{/* Custom Promise-resolved Overlays & Dialogs */}
			{modal && modal.isOpen && (
				<div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-md transition-all duration-300">
					<div className={`relative w-full max-w-md border border-[var(--border-default)] bg-[var(--bg-glass)] backdrop-blur-xl p-6 rounded-[var(--radius-lg)] shadow-[var(--shadow-lg)] transition-all duration-300 transform scale-100 ${
						modal.destructive ? 'hover:border-[var(--accent-coral)]/30' : 'hover:border-[var(--accent-gold)]/20'
					}`}>
						{/* Glow effect matching active state */}
						<div className={`absolute -inset-px -z-10 rounded-[var(--radius-lg)] opacity-10 blur-sm pointer-events-none transition-all ${
							modal.destructive
								? 'bg-[var(--accent-coral)]'
								: modal.type === 'error'
								? 'bg-[var(--accent-coral)]'
								: modal.type === 'success'
								? 'bg-[var(--accent-emerald)]'
								: 'bg-[var(--accent-gold)]'
						}`} />

						<div className="flex items-start gap-4">
							{/* Icon Indicator */}
							<div className={`flex-shrink-0 w-10 h-10 rounded-full flex items-center justify-center bg-white/5 border border-white/10`}>
								{modal.type === 'success' && (
									<svg className="w-5 h-5 text-[var(--accent-emerald)]" fill="none" viewBox="0 0 24 24" stroke="currentColor">
										<path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
									</svg>
								)}
								{modal.type === 'error' && (
									<svg className="w-5 h-5 text-[var(--accent-coral)]" fill="none" viewBox="0 0 24 24" stroke="currentColor">
										<path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
									</svg>
								)}
								{modal.type === 'warning' && (
									<svg className="w-5 h-5 text-[var(--accent-gold)]" fill="none" viewBox="0 0 24 24" stroke="currentColor">
										<path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
									</svg>
								)}
								{modal.type === 'info' && (
									<svg className="w-5 h-5 text-[var(--accent-blue)]" fill="none" viewBox="0 0 24 24" stroke="currentColor">
										<path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
									</svg>
								)}
							</div>

							<div className="flex-1 min-w-0">
								<h3 className="text-base font-display font-bold text-[var(--text-primary)]">
									{modal.title}
								</h3>
								<p className="mt-2 text-sm text-[var(--text-secondary)] leading-relaxed">
									{modal.message}
								</p>
							</div>
						</div>

						{/* Action Buttons */}
						<div className="mt-6 flex justify-end gap-3">
							{modal.mode === 'confirm' && (
								<button
									onClick={() => modal.resolve(false)}
									className="px-4 py-2 text-xs font-medium rounded-[var(--radius-md)] border border-[var(--border-default)] bg-transparent text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] transition-all"
								>
									{modal.cancelLabel}
								</button>
							)}
							<button
								onClick={() => modal.resolve(true)}
								className={`px-4 py-2 text-xs font-semibold rounded-[var(--radius-md)] shadow-sm transition-all ${
									modal.destructive
										? 'bg-[var(--accent-coral)] text-white hover:bg-[var(--accent-coral)]/90'
										: 'bg-[var(--accent-gold)] text-[var(--text-contrast)] hover:bg-[var(--accent-gold-bright)]'
								}`}
							>
								{modal.confirmLabel}
							</button>
						</div>
					</div>
				</div>
			)}

			{/* Sliding Toast Notifications Container */}
			<div className="fixed bottom-6 right-6 z-50 flex flex-col gap-3 max-w-sm w-full pointer-events-none">
				{toasts.map((t) => (
					<div
						key={t.id}
						className="pointer-events-auto w-full border border-[var(--border-default)] bg-[var(--bg-glass)] backdrop-blur-xl p-4 rounded-[var(--radius-md)] shadow-[var(--shadow-md)] flex items-start gap-3 animate-fade-in-up transform transition-all duration-300"
					>
						{/* Indicator Icon */}
						<div className="flex-shrink-0 mt-0.5">
							{t.type === 'success' && (
								<span className="w-2 h-2 rounded-full block bg-[var(--accent-emerald)] shadow-[0_0_8px_var(--accent-emerald)] animate-pulse" />
							)}
							{t.type === 'error' && (
								<span className="w-2 h-2 rounded-full block bg-[var(--accent-coral)] shadow-[0_0_8px_var(--accent-coral)] animate-pulse" />
							)}
							{t.type === 'warning' && (
								<span className="w-2 h-2 rounded-full block bg-[var(--accent-gold)] shadow-[0_0_8px_var(--accent-gold)] animate-pulse" />
							)}
							{t.type === 'info' && (
								<span className="w-2 h-2 rounded-full block bg-[var(--accent-blue)] shadow-[0_0_8px_var(--accent-blue)] animate-pulse" />
							)}
						</div>

						<div className="flex-1 min-w-0">
							<p className="text-xs text-[var(--text-secondary)] leading-normal font-medium">
								{t.message}
							</p>
						</div>

						<button
							onClick={() => setToasts((prev) => prev.filter((item) => item.id !== t.id))}
							className="text-[var(--text-faint)] hover:text-[var(--text-primary)] transition-colors"
						>
							<svg className="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
								<path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
							</svg>
						</button>
					</div>
				))}
			</div>
		</AdminUIContext.Provider>
	);
}

import React from "react";

export function BrandIcon({ size = 28, style, className }: { size?: number; style?: React.CSSProperties; className?: string }) {
  return (
    <div className={`brand-icon flex items-center justify-center ${className ?? ""}`} style={{ width: size, height: size, ...style }}>
      {/* eslint-disable-next-line @next/next/no-img-element -- brand logo, fixed display size */}
      <img
        src="/logo.webp"
        alt="HotPlex Logo"
        style={{ width: "100%", height: "100%", objectFit: "contain" }}
      />
    </div>
  );
}

export const WORKER_DISPLAY: Record<string, string> = {
  claude_code: "Claude",
  codex_cli: "Codex",
  opencode_server: "OpenCode",
  acp: "ACP",
};

export function WorkerIcon({ type, className }: { type: string; className?: string }) {
  switch (type) {
    case 'claude_code':
      return (
        <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <path d="M12 2L14.5 9.5L22 12L14.5 14.5L12 22L9.5 14.5L2 12L9.5 9.5Z" />
        </svg>
      );
    case 'opencode_server':
      return (
        <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <rect x="2" y="2" width="20" height="8" rx="2" />
          <rect x="2" y="14" width="20" height="8" rx="2" />
          <line x1="6" y1="6" x2="6.01" y2="6" />
          <line x1="6" y1="18" x2="6.01" y2="18" />
        </svg>
      );
    case 'codex_cli':
      return (
        <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <polyline points="4 17 10 11 4 5" />
          <line x1="12" y1="19" x2="20" y2="19" />
        </svg>
      );
    case 'acp':
      return (
        <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <path d="M13 2L3 14h9l-1 10 10-12h-9l1-10z" />
        </svg>
      );
    default:
      return (
        <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <circle cx="12" cy="12" r="10" />
          <line x1="12" y1="16" x2="12" y2="12" />
          <line x1="12" y1="8" x2="12.01" y2="8" />
        </svg>
      );
  }
}

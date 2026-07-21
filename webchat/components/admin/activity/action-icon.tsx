'use client';

// ActionIcon renders a small inline SVG that classifies an audit action by its
// first path segment (auth/session/message/tool/admin/system). Mirrors the
// heroicons outline style used elsewhere in the admin shell (viewBox 0 0 24 24,
// stroke=currentColor) and is color-tinted per category via className.
//
// The category lookup is shared with the timeline table and the drawer header
// so the icon + color stay consistent across surfaces. Unknown prefixes fall
// back to a neutral dot.

export type ActionCategory = 'auth' | 'session' | 'message' | 'tool' | 'permission' | 'question' | 'elicitation' | 'admin' | 'system' | 'other';

export function actionCategory(action: string): ActionCategory {
  const head = action.split('.', 1)[0];
  switch (head) {
    case 'auth':
      return 'auth';
    case 'session':
      return 'session';
    case 'message':
      return 'message';
    case 'tool':
      return 'tool';
    case 'permission':
      return 'permission';
    case 'question':
      return 'question';
    case 'elicitation':
      return 'elicitation';
    case 'admin':
      return 'admin';
    case 'system':
      return 'system';
    default:
      return 'other';
  }
}

const CATEGORY_COLOR: Record<ActionCategory, string> = {
  auth: 'text-[var(--accent-blue)]',
  session: 'text-[var(--accent-emerald)]',
  message: 'text-[var(--accent-blue)]',
  tool: 'text-[var(--accent-violet,#a78bfa)]',
  permission: 'text-[var(--accent-emerald)]',
  question: 'text-[var(--accent-blue)]',
  elicitation: 'text-[var(--accent-violet,#a78bfa)]',
  admin: 'text-[var(--accent-amber)]',
  system: 'text-[var(--text-muted)]',
  other: 'text-[var(--text-faint)]',
};

function CategoryGlyph({ category }: { category: ActionCategory }) {
  const common = {
    xmlns: 'http://www.w3.org/2000/svg',
    fill: 'none',
    viewBox: '0 0 24 24',
    strokeWidth: 1.8,
    stroke: 'currentColor',
    className: 'w-3.5 h-3.5',
  } as const;
  switch (category) {
    case 'auth':
      // lock
      return (
        <svg {...common}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M16.5 10.5V6.75a4.5 4.5 0 1 0-9 0v3.75m-.75 0h10.5a1.5 1.5 0 0 1 1.5 1.5v6.75a1.5 1.5 0 0 1-1.5 1.5H6.75A1.5 1.5 0 0 1 5.25 19.5v-6.75a1.5 1.5 0 0 1 1.5-1.5Z" />
        </svg>
      );
    case 'session':
      // window/stack
      return (
        <svg {...common}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M3.75 6.75h16.5v10.5H3.75zM3.75 9.75h16.5M6.75 6.75v.01" />
        </svg>
      );
    case 'message':
      // chat bubble
      return (
        <svg {...common}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M4.5 5.25h15a1.5 1.5 0 0 1 1.5 1.5v8.25a1.5 1.5 0 0 1-1.5 1.5H9.75l-4.5 3.75v-3.75H4.5a1.5 1.5 0 0 1-1.5-1.5V6.75a1.5 1.5 0 0 1 1.5-1.5Z" />
        </svg>
      );
    case 'tool':
      // wrench
      return (
        <svg {...common}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M11.42 15.17 6 9.75a4.5 4.5 0 0 1 6.01-6.01l-2.34 2.34 2.25 2.25 2.34-2.34A4.5 4.5 0 0 1 15.17 12l5.42 5.42a2.1 2.1 0 0 1-2.97 2.97Z" />
        </svg>
      );
    case 'permission':
      // key / shield check
      return (
        <svg {...common}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M9 12.75 11.25 15 15 9.75m-3-7.036A11.959 11.959 0 0 1 3.598 6 11.99 11.99 0 0 0 3 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285Z" />
        </svg>
      );
    case 'question':
      // question mark circle
      return (
        <svg {...common}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M9.879 7.519c1.171-1.025 3.071-1.025 4.242 0 1.172 1.025 1.172 2.687 0 3.712-.203.179-.43.326-.67.442-.745.361-1.45.999-1.45 1.827v.75M12 18h.01M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" />
        </svg>
      );
    case 'elicitation':
      // sparkles
      return (
        <svg {...common}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M9.813 15.904 9 18.75l-.813-2.846a4.5 4.5 0 0 0-3.09-3.09L2.25 12l2.846-.813a4.5 4.5 0 0 0 3.09-3.09L9 5.25l.813 2.846a4.5 4.5 0 0 0 3.09 3.09L15.75 12l-2.846.813a4.5 4.5 0 0 0-3.09 3.09ZM18.259 8.715 18 9.75l-.259-1.035a3.375 3.375 0 0 0-2.455-2.456L14.25 6l1.036-.259a3.375 3.375 0 0 0 2.455-2.456L18 2.25l.259 1.035a3.375 3.375 0 0 0 2.456 2.456L21.75 6l-1.035.259a3.375 3.375 0 0 0-2.456 2.456Z" />
        </svg>
      );
    case 'admin':
      // shield
      return (
        <svg {...common}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M12 3 4.5 6v5.25c0 4.5 3.15 7.95 7.5 9 4.35-1.05 7.5-4.5 7.5-9V6L12 3Z" />
        </svg>
      );
    case 'system':
      // gear
      return (
        <svg {...common}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M9.594 3.94c.09-.542.56-.94 1.11-.94h2.59c.55 0 1.02.398 1.11.94l.21 1.06a7.7 7.7 0 0 1 1.7.98l1.03-.42a1.2 1.2 0 0 1 1.45.47l1.3 2.25c.29.5.17 1.13-.28 1.5l-.85.66a7.6 7.6 0 0 1 0 1.96l.85.66c.45.37.57 1 .28 1.5l-1.3 2.25a1.2 1.2 0 0 1-1.45.47l-1.03-.42a7.7 7.7 0 0 1-1.7.98l-.21 1.06c-.09.542-.56.94-1.11.94h-2.59c-.55 0-1.02-.398-1.11-.94l-.21-1.06a7.7 7.7 0 0 1-1.7-.98l-1.03.42a1.2 1.2 0 0 1-1.45-.47l-1.3-2.25a1.2 1.2 0 0 1 .28-1.5l.85-.66a7.6 7.6 0 0 1 0-1.96l-.85-.66a1.2 1.2 0 0 1-.28-1.5l1.3-2.25a1.2 1.2 0 0 1 1.45-.47l1.03.42a7.7 7.7 0 0 1 1.7-.98l.21-1.06Z" />
          <path strokeLinecap="round" strokeLinejoin="round" d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z" />
        </svg>
      );
    default:
      return (
        <svg {...common}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M12 12h.01" />
        </svg>
      );
  }
}

export function ActionIcon({ action, className }: { action: string; className?: string }) {
  const category = actionCategory(action);
  return (
    <span className={`inline-flex items-center justify-center ${CATEGORY_COLOR[category]} ${className ?? ''}`}>
      <CategoryGlyph category={category} />
    </span>
  );
}

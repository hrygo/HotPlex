'use client';

import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { getHighlighter } from '@/lib/highlight';

// JsonViewer renders a pretty-printed, syntax-highlighted JSON value inside a
// scrollable <pre>. Used by the audit drawer to surface detail_json with the
// same highlight.js pipeline as the chat code blocks (lib/highlight.ts, json
// language already registered).
//
// Behavior:
//   - empty / "{}" → localized "no detail" placeholder
//   - invalid JSON → fall back to the raw string, monospace, no highlight
//   - valid JSON → JSON.stringify(,2) → highlight async → dangerouslySetInnerHTML
//
// Highlighting is asynchronous (getHighlighter lazy-loads highlight.js). We
// render the plain pretty-printed JSON immediately and swap in the highlighted
// HTML once the highlighter resolves, so the content is readable before the
// highlight lands.
export function JsonViewer({ json, emptyLabel }: { json: string; emptyLabel?: string }) {
  const { t } = useTranslation('admin');
  const [html, setHtml] = useState<string>('');

  const trimmed = (json ?? '').trim();
  const isEmpty = trimmed === '' || trimmed === '{}';
  const emptyText = emptyLabel ?? t('activity.drawer.no_detail', { defaultValue: 'No detail' });

  // Parse once; keep the raw pretty string for the pre-highlight render and as
  // the fallback when JSON is invalid.
  let pretty = trimmed;
  let invalid = false;
  if (!isEmpty) {
    try {
      pretty = JSON.stringify(JSON.parse(trimmed), null, 2);
    } catch {
      invalid = true;
    }
  }

  useEffect(() => {
    if (isEmpty || invalid) {
      // Render branches for empty/invalid don't read `html`, so nothing to
      // clear. Skip the async highlight entirely.
      return;
    }
    let cancelled = false;
    getHighlighter()
      .then((hljs) => {
        if (cancelled) return;
        try {
          const out = hljs.highlight(pretty, { language: 'json' }).value;
          setHtml(out);
        } catch {
          setHtml('');
        }
      })
      .catch(() => setHtml(''));
    return () => {
      // Reset on re-run (json changed) so a stale highlighted string never
      // outlives its source value.
      cancelled = true;
      setHtml('');
    };
    // pretty is derived from json; re-run when json changes.
  }, [pretty, isEmpty, invalid]);

  if (isEmpty) {
    return <div className="text-xs text-[var(--text-faint)] italic">{emptyText}</div>;
  }
  if (invalid) {
    return (
      <pre className="whitespace-pre-wrap break-words font-mono text-[11px] text-[var(--text-muted)] rounded-md bg-[var(--bg-hover)]/40 p-2.5 max-h-48 overflow-y-auto border border-[var(--border-subtle)]/40">
        {trimmed}
      </pre>
    );
  }
  return (
    <pre className="whitespace-pre-wrap break-words font-mono text-[11px] leading-normal text-[var(--text-secondary)] rounded-md bg-[var(--bg-hover)]/40 p-2.5 overflow-x-auto max-h-48 overflow-y-auto border border-[var(--border-subtle)]/40">
      {html ? <code dangerouslySetInnerHTML={{ __html: html }} /> : <code>{pretty}</code>}
    </pre>
  );
}

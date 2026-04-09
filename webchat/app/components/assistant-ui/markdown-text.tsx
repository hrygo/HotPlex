'use client';

import ReactMarkdown from 'react-markdown';
import type { Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeHighlight from 'rehype-highlight';
import CopyButton from '../ui/CopyButton';

const REMARK_PLUGINS = [remarkGfm];
const REHYPE_PLUGINS = [rehypeHighlight];

export function MarkdownText({ text }: { text: string }) {
  return (
    <div className="markdown-content">
      <ReactMarkdown
        remarkPlugins={REMARK_PLUGINS}
        rehypePlugins={REHYPE_PLUGINS}
        components={MARKDOWN_COMPONENTS}
      >
        {text}
      </ReactMarkdown>
    </div>
  );
}

function CodeBlockWrapper({ children, className }: { children: string; className?: string }) {
  const language = className?.match(/language-(\w+)/)?.[1] ?? null;

  return (
    <div className="code-block-wrapper">
      <div className="code-block-header">
        <span className="code-lang-label">{language || 'code'}</span>
        <CopyButton text={children} className="code-copy-btn" />
      </div>
      <pre style={{ margin: 0, borderRadius: 0, border: 'none', boxShadow: 'none' }}>
        <code className={className}>{children}</code>
      </pre>
    </div>
  );
}

const MARKDOWN_COMPONENTS: Components = {
  pre: ({ children }) => <>{children}</>,
  code: ({ className, children }) => {
    const isInline = !className;
    const codeText = String(children).replace(/\n$/, '');
    if (isInline) return <code>{codeText}</code>;
    return <CodeBlockWrapper className={className}>{codeText}</CodeBlockWrapper>;
  },
  a: ({ href, children }) => (
    <a href={href} target="_blank" rel="noopener noreferrer" style={{ color: 'var(--accent-emerald)' }}>
      {children}
    </a>
  ),
  table: ({ children }) => (
    <div style={{ overflowX: 'auto', margin: '0.5rem 0' }}>
      <table style={{ minWidth: '100%' }}>{children}</table>
    </div>
  ),
};

"use client";

import { useState, useEffect } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { useCopyToClipboard } from "@/lib/hooks/useCopyToClipboard";
import { getHighlighter, type Highlighter } from "@/lib/highlight";
import { useTheme } from "@/context/ThemeContext";
import { useTranslation } from "react-i18next";

function escapeHtml(text: string): string {
    return text
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#39;");
}

function CodeBlock({
    raw,
    lang,
    highlighted,
}: {
    raw: string;
    lang: string;
    highlighted: string;
}) {
    const { t } = useTranslation('chat');
    const { copied, copy } = useCopyToClipboard();
    const [isExpanded, setIsExpanded] = useState(false);
    const lineCount = raw.split("\n").length;
    const isExpandable = lineCount > 10;
    const showContent = !isExpandable || isExpanded;

    const handleCopy = (e: React.MouseEvent) => {
        e.stopPropagation();
        copy(raw);
    };

    return (
        <div className="relative group/code my-6 rounded-[var(--radius-md)] overflow-hidden border border-[var(--border-default)] bg-[var(--bg-base)] shadow-sm">
            <div
                className={`flex items-center justify-between px-4 py-2 bg-[var(--bg-surface)] border-b border-[var(--border-subtle)] ${isExpandable ? "cursor-pointer hover:bg-[var(--bg-hover)]" : ""}`}
                onClick={() => isExpandable && setIsExpanded(!isExpanded)}
            >
                <div className="flex items-center gap-2">
                    {isExpandable && (
                        <svg
                            className={`w-3 h-3 transition-transform duration-200 ${isExpanded ? "rotate-90" : ""}`}
                            fill="none"
                            stroke="currentColor"
                            viewBox="0 0 24 24"
                        >
                            <path
                                strokeLinecap="round"
                                strokeLinejoin="round"
                                strokeWidth={2.5}
                                d="M9 5l7 7-7 7"
                            />
                        </svg>
                    )}
                    <span className="text-[10px] font-mono font-bold tracking-widest text-[var(--text-faint)] uppercase">
                        {lang || t('label.code')}{" "}
                        {isExpandable && t('text.code_lines', { count: lineCount })}
                    </span>
                </div>
                <div className="flex items-center gap-3">
                    <button
                        onClick={handleCopy}
                        className="text-[10px] font-mono font-bold tracking-wider text-[var(--text-muted)] hover:text-[var(--accent-gold)] transition-colors flex items-center gap-1.5"
                    >
                        {copied ? (
                            <>
                                <svg
                                    className="w-3 h-3"
                                    fill="none"
                                    stroke="currentColor"
                                    viewBox="0 0 24 24"
                                >
                                    <path
                                        strokeLinecap="round"
                                        strokeLinejoin="round"
                                        strokeWidth={2.5}
                                        d="M5 13l4 4L19 7"
                                    />
                                </svg>
                                {t('action.copied')}
                            </>
                        ) : (
                            <>
                                <svg
                                    className="w-3 h-3"
                                    fill="none"
                                    stroke="currentColor"
                                    viewBox="0 0 24 24"
                                >
                                    <path
                                        strokeLinecap="round"
                                        strokeLinejoin="round"
                                        strokeWidth={2}
                                        d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 012-2v-8a2 2 0 01-2-2h-8a2 2 0 01-2 2v8a2 2 0 012 2z"
                                    />
                                </svg>
                                {t('action.copy')}
                            </>
                        )}
                    </button>
                </div>
            </div>
            <div
                className="p-4 w-full max-w-full overflow-x-auto custom-scrollbar transition-[max-height] duration-300 ease-in-out relative"
                style={{
                    maxHeight: showContent ? "none" : "200px",
                    overflowY: showContent ? "auto" : "hidden",
                }}
            >
                <code
                    className="block font-mono text-[13px] leading-relaxed whitespace-pre-wrap break-all"
                    dangerouslySetInnerHTML={{ __html: highlighted }}
                />
                {!showContent && (
                    <div
                        className="absolute bottom-0 left-0 right-0 h-12 bg-gradient-to-t from-[var(--bg-base)] to-transparent flex items-end justify-center pb-2 cursor-pointer"
                        onClick={() => setIsExpanded(true)}
                    >
                        <span className="text-[10px] font-bold text-[var(--accent-gold)] tracking-widest bg-[var(--bg-surface)] px-2 py-1 rounded border border-[var(--border-subtle)] shadow-lg">
                            {t('action.show_more')}
                        </span>
                    </div>
                )}
            </div>
        </div>
    );
}

export function MarkdownText({ text }: { text: string }) {
    const [hljs, setHljs] = useState<Highlighter | null>(null);
    const { theme } = useTheme();

    useEffect(() => {
        getHighlighter().then(setHljs);
    }, []);

    if (!text) return null;

    return (
        <div
            className={`prose ${theme === "dark" ? "prose-invert" : ""} max-w-none`}
        >
            <ReactMarkdown
                remarkPlugins={[remarkGfm]}
                components={{
                    pre: ({ children }) => <>{children}</>,
                    code: ({ className, children, ...props }) => {
                        const raw = String(children).replace(/\n$/, "");
                        const langMatch = /language-(\w+)/.exec(
                            className ?? "",
                        );
                        const lang = langMatch?.[1] ?? "";

                        if (!className) {
                            const isTree = /├──|└──|├─|└─|│|──|\|--/.test(raw) || raw.includes("\n");
                            if (isTree) {
                                return (
                                    <code className="block w-full overflow-x-auto whitespace-pre font-mono text-[13px] leading-relaxed bg-[var(--bg-elevated)] text-[var(--accent-gold-bright)] px-4 py-3 my-3 rounded-[var(--radius-sm)] border border-[var(--border-subtle)]">
                                        {raw}
                                    </code>
                                );
                            }
                            return (
                                <code className="bg-[var(--bg-elevated)] text-[var(--accent-gold-bright)] px-1.5 py-0.5 rounded-[var(--radius-xs)] text-[0.9em] font-mono border border-[var(--border-subtle)]">
                                    {raw}
                                </code>
                            );
                        }

                        let highlighted: string;
                        if (hljs && lang && hljs.getLanguage(lang)) {
                            highlighted = hljs.highlight(raw, {
                                language: lang,
                                ignoreIllegals: true,
                            }).value;
                        } else {
                            highlighted = escapeHtml(raw);
                        }

                        return (
                            <CodeBlock
                                raw={raw}
                                lang={lang}
                                highlighted={highlighted}
                            />
                        );
                    },
                    a: ({ href, children }) => (
                        <a
                            href={href}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-[var(--accent-gold)] hover:underline underline-offset-4 decoration-[var(--accent-gold)]"
                        >
                            {children}
                        </a>
                    ),
                    table: ({ children }) => (
                        <div className="my-6 overflow-x-auto rounded-[var(--radius-lg)] border border-[var(--border-default)]">
                            <table className="min-w-full divide-y divide-[var(--border-subtle)] bg-[var(--bg-surface)]">
                                {children}
                            </table>
                        </div>
                    ),
                    th: ({ children }) => (
                        <th className="px-4 py-3 text-left text-xs font-bold uppercase tracking-wider text-[var(--text-muted)] bg-[var(--bg-elevated)]">
                            {children}
                        </th>
                    ),
                    td: ({ children }) => (
                        <td className="px-4 py-3 text-sm text-[var(--text-secondary)] border-t border-[var(--border-subtle)]">
                            {children}
                        </td>
                    ),
                }}
            >
                {text}
            </ReactMarkdown>
        </div>
    );
}

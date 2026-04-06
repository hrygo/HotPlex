"use client";

import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import type { TextMessagePartComponent } from "@assistant-ui/react";
// eslint-disable-next-line @typescript-eslint/no-var-requires
const hljs = require("highlight.js");

/**
 * Escape HTML special characters for safe insertion.
 */
function escapeHtml(text: string): string {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

/**
 * MarkdownText - Renders message text content as formatted Markdown.
 *
 * Used as the `Text` component in MessagePrimitive.Parts.
 * Supports GFM (tables, strikethrough, task lists) and code syntax highlighting.
 */
export const MarkdownText: TextMessagePartComponent = ({ text }) => {
  if (!text) return null;

  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm]}
      components={{
        // Custom pre: just pass through children (removes outer wrapper)
        pre: ({ children }) => <>{children}</>,
        // Custom code: handle both inline and block code
        code: ({ className, children, ...props }) => {
          const raw = String(children).replace(/\n$/, "");
          const langMatch = /language-(\w+)/.exec(className ?? "");
          const lang = langMatch?.[1] ?? "";

          if (!className) {
            // Inline code — escape and render without className
            return (
              <code
                style={{
                  background: "#f3f4f6",
                  padding: "0.1em 0.3em",
                  borderRadius: "0.25rem",
                  fontSize: "0.875em",
                  fontFamily: "ui-monospace, monospace",
                }}
                {...(props as React.HTMLAttributes<HTMLElement>)}
              >
                {raw}
              </code>
            );
          }

          // Code block — syntax highlight with hljs
          let highlighted: string;
          if (lang && hljs.getLanguage(lang)) {
            highlighted = hljs.highlight(raw, {
              language: lang,
              ignoreIllegals: true,
            }).value;
          } else {
            highlighted = escapeHtml(raw);
          }

          return (
            <code
              style={{
                display: "block",
                background: "#1e1e1e",
                color: "#d4d4d4",
                padding: "1rem",
                borderRadius: "0.5rem",
                overflowX: "auto",
                margin: "0.5rem 0",
                fontSize: "0.875em",
                fontFamily: "ui-monospace, SFMono-Regular, monospace",
              }}
              dangerouslySetInnerHTML={{ __html: highlighted }}
              {...(props as React.HTMLAttributes<HTMLElement>)}
            />
          );
        },
      }}
    >
      {text}
    </ReactMarkdown>
  );
};

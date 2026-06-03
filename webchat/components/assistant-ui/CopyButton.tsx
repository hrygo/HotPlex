"use client";

import { useState } from "react";
import { motion, AnimatePresence } from "framer-motion";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function CopyButton({ message, onCopy }: { message: any; onCopy?: () => void }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    let text = "";
    if (typeof message.content === 'string') {
      text = message.content;
    } else if (Array.isArray(message.content)) {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      text = message.content.map((p: any) => p.text || "").filter(Boolean).join("\n\n");
    }

    if (text) {
      navigator.clipboard.writeText(text);
      setCopied(true);
      onCopy?.();
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <button onClick={handleCopy} className={`copy-btn ${copied ? 'copy-btn-success' : ''}`}>
      <AnimatePresence mode="wait" initial={false}>
        {copied ? (
          <motion.div key="check" initial={{ y: 2, opacity: 0 }} animate={{ y: 0, opacity: 1 }} exit={{ y: -2, opacity: 0 }} className="flex items-center gap-1.5">
            <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={3} d="M5 13l4 4L19 7" />
            </svg>
            <span>COPIED</span>
          </motion.div>
        ) : (
          <motion.div key="copy" initial={{ y: 2, opacity: 0 }} animate={{ y: 0, opacity: 1 }} exit={{ y: -2, opacity: 0 }} className="flex items-center gap-1.5">
            <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M8 7v8a2 2 0 002 2h6M8 7V5a2 2 0 012-2h4.586a1 1 0 01.707.293l4.414 4.414a1 1 0 01.293.707V15a2 2 0 01-2 2h-2M8 7H6a2 2 0 00-2 2v10a2 2 0 002 2h8a2 2 0 002-2v-2" />
            </svg>
            <span>COPY</span>
          </motion.div>
        )}
      </AnimatePresence>
    </button>
  );
}

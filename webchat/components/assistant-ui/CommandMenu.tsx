"use client";

import { useState, useEffect, useMemo, useRef, useCallback } from "react";
import type { SkillEntry, SkillStatus } from "@/lib/ai-sdk-transport/client/types";

export interface Command {
  key: string;
  label: string;
  description: string;
  type: "slash" | "skill";
  /** Invokability for skill entries; the menu disables unavailable ones (issue #957). */
  status?: SkillStatus;
}

const SLASH_COMMANDS: Command[] = [
  { key: "/gc", label: "/gc", description: "Trigger garbage collection and session cleanup", type: "slash" },
  { key: "/reset", label: "/reset", description: "Reset current session and clear history", type: "slash" },
  { key: "/park", label: "/park", description: "Park the current session to save resources", type: "slash" },
  { key: "/new", label: "/new", description: "Create a fresh new session", type: "slash" },
  { key: "/cd", label: "/cd", description: "Switch working directory and create new session", type: "slash" },
  { key: "/skills", label: "/skills", description: "List currently loaded skills and their usage", type: "slash" },
  { key: "/help", label: "/help", description: "Show available commands and documentation", type: "slash" },
];

interface CommandMenuProps {
  inputValue: string;
  onSelect: (command: Command) => void;
  isOpen: boolean;
  onClose: () => void;
  skills?: SkillEntry[];
}

export function CommandMenu({ inputValue, onSelect, isOpen, onClose, skills }: CommandMenuProps) {
  const [selectedIndex, setSelectedIndex] = useState(0);
  const selectedRef = useRef<HTMLButtonElement>(null);

  const scrollToSelected = useCallback(() => {
    selectedRef.current?.scrollIntoView({ block: "nearest" });
  }, []);

  const allCommands: Command[] = useMemo(() => [
    ...SLASH_COMMANDS,
    ...(skills ?? []).map(s => ({
      key: `/${s.name}`,
      label: `/${s.name}`,
      description: s.description || `${s.name} skill`,
      type: "skill" as const,
      status: s.status,
    })),
  ], [skills]);

  // Filter commands — "/" mode shows both slash commands and skills
  const isSlash = inputValue.startsWith("/");
  const filterText = isSlash ? inputValue.slice(1).toLowerCase() : inputValue.toLowerCase();

  const filtered = allCommands.filter(cmd => {
    if (!isSlash) {
      if (cmd.type !== "skill") return false;
      if (!filterText) return false;
    }
    if (!filterText) return true;
    return cmd.key.toLowerCase().includes(filterText) ||
           cmd.description.toLowerCase().includes(filterText);
  });
  // NOTE: No .slice() limit — container has max-h-[320px] + overflow-y-auto
  // and scrollIntoView handles keyboard navigation. A hard cap hides skills.

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- reset selection on input change
    setSelectedIndex(0);
    scrollToSelected();
  }, [inputValue, scrollToSelected]);

  useEffect(() => {
    if (!isOpen) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setSelectedIndex(prev => (prev + 1) % filtered.length);
        requestAnimationFrame(scrollToSelected);
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setSelectedIndex(prev => (prev - 1 + filtered.length) % filtered.length);
        requestAnimationFrame(scrollToSelected);
      } else if (e.key === "Enter" && filtered.length > 0) {
        e.preventDefault();
        const selected = filtered[selectedIndex];
        if (selected.type !== "skill" || selected.status !== "unavailable") {
          onSelect(selected);
        }
      } else if (e.key === "Escape") {
        onClose();
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- scrollToSelected is a stable DOM helper
  }, [isOpen, filtered, selectedIndex, onSelect, onClose]);

  if (!isOpen || filtered.length === 0) return null;

  return (
    <div
      style={{ zIndex: 99999, pointerEvents: 'auto' }}
      className="absolute bottom-full left-0 right-0 mb-2 rounded-[var(--radius-lg)] bg-[var(--bg-surface)] border border-[var(--border-default)] shadow-[var(--shadow-lg)] backdrop-blur-2xl overflow-hidden"
    >
      <div className="px-3 py-2 text-[10px] font-mono font-bold text-[var(--text-faint)] uppercase tracking-widest border-b border-[var(--border-subtle)] flex justify-between items-center bg-[rgba(255,255,255,0.02)]">
        <span>{isSlash ? "System Commands" : "Available Skills"}</span>
        <span className="opacity-50">↑↓ to navigate · Enter to select</span>
      </div>

      <div className="max-h-[320px] overflow-y-auto custom-scrollbar">
        {filtered.map((cmd, i) => {
          const unavailable = cmd.type === "skill" && cmd.status === "unavailable";
          return (
          <button
            key={cmd.key}
            ref={i === selectedIndex ? selectedRef : undefined}
            disabled={unavailable}
            className={`w-full px-4 py-3 text-left flex flex-col gap-0.5 transition-all ${
              unavailable
                ? "opacity-45 cursor-not-allowed"
                : i === selectedIndex
                  ? "bg-[var(--bg-hover)] translate-x-1"
                  : "hover:bg-[rgba(255,255,255,0.02)]"
            }`}
            onClick={() => !unavailable && onSelect(cmd)}
            onMouseEnter={() => setSelectedIndex(i)}
          >
            <div className="flex items-center gap-2">
              <span className={`text-xs font-bold ${unavailable ? "line-through decoration-[var(--accent-coral)]/60 text-[var(--text-faint)]" : i === selectedIndex ? "text-[var(--accent-gold)]" : "text-[var(--text-primary)]"}`}>
                {cmd.label}
              </span>
              {cmd.type === "slash" ? (
                <span className="text-[9px] px-1.5 py-0.5 rounded bg-[rgba(251,191,36,0.1)] text-[var(--accent-gold)] font-mono font-bold uppercase">CMD</span>
              ) : cmd.status === "unavailable" ? (
                <span className="text-[9px] px-1.5 py-0.5 rounded bg-[rgba(248,113,113,0.12)] text-[var(--accent-coral)] font-mono font-bold uppercase">UNAVAILABLE</span>
              ) : cmd.status === "discoverable" ? (
                <span className="text-[9px] px-1.5 py-0.5 rounded bg-[rgba(148,163,184,0.12)] text-[var(--text-faint)] font-mono font-bold uppercase">DISCOVERABLE</span>
              ) : (
                <span className="text-[9px] px-1.5 py-0.5 rounded bg-[rgba(52,211,153,0.1)] text-[var(--accent-emerald)] font-mono font-bold uppercase">SKILL</span>
              )}
            </div>
            <p className="text-[11px] text-[var(--text-muted)] line-clamp-1 italic">
              {cmd.description}
            </p>
          </button>
          );
        })}
      </div>
    </div>
  );
}

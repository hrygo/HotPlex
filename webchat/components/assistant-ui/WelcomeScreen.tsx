"use client";

import { BrandIcon } from "@/components/icons";
import type { Suggestion } from "./thread-helpers";
import { useTranslation } from "react-i18next";

export function WelcomeScreen({ suggestions, onSuggestionClick }: { suggestions?: readonly Suggestion[]; onSuggestionClick?: (prompt: string) => void }) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col items-center justify-center py-24 text-center">
      <div className="relative mb-10 flex items-center justify-center">
        <div className="absolute inset-0 bg-[var(--accent-gold)] opacity-10 blur-[100px] rounded-full scale-[2.5]" />

        {/* Orbital rings */}
        <div className="absolute w-36 h-36 border border-[var(--accent-gold)] opacity-20 rounded-full animate-[spin_12s_linear_infinite]" />
        <div className="absolute w-44 h-44 border border-[var(--accent-emerald)] opacity-10 rounded-full animate-[spin_18s_linear_infinite_reverse]" />

        {/* Orbiting particles */}
        <div className="absolute w-full h-full animate-[orbit_5s_linear_infinite]">
           <div className="w-2.5 h-2.5 rounded-full bg-[var(--accent-gold)] shadow-[0_0_10px_var(--accent-gold)]" />
        </div>

        <BrandIcon size={112} className="relative z-10 animate-float" />
      </div>
      <h1 className="text-5xl font-display font-bold tracking-tight mb-4 text-[var(--text-primary)]">HotPlex</h1>
      <p className="text-xl text-[var(--text-muted)] font-medium max-w-lg mx-auto leading-relaxed">
        {t('chat:welcome.tagline', { defaultValue: 'Next-generation autonomous workspace.' })}
      </p>
      {suggestions && suggestions.length > 0 && (
        <div className="flex flex-wrap justify-center gap-3 mt-8 max-w-2xl">
          {suggestions.map((s) => (
            <button
              key={s.title}
              onClick={() => onSuggestionClick?.(s.prompt)}
              className="group px-4 py-2.5 rounded-[var(--radius-md)] bg-[var(--bg-elevated)] border border-[var(--border-subtle)] hover:border-[var(--accent-gold)]/30 hover:bg-[var(--bg-hover)] transition-all active:scale-[0.97] text-left"
            >
              <span className="text-[9px] font-display font-bold text-[var(--accent-gold)] uppercase tracking-wider">{s.label}</span>
              <p className="text-[12px] text-[var(--text-secondary)] mt-0.5 leading-snug">{s.title}</p>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

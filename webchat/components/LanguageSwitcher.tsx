"use client";

import { useLanguage } from "@/lib/i18n/use-language";
import { motion, AnimatePresence } from "framer-motion";

export function LanguageSwitcher({ variant = "icon" }: { variant?: "icon" | "inline" }) {
  const { locale, changeLanguage, supported } = useLanguage();

  const labels: Record<string, string> = {
    en: "English",
    "zh-CN": "简体中文",
  };

  const handleToggle = () => {
    const nextIdx = (supported.indexOf(locale) + 1) % supported.length;
    void changeLanguage(supported[nextIdx]);
  };

  if (variant === "inline") {
    return (
      <div className="flex gap-2">
        {supported.map((lng) => (
          <button
            key={lng}
            type="button"
            onClick={() => void changeLanguage(lng)}
            className={`px-3 py-1.5 rounded-[var(--radius-sm)] text-xs font-bold transition-all ${
              locale === lng
                ? "bg-[var(--accent-gold)] text-black"
                : "border border-[var(--border-default)] bg-[var(--bg-elevated)] text-[var(--text-secondary)] hover:text-[var(--text-primary)] hover:border-[var(--border-bright)]"
            }`}
          >
            {labels[lng]}
          </button>
        ))}
      </div>
    );
  }

  const tooltipText = locale === "zh-CN" ? "Switch to English" : "切换至中文";

  return (
    <button
      type="button"
      onClick={handleToggle}
      className={`p-2 rounded-[var(--radius-sm)] transition-all active:scale-95 flex items-center justify-center relative overflow-hidden focus:outline-none ${
        locale === 'zh-CN'
          ? 'text-[var(--accent-gold)] hover:text-[var(--accent-gold-bright)] hover:bg-[var(--bg-hover)]'
          : 'text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)]'
      }`}
      title={tooltipText}
    >
      <AnimatePresence mode="wait" initial={false}>
        {locale === "zh-CN" ? (
          <motion.div
            key="zh"
            initial={{ opacity: 0, rotate: -90, scale: 0.8 }}
            animate={{ opacity: 1, rotate: 0, scale: 1 }}
            exit={{ opacity: 0, rotate: 90, scale: 0.8 }}
            transition={{ duration: 0.2, ease: "easeInOut" }}
            className="w-5 h-5 flex items-center justify-center font-sans text-xs font-bold select-none"
          >
            中
          </motion.div>
        ) : (
          <motion.div
            key="en"
            initial={{ opacity: 0, rotate: -90, scale: 0.8 }}
            animate={{ opacity: 1, rotate: 0, scale: 1 }}
            exit={{ opacity: 0, rotate: 90, scale: 0.8 }}
            transition={{ duration: 0.2, ease: "easeInOut" }}
            className="w-5 h-5 flex items-center justify-center font-mono text-[10px] font-bold tracking-tight select-none"
          >
            EN
          </motion.div>
        )}
      </AnimatePresence>
    </button>
  );
}

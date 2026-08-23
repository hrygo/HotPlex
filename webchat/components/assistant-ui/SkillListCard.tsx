"use client";

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { SkillEntry, SkillStatus } from "@/lib/ai-sdk-transport/client/types";
import { isCallableSkill } from "@/lib/skill-command-policy";

interface SkillListCardProps {
  skills: SkillEntry[];
}

const STATUS_ORDER: SkillStatus[] = ["callable", "discoverable", "unavailable"];

export function SkillListCard({ skills }: SkillListCardProps) {
  const { t } = useTranslation("chat");
  const groups = useMemo(
    () => STATUS_ORDER.map((status) => ({
      status,
      skills: skills.filter((skill) => {
        const effectiveStatus = skill.status ?? "discoverable";
        return effectiveStatus === status;
      }),
    })).filter((group) => group.skills.length > 0),
    [skills],
  );
  const callableCount = skills.filter((skill) => isCallableSkill(skill.status)).length;

  return (
    <section className="overflow-hidden rounded-[var(--radius-lg)] border border-[var(--border-subtle)] bg-[var(--bg-elevated)] shadow-sm" aria-label={t("text.skills_result_title")}>
      <header className="flex items-center justify-between gap-4 border-b border-[var(--border-subtle)] bg-[var(--bg-surface)] px-4 py-3">
        <div>
          <h3 className="text-sm font-semibold text-[var(--text-primary)]">
            {t("text.skills_result_title")}
          </h3>
          <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
            {t("text.skills_result_summary", { total: skills.length, callable: callableCount })}
          </p>
        </div>
        <span className="rounded-full bg-[rgba(52,211,153,0.1)] px-2 py-1 text-[10px] font-bold uppercase tracking-wider text-[var(--accent-emerald)]">
          /skills
        </span>
      </header>

      <div className="max-h-[520px] space-y-4 overflow-y-auto p-4 custom-scrollbar">
        {groups.map((group) => (
          <div key={group.status}>
            <div className="mb-2 flex items-center gap-2">
              <span className={`h-1.5 w-1.5 rounded-full ${
                group.status === "callable"
                  ? "bg-[var(--accent-emerald)]"
                  : group.status === "unavailable"
                    ? "bg-[var(--accent-coral)]"
                    : "bg-[var(--text-faint)]"
              }`} />
              <h4 className="text-[10px] font-bold uppercase tracking-widest text-[var(--text-muted)]">
                {t(`label.skill_status_${group.status}`)}
              </h4>
              <span className="text-[10px] text-[var(--text-faint)]">{group.skills.length}</span>
            </div>
            <div className="grid gap-2 sm:grid-cols-2">
              {group.skills.map((skill) => (
                <div key={`${group.status}:${skill.name}`} className="rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--bg-base)] px-3 py-2">
                  <div className="flex items-center justify-between gap-2">
                    <code className="truncate text-xs font-semibold text-[var(--text-primary)]">/{skill.name}</code>
                    {isCallableSkill(skill.status) && (
                      <span className="shrink-0 text-[10px] text-[var(--accent-emerald)]">{t("text.skill_invoke_hint")}</span>
                    )}
                  </div>
                  <p className="mt-1 line-clamp-2 text-[11px] leading-relaxed text-[var(--text-muted)]">
                    {skill.description || t("text.skill_no_description")}
                  </p>
                </div>
              ))}
            </div>
          </div>
        ))}
        {skills.length === 0 && (
          <p className="text-sm text-[var(--text-muted)]">{t("text.skills_result_empty")}</p>
        )}
      </div>
    </section>
  );
}

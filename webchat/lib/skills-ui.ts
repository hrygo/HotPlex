export type SkillBadgeKind = 'builtin' | 'managed' | 'external';

type SkillProvenance = {
  builtin?: boolean;
  managed: boolean;
  source?: string;
};

// Built-in provenance is an explicit additive field. Source remains the
// global/project compatibility scope and managed describes filesystem
// mutability; neither is allowed to masquerade as built-in identity.
export function isBuiltinSkill(skill: Pick<SkillProvenance, 'builtin'>): boolean {
  return skill.builtin === true;
}

export function skillActionState(skill: SkillProvenance): { canEdit: boolean; canDelete: boolean } {
  if (isBuiltinSkill(skill)) {
    return { canEdit: false, canDelete: false };
  }
  return { canEdit: skill.managed, canDelete: skill.managed };
}

export function skillBadgeKind(skill: SkillProvenance): SkillBadgeKind {
  if (isBuiltinSkill(skill)) return 'builtin';
  return skill.managed ? 'managed' : 'external';
}

import { describe, expect, it } from 'vitest';
import { skillActionState, skillBadgeKind } from './skills-ui';

describe('built-in skill presentation', () => {
  it('marks only explicit builtin metadata read-only', () => {
    expect(skillActionState({ builtin: true, managed: false })).toEqual({ canEdit: false, canDelete: false });
    expect(skillActionState({ builtin: false, managed: true })).toEqual({ canEdit: true, canDelete: true });
    expect(skillActionState({ managed: false })).toEqual({ canEdit: false, canDelete: false });
  });

  it('uses a dedicated builtin badge independent of source and managed', () => {
    expect(skillBadgeKind({ builtin: true, source: 'global', managed: false })).toBe('builtin');
    expect(skillBadgeKind({ builtin: false, source: 'global', managed: true })).toBe('managed');
    expect(skillBadgeKind({ builtin: false, source: 'project', managed: false })).toBe('external');
  });
});

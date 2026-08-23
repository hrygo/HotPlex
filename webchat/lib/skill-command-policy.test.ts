import { describe, expect, it } from "vitest";
import {
  canSelectSkillCommand,
  isBlockedSkillInput,
  isCallableSkill,
  nextSelectableCommandIndex,
} from "./skill-command-policy";

describe("skill command policy", () => {
  it("allows only explicitly callable skills", () => {
    expect(isCallableSkill("callable")).toBe(true);
    expect(isCallableSkill("discoverable")).toBe(false);
    expect(isCallableSkill("unavailable")).toBe(false);
    expect(isCallableSkill(undefined)).toBe(false);
  });

  it("blocks discoverable and unavailable skill commands from selection", () => {
    expect(
      canSelectSkillCommand({ type: "skill", status: "callable" }),
    ).toBe(true);
    expect(
      canSelectSkillCommand({ type: "skill", status: "discoverable" }),
    ).toBe(false);
    expect(
      canSelectSkillCommand({ type: "skill", status: "unavailable" }),
    ).toBe(false);
    expect(canSelectSkillCommand({ type: "skill" })).toBe(false);
    expect(canSelectSkillCommand({ type: "slash" })).toBe(true);
  });

  it("skips non-callable skills during keyboard navigation", () => {
    const commands = [
      { type: "skill" as const, status: "discoverable" as const },
      { type: "skill" as const, status: "callable" as const },
      { type: "skill" as const, status: "unavailable" as const },
      { type: "slash" as const },
    ];

    expect(nextSelectableCommandIndex(commands, 0, 1)).toBe(1);
    expect(nextSelectableCommandIndex(commands, 1, 1)).toBe(3);
    expect(nextSelectableCommandIndex(commands, 3, 1)).toBe(1);
    expect(nextSelectableCommandIndex(commands, 1, -1)).toBe(3);
  });

  it("blocks direct submission of known non-callable skills", () => {
    const skills = [
      { name: "api-designer", status: "discoverable" as const },
      { name: "available", status: "callable" as const },
    ];

    expect(isBlockedSkillInput("/api-designer", skills)).toBe(true);
    expect(isBlockedSkillInput("/api-designer resource", skills)).toBe(true);
    expect(isBlockedSkillInput("/available resource", skills)).toBe(false);
    expect(isBlockedSkillInput("/api-designer-extra", skills)).toBe(false);
  });
});

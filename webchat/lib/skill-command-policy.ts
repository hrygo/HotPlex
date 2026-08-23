import type { SkillEntry, SkillStatus } from "@/lib/ai-sdk-transport/client/types";

export function isCallableSkill(status?: SkillStatus): status is "callable" {
  return status === "callable";
}

export function canSelectSkillCommand(command: {
  type: "slash" | "skill";
  status?: SkillStatus;
}): boolean {
  return command.type !== "skill" || isCallableSkill(command.status);
}

export function isBlockedSkillInput(
  input: string,
  skills: ReadonlyArray<Pick<SkillEntry, "name" | "status">>,
): boolean {
  const commandName = input.trim().split(/\s+/, 1)[0]?.toLowerCase();
  if (!commandName?.startsWith("/") || commandName === "/") return false;

  const skill = skills.find(
    (entry) => `/${entry.name}`.toLowerCase() === commandName,
  );
  return skill !== undefined && !isCallableSkill(skill.status);
}

export function nextSelectableCommandIndex(
  commands: ReadonlyArray<{ type: "slash" | "skill"; status?: SkillStatus }>,
  currentIndex: number,
  direction: 1 | -1,
): number {
  if (commands.length === 0) return 0;

  for (let offset = 1; offset <= commands.length; offset += 1) {
    const index =
      (currentIndex + direction * offset + commands.length) % commands.length;
    if (canSelectSkillCommand(commands[index])) return index;
  }

  return currentIndex;
}

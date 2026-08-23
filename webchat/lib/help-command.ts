export type HelpCommandEntry = {
  commands: string[];
  args?: string;
  description: string;
};

export type HelpCommandSection = {
  emoji: string;
  title: string;
  entries: HelpCommandEntry[];
};

export type HelpCommandDocument = {
  title: string;
  sections: HelpCommandSection[];
};

const commandTokenPattern = /^\/(?:[a-z][\w-]*|[?])$|^\$|^[?？]$/iu;

function parseEntry(commandText: string, description: string): HelpCommandEntry | null {
  const tokens = Array.from(commandText.matchAll(/`([^`]+)`/gu), (match) => match[1]);
  const commandCount = tokens.findIndex((token) => !commandTokenPattern.test(token));
  const commands = (commandCount === -1 ? tokens : tokens.slice(0, commandCount));

  if (commands.length === 0 || commands.some((command) => !commandTokenPattern.test(command))) {
    return null;
  }

  const args = commandCount === -1 ? undefined : tokens.slice(commandCount).join(" ");
  return { commands, args, description };
}

/**
 * Parses the stable Markdown format emitted by the gateway's /help command.
 * Returns null for ordinary Markdown so callers can keep the normal renderer.
 */
export function parseHelpCommandText(text: string): HelpCommandDocument | null {
  const lines = text.split(/\r?\n/u);
  const titleIndex = lines.findIndex((line) => line.trim().length > 0);
  const titleMatch = lines[titleIndex]?.trim().match(/^📖\s+\*(.+)\*$/u);

  if (!titleMatch) return null;

  const sections: HelpCommandSection[] = [];
  let currentSection: HelpCommandSection | undefined;
  let hasMalformedLine = false;

  for (const line of lines.slice(titleIndex + 1)) {
    const trimmed = line.trim();
    if (!trimmed) continue;

    const sectionMatch = trimmed.match(/^\*([^\s]+)\s+(.+?)\*$/u);
    if (sectionMatch) {
      currentSection = {
        emoji: sectionMatch[1],
        title: sectionMatch[2],
        entries: [],
      };
      sections.push(currentSection);
      continue;
    }

    const entryMatch = trimmed.match(/^•\s*(.+?)(?:\s+—\s+(.+))?$/u);
    if (entryMatch && currentSection) {
      const entry = parseEntry(entryMatch[1], entryMatch[2] ?? "");
      if (entry) {
        currentSection.entries.push(entry);
        continue;
      }
    }

    hasMalformedLine = true;
  }

  if (
    hasMalformedLine ||
    sections.length === 0 ||
    sections.some((section) => section.entries.length === 0)
  ) {
    return null;
  }

  return { title: titleMatch[1], sections };
}

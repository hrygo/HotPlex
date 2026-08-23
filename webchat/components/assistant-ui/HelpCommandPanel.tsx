import type { HelpCommandDocument } from "@/lib/help-command";

export function HelpCommandPanel({ document }: { document: HelpCommandDocument }) {
  const titleId = "help-command-panel-title";

  return (
    <section className="help-command-panel" aria-labelledby={titleId}>
      <header className="help-command-header">
        <div className="help-command-header-icon" aria-hidden="true">
          📖
        </div>
        <div className="min-w-0">
          <h2 id={titleId} className="help-command-title">
            {document.title}
          </h2>
          <p className="help-command-subtitle">快捷指令速查</p>
        </div>
      </header>

      <div className="help-command-grid">
        {document.sections.map((section) => (
          <section className="help-command-section" key={`${section.emoji}-${section.title}`}>
            <h3 className="help-command-section-title">
              <span aria-hidden="true">{section.emoji}</span>
              <span>{section.title}</span>
            </h3>

            <ul className="help-command-list">
              {section.entries.map((entry) => (
                <li className="help-command-entry" key={`${entry.commands.join("-")}-${entry.args ?? ""}`}>
                  <div className="help-command-tokens" aria-label={entry.commands.join(", ")}>
                    {entry.commands.map((command) => (
                      <code className="help-command-badge" key={command}>
                        {command}
                      </code>
                    ))}
                    {entry.args && <code className="help-command-arg">{entry.args}</code>}
                  </div>
                  <p className="help-command-description">{entry.description}</p>
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
    </section>
  );
}

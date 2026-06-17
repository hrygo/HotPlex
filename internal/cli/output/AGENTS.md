# output — Terminal Rendering for `doctor` / `onboard`

## OVERVIEW
ANSI-aware human output plus a JSON envelope for machine consumers. Renders `cli.Diagnostic` results (doctor), wizard step UI (onboard), and the `JSONReport` consumed by `--output json`. All color output is gated on `IsTTY` so pipes and logs stay clean.

## STRUCTURE
```
output/
  printer.go      # PrintDiagnostic (✓/⚠/✗ + category + fix hint), PrintSummary (counts)
  report.go       # JSONReport / JSONSummary / WriteJSON for --output json
  theme.go        # ANSI helpers (Bold/Dim/Green/...) + wizard UI builders (SectionHeader, StepLine, CommandBox, NoteBox)
  tty.go          # IsTTY via go-isatty
  printer_test.go # PrintDiagnostic/PrintSummary tests
  report_test.go  # JSONReport tests
  theme_test.go   # theme + UI builder tests
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Render one diagnostic | `printer.go:20` | `PrintDiagnostic(out, d, verbose)`: picks symbol+color by `d.Status`, prints `FixHint` line |
| Summary line | `printer.go:60` | `PrintSummary(pass,warn,fail,fixable)` — colored bold when TTY |
| Machine output | `report.go:24` | `NewJSONReport(version, diags)` tallies statuses; `WriteJSON` encodes with 2-space indent |
| Status symbol | `theme.go:43` | `StatusSymbol("pass"|"warn"|"fail"|"skip")` → colored glyph |
| Wizard step line | `theme.go:65` | `StepLine(name, status, detail)` |
| Section / note / command boxes | `theme.go:59` / `:87` / `:74` | `SectionHeader`, `NoteBox`, `CommandBox` (numbered next-steps) |
| Raw ANSI styles | `theme.go:10+` | `Style`, `Bold`, `Dim`, `Green`, `Yellow`, `Red`, `Cyan`, `Sprintf`, `Fprintf` |
| TTY detection | `tty.go:11` | `IsTTY(w)` — returns false for non-`*os.File` writers |

## KEY PATTERNS

**Color is TTY-gated, not flag-gated**
- `PrintDiagnostic`/`PrintSummary` call `IsTTY(out)` and strip ANSI when false. Don't add a `--no-color` flag; redirecting output is the contract.

**Two render targets, one diagnostic slice**
- Human: `PrintDiagnostic` per item + `PrintSummary` footer.
- Machine: `NewJSONReport(...)` → `WriteJSON`. Both consume the same `[]cli.Diagnostic` from `DefaultRegistry.All()`.

**ANSI code constants live in one place**
- `ansiReset/Bold/Dim/Green/Yellow/Red/Cyan` are private to the package; callers use `Bold(...)`/`Green(...)` so reset tags can never be forgotten.

**Wizard UI is composable**
- `SectionHeader`, `StepLine`, `ConfigLine`, `CommandBox`, `NoteBox`, `Fdivider` return strings; the wizard concatenates and prints them. No direct `fmt.Fprintln` inside builders.

## ANTI-PATTERNS
- ❌ Emit ANSI escapes directly — go through `theme.go` helpers so resets always balance.
- ❌ Add table/yaml renderers here without a TTY gate — current contract is human + JSON only; new formats must respect `IsTTY`.
- ❌ Print diagnostics with `fmt.Println` from `cmd/hotplex` — call `output.PrintDiagnostic` so verbose/fix-hint behavior stays uniform.
- ❌ Hardcode status glyphs — use `StatusSymbol`, so `skip`/`warn` stay consistent across doctor and wizard.

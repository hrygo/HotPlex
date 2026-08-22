# onboard — Interactive Setup Wizard for `hotplex onboard`

## OVERVIEW
Multi-step interactive wizard producing `config.yaml` + `.env` + agent-config skeletons. Drives platform credential collection, STT/TTS preflight, optional service install, and binary install. Supports `--non-interactive` for CI. The `templates/` subdir (SOUL/AGENTS/TOOLS/USER/MEMORY .md files) has its **own AGENTS.md** and is out of scope here.

## STRUCTURE
```
onboard/
  wizard.go                # 1320 lines: Run() entry, step flow, all prompts, ExistingConfig detection
  templates.go             # BuildConfigYAML: YAML-AST mutation of embedded configs/config.yaml; GenerateSecret
  agentconfig_templates.go # go:embed templates/*.md + guides/*.md; DefaultTemplates(), ShowPlatformGuide()
  yamlutil.go              # yaml.Node helpers: lookupKey/lookupPath/setScalar/setBool/setStringList/replaceBlock
  wizard_test.go           # flow tests
  wizard_coverage_test.go  # branch coverage
  templates/               # EMBEDDED — has its own AGENTS.md, do not document here
  guides/                  # slack.md, feishu.md platform setup guides (embedded, printed to stderr)
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Entry point | `wizard.go:168` | `Run(ctx, WizardOptions) (*WizardResult, error)` |
| Options struct | `wizard.go:35` | `NonInteractive`, `Force`, `EnableSlack`, `EnableFeishu`, `InstallService`, `ServiceLevel`, `SlackAllowFrom`, policy fields |
| Detect existing state | `wizard.go:77` | `detectExistingConfig` scans config.yaml + .env; `SlackReady()`/`FeishuReady()` predicates |
| Step list (interactive) | `wizard.go:407` | `buildSteps()`: required config, worker dep, messaging, agent-config, STT, TTS, service, binary |
| Non-interactive platform | `wizard.go:315` | `buildPlatformNonInteractive` materializes opts → `messagingPlatformConfig` |
| Config generation | `templates.go:43` | `BuildConfigYAML`: returns embedded YAML verbatim when no mutation needed (preserves comments), else AST-mutates |
| Keep-existing platform block | `templates.go:54` + `yamlutil.go:101` | `replaceBlock` deep-copies prior Slack/Feishu block when user chose "keep" |
| Agent-config templates | `agentconfig_templates.go:10` | `//go:embed templates/*.md`; `DefaultTemplates()` returns the 5 canonical files |
| Platform guides | `agentconfig_templates.go:45` | `//go:embed guides/*.md`; `ShowPlatformGuide("slack")` prints to stderr |
| Prompt primitives | `wizard.go:1043+` | `promptWithValidation`, `promptChoice`, `promptYesNo`, `promptWithDefault`, `promptCommaList` |
| STT/TTS install fix | `wizard.go:887` / `:895` | `installSTTDeps` / `installTTSDeps` shell out to pip (only allowed exec path besides `claude`) |

## KEY PATTERNS

**WizardResult step accumulation (wizard.go:291)**
- Each step returns a `StepResult`; `WizardResult.add()` appends; `hasFail()` short-circuits downstream steps.

**Config-aware credential preservation**
- `readExistingEnvValue` / `readExistingConfigValue` / `readExistingEnvCredentials` (wizard.go:537+) prefill prompts so re-runs don't wipe secrets.

**YAML AST mutation over string replace**
- `templates.go` parses embedded `configs/config.yaml` into `yaml.Node`, walks via `lookupPath`, mutates with `setBool/setScalar/setStringList`. Never `sed`/regex on YAML source — comments would be lost and indentation is unsafe.

**Env file write merge**
- `buildEnvContent` (wizard.go:779) reads existing `.env`, preserves unknown keys, appends generated `HOTPLEX_ADMIN_TOKEN_1` and platform creds; `GenerateSecret()` (templates.go:139) yields 32-byte base64.

**Non-interactive determinism**
- With `NonInteractive=true` the wizard skips all `prompt*` calls; `EnableSlack`/`EnableFeishu` flags and `*Policy`/`*AllowFrom` fields fully specify output. Required-field gaps become failing `StepResult`s instead of prompts.

## ANTI-PATTERNS
- ❌ Mutate `configs/config.yaml` via string ops — go through `yamlutil.go` AST helpers.
- ❌ Print setup guides to stdout — `ShowPlatformGuide` writes to stderr so pipes/JSON output stay clean.
- ❌ Read templates via `os.ReadFile` — they are `go:embed`-ed; use `readTemplate` / `templateFS`.
- ❌ Add a new step without wiring it into both `buildSteps()` (interactive) and the non-interactive path.
- ❌ Forget the `templates/` subdir has its own AGENTS.md — edit that file for template content, not here.

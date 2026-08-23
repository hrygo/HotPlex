# HotPlex Self-Awareness Phase B Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (- [ ]) syntax for tracking.

**Goal:** Deliver the embedded, provenance-aware HotPlex Built-in Skills framework, explicit skills lifecycle commands, deterministic CLI references, safe native-root reconciliation, and evidence-based Admin/WebChat presentation without changing the AEP wire contract.

**Architecture:** internal/skills/builtin owns embedded canonical packages, generated manifests, inventory metadata, and read-only public views. internal/skills/reconcile owns explicit filesystem synchronization, canonical target deduplication, receipts, hashes, staging, rollback, and typed reports. Cobra command construction remains authoritative for generated CLI documentation; configuration, lifecycle hooks, Worker evidence, and HTTP/UI adapters consume these typed interfaces without placing built-ins in the existing user-Skill scanner or AgentConfig prompt.

**Tech Stack:** Go 1.24, embed, io/fs, Cobra, crypto/sha256, encoding/json, filepath, testify/require, Go race tests, TypeScript, React, Vitest, and the existing HotPlex Admin/WebChat handlers.

**Spec:** docs/superpowers/specs/2026-08-22-hotplex-self-awareness-design.md

## Global Constraints

- Implement Phase B only. Do not migrate phrases.md or db-stats.md, and do not change Phase A runtime-facts or META semantics except where Task 8 names affected current documentation.
- The canonical package tree is exactly internal/skills/builtin/hotplex-cli and internal/skills/builtin/hotplex-operator. Each package uses only portable Agent Skills frontmatter fields.
- hotplex-cli contains SKILL.md and references/cron.md, references/slack.md, references/diagnostics.md, and generated references/cli-surface.generated.md. hotplex-operator contains SKILL.md and service-lifecycle.md, install-update.md, configuration.md, and admin-audit.md under references/.
- The repository .agents/skills/hotplex-cli tree is generated from, or checked byte-for-byte against, the embedded canonical hotplex-cli tree. It is never an independently authored copy.
- $HOTPLEX_HOME/skills/builtin/<package-version>/<name>/ is an immutable inventory and is not a Worker native root. $HOTPLEX_HOME/state/skills/ stores native projection receipts keyed by the canonical package target identity.
- Worker native roots are under the operating-system UserHome: <UserHome>/.claude/skills and <UserHome>/.agents/skills. Inventory and receipt state are under the independent HotplexHome. Never substitute HotplexHome for UserHome.
- Native-root writes happen only in an explicit hotplex skills sync or hotplex skills remove operation, or an explicitly requested --sync-skills lifecycle step. Gateway startup and ordinary doctor checks are read-only.
- Worker, profile, and operation values are closed enums. An empty resolved Worker target set returns a bounded error requiring explicit --worker; it never falls back to worker.RegisteredTypes().
- ACP has no filesystem projection target and returns a bounded unsupported result without writing. Codex and OpenCode share the canonical .agents/skills target and report all Worker aliases in one reconciliation item.
- An approved native root may be a symlink only when canonicalization resolves it inside canonical UserHome and the corresponding approved native-root base (.claude or .agents). A package target that is itself a symlink is always a conflict.
- Native projection receipts include schema version, package version, the package manifest's declared profile, package target identity, all Worker aliases, manifest hash, and projected tree hash. A package target identity is `<canonical native root>/<package name>`, so receipt keys are per package target rather than per root. Receipt writes are atomic. Update is stage → backup → new target → receipt; receipt or second-rename failure rolls back. Remove only removes a selected native projection when its matching receipt and unchanged tree prove ownership; it never removes the content-addressed immutable inventory. Old inventory remains available for future projections and GC is outside Phase B.
- A missing receipt, invalid receipt, changed tree, unexpected symlink, path escape, collision, or stale Worker evidence never overwrites user content and remains visible in the typed report.
- Filesystem tests use t.TempDir() and injected paths/operations. No test writes $HOME, ~/.hotplex, ~/.claude/skills, ~/.agents/skills, the repository working tree, a real executable, or a real service directory.
- The generator never invokes a shell or os/exec to invoke itself; it uses Go file APIs. The existing non-main branch fix/turn-integrity-init-reliability is the isolated development branch; do not create another worktree or branch.
- Preserve existing skills.Skill.Source values (global and project), existing AgentConfig SKILLS.md compatibility, existing Admin/WebChat Agent Skills meaning, and all existing AEP SkillStatus, SkillEntry, JSON tags, SDKs, examples, and protocol fixtures.
- Built-in provenance is optional HTTP/UI metadata (builtin and builtin_package_version) and never a new source enum. Do not add AEP fields in Phase B.
- Native Worker progressive disclosure remains owned by Claude Code, Codex CLI, and OpenCode. HotPlex records evidence but does not duplicate their catalog in the AgentConfig prompt.
- Every implementation task follows red-green TDD, uses table-driven testify/require tests, runs gofmt, and ends in one independently reviewable commit. No task pushes.

## File Map

| Responsibility | Files | Boundary |
|---|---|---|
| Canonical assets and manifests | internal/skills/builtin/assets.go, manifest.go, manifest.generated.go, the two embedded package trees | Embedded bytes, manifest/hash validation, no host writes |
| CLI renderer and generated development copy | cmd/hotplex/root.go, cmd/hotplex/cli_surface.go, cmd/gen-builtin-skills/main.go, .agents/skills/hotplex-cli/** | The real Cobra tree is the only CLI inventory source |
| User/Worker native catalog parsing | internal/skills/scanner.go, internal/worker/claudecode/skill_catalog.go | Explicit native root only; never scans the Built-in inventory |
| Safe synchronization | internal/skills/reconcile/types.go, paths.go, fs.go, receipts.go, reconciler.go | Canonical roots, receipts, hashes, staging, rollback, typed reports |
| Configuration target selection | internal/config/worker_targets.go | Resolved enabled messaging platform/bot types only |
| Skills CLI | cmd/hotplex/skills_cmd.go, skills_status.go, skills_sync.go, skills_remove.go | Cobra validation and typed report presentation |
| Lifecycle hooks | internal/cli/checkers/builtin_skills.go, internal/cli/onboard/wizard.go, cmd/hotplex/onboard.go, cmd/hotplex/update.go, cmd/hotplex/gateway_run.go | Explicit mutation only; status-only startup/doctor |
| Worker evidence and dispatch | internal/worker/claudecode/skill_catalog.go, internal/gateway/native_catalog.go, worker_cmds.go, handler.go | Existing optional catalog interfaces; no Worker main-interface/AEP change |
| HTTP/UI provenance | internal/skills/builtin/public.go, internal/admin/skill_handlers.go, internal/admin/admin.go, internal/gateway/skill_handlers.go, WebChat Skills API/types/components | Read-only built-ins; user same-name wins |
| Documentation and quality gates | current docs listed in Task 8, internal/skills/builtin/quality_test.go, CLI/example tests | Documentation follows shipped behavior and generated CLI output |

---

### Task 1: Embed canonical Built-in packages and generate the public CLI surface

**Files:**

- Create: internal/skills/builtin/assets.go
- Create: internal/skills/builtin/manifest.go
- Create: internal/skills/builtin/manifest.generated.go
- Create: internal/skills/builtin/manifest_test.go
- Create: internal/skills/builtin/hotplex-cli/SKILL.md
- Create: internal/skills/builtin/hotplex-cli/references/cron.md
- Create: internal/skills/builtin/hotplex-cli/references/slack.md
- Create: internal/skills/builtin/hotplex-cli/references/diagnostics.md
- Create: internal/skills/builtin/hotplex-cli/references/cli-surface.generated.md
- Create: internal/skills/builtin/hotplex-operator/SKILL.md
- Create: internal/skills/builtin/hotplex-operator/references/service-lifecycle.md
- Create: internal/skills/builtin/hotplex-operator/references/install-update.md
- Create: internal/skills/builtin/hotplex-operator/references/configuration.md
- Create: internal/skills/builtin/hotplex-operator/references/admin-audit.md
- Create: cmd/hotplex/root.go
- Create: cmd/hotplex/cli_surface.go
- Create: cmd/hotplex/cli_surface_test.go
- Create: cmd/gen-builtin-skills/main.go
- Create: cmd/gen-builtin-skills/main_test.go
- Modify: cmd/hotplex/main.go
- Replace from the canonical tree: .agents/skills/hotplex-cli/SKILL.md
- Create from the canonical tree: .agents/skills/hotplex-cli/references/cron.md
- Create from the canonical tree: .agents/skills/hotplex-cli/references/slack.md
- Create from the canonical tree: .agents/skills/hotplex-cli/references/diagnostics.md
- Create from the canonical tree: .agents/skills/hotplex-cli/references/cli-surface.generated.md

**Interfaces:**

- Produces builtin.Profile, builtin.PackageManifest, builtin.AssetManifest, and builtin.Registry.
- builtin.NewRegistry() (*Registry, error) validates generated manifests against embedded bytes.
- (*Registry).Packages() []PackageManifest, (*Registry).Package(name string) (PackageManifest, bool), and (*Registry).ReadFile(packageName, relativePath string) ([]byte, error) are the asset reads consumed by later tasks.
- Produces the cumulative ProfilePackageSet: runtime contains exactly hotplex-cli; operator contains hotplex-cli followed by hotplex-operator. Registry package selection for status, sync, remove, inventory publication, and public listing always uses this set.
- newRootCmd() *cobra.Command becomes the single root assembly used by main() and the renderer.
- renderPublicCLISurface(root *cobra.Command) ([]byte, error) returns deterministic Markdown; it sorts command paths and flags, skips hidden commands/flags, and emits no defaults, paths, runtime values, or secrets.
- cmd/gen-builtin-skills regenerates manifest.generated.go, copies the complete canonical hotplex-cli tree to .agents/skills/hotplex-cli, and exits non-zero on an incomplete or mismatched generated input. It scans canonical bytes with Go file APIs only, imports no builtin registry package, and never invokes shell, os/exec, or its own command.

- [ ] Step 1: Add red tests for package shape, frontmatter, manifest determinism, and copy parity.

Add table-driven tests with these exact package names, profiles, and files:

~~~go
func TestCanonicalPackagesHavePortableFrontmatterAndClosedReferences(t *testing.T) {
    cases := []struct {
        name    string
        profile builtin.Profile
        files   []string
    }{
        {
            name:    "hotplex-cli",
            profile: builtin.ProfileRuntime,
            files: []string{
                "SKILL.md",
                "references/cron.md",
                "references/slack.md",
                "references/diagnostics.md",
                "references/cli-surface.generated.md",
            },
        },
        {
            name:    "hotplex-operator",
            profile: builtin.ProfileOperator,
            files: []string{
                "SKILL.md",
                "references/service-lifecycle.md",
                "references/install-update.md",
                "references/configuration.md",
                "references/admin-audit.md",
            },
        },
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            registry, err := builtin.NewRegistry()
            require.NoError(t, err)
            manifest, ok := registry.Package(tc.name)
            require.True(t, ok)
            require.Equal(t, tc.profile, manifest.Profile)
            require.ElementsMatch(t, tc.files, manifest.Paths())
            body, err := registry.ReadFile(tc.name, "SKILL.md")
            require.NoError(t, err)
            require.Contains(t, string(body), "name: "+tc.name)
            require.Contains(t, string(body), "description:")
            require.Contains(t, string(body), "compatibility:")
        })
    }
}

func TestRepositoryRuntimeSkillMatchesEmbeddedCanonicalTree(t *testing.T) {
    require.Equal(t, canonicalTree(t, "hotplex-cli"), repositoryTree(t, ".agents/skills/hotplex-cli"))
}

func TestProfilePackageSetIsCumulativeAndStable(t *testing.T) {
    require.Equal(t, []string{"hotplex-cli"}, builtin.ProfilePackageSet[builtin.ProfileRuntime])
    require.Equal(t, []string{"hotplex-cli", "hotplex-operator"}, builtin.ProfilePackageSet[builtin.ProfileOperator])
}
~~~

The tests also reject a hotplex-cli description that authorizes Feishu, releases, service installation, binary updates, or Admin mutations, and reject a hotplex-operator description without explicit operator/Admin authority.

- [ ] Step 2: Run the focused red tests.

Run:

~~~bash
go test ./internal/skills/builtin ./cmd/hotplex ./cmd/gen-builtin-skills -run 'TestCanonicalPackages|TestRepositoryRuntimeSkill|TestRenderPublicCLI' -count=1
~~~

Expected: compilation fails because the builtin package, root builder, renderer, canonical package files, and generated copy do not exist.

- [ ] Step 3: Implement the embedded registry and generated manifest.

Define the closed profiles and deterministic manifest types:

~~~go
type Profile string

const (
    ProfileRuntime  Profile = "runtime"
    ProfileOperator Profile = "operator"
)

type AssetManifest struct {
    Path   string
    Size   int64
    SHA256 string
}

type PackageManifest struct {
    Name    string
    Version string
    Profile Profile
    Assets  []AssetManifest
}

func (m PackageManifest) Paths() []string

type InstalledPackage struct {
    Manifest      PackageManifest
    InventoryPath string
}

var ProfilePackageSet = map[Profile][]string{
    ProfileRuntime:  {"hotplex-cli"},
    ProfileOperator: {"hotplex-cli", "hotplex-operator"},
}
~~~

Embed only the two canonical package directories. Keep manifest.generated.go as a sorted Go literal generated from embedded package bytes and commit that generated file in the same change as the registry source, so the first compile never depends on running a generator. NewRegistry validates path normalization, package/profile uniqueness, file hashes, total file count, per-file size, and total package size. It rejects parent traversal, absolute paths, duplicate paths, missing generated entries, and generated entries whose hash differs from embedded bytes. cmd/gen-builtin-skills has an independent bootstrap test that runs against a temporary canonical tree and proves it does not import or call the registry.

- [ ] Step 4: Refactor root construction and implement the deterministic renderer.

Move the existing root cobra.Command construction from main() to newRootCmd() in cmd/hotplex/root.go, preserving command registration and behavior. main() handles the package-main-only internal generation mode before service detection and otherwise executes newRootCmd().

Implement the renderer with these rules:

~~~go
func renderPublicCLISurface(root *cobra.Command) ([]byte, error) {
    // Walk sorted public command paths.
    // Skip command.Hidden and flag.Hidden.
    // Emit Use, Short, aliases, and public long-flag value shapes.
    // Never emit flag defaults, file-system paths, environment values, or tokens.
    // Return one byte-identical Markdown document for one Cobra tree.
}
~~~

The internal mode writes only the requested output file and never executes a command handler. The required generation order is: run the package-main internal mode to create cli-surface.generated.md, then run cmd/gen-builtin-skills to scan existing canonical bytes, generate manifest.generated.go, and mirror the complete runtime package. cmd/gen-builtin-skills does not import the not-yet-generated builtin registry and refuses to copy a partial tree.

- [ ] Step 5: Write the canonical Skill routers and references.

Make each SKILL.md a short router. hotplex-cli states exact positive/negative scope, classifies Cron/Slack/diagnostics as read-only or explicitly requested mutations, requires current binary help when prose differs, requires postconditions, and prohibits printing tokens or arbitrary environment values. hotplex-operator states explicit operator/Admin authority and routes service, update, configuration, audit, Admin mutation, and Built-in sync/remove operations.

Write references/cron.md with the accepted cron:, every:, at:<RFC3339>, and at:+<duration> forms; isolated versus --attach; self-contained isolated prompts; present GATEWAY_* identity usage without printing values; lifecycle limits; --silent, delete-after-run, retries, timeout, and allowed-tools validation; portable examples; and mandatory independent cron get verification.

Write Slack and diagnostics references with explicit mutation confirmation, read-only status/doctor/security/config/service/log/Cron paths, and operator routing for privileged operations. Use the generated CLI section for exact flags instead of duplicating the full command inventory.

- [ ] Step 6: Regenerate and run green tests.

Run:

~~~bash
gofmt -w internal/skills/builtin cmd/hotplex/root.go cmd/hotplex/cli_surface.go cmd/hotplex/cli_surface_test.go cmd/gen-builtin-skills
go run ./cmd/hotplex --internal-generate-cli-surface --output internal/skills/builtin/hotplex-cli/references/cli-surface.generated.md
go run ./cmd/gen-builtin-skills
go test ./internal/skills/builtin ./cmd/hotplex ./cmd/gen-builtin-skills -count=1 -race -shuffle=on
~~~

Expected: all package, renderer, manifest, and canonical-copy tests pass; rerunning both generation commands produces no diff.

- [ ] Step 7: Commit the canonical package slice.

~~~bash
git add internal/skills/builtin cmd/hotplex/main.go cmd/hotplex/root.go cmd/hotplex/cli_surface.go cmd/hotplex/cli_surface_test.go cmd/gen-builtin-skills .agents/skills/hotplex-cli
git commit -m "feat(skills): embed canonical built-in packages"
~~~

### Task 2: Implement canonical native-root reconciliation, receipts, and rollback

**Files:**

- Create: internal/skills/reconcile/types.go
- Create: internal/skills/reconcile/paths.go
- Create: internal/skills/reconcile/fs.go
- Create: internal/skills/reconcile/receipts.go
- Create: internal/skills/reconcile/reconciler.go
- Create: internal/skills/reconcile/reconcile_test.go
- Create: internal/skills/reconcile/rollback_test.go
- Modify only when required for the explicit native-root scanner: internal/skills/scanner.go
- Test scanner changes in: internal/skills/scanner_test.go

**Interfaces:**

- Consumes builtin.Registry, builtin.PackageManifest, builtin.Profile, and builtin.ReadFile.
- Produces:

~~~go
type WorkerType string

const (
    WorkerClaude   WorkerType = "claude_code"
    WorkerCodex    WorkerType = "codex_cli"
    WorkerOpenCode WorkerType = "opencode_server"
    WorkerACP      WorkerType = "acp"
)

type Target struct {
    CanonicalRoot string
    WorkerAliases []WorkerType
    ReasonCode    string
}

type Options struct {
    Profile     builtin.Profile
    WorkerTypes []WorkerType
    DryRun      bool
}

type Report struct {
    Profile builtin.Profile
    Items   []Item
}

type Item struct {
    Target        string
    WorkerAliases []WorkerType
    Action        string
    Outcome       string
    ReasonCode    string
    BackupPath    string
}

// PackageTargetIdentity returns the canonical native-root/package identity used
// by native projection items and receipts. It accepts only a validated manifest
// package name (no separators or traversal) and is not the inventory path.
func PackageTargetIdentity(canonicalNativeRoot, packageName string) string
~~~

~~~go
type Paths struct {
    UserHome     string
    HotplexHome  string
    InventoryDir string
    StateDir     string
    NativeRoots  map[WorkerType]string
}

type FileSystem interface {
    Lstat(string) (os.FileInfo, error)
    ReadDir(string) ([]os.DirEntry, error)
    ReadFile(string) ([]byte, error)
    MkdirAll(string, os.FileMode) error
    WriteFile(string, []byte, os.FileMode) error
    Rename(string, string) error
    Remove(string) error
    RemoveAll(string) error
    EvalSymlinks(string) (string, error)
    SyncFile(string) error
    SyncDir(string) error
}

type Reconciler struct {
    // registry, paths, and fs are private implementation dependencies.
}

type Runner interface {
    Status(context.Context, Options) (Report, error)
    Sync(context.Context, Options) (Report, error)
    Remove(context.Context, Options) (Report, error)
}

func ParseWorkerType(string) (WorkerType, error)
func (r Report) Err() error
~~~

The production osFS implements FileSystem with os.Lstat, os.ReadDir, os.ReadFile, os.MkdirAll, os.WriteFile, os.Rename, os.Remove, os.RemoveAll, filepath.EvalSymlinks, and explicit `SyncFile` and `SyncDir` helpers. `SyncFile` flushes temporary files before rename; `SyncDir` flushes the parent directory after every durability-relevant rename. Unix implementations open and sync the directory. Windows has no portable directory-fsync primitive in the Go standard library, so its implementation returns a typed `ErrDirSyncUnsupported`; reconciliation surfaces a failed item and rolls back rather than silently claiming stronger durability. Stable reason constants include ReasonUnsupportedWorker, ReasonCollision, ReasonDrift, ReasonInvalidReceipt, ReasonRootOutsideHome, ErrInventoryOutsideHotplexHome, ReasonReceiptWriteFailed, ReasonInventoryBlocked, ErrDirSyncUnsupported, and ReasonUnchanged.

- ResolveTargets(userHome string, workerTypes []WorkerType) ([]Target, error) validates UserHome, rejects an empty target list with ErrNoWorkerTargets, rejects unknown types, records an unsupported ACP target without a write root, canonicalizes native roots, and root-deduplicates Codex/OpenCode while retaining the complete normalized alias set `[WorkerCodex, WorkerOpenCode]` even when only one shared-root alias was selected.
- New(registry *builtin.Registry, paths Paths, fs FileSystem) (*Reconciler, error) validates absolute UserHome and HotplexHome, keeps inventory/state below HotplexHome, keeps native roots below UserHome and their approved native-root base, and performs no write.
- Reconciler.Status(ctx context.Context, options Options) (Report, error), Sync, and Remove are the only reconciliation entry points.
- Reconciler.ListInventory(ctx context.Context, profile builtin.Profile) ([]builtin.InstalledPackage, error) reads immutable inventory/package hashes for the later read-only HTTP/UI view; it does not require a native projection receipt.
- Reconciler implements Runner, and `Report.Err` returns the bounded sentinel `ErrReportActionRequired` when any typed item has outcome `conflict`, `drift`, or `failed`; it returns nil for `unchanged` and `changed`. The typed report retains the stable reason code and full item details; `Err` never embeds paths, hashes, or secrets.
- skills.ScanRoot(root, source string, managed bool) ([]skills.Skill, error) is the only scanner helper exposed for the Claude adapter; it scans one explicit root and never the versioned Built-in inventory.

- [ ] Step 1: Add red tests for closed targets, root policy, report values, and receipt identity.

Use a home created by t.TempDir():

~~~go
func TestResolveTargetsDeduplicatesSharedAgentsRootAndKeepsAllAliases(t *testing.T) {
    userHome := t.TempDir()
    for _, selected := range []WorkerType{WorkerCodex, WorkerOpenCode} {
        targets, err := ResolveTargets(userHome, []WorkerType{selected})
        require.NoError(t, err)
        require.Len(t, targets, 1)
        require.Equal(t, []WorkerType{WorkerCodex, WorkerOpenCode}, targets[0].WorkerAliases)
        require.Equal(t, filepath.Join(userHome, ".agents", "skills"), targets[0].CanonicalRoot)
    }
}

func TestResolveTargetsRejectsEmptyListAndACPWithoutWriting(t *testing.T) {
    _, err := ResolveTargets(t.TempDir(), nil)
    require.ErrorIs(t, err, ErrNoWorkerTargets)
    targets, err := ResolveTargets(t.TempDir(), []WorkerType{WorkerACP})
    require.NoError(t, err)
    require.Len(t, targets, 1)
    require.Equal(t, ReasonUnsupportedWorker, targets[0].ReasonCode)
}

func TestNativeRootSymlinkMustResolveInsideUserHomeAndApprovedBase(t *testing.T) {
    userHome := t.TempDir()
    outside := t.TempDir()
    require.NoError(t, os.Symlink(outside, filepath.Join(userHome, ".claude")))
    _, err := ResolveTargets(userHome, []WorkerType{WorkerClaude})
    require.ErrorIs(t, err, ErrRootOutsideHome)
}

func TestInventorySymlinkMustResolveInsideHotplexHome(t *testing.T) {
    userHome := t.TempDir()
    hotplexHome := t.TempDir()
    outside := t.TempDir()
    require.NoError(t, os.Symlink(outside, filepath.Join(hotplexHome, "skills")))
    paths := Paths{
        UserHome: userHome,
        HotplexHome: hotplexHome,
        InventoryDir: filepath.Join(hotplexHome, "skills", "builtin"),
        StateDir: filepath.Join(hotplexHome, "state", "skills"),
    }
    registry, err := builtin.NewRegistry()
    require.NoError(t, err)
    _, err = New(registry, paths, osFS{})
    require.ErrorIs(t, err, ErrInventoryOutsideHotplexHome)
}

func TestReceiptKeyUsesCanonicalTargetIdentity(t *testing.T) {
    state := t.TempDir()
    userHome := t.TempDir()
    root := filepath.Join(userHome, ".agents", "skills")
    equivalentRoot := filepath.Join(userHome, "nested", "..", ".agents", "skills")
    first := ReceiptPath(state, PackageTargetIdentity(root, "hotplex-cli"))
    second := ReceiptPath(state, PackageTargetIdentity(equivalentRoot, "hotplex-cli"))
    require.Equal(t, first, second)
    require.NotEqual(t, first, ReceiptPath(state, PackageTargetIdentity(root, "hotplex-operator")))
}

func TestReportErrIsBoundedForActionRequiredOutcomes(t *testing.T) {
    for _, outcome := range []string{"conflict", "drift", "failed"} {
        report := Report{Items: []Item{{Outcome: outcome, ReasonCode: ReasonCollision}}}
        require.ErrorIs(t, report.Err(), ErrReportActionRequired)
    }
    for _, outcome := range []string{"unchanged", "changed"} {
        report := Report{Items: []Item{{Outcome: outcome}}}
        require.NoError(t, report.Err())
    }
}
~~~

Add assertions for action values none/install/update/remove, outcome values unchanged/changed/conflict/drift/failed, and stable reason codes. Human-readable path strings must never be used as control inputs.

- [ ] Step 2: Run the red reconciliation tests.

~~~bash
go test ./internal/skills/reconcile -run 'Test.*(ResolveTargets|ReceiptKey|RootSymlink|InventorySymlink)' -count=1
~~~

Expected: compilation fails because the reconcile package, types, path resolver, and receipt functions do not exist.

- [ ] Step 3: Implement closed target resolution and canonical path policy.

Implement Paths with separate UserHome and HotplexHome values. Derive inventory and receipt state only from HotplexHome. Map Claude to <user-home>/.claude/skills, Codex/OpenCode to <user-home>/.agents/skills, and ACP to an unsupported target. Sort aliases by the closed Worker value and normalize every shared `.agents/skills` target to the complete `[WorkerCodex, WorkerOpenCode]` alias set, regardless of whether one or both aliases were requested.

Canonicalize UserHome and HotplexHome independently with filepath.Abs, filepath.Clean, and filepath.EvalSymlinks. For Claude, the approved native-root base is <user-home>/.claude; for Codex/OpenCode it is <user-home>/.agents. If a native root exists as a symlink, accept it only when the resolved path remains below both canonical UserHome and its approved native-root base. If the final root does not exist, validate its nearest existing ancestor and append the missing suffix. Validate inventory ancestors against HotplexHome. Reject absolute or parent-traversal targets, and reject a package directory that is a symlink even when it resolves inside an approved root.

- [ ] Step 4: Implement manifest/tree hashes and atomic receipt storage.

Define native projection receipt fields exactly:

~~~go
type Receipt struct {
    SchemaVersion       int
    PackageVersion      string
    PackageName         string
    Profile             builtin.Profile
    CanonicalTarget     string // <canonical native root>/<package name>
    WorkerAliases       []WorkerType
    ManifestSHA256      string
    ProjectedTreeSHA256 string
}
~~~

Use a full SHA-256 hex digest of the package target identity returned by `PackageTargetIdentity(canonicalNativeRoot, packageName)` for the native receipt filename under StateDir; two packages projected into one root therefore have distinct receipts. Hash sorted relative paths, file sizes, and bytes; never include absolute paths in the tree hash. Every staged file is flushed with `SyncFile`; the completed stage directory is flushed with `SyncDir` before rename, and its parent is flushed with `SyncDir` after rename. Write a temporary receipt beside the final receipt, flush it through `FileSystem.SyncFile`, rename it into place, and call `FileSystem.SyncDir` on the receipt directory. Reject malformed JSON, unexpected enum values, mismatched package target identity, unsorted aliases, and hash mismatches. `Receipt.Profile` is copied from that package manifest's declared/minimum profile (`hotplex-cli` is `runtime`, `hotplex-operator` is `operator`), never from `Options.Profile`; an operator operation therefore does not make an existing `hotplex-cli` receipt appear to be operator-scoped. Immutable inventory uses its own versioned package identity and manifest metadata; it is not a native projection receipt and is never removed by `Remove`.

- [ ] Step 5: Implement install/update/remove/status transactions.

Implement these transitions:

~~~go
func (r *Reconciler) Sync(ctx context.Context, options Options) (Report, error) {
    // Resolve the cumulative ProfilePackageSet for options.Profile.
    // Stage and verify every selected package below
    // HotplexHome/skills/builtin/<version>/<name> before touching native roots.
    // Verify every manifest path, file hash, size, and inventory tree hash.
    // If any inventory package is different, modified, invalid, or symlinked,
    // report that conflict and report every native package as blocked with the
    // stable ReasonInventoryBlocked code; leave all native projections untouched
    // for this invocation.
    // When every inventory package is ready (already verified or newly published),
    // publish missing inventory packages with stage -> verify -> rename -> receipt.
    // Any inventory publication or durability failure uses the same gate and
    // blocks every native projection in this invocation.
    // Only after the complete inventory set is ready, stage native projections.
    // For a matching receipt and unchanged current tree, rename old target to a
    // unique sibling backup, sync its parent, rename stage to target, sync the
    // parent, and write a receipt whose Profile is the manifest profile. Roll
    // back if any second rename, receipt write, or directory sync fails.
    // For collision, modified tree, invalid receipt, symlink, or escape, preserve
    // the target and return a typed conflict/drift item.
}
~~~

Status detects inventory state, matching/missing/invalid inventory and projection receipts, staging/backup remnants, root collisions, and Worker aliases without writing. Dry-run computes both inventory publication and native projection changes and performs zero writes. An interrupted operation that can be proven safe is recoverable drift; an ambiguous interrupted operation reports drift and preserves the backup.

Status, Sync, and Remove all resolve the cumulative ProfilePackageSet, so runtime always means hotplex-cli and operator always means hotplex-cli plus hotplex-operator. `Options.Profile` selects that cumulative package set; every native receipt stores each package manifest's own declared profile. Remove only processes selected Worker native roots and their selected package targets: it requires a matching package-target receipt and unchanged projected tree, renames the native target to a unique sibling backup, then removes the receipt through a unique tombstone rename and parent-directory sync. If tombstone removal or receipt-directory sync fails, it restores the receipt and target from the backup before returning a failed item. Remove never deletes immutable inventory directories or inventory metadata; old versions remain and inventory GC is outside Phase B. Status and dry-run are strictly zero-write and never auto-recover staging, backup, or tombstone remnants; they only report proven recoverable or ambiguous interruption. Explicit sync/remove is the only recovery path and may mutate only after the matching receipt, unchanged tree, target identity, and hashes prove ownership.

- [ ] Step 6: Add failure-injection tests and run green.

Implement a recordingFS test double around production osFS with deterministic failures for second directory rename, receipt rename, receipt removal, and backup removal. Add:

~~~go
func TestDryRunLeavesHomeByteIdentical(t *testing.T) {}
func TestSyncInitialInstallWritesInventoryProjectionAndReceipt(t *testing.T) {}
func TestSyncPublishesInventoryBeforeNativeProjection(t *testing.T) {}
func TestModifiedInventoryIsConflictAndBlocksProjection(t *testing.T) {}
func TestAnyInventoryConflictBlocksEveryNativeProjection(t *testing.T) {}
func TestDryRunDoesNotCreateInventoryOrProjection(t *testing.T) {}
func TestSyncIsIdempotentWhenManifestAndTreeMatch(t *testing.T) {}
func TestOperatorSyncDoesNotReclassifyCliReceiptForRuntimeStatus(t *testing.T) {}
func TestSharedRootReceiptAndReportKeepAllWorkerAliases(t *testing.T) {}
func TestUpdateRollsBackWhenSecondRenameFails(t *testing.T) {}
func TestUpdateRollsBackWhenReceiptWriteFails(t *testing.T) {}
func TestStatusReportsUnrecoverableInterruptionAndKeepsBackup(t *testing.T) {}
func TestRemoveRequiresMatchingReceiptAndUnchangedTree(t *testing.T) {}
func TestRemoveRollsBackWhenReceiptDeleteFails(t *testing.T) {}
func TestRemovePreservesImmutableInventory(t *testing.T) {}
func TestReceiptTombstoneRollbackRestoresReceipt(t *testing.T) {}
func TestPackageSymlinkIsConflictAndNeverFollowed(t *testing.T) {}
func TestInventoryAndReceiptNeverEnterGenericSkillScanner(t *testing.T) {}
~~~

Every test creates separate userHome := t.TempDir() and hotplexHome := t.TempDir(), passes explicit Paths, and snapshots both trees before and after dry-run or status. No test uses a process home or repository path.

~~~bash
gofmt -w internal/skills/reconcile internal/skills/scanner.go internal/skills/scanner_test.go
go test ./internal/skills/reconcile ./internal/skills -count=1 -race -shuffle=on
~~~

Expected: all reconciliation and scanner tests pass, including deterministic rollback and zero-write assertions.

- [ ] Step 7: Commit the reconciliation slice.

~~~bash
git add internal/skills/reconcile internal/skills/scanner.go internal/skills/scanner_test.go
git commit -m "feat(skills): add safe native-root reconciliation"
~~~

### Task 3: Resolve enabled Worker targets from messaging configuration

**Files:**

- Create: internal/config/worker_targets.go
- Create: internal/config/worker_targets_test.go

**Interfaces:**

- Consumes Config.ResolveWorkerType(platform, botName), normalized Slack.Enabled, Feishu.Enabled, Yuanxin.Enabled, and each normalized platform Bots slice.
- Produces:

~~~go
func (c *Config) EnabledWorkerTypes() []string
~~~

The result is sorted and duplicate-free. It contains only effective Worker strings from enabled messaging platform/bot configuration. It returns an empty slice when no messaging platform is enabled; it never returns every registered Worker type. WebChat workspace preferences are outside this static resolver.

- [ ] Step 1: Add red resolver tests.

~~~go
func TestEnabledWorkerTypesUsesResolvedPlatformAndBotValues(t *testing.T) {
    cfg := Config{
        Messaging: MessagingConfig{
            Slack: SlackConfig{
                MessagingPlatformConfig: MessagingPlatformConfig{
                    Enabled: true, WorkerType: "claude_code",
                },
                Bots: []SlackBotConfig{
                    {Name: "z", WorkerType: "codex_cli"},
                    {Name: "a", WorkerType: "claude_code"},
                },
            },
            Feishu: FeishuConfig{
                MessagingPlatformConfig: MessagingPlatformConfig{
                    Enabled: true, WorkerType: "codex_cli",
                },
            },
        },
    }
    require.Equal(t, []string{"claude_code", "codex_cli"}, cfg.EnabledWorkerTypes())
}

func TestEnabledWorkerTypesExcludesDisabledPlatformsAndDoesNotUseRegistry(t *testing.T) {
    cfg := Config{
        Messaging: MessagingConfig{
            Slack: SlackConfig{
                MessagingPlatformConfig: MessagingPlatformConfig{Enabled: false},
                Bots: []SlackBotConfig{{Name: "disabled", WorkerType: "acp"}},
            },
        },
    }
    require.Empty(t, cfg.EnabledWorkerTypes())
}
~~~

Also test enabled Yuanxin, an enabled platform with no bot list, an empty shared default, duplicate aliases, and platform fallback when a bot leaves WorkerType empty.

- [ ] Step 2: Run the resolver red test.

~~~bash
go test ./internal/config -run 'TestEnabledWorkerTypes' -count=1
~~~

Expected: compilation fails because Config.EnabledWorkerTypes is not defined.

- [ ] Step 3: Implement the string-boundary resolver.

Append the resolved type for each enabled Slack, Feishu, and Yuanxin platform. When a platform has normalized bots, call ResolveWorkerType once per bot; otherwise call it with an empty bot name. Use a map for deduplication and slices.Sort before returning. Do not import internal/worker, because internal/config is the string contract that prevents an import cycle.

- [ ] Step 4: Run green tests and commit.

~~~bash
gofmt -w internal/config/worker_targets.go internal/config/worker_targets_test.go
go test ./internal/config -count=1 -race -shuffle=on
git add internal/config/worker_targets.go internal/config/worker_targets_test.go
git commit -m "feat(config): resolve enabled skill sync workers"
~~~

Expected: all resolver cases pass and the commit contains only the new resolver and tests.

### Task 4: Add hotplex skills status/sync/remove with typed output

**Files:**

- Create: cmd/hotplex/skills_cmd.go
- Create: cmd/hotplex/skills_status.go
- Create: cmd/hotplex/skills_sync.go
- Create: cmd/hotplex/skills_remove.go
- Create: cmd/hotplex/skills_cmd_test.go
- Modify: cmd/hotplex/root.go
- Modify: cmd/hotplex/main.go only if root registration requires the Task 1 extraction

**Interfaces:**

- Consumes Config.EnabledWorkerTypes, builtin.NewRegistry, reconcile.New, reconcile.Options, reconcile.Report, and existing configFlag.
- Produces:

~~~go
type skillsCommandDeps struct {
    LoadConfig      func(string) (*config.Config, error)
    UserHomeDir     func() string
    HotplexHomeDir  func() string
    NewReconciler   func(userHome, hotplexHome string) (*reconcile.Reconciler, error)
    Output          io.Writer
}

func newSkillsCmd() *cobra.Command
func newSkillsCmdWithDeps(deps skillsCommandDeps) *cobra.Command
~~~

Production newSkillsCmd supplies config.Load, os.UserHomeDir, config.HotplexHome, builtin.NewRegistry, and explicit UserHome/HotplexHome Paths. Tests use newSkillsCmdWithDeps with separate temp userHome and hotplexHome directories. Every status, sync, and remove command passes the selected profile to the same cumulative ProfilePackageSet.

The subcommands are newSkillsStatusCmd, newSkillsSyncCmd, and newSkillsRemoveCmd. --profile accepts only runtime and operator. --worker is a repeatable string-array flag converted through reconcile.ParseWorkerType. --dry-run exists on sync and remove. --json exists on all three. With no explicit --worker, the command uses Config.EnabledWorkerTypes; an empty result emits a typed bounded error requiring --worker. Human output is rendered from Report; JSON output encodes Report directly. ACP produces a failed/unsupported_worker item and performs no writes.

- [ ] Step 1: Add red command and output tests.

~~~go
func TestSkillsCommandsExposeClosedSubcommandsAndFlags(t *testing.T) {}
func TestSkillsSyncUsesResolvedConfigTargetsWhenWorkerFlagIsAbsent(t *testing.T) {}
func TestSkillsCommandRequiresExplicitWorkerWhenResolvedSetIsEmpty(t *testing.T) {}
func TestSkillsCommandRejectsUnknownProfileAndWorker(t *testing.T) {}
func TestSkillsCommandACPProducesUnsupportedReportWithoutWrite(t *testing.T) {}
func TestSkillsDryRunDoesNotChangeUserOrHotplexHome(t *testing.T) {}
func TestSkillsJSONOutputIsReportOnly(t *testing.T) {}
func TestSkillsCommandReturnsBoundedErrorForConflictAndDrift(t *testing.T) {}
~~~

The JSON test decodes output into reconcile.Report and rejects human path/error prose mixed into the JSON stream.

- [ ] Step 2: Run the command red tests.

~~~bash
go test ./cmd/hotplex -run 'TestSkills(Commands|Sync|Command|DryRun|JSON)' -count=1
~~~

Expected: compilation fails because the skills command tree, dependency seam, and typed output functions do not exist.

- [ ] Step 3: Implement command dependency construction and validation.

Build every subcommand with a shared loader:

~~~go
func loadSkillsOptions(
    cmd *cobra.Command,
    configPath string,
    profile string,
    workerFlags []string,
    dryRun bool,
) (reconcile.Options, error) {
    // Expand and load config.
    // Parse the closed profile.
    // Parse every --worker value.
    // If no explicit workers, use Config.EnabledWorkerTypes().
    // If still empty, return reconcile.ErrNoWorkerTargets.
    // Return typed options only; never concatenate a path or shell command.
}
~~~

The loader passes UserHomeDir to native-target resolution and HotplexHomeDir to inventory/state resolution. Call only Status, Sync, or Remove from each handler. Print the typed report after the operation, then return `Report.Err()` for any conflict, drift, or failed item; unchanged and changed reports return nil. This preserves report observability while giving scripts a bounded non-zero exit code when explicit action is required.

- [ ] Step 4: Register the command and implement presentation.

Add newSkillsCmd to newRootCmd beside newCronCmd. Human output contains action, outcome, reason code, aliases, and bounded presentation paths. JSON output uses json.NewEncoder and no additional stdout writes. Do not expose receipt hashes as control input or print environment values.

- [ ] Step 5: Run green tests and package verification.

~~~bash
gofmt -w cmd/hotplex/skills_cmd.go cmd/hotplex/skills_status.go cmd/hotplex/skills_sync.go cmd/hotplex/skills_remove.go cmd/hotplex/skills_cmd_test.go cmd/hotplex/root.go
go test ./cmd/hotplex -run 'TestSkills(Commands|Sync|Command|DryRun|JSON)' -count=1 -race -shuffle=on
go test ./cmd/hotplex -count=1 -race -shuffle=on
~~~

Expected: all command validation, output, ACP, empty-target, and zero-write tests pass.

- [ ] Step 6: Commit the CLI slice.

~~~bash
git add cmd/hotplex/root.go cmd/hotplex/skills_cmd.go cmd/hotplex/skills_status.go cmd/hotplex/skills_sync.go cmd/hotplex/skills_remove.go cmd/hotplex/skills_cmd_test.go
git commit -m "feat(cli): add built-in skill lifecycle commands"
~~~

### Task 5: Add Claude native catalog evidence and preserve the Worker matrix

**Files:**

- Create: internal/worker/claudecode/skill_catalog.go
- Create: internal/worker/claudecode/skill_catalog_test.go
- Modify only if required by the explicit native-root helper: internal/skills/scanner.go
- Test scanner changes in: internal/skills/scanner_test.go
- Modify: internal/worker/claudecode/worker.go
- Modify: internal/gateway/native_catalog_test.go
- Modify: internal/gateway/skill_dispatch_matrix_test.go
- Modify: internal/gateway/native_command_contract_test.go
- Modify: internal/worker/native_commands_test.go

**Interfaces:**

- Consumes the existing optional worker.SkillCatalogProvider, worker.NativeCommandCatalogProvider, worker.SkillDescriptor, worker.NativeCommandDescriptor, and worker.AsNativeCatalogProvider.
- Produces Claude’s exact native-root catalog provider without adding a method to worker.Worker:

~~~go
func (w *Worker) ListInvokableSkills(ctx context.Context, workDir string) ([]worker.SkillDescriptor, error)
~~~

Claude Worker stores an injectable userHomeDir func() (string, error) for tests and defaults to os.UserHomeDir in New. The provider reads only <UserHome>/.claude/skills, uses the explicit shallow ScanRoot helper, maps Name, Description, and absolute Path, and never scans HotplexHome/.hotplex/skills, the project root, or the Built-in inventory.

- [ ] Step 1: Add red Claude catalog and matrix tests.

Create this temp-home fixture:

~~~text
<temp-home>/.claude/skills/oracle-dba/SKILL.md
<temp-home>/.hotplex/skills/not-native/SKILL.md
<temp-home>/project/.claude/skills/project-only/SKILL.md
~~~

Add:

~~~go
func TestClaudeCatalogUsesOnlyExactNativeRoot(t *testing.T) {}
func TestClaudeCatalogReturnsNativePathAndMetadata(t *testing.T) {}
func TestClaudeCatalogSkipsSymlinkEntries(t *testing.T) {}
func TestFilesystemOnlySkillRemainsDiscoverableForWorkerWithoutEvidence(t *testing.T) {}
func TestNativeCatalogMatrixKeepsCodexOpenCodeACPEvidence(t *testing.T) {}
func TestProjectionAloneDoesNotMakeACPCallable(t *testing.T) {}
~~~

The Gateway matrix asserts that short /name, explicit /worker name, WebChat/structured resolution, busy replay, and crash replay all use one callable decision. A file under a non-native root remains discoverable and returns the existing bounded NOT_SUPPORTED path.

- [ ] Step 2: Run the evidence red tests.

~~~bash
go test ./internal/worker/claudecode ./internal/gateway -run 'Test(ClaudeCatalog|NativeCatalogMatrix|ProjectionAlone|FilesystemOnly)' -count=1
~~~

Expected: Claude tests fail because ListInvokableSkills and its injected home boundary do not exist; matrix tests fail because Claude currently has no catalog evidence.

- [ ] Step 3: Implement the bounded Claude provider.

Add the injectable home resolver to Claude Worker while preserving constructors and behavior:

~~~go
type Worker struct {
    // existing fields
    userHomeDir func() (string, error)
}

func (w *Worker) nativeSkillRoot() (string, error) {
    resolve := w.userHomeDir
    if resolve == nil {
        resolve = os.UserHomeDir
    }
    userHome, err := resolve()
    if err != nil {
        return "", fmt.Errorf("claude: resolve home: %w", err)
    }
    return filepath.Join(userHome, ".claude", "skills"), nil
}
~~~

Implement ListInvokableSkills with context cancellation, exact-root scanning, frontmatter validation, bounded metadata, and deterministic name sorting. Add compile-time assertion var _ worker.SkillCatalogProvider = (*Worker)(nil). Do not alter worker.Worker, NativeCommandKind, SkillStatus, or AEP types.

- [ ] Step 4: Verify Gateway evidence precedence and stale behavior.

Keep internal/gateway/native_catalog.go precedence as Gateway fixed commands > authoritative Worker catalog > filesystem-only skills. Add tests proving that a Claude native descriptor is callable, a .hotplex/skills descriptor is discoverable only, and a stale/missing Worker query never falls back to filesystem invocation. Keep DeclaredSkillCatalogOwner as worker for normal Claude/Codex/OpenCode/ACP Workers and none only for unknown/noop/nil.

- [ ] Step 5: Run green Worker/Gateway tests.

~~~bash
gofmt -w internal/worker/claudecode/worker.go internal/worker/claudecode/skill_catalog.go internal/worker/claudecode/skill_catalog_test.go internal/skills/scanner.go internal/skills/scanner_test.go internal/gateway/native_catalog_test.go internal/gateway/skill_dispatch_matrix_test.go internal/gateway/native_command_contract_test.go internal/worker/native_commands_test.go
go test ./internal/worker/claudecode ./internal/worker/codexcli ./internal/worker/opencodeserver ./internal/worker/acp ./internal/gateway -count=1 -race -shuffle=on
~~~

Expected: Claude, Codex, OpenCode, ACP, catalog merge, dispatch matrix, and replay revalidation tests pass without an AEP or Worker-main-interface change.

- [ ] Step 6: Commit the Worker evidence slice.

~~~bash
git add internal/worker/claudecode internal/skills/scanner.go internal/skills/scanner_test.go internal/gateway/native_catalog_test.go internal/gateway/skill_dispatch_matrix_test.go internal/gateway/native_command_contract_test.go internal/worker/native_commands_test.go
git commit -m "feat(worker): verify Claude native skill catalog"
~~~

### Task 6: Wire explicit lifecycle behavior into onboard, doctor, update, and Gateway startup

**Files:**

- Create: internal/cli/checkers/builtin_skills.go
- Create: internal/cli/checkers/builtin_skills_test.go
- Modify: internal/cli/onboard/wizard.go
- Modify: internal/cli/onboard/wizard_test.go
- Modify: cmd/hotplex/onboard.go
- Create: cmd/hotplex/update_skills_test.go
- Modify: cmd/hotplex/update.go
- Modify: cmd/hotplex/doctor.go
- Modify: cmd/hotplex/gateway_run.go
- Create: cmd/hotplex/builtin_skills_lifecycle_test.go

**Interfaces:**

- Consumes the Task 2 Runner:

~~~go
type Runner interface {
    Status(context.Context, Options) (Report, error)
    Sync(context.Context, Options) (Report, error)
    Remove(context.Context, Options) (Report, error)
}
~~~

- Extends onboard.WizardOptions with SyncSkills bool and injectable SkillRunner reconcile.Runner. The CLI passes SyncSkills only for explicit --sync-skills; tests pass a temp-path runner.
- Adds NewBuiltinSkillsChecker(statusFn func(context.Context) (reconcile.Report, error)) cli.Checker. The checker has no FixFunc, is read-only, and is registered under skills.
- Adds --sync-skills to onboard and update. Onboard uses runtime profile only. Update accepts closed --skills-profile runtime|operator when --sync-skills is present; absent --sync-skills performs no native-root write.
- Lifecycle construction injects separate UserHomeDir and HotplexHomeDir values into reconciliation. Gateway startup calls read-only status after both paths are resolved and logs bounded drift reason codes; it never calls Sync, Remove, or a write operation.

- [ ] Step 1: Add red checker and lifecycle tests.

Add tests with a fake typed runner and temp paths:

~~~go
func TestBuiltinSkillsCheckerIsReadOnlyAndHasNoFix(t *testing.T) {}
func TestOnboardDoesNotSyncWithoutExplicitFlag(t *testing.T) {}
func TestOnboardExplicitSyncUsesRuntimeProfileOnly(t *testing.T) {}
func TestOnboardReportsTypedSyncOutcomeAndRoots(t *testing.T) {}
func TestUpdateDoesNotSyncWithoutExplicitFlag(t *testing.T) {}
func TestUpdateExplicitSyncUsesSelectedProfile(t *testing.T) {}
func TestGatewayStartupStatusDoesNotWrite(t *testing.T) {}
func TestDoctorSkillsStatusDoesNotWrite(t *testing.T) {}
~~~

Snapshot every entry under the temp HotPlex home before and after ordinary onboard, update, doctor, and Gateway status paths and require byte-identical trees. Assert that doctor --fix does not call the Built-in checker’s nonexistent FixFunc.

- [ ] Step 2: Run the lifecycle red tests.

~~~bash
go test ./internal/cli/checkers ./internal/cli/onboard ./cmd/hotplex -run 'Test(BuiltinSkills|Onboard|Update|GatewayStartup|DoctorSkills)' -count=1
~~~

Expected: compilation fails because the checker, SyncSkills option, update flag, and lifecycle runner seams do not exist.

- [ ] Step 3: Implement the read-only checker.

Construct the checker with an injected status function and map report items to existing cli.Diagnostic fields. Status and drift become pass/warn diagnostics; conflict, failed, unsupported, and invalid receipt items become fail diagnostics with stable reason codes. Leave FixFunc nil. Register it without changing the existing AgentConfig checker’s legacy SKILLS.md behavior.

- [ ] Step 4: Implement explicit onboard synchronization.

Add SyncSkills and SkillRunner to WizardOptions. In both full-config and keep-existing flows, run stepBuiltinSkills only when SyncSkills is true. Load the resulting config, call EnabledWorkerTypes, build runtime reconcile.Options, and call the injected runner. Format the step only from the typed report, including canonical root and aliases. Do not select operator profile in onboard.

Add --sync-skills to cmd/hotplex/onboard.go, pass it to WizardOptions, and update help text to state that the flag writes only explicit runtime projections.

- [ ] Step 5: Implement explicit update synchronization and read-only startup.

Add --sync-skills and --skills-profile to newUpdateCmd. After successful binary replacement and before optional restart, call the typed runner only when --sync-skills is set. Keep internal/updater/updater.go binary-only. If synchronization fails, print the typed report, return its bounded error, and do not report projection success.

After Gateway configuration and skills.NewLocator creation, create a read-only reconciliation status reader and emit only bounded drift/conflict reason codes. Keep GatewayDeps.SkillsLocator and all existing user-Skill APIs unchanged.

- [ ] Step 6: Run green lifecycle tests.

~~~bash
gofmt -w internal/cli/checkers/builtin_skills.go internal/cli/checkers/builtin_skills_test.go internal/cli/onboard/wizard.go internal/cli/onboard/wizard_test.go cmd/hotplex/onboard.go cmd/hotplex/update.go cmd/hotplex/update_skills_test.go cmd/hotplex/doctor.go cmd/hotplex/gateway_run.go cmd/hotplex/builtin_skills_lifecycle_test.go
go test ./internal/cli/checkers ./internal/cli/onboard ./cmd/hotplex -count=1 -race -shuffle=on
~~~

Expected: status-only ordinary paths remain zero-write, explicit onboard/update paths call the same reconciliation runner, and doctor/Gateway status never mutate native roots.

- [ ] Step 7: Commit the lifecycle slice.

~~~bash
git add internal/cli/checkers/builtin_skills.go internal/cli/checkers/builtin_skills_test.go internal/cli/onboard/wizard.go internal/cli/onboard/wizard_test.go cmd/hotplex/onboard.go cmd/hotplex/update.go cmd/hotplex/update_skills_test.go cmd/hotplex/doctor.go cmd/hotplex/gateway_run.go cmd/hotplex/builtin_skills_lifecycle_test.go
git commit -m "feat(skills): wire explicit lifecycle synchronization"
~~~

### Task 7: Expose optional Built-in provenance in Admin and WebChat as read-only metadata

**Files:**

- Create: internal/skills/builtin/public.go
- Create: internal/skills/builtin/public_test.go
- Modify: internal/skills/skills.go
- Modify: internal/admin/admin.go
- Modify: internal/admin/skill_handlers.go
- Modify: internal/admin/skill_handlers_test.go
- Modify: internal/gateway/skill_handlers.go
- Modify: internal/gateway/skill_handlers_test.go
- Modify: cmd/hotplex/gateway_run.go
- Modify: cmd/hotplex/routes.go
- Modify: webchat/lib/api/skills.ts
- Modify: webchat/lib/api/admin-skills.ts
- Modify: webchat/app/components/chat/settings-modal/skills-tab.tsx
- Modify: webchat/app/admin/skills/page.tsx
- Create or modify the nearest existing WebChat Skills component test files for the read-only badge and action guard

**Interfaces:**

- Adds only optional fields to skills.Skill and matching HTTP/TypeScript models:

~~~go
Builtin               bool
BuiltinPackageVersion string
~~~

- Produces a read-only builtin.PublicCatalog:

~~~go
type PublicCatalog interface {
    List(context.Context, string) ([]skills.Skill, error)
    Read(context.Context, string, string) (*skills.Detail, error)
}
~~~

The provider combines the embedded canonical registry with the immutable HotplexHome inventory and does not require an existing Worker projection or native projection receipt for visibility. It does not call user CRUD methods. It returns SourceGlobal to preserve the current source contract, Managed=false, and optional Built-in fields. Admin and WebChat handlers merge Built-ins only into read surfaces. User/project entries with the same name win; CLI reconciliation status remains the collision diagnostic source. Built-ins return a bounded read-only error from update/delete/replace paths.

- [ ] Step 1: Add red Go and TypeScript model/handler tests.

~~~go
func TestBuiltinPublicCatalogListsCanonicalAndInventoryWithoutProjection(t *testing.T) {}
func TestAdminListUserSkillShadowsBuiltinSameName(t *testing.T) {}
func TestAdminBuiltinDetailIsReadableButNotMutable(t *testing.T) {}
func TestWebChatMergedListIncludesUniqueBuiltinAsReadOnly(t *testing.T) {}
func TestWorkspaceSkillCRUDNeverMutatesBuiltinInventory(t *testing.T) {}
~~~

Require source == global, builtin == true, a non-empty builtin_package_version, managed == false, and stable SKILL_BUILTIN_READONLY for update/delete/replace. Add a WebChat test that renders a Built-in badge and disables edit/delete controls while retaining user same-name precedence.

- [ ] Step 2: Run the provenance red tests.

~~~bash
go test ./internal/skills/builtin ./internal/admin ./internal/gateway -run 'Test(Builtin|Admin.*Builtin|WebChat.*Builtin|Workspace.*Builtin)' -count=1
cd webchat && pnpm exec vitest run lib/api app/components/chat/settings-modal app/admin/skills --passWithNoTests
~~~

Expected: compilation fails because optional fields, public catalog, handler injection, and read-only guards do not exist; the UI test fails because no Built-in badge/action guard exists.

- [ ] Step 3: Implement the canonical/inventory public provider.

Enumerate the embedded ProfilePackageSet and reconcile it with immutable inventory directories under HotplexHome. A package remains discoverable when its inventory exists without any native projection or projection receipt. Resolve package body and reference file names from the embedded registry or verified immutable inventory, never from a user-writable native projection. Set optional provenance fields and preserve the existing Skill.FilePath omission from JSON.

- [ ] Step 4: Merge read-only metadata without changing CRUD semantics.

Add SetBuiltinSkillsCatalog to AdminAPI and SkillHandlers. For list/get reads, merge unique Built-ins after user entries and suppress a Built-in with a user/project name collision. For Admin install/update/delete and workspace install/delete, reject a target name matching a Built-in with SKILL_BUILTIN_READONLY; do not pass it to Locator.Install, CreateText, Update, or Delete.

Wire the provider from cmd/hotplex/gateway_run.go and cmd/hotplex/routes.go. Keep /skills semantics as real Agent Skills; do not expose TOOLS.md, META-COGNITION.md, or other AgentConfig files.

- [ ] Step 5: Implement WebChat optional fields and action guards.

Add optional builtin and builtin_package_version fields to Skill, SkillDetail, SkillInstallResult, AdminSkill, and AdminSkillDetail. Render a non-editable Built-in badge for builtin == true; do not add source == builtin logic. Disable edit/delete/replace controls for Built-ins and leave all user/project controls unchanged.

- [ ] Step 6: Run green API/UI tests and compatibility checks.

~~~bash
gofmt -w internal/skills/skills.go internal/skills/builtin/public.go internal/skills/builtin/public_test.go internal/admin/admin.go internal/admin/skill_handlers.go internal/admin/skill_handlers_test.go internal/gateway/skill_handlers.go internal/gateway/skill_handlers_test.go cmd/hotplex/gateway_run.go cmd/hotplex/routes.go
go test ./internal/skills/builtin ./internal/admin ./internal/gateway -count=1 -race -shuffle=on
cd webchat && pnpm exec vitest run lib/api app/components/chat/settings-modal app/admin/skills --passWithNoTests
pnpm exec tsc --noEmit
~~~

Expected: API fields remain backward-compatible when omitted, Built-in reads are read-only, user collisions resolve deterministically, and WebChat type/tests pass.

- [ ] Step 7: Commit the provenance slice.

~~~bash
git add internal/skills/skills.go internal/skills/builtin/public.go internal/skills/builtin/public_test.go internal/admin/admin.go internal/admin/skill_handlers.go internal/admin/skill_handlers_test.go internal/gateway/skill_handlers.go internal/gateway/skill_handlers_test.go cmd/hotplex/gateway_run.go cmd/hotplex/routes.go webchat/lib/api/skills.ts webchat/lib/api/admin-skills.ts webchat/app/components/chat/settings-modal/skills-tab.tsx webchat/app/admin/skills/page.tsx
git commit -m "feat(skills): expose built-in provenance read-only"
~~~

### Task 8: Update current documentation, Skill validation, and full Phase B gates

**Files:**

- Modify: docs/explanation/agent-config-system.md
- Modify: docs/explanation/cron-design.md
- Modify: docs/tutorials/agent-personality.md
- Modify: docs/tutorials/skills-setup.md
- Modify: docs/tutorials/cron-scheduled-tasks.md
- Modify: docs/guides/user/commands-cheatsheet.md
- Modify: docs/reference/cli.md
- Modify: docs/reference/configuration.md
- Modify: docs/reference/admin-api.md
- Create: internal/skills/builtin/quality_test.go
- Create or modify: cmd/hotplex/skill_examples_test.go

**Interfaces:**

- Consumes the shipped canonical registry, generated CLI surface, Cron Cobra validators, Config.EnabledWorkerTypes, typed reconciliation reports, and optional HTTP provenance fields.
- Produces documentation covering hotplex-cli and hotplex-operator scope/authorization; hotplex skills status/sync/remove; profiles, --worker, --dry-run, JSON reports, and empty-target behavior; inventory versus native roots; receipt/hash/backup/conflict behavior; zero-write Gateway startup; TOOLS.md/legacy SKILLS.md versus real Agent Skills; /skills, Admin API Skills, and WebChat Skills; Cron schedule forms and independent cron get; and the fact that Phase C legacy manual migration is not shipped.

- [ ] Step 1: Add red documentation/Skill quality tests.

Create:

~~~go
func TestBuiltinSkillFrontmatterAndReferenceClosure(t *testing.T) {}
func TestBuiltinDescriptionsHavePositiveAndNegativeBoundaries(t *testing.T) {}
func TestGeneratedCLISurfaceHasNoHiddenOrSensitiveValues(t *testing.T) {}
func TestCronExamplesMatchCurrentCobraValidators(t *testing.T) {}
func TestDocsDescribeInventoryProjectionAndExplicitSync(t *testing.T) {}
~~~

The tests parse every frontmatter block, require directory-name/name equality, require one-level reference links that resolve inside the package, reject shell-specific date arithmetic, and reject docs claiming automatic startup synchronization, Built-in CRUD mutability, source builtin, or ACP filesystem support.

- [ ] Step 2: Run the documentation red tests.

~~~bash
go test ./internal/skills/builtin ./cmd/hotplex -run 'Test(BuiltinSkill|GeneratedCLISurface|CronExamples|DocsDescribe)' -count=1
~~~

Expected: the tests fail against the not-yet-updated current documentation and Skill reference wording.

- [ ] Step 3: Update the named current documentation.

Keep historical archive/spec documents unchanged. Replace exhaustive hand-authored CLI inventories with links to generated cli-surface.generated.md; retain decision-oriented Cron, Slack, diagnostics, operator, sync, and verification guidance. State that running hotplex <domain> --help wins over prose. State that a Skill listing does not grant invocation and discoverable is not callable.

- [ ] Step 4: Add parser-backed example checks.

Use the actual newCronCmd() and its validators to parse examples for all four schedule forms, --attach, recurring lifecycle flags, retries, timeout, silent/delete-after-run, and allowed tools. Assert every create example includes an independent cron get step; never execute a network mutation or create a real Cron job during the test.

- [ ] Step 5: Run green quality and full gates.

~~~bash
gofmt -w internal/skills/builtin/quality_test.go cmd/hotplex/skill_examples_test.go
go test ./internal/skills/builtin ./cmd/hotplex -count=1 -race -shuffle=on
go test ./internal/agentconfig ./internal/config ./internal/skills/... ./internal/cli/... ./internal/admin ./internal/gateway ./internal/worker/... -count=1 -race -shuffle=on
cd webchat && pnpm exec vitest run --passWithNoTests && pnpm exec tsc --noEmit
cd ..
go test ./... -count=1
go run ./cmd/hotplex --internal-generate-cli-surface --output internal/skills/builtin/hotplex-cli/references/cli-surface.generated.md
go run ./cmd/gen-builtin-skills
git diff --exit-code -- internal/skills/builtin .agents/skills/hotplex-cli
git diff --check
~~~

Expected: focused race suites, WebChat tests/typecheck, full Go tests, generated artifact parity, and whitespace checks all pass. The generation diff command exits zero with no output.

- [ ] Step 6: Commit the documentation and quality slice.

~~~bash
git add docs/explanation/agent-config-system.md docs/explanation/cron-design.md docs/tutorials/agent-personality.md docs/tutorials/skills-setup.md docs/tutorials/cron-scheduled-tasks.md docs/guides/user/commands-cheatsheet.md docs/reference/cli.md docs/reference/configuration.md docs/reference/admin-api.md internal/skills/builtin/quality_test.go cmd/hotplex/skill_examples_test.go
git commit -m "docs: document built-in skill operations"
~~~

## Dependency and Review Order

1. Task 1 establishes canonical bytes, manifest types, generated CLI output, and .agents/skills/hotplex-cli parity.
2. Task 2 consumes the registry and establishes the only native-root write/recovery engine.
3. Task 3 establishes the configuration-to-Worker target boundary without importing internal/worker.
4. Task 4 exposes typed CLI commands and validates all profile/Worker values.
5. Task 5 adds Claude native catalog evidence and proves the existing Codex/OpenCode/ACP matrix remains fail-closed.
6. Task 6 connects explicit synchronization to lifecycle commands while keeping doctor and Gateway startup read-only.
7. Task 7 exposes optional HTTP/UI provenance and keeps all Built-ins read-only.
8. Task 8 updates current documentation, validates references and examples against Cobra validators, and runs the full gate.

## Acceptance Coverage

- Spec §9 is covered by Tasks 1 and 8: portable frontmatter, progressive-disclosure routers, references, generated CLI surface, Cron/Slack/diagnostics/operator boundaries, and deferred legacy domains.
- Spec §10 is covered by Tasks 1, 2, 3, 4, and 6: embedded inventory, explicit profiles, resolved target selection, receipts, canonical roots, safe staging/rollback, status/dry-run, and lifecycle policy.
- Spec §11 is covered by Tasks 4, 5, and 7: evidence tiers, native catalog ownership, real Agent Skills API meaning, optional provenance, and unified callability.
- Spec §12 is covered by Tasks 1, 2, 4, 5, and 6: bounded discovery, closed enums, path/symlink/hash validation, explicit authorization, no secret output, and fail-closed stale evidence.
- Spec §13 is covered by Tasks 5, 7, and 8: no Worker/AEP break, preserved source values, AgentConfig compatibility, generated Cron compatibility, and safe rollback.
- Spec §14 is covered by every task’s red/green tests and Task 8’s focused race, WebChat, generated-drift, and full Go gates.
- Spec §15 is covered by the eight independently committed tasks, with Phase C explicitly excluded.

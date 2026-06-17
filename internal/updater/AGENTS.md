# Self-Update Package

## OVERVIEW
GitHub Releases-driven binary self-update: check latest, download archive, verify sha256, extract, atomic rename. Used by `cmd/hotplex/update.go`. Windows cannot replace a running exe and falls back to `scripts/install.ps1`.

## STRUCTURE
```
updater/
  updater.go        # Full pipeline: Check → Download → VerifyChecksum → Extract → Replace
  updater_test.go   # Table-driven tests
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Build updater | `updater.go:60` New | Production defaults: repo `hrygo/hotplex`, 30s client, GOOS/GOARCH from runtime |
| Query latest release | `updater.go:90` Check | `GET /repos/{repo}/releases/latest`, 1MB JSON cap, 403 = rate-limited |
| Resolve asset | `updater.go:120` Check loop | Prefers archive; legacy raw-binary fallback sets `IsLegacyBinary` |
| Download archive | `updater.go:157` Download | New client without Timeout (ctx controls deadline), 200MB cap |
| Verify checksum | `updater.go:292` VerifyChecksum | Downloads `checksums.txt` (64KB cap), sha256 compare via `EqualFold` |
| Extract binary | `updater.go:209` extractBinary | zip on Windows, tar.gz elsewhere; path-traversal guard (`..`/abs rejected) |
| Atomic replace | `updater.go:342` Replace | 3-step rename: `exe → exe.old`, `new → exe`, remove backup; rollback on step-2 failure |
| Pre-flight writability | `updater.go:379` IsWritable | Resolves symlinks, probes `O_WRONLY`; hints `sudo` on EPERM |
| Docker detection | `updater.go:393` IsDocker | `/.dockerenv` or cgroup scan; caller disables self-update |
| Version compare | `updater.go:437` versionEqual | `v` prefix normalized; equality only, no semver ordering |

## KEY PATTERNS

**Atomic rename (Replace)**
- Renames live exe to `exe.old` then moves new binary into place. Relies on POSIX rename atomicity on the same filesystem.
- On failure mid-install, rolls the backup back so the prior version keeps running.
- Backup `.old` removal is best-effort (`_ = os.Remove`).

**Archive selection (Check)**
- Two asset shapes supported: `hotplex-{os}-{arch}.tar.gz` (or `.zip` on Windows) and the legacy raw `hotplex-{os}-{arch}` binary.
- Sets `CheckResult.IsLegacyBinary` so callers know the pre-archive asset was used.

**Checksum enforcement**
- `VerifyChecksum` refuses empty `checksums.txt` URL rather than silently skip. Parses `{hash}  {filename}` lines, tolerant of path prefixes via `filepath.Base`.
- `EqualFold` compare handles lowercase/uppercase hex mix.

**Path safety in extraction**
- `extractFromTarGz` rejects entries containing `..` or absolute paths before writing.
- Both extractors cap output at 200MB via `io.LimitReader`.

**Docker short-circuit**
- `IsDocker()` lets `update.go` surface a user-facing message instead of attempting an in-place replace inside a throwaway container layer.

## ANTI-PATTERNS
- ❌ Call `Replace` on Windows — running exe is locked; redirect users to `scripts/install.ps1`
- ❌ Skip `VerifyChecksum` when `checksums.txt` is absent — package returns an error by design
- ❌ Use a single shared `http.Client` with `Timeout` for download — large files time out; package builds a no-timeout client and relies on `ctx`
- ❌ Trust `versionEqual` for ordering — it is equality only; there is no semver `<`/`>` here

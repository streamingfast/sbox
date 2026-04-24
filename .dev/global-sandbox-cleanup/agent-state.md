# Feature: global-sandbox-cleanup — Agent State

## Status

Complete. Build passes, all tests pass. Feedback 2 UI rework also complete.

## Implementation Plan

### 1. Last-used timestamp tracking (in `entrypoint.go` / `prune.go`)

- Add `WriteLastUsed(workspaceDir string) error` to a new `prune.go` file in the `sbox` package.
  Writes `time.Now().UTC().Format(time.RFC3339)` to `<workspaceDir>/.sbox/last-used`.
- Call `WriteLastUsed` from `PrepareSboxDirectory` in `entrypoint.go`, after creating the `.sbox` dir.
- Add `ReadLastUsed(workspaceDir string) (time.Time, error)` for reading it back.

### 2. Prune logic (new file `prune.go` in `sbox` package)

Types and functions:
- `PruneCandidate` struct: `SandboxName`, `WorkspacePath`, `LastUsed time.Time`, `Reason string`, `IsStale bool`
- `PruneOptions` struct: `Keep int`
- `FindPruneCandidates(opts PruneOptions) ([]PruneCandidate, error)`
  - Call `ListDockerSandboxes()` to get all sandboxes from `docker sandbox ls`
  - Call `ListProjects()` to get all known project entries
  - Cross-reference: build a map of workspace -> DockerSandbox
  - For each known project entry:
    - If workspace path does not exist on disk => stale, add as candidate
    - Otherwise read `.sbox/last-used` for last-used time
  - Also check for docker sandboxes not in any project entry (purely orphaned docker sandbox)
  - Sort by last-used ascending (oldest first)
  - Keep the N most recently used (by `opts.Keep`); mark the rest as candidates
  - Stale ones are always candidates regardless of keep
  - Return the candidate list (sorted: stale first, then oldest-first for the rest)
- `PruneAll(candidates []PruneCandidate, config *Config) []PruneError`
  - For each candidate:
    1. `docker sandbox rm <name>` if sandbox exists
    2. `os.RemoveAll(<workspacePath>/.sbox)` if workspace exists
    3. `RemoveProjectData(<workspacePath>)` to remove project config entry

### 3. CLI command (`cmd/sbox/prune.go`)

- `PruneCommand` using the `Command(...)` pattern
- Flags: `--keep int` (default 5), `--force bool`
- In dry-run mode: print candidates table and exit
- In force mode: call `PruneAll`, report results
- Register `PruneCommand` in `main.go`

## Task Checklist

- [x] Write `prune.go` in `sbox` package with `WriteLastUsed`, `ReadLastUsed`, `FindPruneCandidates`, `PruneAll`
- [x] Call `WriteLastUsed` from `PrepareSboxDirectory` in `entrypoint.go`
- [x] Write `cmd/sbox/prune.go` CLI command
- [x] Register `PruneCommand` in `cmd/sbox/main.go`
- [x] Run tests and build
- [x] Update CHANGELOG.md

## Feedback 2 — UI Rework (2026-04-24)

- [x] Create `stylex/stylex.go` package (copied from firehose-core)
- [x] Update `FindPruneCandidates` to return `(candidates, kept []PruneCandidate, err error)`
- [x] Rewrite `cmd/sbox/prune.go` output with three lipgloss table sections (Missing / Too old / Keeping)
- [x] `go mod tidy` to promote `charmbracelet/lipgloss` and `go-isatty` to direct deps
- [x] All tests pass, build succeeds

## Notes

- `ListDockerSandboxes()` already exists in `sandbox.go` and parses `docker sandbox ls` output.
- `ListProjects()` already exists in `config.go` and reads `~/.config/sbox/projects/`.
- `RemoveProjectData()` already exists in `config.go`.
- `RemoveDockerSandbox()` / `RemoveDockerSandboxByName()` already exist in `sandbox.go`.
- The `.sbox/last-used` file is a plain RFC3339 timestamp string.
- `PrepareSboxDirectory` is called by sandbox.go, backend_sandbox.go, backend_container.go.

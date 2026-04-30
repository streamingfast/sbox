# Feature: global-stop — Agent State

## Status

Complete. Build passes, all tests pass.

## Design

`sbox stop` is converted from a `Command(...)` leaf to a `CommandOptionFunc` that registers
a `*cobra.Command` with `RunE` (the existing stop-current-project behavior) AND child commands
for global stop. This preserves 100% backward compatibility while adding new subcommands.

### Command Structure

- `sbox stop [--rm] [--all] [-w workspace]` — unchanged, stops current project's sandbox/container
- `sbox stop all [--keep N] [--force]` — stop all running sandboxes + containers globally
- `sbox stop sandbox [--keep N] [--force]` — stop running sandboxes only (global)
- `sbox stop container [--keep N] [--force]` — stop running containers only (global)

Default `--keep` is 3 (keep 3 most recently used running).
Default behavior is dry-run; use `--force` to actually stop.

### Implementation

- `sandbox.go`: Added `StopDockerSandboxByName(name string) error` — runs `docker sandbox stop <name>`
- `stop_global.go` (new, sbox package):
  - `StopOptions` struct: `Keep int`
  - `FindSandboxStopCandidates(opts StopOptions) (candidates, kept []PruneCandidate, err error)` — reuses `FindPruneCandidates` with huge keep to get full pool, then filters to running sandboxes, splits keep vs stop candidates
  - `FindContainerStopCandidates(opts StopOptions) (candidates, kept []ContainerPruneCandidate, err error)` — similarly reuses `FindContainerPruneCandidates`
  - `StopOneSandboxCandidate(c PruneCandidate) error` — calls `StopDockerSandboxByName`
  - `StopOneContainerCandidate(c ContainerPruneCandidate) error` — runs `docker stop <containerID>`
- `cmd/sbox/stop.go`: Completely rewritten:
  - `StopCommand` converted to `CommandOptionFunc` registering a `*cobra.Command` with `RunE = stopE` and child commands
  - `stopE` — unchanged logic for current-project stop
  - `newStopAllCmd()`, `newStopSandboxCmd()`, `newStopContainerCmd()` — build the three global stop subcommands
  - `stopSandboxes(cmd, suppressDryRunHeader)` / `stopContainers(cmd, suppressDryRunHeader)` — shared logic
  - Table rendering via `printSandboxStopSection` / `printContainerStopSection` using lipgloss + stylex (same style as prune)
- `CHANGELOG.md`: Entry added under new `## Unreleased` section

## Task Checklist

- [x] Add `StopDockerSandboxByName` to `sandbox.go`
- [x] Create `stop_global.go` with `StopOptions`, `FindSandboxStopCandidates`, `FindContainerStopCandidates`, `StopOneSandboxCandidate`, `StopOneContainerCandidate`
- [x] Convert `StopCommand` from `Command(...)` to `CommandOptionFunc` with `RunE` + child commands
- [x] Add `newStopAllCmd`, `newStopSandboxCmd`, `newStopContainerCmd` helpers
- [x] Add `stopSandboxes`, `stopContainers` shared logic
- [x] Add `printSandboxStopSection`, `printContainerStopSection` table rendering
- [x] `go test ./...` passes
- [x] `go build ./...` passes
- [x] CHANGELOG.md updated
- [x] Agent-state.md updated

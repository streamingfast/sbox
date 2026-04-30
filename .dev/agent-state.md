# sbox Agent State

## Features Tracking

- [x] `global-sandbox-cleanup` — Sandboxes take a lot of space; need a global way to cleanup last-used sandboxes.
- [x] `backend-host` — Add `--backend=host` so that sbox runs locally (no entrypoint but supports sbox loop and bypass permission).
- [x] `global-stop` — Similar to `sbox prune`, have a way to stop container/sandboxes via `sbox stop all`.
- [x] `add-size-to-info` — Add the size on disk for the sbox project (sandbox size, container, volumes). Complete.

## Bugs Tracking

- [x] `sandbox instructions for firewall` — Fixed in commit 50c155e. The `{{SBOX_SANDBOX_NAME}}` placeholder is substituted with the actual sandbox name in `embedded/sandbox_backend.md`. The backtick-wrapped example command is also in place.

## Feature Status Details

### global-sandbox-cleanup

**Status**: Complete.

See `.dev/global-sandbox-cleanup/agent-state.md` for full details.

### backend-host

**Status**: Complete.

Key design decisions:
1. `HostBackend` in `backend_host.go` implements the `Backend` interface.
2. `Run()` calls `PrepareSboxDirectory` (writes `.sbox/` with CLAUDE.md, env, entrypoint.yaml), loads env vars into current process, then runs the agent via `spec.FindBinary()` + `spec.ExecArgs()`.
3. Loop mode reuses `runLoop()` from `entrypoint.go` directly (same logic, in-process).
4. Single-prompt mode reuses `runAgentWithStreamTransformer()` from `entrypoint.go`.
5. Interactive mode uses `hostRunAgent()` which forks the agent as a child process with signal forwarding.
6. `Shell`, `Stop`, `Find`, `FindRunning` return/print clear unsupported messages; `sbox stop` and `sbox shell` commands check for `BackendHost` and return an error with exit code 1.
7. `sbox info` shows "host backend — no container" instead of container status.
8. `sbox loop` skips the post-loop `backend.Stop()` call for host backend.
9. `--profile` flag shows a warning and is ignored for host backend.
10. Embedded `embedded/host_backend.md` is injected into CLAUDE.md via `GetBackendContextMD`.
11. `ValidBackendTypes`, `ValidateBackend`, `GetBackend`, and `Capitalize` all updated for `BackendHost`.

### global-stop

**Status**: Complete.

`sbox stop` is now a command group with:
- `sbox stop [--rm] [--all] [-w workspace]` — unchanged current-project stop behavior
- `sbox stop all [--keep N] [--force]` — stop all running sandboxes + containers globally
- `sbox stop sandbox [--keep N] [--force]` — stop running sandboxes only
- `sbox stop container [--keep N] [--force]` — stop running containers only

New files: `stop_global.go` (sbox package), updated `cmd/sbox/stop.go`, updated `sandbox.go`
(added `StopDockerSandboxByName`), CHANGELOG.md updated.

See `.dev/global-stop/agent-state.md` for full implementation details.

### add-size-to-info

**Status**: Complete.

New `size.go` package in the `sbox` package with `DiskSizeInfo`, `FormatBytes`,
`GetContainerDiskSize`, `GetVolumeDiskSize`, `GetSandboxDiskSize`, `GetContainerVolumeNameByID`.

`cmd/sbox/info.go` updated with `printDiskSize` which is called from `printContainerStatus`.
Shows `Size: X MB (container) + Y GB (volume)` for container backend, or `Size: X GB` for
sandbox backend. Size is omitted silently when unavailable.

See `.dev/add-size-to-info/agent-state.md` for full implementation details.

## Codebase Notes

### Project Structure

- `backend.go` — `Backend` interface, `BackendType`, `BackendOptions`, `ResolveBackendType`, `GetBackend`
- `backend_sandbox.go` — `SandboxBackend` implementing `Backend` via `docker sandbox` commands
- `backend_container.go` — `ContainerBackend` implementing `Backend` via `docker run` commands
- `backend_host.go` — `HostBackend` implementing `Backend` for direct host execution
- `entrypoint.go` — `RunEntrypoint`, `EntrypointConfig`, `PrepareSboxDirectory`; runs inside sandbox/container
- `sandbox.go` — Low-level sandbox functions (`RunSandbox`, `ListDockerSandboxes`, `FindDockerSandbox`, `CreateDockerSandbox`, `StopDockerSandboxByName`, etc.)
- `config.go` — `Config`, `ProjectConfig`, `SboxFileConfig`, `LoadConfig`, `GetProjectConfig`, `ListProjects`
- `agent.go` — `AgentType`, `AgentSpec` interface, `GetAgentSpec`
- `prune.go` — `WriteLastUsed`, `ReadLastUsed`, `PruneCandidate`, `ContainerPruneCandidate`, `FindPruneCandidates`, `FindContainerPruneCandidates`, `PruneOne`, `PruneOneContainer`
- `stop_global.go` — `StopOptions`, `FindSandboxStopCandidates`, `FindContainerStopCandidates`, `StopOneSandboxCandidate`, `StopOneContainerCandidate`
- `cmd/sbox/` — CLI commands: `run.go`, `stop.go`, `clean.go`, `info.go`, `backend.go`, `common.go`, `prune.go`, etc.

### Key Types

- `Backend` interface methods: `Name`, `Run`, `Shell`, `Stop`, `Find`, `FindRunning`, `List`, `Remove`, `Cleanup`, `SaveCache`
- `BackendOptions` — carries all options from `sbox run` to the backend's `Run()` method
- `ContainerInfo` — returned by `Find`, `FindRunning`, `Stop`, `List`
- `ValidBackendTypes` — slice checked by `ValidateBackend` and displayed in `backend list`
- `DefaultBackend` = `BackendSandbox`

### Important Patterns

- New backends: (1) add `BackendType` const, (2) add to `ValidBackendTypes`, (3) implement `Backend` interface in `backend_<name>.go`, (4) add case to `GetBackend`, (5) update `ValidateBackend`
- Cleanup commands live in `cmd/sbox/clean.go` (handles template images and project data)
- `ListProjects()` in `config.go` returns all known projects from `~/.config/sbox/projects/`
- `ListDockerSandboxes()` in `sandbox.go` calls `docker sandbox ls` and parses its table output
- `prune.go` uses `destel/rill` for concurrent project inspection (2×CPU goroutines)
- Prune and stop candidate lists are built via `FindPruneCandidates` / `FindContainerPruneCandidates`
- Stop functions reuse prune candidate discovery but filter by running status and skip deletion

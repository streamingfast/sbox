# sbox Agent State

## Features Tracking

- [x] `global-sandbox-cleanup` — Sandboxes take a lot of space; need a global way to cleanup last-used sandboxes.
- [x] `backend-host` — Add `--backend=host` so that sbox runs locally (no entrypoint but supports sbox loop and bypass permission).

## Feature Status Details

### global-sandbox-cleanup

**Status**: Spec incomplete — needs clarification before implementation can begin.

Key questions pending from user:
1. What does "global" mean? Is this a new `sbox cleanup` command? Or an enhancement to `sbox clean`?
2. What is "last used"? By last-modified time on the workspace? By sandbox creation time from `docker sandbox ls`?
3. What gets cleaned up? Docker sandboxes only (via `docker sandbox rm`)? Also `.sbox/` workspace dirs? Also project config in `~/.config/sbox/projects/`?
4. Should there be a `--keep N` flag to keep the N most recent sandboxes?
5. Should it interactively list and confirm, or just a dry-run / force mode?
6. Should it check if the workspace directory still exists and clean stale entries automatically?

See `.dev/global-sandbox-cleanup/spec.md` for current (minimal) spec.

### backend-host

**Status**: Implemented.

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

Files changed:
- `backend.go` — added `BackendHost`, updated `ValidBackendTypes`, `ValidateBackend`, `GetBackend`, `Capitalize`
- `backend_host.go` — new file, full `Backend` implementation
- `embed.go` — added `HostBackendContextMD` embed
- `embedded/host_backend.md` — new embedded context for host environment
- `cmd/sbox/run.go` — `--profile` warning, description updated
- `cmd/sbox/loop.go` — `--profile` warning, skip stop for host backend
- `cmd/sbox/stop.go` — unsupported error for host backend
- `cmd/sbox/shell.go` — unsupported error for host backend
- `cmd/sbox/info.go` — host-backend-aware status display
- `CHANGELOG.md` — entry added

## Codebase Notes

### Project Structure

- `backend.go` — `Backend` interface, `BackendType`, `BackendOptions`, `ResolveBackendType`, `GetBackend`
- `backend_sandbox.go` — `SandboxBackend` implementing `Backend` via `docker sandbox` commands
- `backend_container.go` — `ContainerBackend` implementing `Backend` via `docker run` commands
- `entrypoint.go` — `RunEntrypoint`, `EntrypointConfig`, `PrepareSboxDirectory`; runs inside sandbox/container
- `sandbox.go` — Low-level sandbox functions (`RunSandbox`, `ListDockerSandboxes`, `FindDockerSandbox`, `CreateDockerSandbox`, etc.)
- `config.go` — `Config`, `ProjectConfig`, `SboxFileConfig`, `LoadConfig`, `GetProjectConfig`, `ListProjects`
- `agent.go` — `AgentType`, `AgentSpec` interface, `GetAgentSpec`
- `cmd/sbox/` — CLI commands: `run.go`, `stop.go`, `clean.go`, `info.go`, `backend.go`, `common.go`, etc.

### Key Types

- `Backend` interface methods: `Name`, `Run`, `Shell`, `Stop`, `Find`, `FindRunning`, `List`, `Remove`, `Cleanup`, `SaveCache`
- `BackendOptions` — carries all options from `sbox run` to the backend's `Run()` method
- `ContainerInfo` — returned by `Find`, `FindRunning`, `Stop`, `List`
- `ValidBackendTypes` — slice checked by `ValidateBackend` and displayed in `backend list`
- `DefaultBackend` = `BackendSandbox`

### Recent Uncommitted Changes (already in tree, not yet committed)

- `backend.go`: Added `AgentArgs []string` to `BackendOptions`
- `cmd/sbox/run.go`: Added `ArbitraryArgs()` and pass `args` into `BackendOptions.AgentArgs`
- `entrypoint.go`: Added `AgentArgs` to `EntrypointConfig`, appended in `RunEntrypoint`, written in `PrepareSboxDirectory`

### Important Patterns

- New backends: (1) add `BackendType` const, (2) add to `ValidBackendTypes`, (3) implement `Backend` interface in `backend_<name>.go`, (4) add case to `GetBackend`, (5) update `ValidateBackend`
- Cleanup commands live in `cmd/sbox/clean.go` (handles template images and project data)
- `ListProjects()` in `config.go` returns all known projects from `~/.config/sbox/projects/`
- `ListDockerSandboxes()` in `sandbox.go` calls `docker sandbox ls` and parses its table output

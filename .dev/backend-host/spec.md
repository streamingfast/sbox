# Feature: `--backend=host`

## Overview

Add a `host` backend type to `sbox` so that the AI agent runs directly on the host machine with no Docker or MicroVM isolation. This is useful for development workflows where the overhead of a container is undesirable or where the host environment is already a safe, trusted context.

Activation: `sbox run --backend=host` (or `backend: host` in `sbox.yaml` / project config).

---

## Design Decisions

### Agent execution

The agent binary (e.g. `claude`, `opencode`) is launched as a child process of `sbox` directly on the host, using the same PATH as the invoking shell. `--dangerously-skip-permissions` is passed just as it would be inside a sandbox, so permission-bypass behavior is preserved.

### .sbox/ directory

The `.sbox/` directory is still written on every run. It carries:
- `CLAUDE.md` / OpenCode global instructions injected by sbox
- `.sbox/env` — environment variables assembled from global config, project config, and sbox.yaml

The env file is loaded into the current process (via `os.Setenv`) so the agent child process inherits them, mirroring what the entrypoint does inside the sandbox.

### Modes

All three execution modes work with the host backend:

| Mode | Description |
|------|-------------|
| Interactive | Agent runs as a child process; signals (SIGINT, SIGTERM, etc.) are forwarded. |
| Single-prompt | Agent is run with `-p --output-format=stream-json`; output is processed by the stream transformer. |
| Loop | `sbox loop` behavior is supported; the loop runs in-process on the host instead of inside a container. |

### Plugin forwarding — not applicable

The host backend does **not** forward plugin directories to the agent via `--plugin-dir`. Plugins are already installed natively on the host in the agent's own config directory (e.g. `~/.claude/plugins`). The agent discovers them automatically; adding `--plugin-dir` would be redundant and could cause double-loading.

The `hostCollectPluginDirs` helper and all related code have been removed from `backend_host.go`.

### `sbox stop` support

When the agent is launched in interactive mode, its PID is written to `.sbox/host.pid`. `sbox stop` reads this file and:
1. Sends `SIGTERM` to the process.
2. Polls every 100 ms for up to 5 seconds.
3. Escalates to `SIGKILL` if the process has not exited.
4. Removes `.sbox/host.pid` when done.

If the PID file does not exist or the process is already gone, `sbox stop` exits cleanly with no error.

### Unsupported commands

The following commands are not meaningful for the host backend and exit with a clear error message (exit code 1):

- `sbox shell` — no container to open a shell in.
- `sbox info` — no container metadata to show.
- `sbox remove` — nothing to remove at the Docker/sandbox layer.

### `--profile` flag

Using `--profile` with the host backend prints a warning that profiles are ignored and proceeds. The flag is silently accepted so existing scripts do not break.

---

## Implementation Notes

- Backend type constant: `BackendHost` (`"host"`)
- Implementation file: `backend_host.go`
- PID file path: `<workspace>/.sbox/host.pid`
- `sbox stop` integration: handled by `HostBackend.Stop()` which is called by `cmd/sbox/stop.go` through the common `ctx.Backend.Stop()` dispatch path — no special-casing needed in the stop command.

---

## Feedback

### Feedback 1 (resolved)

**Plugin forwarding removed**: The original implementation called `hostCollectPluginDirs` to pass `--plugin-dir` flags to the agent on the host backend. This was incorrect — the host agent already has its plugins in its native config directory. The call and the helper function have been removed. `nil` is passed as the plugin-dirs argument in all three execution paths (interactive, prompt, loop).

**`sbox stop` verified**: PID writing (`writeHostPID`), signal dispatch, and SIGKILL escalation were already in place from the initial implementation. No additional work required.

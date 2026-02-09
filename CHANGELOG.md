# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## v1.1.0

### Added

- Backend abstraction to support multiple container execution backends
  - **Sandbox backend** (default): Uses Docker sandbox MicroVM for enhanced isolation
  - **Container backend**: Uses standard Docker containers with named volume persistence
- `--backend` flag for `sbox run` to select the backend type (`sandbox` or `container`)
- `backend` field in `sbox.yaml` to configure backend per-project
- `backend` field in project config (persisted when using `--backend` flag)
- `default_backend` field in global config (`~/.config/sbox/config.yaml`)
- Named volume persistence for container backend (`sbox-claude-<hash>`) to persist `.claude` folder across sessions
- `sbox info` now displays the configured backend type for each project
- `sbox stop --rm --all` now removes persistence volumes for container backend projects
- Claude state caching for sandbox backend
  - Entire `.claude` folder is synced to `.sbox/claude-cache/` on `sbox stop`
  - Cache is restored on sandbox creation, preserving auth across recreations
  - Automatically saves cache before `sbox run --recreate`
  - Uses `rsync` for efficient synchronization
  - Preserves: credentials, settings, projects, plugins, agents, shell-snapshots, todos, etc.
- Platform-aware Docker socket mounting for container backend
  - macOS: Checks `~/.docker/run/docker.sock` first, then `/var/run/docker.sock`
  - Linux: Uses `/var/run/docker.sock`
  - `SBOX_DOCKER_SOCKET` environment variable for explicit path override
- `sbox info` now shows both create and run commands for sandbox backend

### Changed

- CLI commands (`shell`, `stop`, `info`) now automatically detect the backend from project configuration
- Backend resolution priority: CLI flag > sbox.yaml > project config > global config > default (sandbox)
- Refactored CLI commands to use shared `WorkspaceContext` for config loading
- Added `Capitalize()` method to `BackendType` for consistent display formatting
- Added `SaveCache()` and `Cleanup()` methods to `Backend` interface for proper encapsulation

## v1.0.0

### Added

- Multi-architecture support for dev builds (auto-detects amd64/arm64 from Docker)
- `--debug` flag for `sbox run` to enable debug output from docker sandbox commands
- `Makefile` with `build-image` target for building the sbox binary image locally

### Changed

- Renamed config file from `.sbox` to `sbox.yaml` to avoid conflict with `.sbox/` directory
- Template images now use `CMD` instead of `ENTRYPOINT` for docker sandbox compatibility
  - Docker sandbox requires its own entrypoint to manage container lifecycle
  - Setting `ENTRYPOINT` caused containers to be killed (SIGKILL/exit 137)
  - The `sbox entrypoint` command now runs via `CMD` after sandbox initialization
- Migrated from container-based Docker sandbox to MicroVM-based Docker sandbox
  - `docker ps` replaced with `docker sandbox ls` for sandbox discovery
  - `docker exec` replaced with `docker sandbox exec` for shell connections
  - `docker stop/rm` replaced with `docker sandbox stop/rm` for sandbox lifecycle
  - `--mount-docker-socket` flag is now ignored (Docker is automatically available in MicroVM sandboxes)
  - Mount introspection (`CheckBrokenMounts`, `CheckMountMismatch`) disabled for MicroVM sandboxes

### Added

- `substreams` profile for blockchain development with Substreams and Firehose Core CLIs
  - Automatically includes `rust` profile as a dependency
  - Installs `substreams` CLI from `ghcr.io/streamingfast/substreams:latest`
  - Installs `firecore` CLI from `ghcr.io/streamingfast/firehose-core:latest`
  - Installs `buf` CLI (latest) and `protoc` (latest) for protobuf development
- `sbox env` command group for managing environment variables passed to the sandbox
  - `sbox env add FOO=bar BAZ` — add explicit values or host passthrough variables
  - `sbox env add --global TOKEN` — add to global config (shared across all projects)
  - `sbox env remove FOO BAZ` — remove by name (use `--global` for global config)
  - `sbox env list` — show all vars with source labels (`[global]`, `[project]`, `[sbox.yaml]`)
  - Project-specific envs override global ones; `sbox.yaml` file overrides global
  - `sbox info` now shows environment variables with their source
- Profile dependency support - profiles can now declare dependencies on other profiles
- Agent sharing via `--agents` CLI flag (converts `~/.claude/agents/*.md` to JSON)
- Plugin sharing via `--plugin-dir` flag (each installed plugin is mounted separately)
- `--recreate` flag for `sbox run` to force rebuild the template image and recreate the sandbox
- Mount mismatch detection with `--recreate` suggestion
- Docker command display in `sbox info`
- `--all` flag to `sbox info` to list all known projects
- `--workspace/-w` flag to `sbox info` to specify workspace directory
- `sbox auth` command for configuring API key shared across all sandboxes
  - Prompts for Anthropic API key and stores it as a global environment variable
  - API key is automatically passed to all sandbox sessions via `-e ANTHROPIC_API_KEY=...`
  - `--status` flag to check if API key is configured
  - `--logout` flag to remove stored API key

### Changed

- `sbox stop --rm` no longer removes project configuration (profiles, envs, etc.)
  - Previously `--rm` deleted both the sandbox and all project config data
  - Now `--rm` only removes the Docker sandbox container
  - Use `sbox stop --rm --all` to also remove project configuration (with confirmation prompt)
- `sbox info` now shows current project info by default (use `--all` for all projects)

### Fixed

- Consolidated `--rebuild` and `--recreate` flags into single `--recreate` flag
  - `--recreate` now both rebuilds the template image and recreates the sandbox
- Rust profile now installs to system-wide location (`/usr/local/cargo`, `/usr/local/rustup`)
  - Previously installed to `/root/.cargo` which wasn't accessible to the sandbox `agent` user
  - Fixed `chmod` to include write permissions (`a+rwx`) so `rustup` operations work (uses `/usr/local/rustup/tmp/`)
  - `cargo`, `rustc`, and `rustup` commands now work correctly inside the sandbox
- Integration tests now run as `agent` user to match the sandbox environment and catch permission issues
- Authentication now uses API key via environment variable instead of credentials file mounting
  - Simpler and more reliable than the previous OAuth/credentials.json approach
  - API key stored in global config and passed as `-e ANTHROPIC_API_KEY=...` to all sandboxes

### Removed

- `sbox status` command (consolidated into `sbox info`)

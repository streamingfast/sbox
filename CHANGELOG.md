# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## v1.5.1

### Added

- Background agent auto-updater: sbox entrypoint now wraps the agent as a child process instead of exec'ing, runs a background update check every 15 minutes, and updates the agent binary if the last update was more than 24 hours ago. After each update, the shim wrapper is automatically repaired so the sbox entrypoint remains the entry point.
- Disable agent's built-in auto-updater (`DISABLE_AUTOUPDATER=1`) to prevent it from overwriting the sbox shim wrapper. Updates are now managed by sbox itself.

## v1.5.0

### Added

- `sbox run --startup-delay` flag to delay agent startup inside the sandbox. If set to `0`, waits forever without starting the agent (useful for attaching a shell and debugging). Otherwise accepts a Go duration (e.g. `30s`, `5m`).
- `sbox loop --confirmations` flag to configure the number of consecutive goal completions required (default: 2). Also configurable via `loop_confirmations` in global config (`~/.config/sbox/config.yaml`) or `sbox.yaml`.
- `sbox loop <prompt>` command: runs the agent in a loop until the goal described by the prompt is completed
  - Same `--backend` and `--agent` support as `sbox run`
  - Augments the prompt with loop instructions so the agent knows to assess and work toward the goal
  - Agent writes `.sbox/loop.completion` when the goal is reached
  - Loop stops after the agent confirms completion the required number of consecutive times

### Fixed

- `sbox loop` now stops immediately when the agent exits with an error or is killed, instead of continuing to loop
- `sbox loop` now stops the sandbox when the loop exits (completion, error, or Ctrl+C), preventing the container from running in the background
- Fix container backend not producing output in loop/prompt mode due to TTY allocation (`-it`) mangling stream-json output
- Transfer `tui.json` from `~/.config/opencode/tui.json` to `.sbox/` (host→sandbox) and from `.sbox/tui.json` to agent home (sandbox entrypoint) for OpenCode
- OpenCode state persistence across `sbox stop` and `sbox run --recreate`
  - `~/.config/opencode` synced to `.sbox/opencode-cache/` on stop/recreate; restored on next run
  - `~/.local/share/opencode` synced to `.sbox/opencode-share-cache/` on stop/recreate; restored after initial file seeding so cached auth/session data takes precedence

## v1.4.0

### Added

- OpenCode support as an alternative AI agent to Claude
  - `--agent` flag for `sbox run` to select the AI agent type (`claude` or `opencode`)
  - `agent` field in `sbox.yaml` to configure agent per-project
  - `agent` field in project config (persisted when using `--agent` flag)
  - `default_agent` field in global config (`~/.config/sbox/config.yaml`)
  - `sbox agent` command group for managing the default AI agent
    - `sbox agent list` — show available agents
    - `sbox agent set <agent>` — set default agent globally
    - `sbox agent show` — show current default agent
  - Agent resolution priority: CLI flag > sbox.yaml > project config > global config > default (claude)
  - Entrypoint automatically detects and launches the configured agent (claude or opencode)
  - Sandbox/container names now include agent type: `sbox-<agent>-<workspace>` (e.g., `sbox-opencode-myproject`)
    - This allows running different agents for the same workspace
    - Sandbox name is automatically regenerated when switching agents
    - Previous sandboxes named `sbox-claude-<workspace>` will be automatically detected for Claude agent
  - Agent-specific Docker sandbox templates:
    - Claude agent uses `docker/sandbox-templates:claude-code`
    - OpenCode agent uses `docker/sandbox-templates:opencode`
    - Template is automatically selected based on the configured agent
  - Agent abstraction via `AgentSpec` interface for extensibility
    - Encapsulates agent-specific behavior (binary name, wrapper name, template image, config directory, binary discovery, exec args)
    - Template builder generates agent-specific wrapper scripts (e.g., `claude-wrapper` or `opencode-wrapper`)
    - Wrapper script automatically renames agent binary to `<name>-real` and symlinks to wrapper for proper initialization
    - Template hash includes agent type to ensure separate template images for different agents
    - Entrypoint uses agent-specific config directories (`.claude` for Claude, `.config/opencode` for OpenCode)
    - Agent setup (CLAUDE.md/AGENTS.md, agents, plugins) works with appropriate config directory per agent
    - Container backend uses agent-specific volume names (`sbox-claude-*` vs `sbox-opencode-*`)
    - Container backend mounts agent config directory based on agent type (`.claude` or `.opencode`)
    - Settings.json mount location is agent-aware
    - Template build pre-creates agent config directory with proper ownership to avoid permission issues with volume mounts
    - OpenCode agent uses appropriate command-line arguments (no `--dangerously-skip-permissions` flag)
    - Entrypoint passes workspace path to OpenCode when no arguments provided
    - Config now supports agent-specific home directories (`opencode_home` defaults to `~/.config/opencode`, `claude_home` defaults to `~/.claude`)
    - Plugins and agents are loaded from agent-specific home directories (`~/.config/opencode` for OpenCode, `~/.claude` for Claude)
    - Settings.json files mounted from appropriate agent home directory
    - Container config directory matches host XDG conventions (`.config/opencode` for OpenCode)
    - OpenCode config file (`~/.config/opencode/opencode.json`) is automatically prepared via `.sbox/` directory:
      - If no config exists, a default one is created with full permissions
      - Existing config is loaded and permissions are automatically set to `{"*": "allow"}` for sandbox/container environment
      - All other user config fields (like `model`, preferences, etc.) are preserved
      - Ensures OpenCode runs with full permissions in the isolated environment
    - OpenCode authentication file (`~/.local/share/opencode/auth.json`) is automatically copied if present:
      - Shares local authentication credentials with the sandbox/container
      - Enables seamless OpenCode usage without re-authentication
      - Copied to `/home/agent/.local/share/opencode/auth.json` in the container
    - CLAUDE.md/AGENTS.md concatenation works for both agents:
      - Same walking and concatenation logic for all agents
      - Claude: Final file placed at `~/.claude/CLAUDE.md`
      - OpenCode: Final file placed at `~/.config/opencode/AGENTS.md`
      - Discovers and merges all CLAUDE.md and AGENTS.md files from parent directories
      - Includes backend-specific context (sandbox vs container)
      - Symlink deduplication: If CLAUDE.md is a symlink to AGENTS.md (or vice versa), only includes the file once
      - Renamed functions for clarity: `setupCLAUDEMD` → `setupRules`, `prepareCLAUDEMD` → `prepareRules`

### Fixed

- OpenCode config file preparation now correctly preserves all user fields (like `model`, preferences, etc.) while only adding/overriding the `permission` field for sandbox safety

## v1.3.3

### Changed

- `sbox run --recreate` now automatically pulls the latest base image to ensure you get the newest Claude Code version

### Fixed

- `sbox run --recreate` now properly removes existing containers in container backend (previously only worked for sandbox backend)

## v1.3.2

### Added

- `cpp` profile for C/C++ development (Boost, libc++, libstdc++, autoconf, automake, libtool, ninja-build, libzstd-dev, zlib1g-dev)

### Changed

- **BREAKING**: Replace `SBOX_DEV=1` with `SBOX_ENTRYPOINT_IMAGE` environment variable for more flexible entrypoint image configuration
  - `SBOX_ENTRYPOINT_IMAGE=local` - builds sbox binary locally (equivalent to old `SBOX_DEV=1`)
  - `SBOX_ENTRYPOINT_IMAGE=dev` - uses `ghcr.io/streamingfast/sbox:dev` image (tag only)
  - `SBOX_ENTRYPOINT_IMAGE=ghcr.io/custom/sbox:tag` - uses custom registry/image (full image)
  - If not set, uses version-based image (e.g., `ghcr.io/streamingfast/sbox:v1.3.2`)
- Sensitive environment variables (KEY, TOKEN, SECRET, PASSWORD, etc.) are now masked in `sbox info` and `sbox env list` output

### Fixed

- Fix Docker image tag format to include `v` prefix for semantic versions (e.g., `v1.3.1` instead of `1.3.1`) to match published image tags

## v1.3.1

### Added

- `sql` profile for SQL development (PostgreSQL client)

### Fixed

- Fix custom template images not found by removing `--pull-template never` flag (default `missing` policy correctly finds images in local Docker daemon)

## v1.3.0

### Added

- `firehose` profile for blockchain data streaming (substreams, firecore, fireeth, dummy-blockchain, grpcurl)
- Embedded documentation now instructs agents to:
  - Treat `.sbox/` directory as read-only (managed by sbox)
  - Use `/tmp/` for temporary clones and downloads to avoid cluttering the workspace

### Changed

- `sbox profile add` and `sbox profile remove` now accept multiple profiles (e.g., `sbox profile add go rust`)

### Fixed

- `sbox stop --rm` now removes stopped sandboxes/containers (previously only worked on running ones)
- Replace deprecated `--load-local-template` with `--pull-template never` for Docker Desktop 4.61+

## v1.2.0

### Added

- Named volume persistence for container backend (`sbox-claude-<hash>`) to persist `.claude` folder across sessions
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
- `javascript` profile for JavaScript/TypeScript development (installs pnpm and yarn)
- Backend-specific context instructions embedded in CLAUDE.md/AGENTS.md hierarchy
  - **Sandbox backend**: Documents MicroVM environment, native Docker access, persistence behavior
  - **Container backend**: Documents Docker socket requirements, sudo usage, volume mount caveats
  - Both include instructions for Docker, Docker Compose, and Testcontainers usage

### Changed

- Refactored CLI commands to use shared `WorkspaceContext` for config loading
- Added `Capitalize()` method to `BackendType` for consistent display formatting
- Added `SaveCache()` and `Cleanup()` methods to `Backend` interface for proper encapsulation

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
- `javascript` profile for JavaScript/TypeScript development (installs pnpm and yarn)
- Backend-specific context instructions embedded in CLAUDE.md/AGENTS.md hierarchy
  - **Sandbox backend**: Documents MicroVM environment, native Docker access, persistence behavior
  - **Container backend**: Documents Docker socket requirements, sudo usage, volume mount caveats
  - Both include instructions for Docker, Docker Compose, and Testcontainers usage

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

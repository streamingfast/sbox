# sbox

A Docker sandbox wrapper for AI Code agents (Claude Code, OpenCode) that provides seamless sharing of agents, plugins, credentials, and project configuration. Supports both Docker sandbox (MicroVM) and standard container backends.

## Why sbox?

Running Claude Code in Docker sandbox or container mode provides security isolation, but loses access to your `~/.claude` configuration. **sbox** bridges this gap by:

- **Sharing agents** — Your `~/.claude/agents/*.md` files are converted to JSON and passed via `--agents`
- **Sharing plugins** — Installed plugins from `~/.claude/plugins/` are mounted and loaded via `--plugin-dir`
- **Concatenating CLAUDE.md** — Merges `CLAUDE.md` and `AGENTS.md` files from parent directories into a single file
- **Profile system** — Install additional tools (Go, Rust, Substreams, etc.) via custom Docker images with dependency support
- **Environment variables** — Pass host environment variables to the sandbox with global and per-project configuration
- **Project management** — Track sandbox state, profiles, volumes, and configuration per project
- **Multiple backends** — Choose between Docker sandbox (MicroVM) or standard containers
- **Multiple agents** — Choose between Claude Code or OpenCode as your AI agent

## Installation

```bash
go install github.com/streamingfast/sbox/cmd/sbox@latest
```

## Quick Start

```bash
# Navigate to your project
cd ~/projects/my-app

# Launch Claude in sandbox
sbox run

# In another terminal, check project info
sbox info

# Connect a shell to the running sandbox
sbox shell

# When done, stop and clean up the container
sbox stop --rm
```

## Commands

### `sbox run`

Launch Claude in Docker sandbox or container with all configured mounts.

```bash
sbox run                      # Run in current directory
sbox run -w /path/to/project  # Specify workspace
sbox run --docker-socket      # Enable Docker-in-Docker
sbox run --profile go         # Use Go profile for this session
sbox run --recreate           # Rebuild image and recreate sandbox
sbox run --backend container  # Use container backend instead of sandbox
sbox run --agent opencode     # Use OpenCode instead of Claude
sbox run --debug              # Enable debug output for docker commands
```

### `sbox info`

Show project info for the current directory, or list all known projects.

```bash
sbox info                     # Current project info
sbox info --all               # List all known projects
sbox info -w /path/to/project # Info for a specific workspace
```

### `sbox shell`

Open a bash shell in the running sandbox.

```bash
sbox shell
```

### `sbox stop`

Stop the running sandbox.

```bash
sbox stop            # Stop container
sbox stop --rm       # Stop and remove container (keeps project config)
sbox stop --rm --all # Stop, remove container, and delete project config
```

### `sbox profile`

Manage development profiles (pre-configured tool installations).

```bash
sbox profile list              # Show available profiles
sbox profile add go            # Add Go profile to project
sbox profile remove go         # Remove profile from project
```

Available profiles:
- **go** — Go toolchain
- **rust** — Rust toolchain (cargo, rustc, rustup)
- **substreams** — Substreams and Firehose Core CLIs, buf, protoc (automatically includes rust)
- **firehose** — Firehose CLI tools (substreams, firecore, fireeth, dummy-blockchain) and grpcurl
- **javascript** — JavaScript/TypeScript development tools (pnpm, yarn)
- **docker** — Docker CLI tools for container management
- **bash-utils** — Common shell utilities (jq, yq, curl, wget, git, vim, nano, htop, tree)

Profiles can declare dependencies on other profiles. For example, `substreams` automatically pulls in `rust`.

### `sbox backend`

Manage which container backend (Sandbox or Container) to use.

```bash
sbox backend list              # Show available backends
sbox backend set container     # Set Container as default globally
sbox backend show              # Show current default backend
```

The backend can be configured at multiple levels (later overrides earlier):
1. Global config (`default_backend` in `~/.config/sbox/config.yaml`)
2. `sbox.yaml` file (`backend` field)
3. Project config (persisted from `--backend` flag)
4. CLI flag (`--backend`)

### `sbox agent`

Manage which AI agent (Claude Code or OpenCode) to use.

```bash
sbox agent list              # Show available agents
sbox agent set opencode      # Set OpenCode as default globally
sbox agent show              # Show current default agent
```

The agent can be configured at multiple levels (later overrides earlier):
1. Global config (`default_agent` in `~/.config/sbox/config.yaml`)
2. `sbox.yaml` file (`agent` field)
3. Project config (persisted from `--agent` flag)
4. CLI flag (`--agent`)

### `sbox env`

Manage environment variables passed to the sandbox. Name-only variables (e.g. `FOO`) are resolved from the host environment at launch time.

```bash
sbox env list                          # Show all vars with source labels
sbox env add FOO=bar BAZ              # Add to current project
sbox env add --global TOKEN SECRET    # Add to global config (all projects)
sbox env remove FOO BAZ              # Remove from current project
sbox env remove --global TOKEN       # Remove from global config
```

Environment variables are merged from three sources (later overrides earlier):
1. Global config (`~/.config/sbox/config.yaml`)
2. `sbox.yaml` file (checked into repo)
3. Project config (`~/.config/sbox/projects/<hash>/config.yaml`)

### `sbox auth`

Configure the Anthropic API key for all sandbox sessions.

```bash
sbox auth            # Prompt for API key and store it
sbox auth --status   # Check if API key is configured
sbox auth --logout   # Remove stored API key
```

The API key is stored in the global config and passed as `ANTHROPIC_API_KEY` to all sandboxes. This avoids the need to modify shell configuration files or restart Docker Desktop.

### `sbox config`

View or edit global configuration.

```bash
sbox config                           # Show all config
sbox config claude_home               # Show specific setting
sbox config docker_socket auto        # Set docker socket behavior (auto/always/never)
sbox config default_backend container # Set default backend (sandbox/container)
sbox config default_agent opencode    # Set default agent (claude/opencode)
```

### `sbox clean`

Clean up cached data.

```bash
sbox clean           # Clean current project cache
sbox clean --images  # Remove cached profile images
sbox clean --all     # Remove all cached data
```

## Configuration

### Global Config (`~/.config/sbox/config.yaml`)

```yaml
claude_home: ~/.claude
docker_socket: auto    # auto | always | never
default_backend: sandbox  # sandbox | container
default_agent: claude  # claude | opencode
envs:
  - TOKEN
  - SECRET=default_value
```

### Project Config (`sbox.yaml`)

A `sbox.yaml` file can be checked into your repository to share configuration with your team:

```yaml
profiles:
  - go
  - rust
volumes:
  - ~/data:/mnt/data:ro
docker_socket: always
backend: sandbox  # sandbox | container
agent: claude  # claude | opencode
envs:
  - API_KEY
```

Per-project config is also stored at `~/.config/sbox/projects/<hash>/config.yaml` for settings managed via CLI commands.

## Backends

sbox supports two execution backends:

### Sandbox Backend (default)

Uses Docker's sandbox feature (MicroVM-based isolation) for enhanced security. The sandbox provides:
- Full isolation via lightweight MicroVM
- Native Docker support (Docker runs inside the MicroVM)
- Automatic state persistence via `.sbox/claude-cache/` sync on stop

```bash
sbox run                  # Uses sandbox by default
sbox run --backend sandbox
```

### Container Backend

Uses standard Docker containers with named volume persistence. Useful when:
- Docker sandbox is not available
- You need direct host Docker socket access
- You prefer traditional container behavior

```bash
sbox run --backend container
```

The container backend uses a named volume (`sbox-claude-<hash>`) to persist the `.claude` folder across sessions.

### Backend Resolution

The backend is resolved from multiple sources (later overrides earlier):
1. Global config (`default_backend` in `~/.config/sbox/config.yaml`)
2. `sbox.yaml` file (`backend` field)
3. Project config (`~/.config/sbox/projects/<hash>/config.yaml`)
4. CLI flag (`--backend`)

## Agents

sbox supports multiple AI agents:

### Claude Code (default)

Uses [Claude Code](https://claude.ai/code) as the AI agent. This is the default and most widely tested option.

```bash
sbox run                  # Uses Claude by default
sbox run --agent claude
```

### OpenCode

Uses OpenCode as the AI agent. OpenCode must be installed in the sandbox environment for this to work.

```bash
sbox run --agent opencode
```

### Agent Resolution

The agent is resolved from multiple sources (later overrides earlier):
1. Global config (`default_agent` in `~/.config/sbox/config.yaml`)
2. `sbox.yaml` file (`agent` field)
3. Project config (`~/.config/sbox/projects/<hash>/config.yaml`)
4. CLI flag (`--agent`)

## How It Works

### Agent Sharing

sbox reads your `~/.claude/agents/*.md` files, parses their YAML frontmatter, and converts them to JSON format for the `--agents` CLI flag:

```
~/.claude/agents/my-agent.md  →  --agents '{"my-agent": {"description": "...", "prompt": "..."}}'
```

### Plugin Sharing

The plugins cache directory is mounted, and each installed plugin gets a `--plugin-dir` flag:

```
~/.claude/plugins/cache/  →  /mnt/claude-plugins/
                          →  --plugin-dir /mnt/claude-plugins/official/my-plugin/abc123
```

### CLAUDE.md Concatenation

sbox walks up from your workspace directory, collecting all `CLAUDE.md` and `AGENTS.md` files, and concatenates them into a single file mounted at `~/.claude/CLAUDE.md` inside the container.

### Profile System

Profiles extend the base Claude sandbox image with additional tools:

```bash
sbox profile add go    # Adds Go profile to this project
sbox run               # Builds custom image with Go installed, then launches sandbox
sbox run --recreate    # Rebuilds image and recreates sandbox (after profile changes)
```

Profiles support dependencies — adding `substreams` automatically includes `rust`. Custom profiles can be defined in `~/.config/sbox/profiles/`.

### Claude State Persistence

sbox preserves your Claude state across sessions:

- **Sandbox backend**: The entire `.claude` folder is synced to `.sbox/claude-cache/` when running `sbox stop`. This cache is restored on the next `sbox run`, preserving credentials, settings, projects, plugins, shell snapshots, and more.
- **Container backend**: Uses a named Docker volume (`sbox-claude-<hash>`) that persists automatically.

### Environment Variables

Environment variables configured via `sbox env` are resolved at sandbox launch time. Name-only entries (e.g. `FOO`) are resolved from the current host environment. If a host variable is not set, it is skipped.

### Advanced: Custom Entrypoint Image

By default, sbox uses the published `ghcr.io/streamingfast/sbox` image matching the installed version. You can override this with the `SBOX_ENTRYPOINT_IMAGE` environment variable for development or testing:

```bash
# Build sbox binary locally from source (for development)
SBOX_ENTRYPOINT_IMAGE=local sbox run

# Use a specific tag from the official registry
SBOX_ENTRYPOINT_IMAGE=dev sbox run
SBOX_ENTRYPOINT_IMAGE=v1.3.0 sbox run

# Use a custom registry or image
SBOX_ENTRYPOINT_IMAGE=myregistry.io/custom/sbox:test sbox run

# Default behavior (no env var set) - uses version-based tag
sbox run  # Uses ghcr.io/streamingfast/sbox:v1.3.2
```

**How it works:**
- `SBOX_ENTRYPOINT_IMAGE=local` — Cross-compiles the sbox binary from your local source tree and includes it in the custom template image. Requires running from the sbox repository directory.
- `SBOX_ENTRYPOINT_IMAGE=<tag>` — Uses `ghcr.io/streamingfast/sbox:<tag>` (e.g., `dev`, `v1.2.0`, `latest`)
- `SBOX_ENTRYPOINT_IMAGE=<full-image>` — Uses the exact image specified (e.g., `myregistry.io/org/sbox:custom`)
- Not set — Uses `ghcr.io/streamingfast/sbox:v<version>` where version matches the installed sbox CLI

This is useful for:
- Testing unreleased sbox features during development
- Using development builds from a custom registry
- Pinning to a specific sbox version different from your CLI

## License

MIT

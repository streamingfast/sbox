# sbox

A Docker sandbox wrapper for [Claude Code](https://claude.ai/code) that provides seamless sharing of agents, plugins, credentials, and project configuration.

## Why sbox?

Running Claude Code in Docker sandbox mode provides security isolation, but loses access to your `~/.claude` configuration. **sbox** bridges this gap by:

- **Sharing agents** — Your `~/.claude/agents/*.md` files are converted to JSON and passed via `--agents`
- **Sharing plugins** — Installed plugins from `~/.claude/plugins/` are mounted and loaded via `--plugin-dir`
- **Concatenating CLAUDE.md** — Merges `CLAUDE.md` and `AGENTS.md` files from parent directories into a single file
- **Profile system** — Install additional tools (Go, Rust, Substreams, etc.) via custom Docker images with dependency support
- **Environment variables** — Pass host environment variables to the sandbox with global and per-project configuration
- **Project management** — Track sandbox state, profiles, volumes, and configuration per project

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

Launch Claude in Docker sandbox with all configured mounts.

```bash
sbox run                      # Run in current directory
sbox run -w /path/to/project  # Specify workspace
sbox run --docker-socket      # Enable Docker-in-Docker
sbox run --profile go         # Use Go profile for this session
sbox run --recreate           # Rebuild image and recreate sandbox
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

Profiles can declare dependencies on other profiles. For example, `substreams` automatically pulls in `rust`.

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
sbox config                    # Show all config
sbox config claude_home        # Show specific setting
sbox config docker_socket auto # Set docker socket behavior (auto/always/never)
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
docker_socket: auto  # auto | always | never
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
envs:
  - API_KEY
```

Per-project config is also stored at `~/.config/sbox/projects/<hash>/config.yaml` for settings managed via CLI commands.

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

### Environment Variables

Environment variables configured via `sbox env` are resolved at sandbox launch time. Name-only entries (e.g. `FOO`) are resolved from the current host environment. If a host variable is not set, it is skipped.

## License

MIT

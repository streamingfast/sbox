# sbox

A Docker sandbox wrapper for [Claude Code](https://claude.ai/code) that provides seamless sharing of agents, plugins, credentials, and project configuration.

## Why sbox?

Running Claude Code in Docker sandbox mode provides security isolation, but loses access to your `~/.claude` configuration. **sbox** bridges this gap by:

- **Sharing agents** - Your `~/.claude/agents/*.md` files are converted to JSON and passed via `--agents`
- **Sharing plugins** - Installed plugins from `~/.claude/plugins/` are mounted and loaded via `--plugin-dir`
- **Sharing credentials** - Persistent authentication across sandbox sessions (Missing)
- **Concatenating CLAUDE.md** - Merges `CLAUDE.md` and `AGENTS.md` files from parent directories
- **Profile system** - Install additional tools (Go, Rust, etc.) via custom Docker images

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

# ... work with Claude ...

# In another terminal, check status
sbox status

# Connect a shell to the running sandbox
sbox shell

# When done, stop and clean up
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
sbox run --recreate           # Remove existing sandbox first (for mount changes)
```

### `sbox status`

Show sandbox status for the current project.

```bash
sbox status
# Project:  my-app
# Path:     /Users/me/projects/my-app
# Hash:     a1b2c3d4e5f6
# Status:   running (claude-sandbox-2026-01-19-143701)
# Profiles: [go]
# Command:  docker sandbox run -v ... claude --agents {...}
```

### `sbox info`

List all known projects with their status.

```bash
sbox info
# Projects:
#   my-app (running)
#     Path:     /Users/me/projects/my-app
#     Profiles: [go]
#   other-project (stopped)
#     Path:     /Users/me/projects/other
```

### `sbox shell`

Open a bash shell in the running sandbox.

```bash
sbox shell
# agent@claude-sandbox:~/workspace$
```

### `sbox stop`

Stop the running sandbox.

```bash
sbox stop       # Stop container
sbox stop --rm  # Stop and remove container + project data
```

### `sbox profile`

Manage development profiles (pre-configured tool installations).

```bash
sbox profile list              # Show available profiles
sbox profile add go            # Add Go profile to project
sbox profile remove go         # Remove profile from project
```

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
claude_home: ~/.claude        # Claude configuration directory
docker_socket: auto           # auto | always | never
```

### Project Config (`.sbox.yaml`)

```yaml
profiles:
  - go
  - rust
volumes:
  - ~/data:/mnt/data:ro
docker_socket: always
```

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
sbox run --profile go  # Builds image with Go toolchain installed
```

Custom profiles can be defined in `~/.config/sbox/profiles/`.

## License

MIT

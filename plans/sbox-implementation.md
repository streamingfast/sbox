# Implementation Plan: sbox

## ULTIMATE GOAL
Create a Go CLI tool called `sbox` that wraps Docker's `docker sandbox claude` functionality to provide:
- Sharing of `~/.claude/{agents,plugins}` within the sandbox
- Sharing a single Claude credentials.json for persistent authentication
- Proper sharing of AGENTS.md/CLAUDE.md parent hierarchy (read, concatenated, stored for Claude access)
- Easy Docker "profiles" system for installing tools (go, rust, docker, bash utilities)
- Auto-mount Docker socket access without requiring sudo

## Status: COMPLETE - All Features Implemented

### Recent Updates (Ralph Loop Iteration 1)

1. **Fixed user error visibility** - Errors now print to stderr instead of only being logged. Changed from `OnCommandErrorLogAndExit` to `OnCommandError` with custom handler.

2. **Implemented per-project `.sbox` config file support**
   - Created `SboxFileConfig` type for `.sbox` file format (YAML)
   - Added `FindSboxFile()` to walk up directory tree to find `.sbox` files
   - Added `MergeProjectConfig()` to combine `.sbox` settings with project config
   - Supports: profiles, volumes (with relative path resolution), docker_socket override
   - Paths starting with `./` or `../` are resolved relative to `.sbox` file location
   - Paths starting with `~` are expanded to home directory

3. **Added support for mounting additional volumes**
   - `ProjectConfig` now includes `Volumes` field
   - Updated `buildVolumeMounts()` to include additional volumes from project config
   - Volume format: `"hostpath:containerpath[:ro]"`

4. **Fixed `sbox shell` sandbox detection bug**
   - Improved `FindRunningSandbox()` to:
     - Resolve symlinks for both workspace and mount paths
     - Check all sandbox-related containers (by image/name patterns)
     - Use `|` delimiter in docker inspect format to avoid issues with paths containing `:`
   - Added `IsInsideSandbox()` to detect when running inside a container
   - `ConnectShell()` now gives helpful error when already inside a sandbox

5. **Implemented `sbox info` command**
   - Lists all known projects from `~/.config/sbox/projects/`
   - Shows: workspace path, hash, running status, profiles, volumes, docker socket setting
   - Indicates if workspace path no longer exists
   - Added `ListProjects()` function to config.go
   - Added `ProjectInfo` type to hold project information
   - Updated `ProjectConfig` to store `WorkspacePath` for listing purposes

6. **Implemented `sbox stop` command**
   - Stops the running Docker sandbox container for the current project
   - Supports `--rm` flag to:
     - Remove the Docker container after stopping
     - Remove project data (config, cached files)
   - Supports `-w, --workspace` flag to specify workspace directory
   - Added `SandboxContainer` struct to hold container info (ID, Name, Image)
   - Refactored `FindRunningSandbox()` to return `*SandboxContainer` instead of just ID
   - Added `StopSandbox(workspaceDir, remove bool)` function to sandbox.go
   - Added `RemoveProjectData()` function to config.go
   - `sbox info` now shows container name when sandbox is running

---

## Analysis Summary

### Docker Sandbox Claude Capabilities
Based on research of [Docker Sandboxes documentation](https://docs.docker.com/ai/sandboxes/):

**Available flags for `docker sandbox run`:**
- `-w, --workspace` - Set the working directory path (default: current directory)
- `-v, --volume` - Attach volumes using `hostpath:sandboxpath[:ro]` format
- `-e, --env` - Define environment variables using KEY=VALUE format
- `--mount-docker-socket` - Expose the host's Docker socket inside sandbox
- `-t, --template` - Specify custom container image for sandbox foundation
- `--name` - Assign a custom identifier to the sandbox
- `--credentials` - Control credential access (host, sandbox, or none)
- `-d, --detached` - Launch sandbox without interactive agent execution
- `-q, --quiet` - Minimize output verbosity

**Key behaviors:**
- One sandbox per workspace (reuses existing container)
- Workspace mounted at same absolute path inside container
- Credentials stored in persistent `docker-claude-sandbox-data` volume
- Base image: `docker/sandbox-templates:claude-code` includes Node.js, Go, Python 3, Git, Docker CLI, GitHub CLI, ripgrep, jq

### Claudebox Inspiration Points
From analyzing the claudebox codebase:
- Profile system with `get_profile_<name>()` functions generating Dockerfile snippets
- Per-project isolation via `~/.claudebox/projects/<project-name>/`
- Profile INI configuration files
- Sharing of `.claude/` directory, `.claude.json`, `.config/`, `.cache/`
- Docker socket mounting via environment variable toggle
- SSH key sharing (read-only)
- MCP server configuration handling

### StreamingFast Libraries
**cli library (develop branch):**
- Uses `Run()` with nested `Command()` and `Group()` patterns
- `ConfigureVersion()` for version injection
- `ConfigureViper()` for config management
- `Flags()` with `pflag.FlagSet` for flag definitions
- `OnCommandErrorLogAndExit()` for error handling
- `ExactArgs()`, `ExamplePrefixed()`, `Description()` helpers

**logging library (develop branch):**
- `logging.Register()` to register package loggers
- `logging.RootLogger()` returns `zlog` and `tracer`
- `logging.InstantiateLoggers()` initializes the system
- By default, nothing is logged until explicitly configured
- `DLOG` environment variable enables debug/trace logging

---

## Implementation Tasks

### Priority 1: Core Project Structure

- [x] **Create go.mod with dependencies** - Add `github.com/streamingfast/cli@develop` and `github.com/streamingfast/logging@develop`
  - File: `go.mod`
  - Status: Completed - using `github.com/streamingfast/cli v0.0.4-0.20250815192146-d8a233ec3d0b` and `github.com/streamingfast/logging v0.0.0-20230608130331-f22c91403091`

- [x] **Create main CLI entrypoint** - Implement `cmd/sbox/main.go` using streamingfast/cli Nested pattern with Run(), ConfigureVersion(), ConfigureViper("SBOX"), and OnCommandErrorLogAndExit()
  - File: `cmd/sbox/main.go`
  - Status: Completed - full CLI skeleton with run, profile (list/add/remove), config, and clean commands

- [x] **Create root package with logging setup** - Implement `sbox.go` with logging registration using `logging.Register()` and package-level zlog variable
  - File: `sbox.go`
  - Status: Completed - SetupLogging() function initializes loggers, nothing logged by default

### Priority 2: Configuration System

- [x] **Create config types and loading** - Define Config struct with fields for: ClaudeHome, SboxDataDir, Profiles, DockerSocket, and methods for loading from `~/.sbox/config.yaml` with Viper
  - File: `config.go`
  - Status: Completed - Config and ProjectConfig structs, LoadConfig(), SaveConfig(), GetProjectConfig(), SaveProjectConfig()

- [x] **Create profile configuration types** - Define Profile struct with Name, Description, InstallScript fields; implement loading from `~/.sbox/profiles/` directory
  - File: `profiles.go`
  - Status: Completed - Profile struct, BuiltinProfiles map with go/rust/docker/bash-utils, GetProfile(), ListProfiles()

### Priority 3: Claude MD File Handling

- [x] **Implement CLAUDE.md/AGENTS.md hierarchy discovery** - Create function to walk up directory tree finding all CLAUDE.md and AGENTS.md files from current directory to root
  - File: `mdfiles.go`
  - Status: Completed - DiscoverMDFiles() walks up directory tree

- [x] **Implement MD file concatenation** - Create function to read and concatenate discovered MD files in order (root to current), adding clear delimiters showing source paths
  - File: `mdfiles.go`
  - Status: Completed - ConcatenateMDFiles() with clear delimiters

- [x] **Implement MD file storage for sandbox** - Create function to write concatenated MD content to `~/.sbox/current-claude.md` that will be mounted into sandbox
  - File: `mdfiles.go`
  - Status: Completed - PrepareMDForSandbox() orchestrates full workflow

### Priority 4: Docker Sandbox Wrapper

- [x] **Create sandbox execution wrapper** - Implement function to build and execute `docker sandbox run claude` with all required flags and volume mounts
  - File: `sandbox.go`
  - Status: Completed - RunSandbox() with SandboxOptions struct

- [x] **Implement volume mount generation** - Create function to generate `-v` flags for: `~/.claude/agents`, `~/.claude/plugins`, credentials, concatenated MD files
  - File: `sandbox.go`
  - Status: Completed - buildVolumeMounts() generates all volume mounts

- [ ] **Implement environment variable passthrough** - Create function to collect and pass through relevant environment variables via `-e` flags (ANTHROPIC_API_KEY, etc.)
  - File: `sandbox.go`
  - Note: Deferred - Docker sandbox handles this automatically

- [x] **Implement Docker socket mounting** - Add `--mount-docker-socket` flag support with optional auto-enable via config
  - File: `sandbox.go`
  - Status: Completed - shouldMountDockerSocket() with auto/always/never logic

### Priority 5: Profile System

- [x] **Define core profile definitions** - Profile definitions embedded in profiles.go with DockerfileSnippet for: go, rust, docker, bash-utils
  - File: `profiles.go`
  - Status: Completed

- [x] **Implement profile installation via custom template** - Create Dockerfile template system that extends `docker/sandbox-templates:claude-code` with profile installations
  - File: `template.go`
  - Status: Completed - TemplateBuilder generates Dockerfiles with profile snippets

- [x] **Implement template building** - Create function to build custom Docker image with selected profiles, caching for reuse
  - File: `template.go`
  - Status: Completed - Build() method with ProfilesHash() for caching

### Priority 6: CLI Commands

- [x] **Implement `sbox run` command (default)** - Main command to launch sandbox with all configured mounts and settings; should be the default when running just `sbox`
  - File: `cmd/sbox/main.go`
  - Status: Completed - runE() wired to RunSandbox()

- [x] **Implement `sbox profile add` command** - Add profiles to current project's configuration
  - File: `cmd/sbox/main.go`
  - Status: Completed - profileAddE() validates and saves profile

- [x] **Implement `sbox profile remove` command** - Remove profiles from current project's configuration
  - File: `cmd/sbox/main.go`
  - Status: Completed - profileRemoveE() removes and saves

- [x] **Implement `sbox profile list` command** - List available and installed profiles
  - File: `cmd/sbox/main.go`
  - Status: Completed - Shows all profiles with checkmarks for installed

- [x] **Implement `sbox config` command** - View/edit configuration settings
  - File: `cmd/sbox/main.go`
  - Status: Completed - Shows config, allows setting claude_home and docker_socket

- [x] **Implement `sbox clean` command** - Clean up sandbox containers, images, and cached data
  - File: `cmd/sbox/main.go`
  - Status: Completed - CleanTemplates() removes cached images, --all removes project data

- [x] **Implement `sbox shell` command** - Connect to running sandbox container
  - File: `cmd/sbox/main.go`, `sandbox.go`
  - Status: Completed - FindRunningSandbox() + ConnectShell()

### Priority 7: Credentials Handling

- [x] **Implement credentials.json sharing** - Mount `~/.claude/credentials.json` (or equivalent) into sandbox for persistent authentication
  - File: `sandbox.go`
  - Status: Completed - buildVolumeMounts() mounts credentials.json

- [x] **Implement MCP config handling** - Handle `.claude/settings.json` and `.claude/settings.local.json` MCP server configurations
  - File: `sandbox.go`
  - Status: Completed - buildVolumeMounts() mounts settings.json and settings.local.json

### Priority 8: Polish and Documentation

- [x] **Add verbose/debug flag support** - Implemented via streamingfast/logging with DLOG env var
  - File: `cmd/sbox/main.go`
  - Status: Completed - Uses logging.PackageLogger and DLOG for debug

- [x] **Add version command** - Version info via ConfigureVersion()
  - File: `cmd/sbox/main.go`
  - Status: Completed - `sbox --version` shows version

- [x] **Unit tests** - Comprehensive test coverage
  - File: `sbox_test.go`
  - Status: Completed - 11 tests covering all core functionality

- [x] **Integration tests** - Docker profile testing
  - File: `integration_test.go`
  - Status: Completed - Tests each profile build with `go test -tags=integration`

---

## Completed Items
- [x] Resolved open design questions with reasonable defaults for simplicity
- [x] Created go.mod with streamingfast/cli and streamingfast/logging from develop branches
- [x] Created cmd/sbox/main.go with full CLI skeleton using Nested pattern
- [x] Created sbox.go with logging setup (nothing logged by default)
- [x] Verified build works with `go build ./cmd/sbox`

---

## Technical Design Details

### Directory Structure
```
sbox/
├── cmd/
│   └── sbox/
│       ├── main.go         # CLI entrypoint with Run()
│       ├── run.go          # Default run command
│       ├── profile.go      # Profile management commands
│       ├── config.go       # Config command
│       └── clean.go        # Cleanup command
├── profiles/
│   ├── go.sh              # Go profile installation script
│   ├── rust.sh            # Rust profile installation script
│   ├── docker.sh          # Docker-in-docker profile
│   └── bash-utils.sh      # Bash utilities profile
├── sbox.go                # Root package, logging setup
├── config.go              # Configuration types and loading
├── profiles.go            # Profile definitions and management
├── mdfiles.go             # CLAUDE.md/AGENTS.md handling
├── sandbox.go             # Docker sandbox wrapper
├── template.go            # Custom template building
├── go.mod
├── go.sum
└── README.md
```

### Configuration File (~/.sbox/config.yaml)
```yaml
# Default settings
docker_socket: auto        # auto|always|never
default_profiles:
  - go
  - bash-utils

# Per-project overrides stored in ~/.sbox/projects/<hash>/
```

### Data Directory Structure (~/.sbox/)
```
~/.sbox/
├── config.yaml            # Global configuration
├── profiles/              # Custom profile definitions
├── templates/             # Built Docker templates cache
│   └── <profile-hash>/    # Cached images per profile combination
├── projects/              # Per-project data
│   └── <project-hash>/
│       ├── profiles.ini   # Project-specific profiles
│       └── settings.yaml  # Project-specific settings
└── current-claude.md      # Concatenated MD files for current session
```

### Volume Mounts Strategy
The `sbox run` command will generate these volume mounts:
1. `~/.claude/agents:/home/agent/.claude/agents:ro` - Claude agents
2. `~/.claude/plugins:/home/agent/.claude/plugins:ro` - Claude plugins
3. `~/.sbox/current-claude.md:/home/agent/.claude/CLAUDE.md:ro` - Concatenated MD hierarchy
4. `~/.claude/credentials.json:/home/agent/.claude/credentials.json` - Auth credentials (if exists)
5. `~/.ssh:/home/agent/.ssh:ro` - SSH keys

### Profile System Design
Profiles are shell scripts that run inside the sandbox to install additional tools. The system:
1. Builds a custom Docker image extending `docker/sandbox-templates:claude-code`
2. Copies profile scripts into the image
3. Runs profile scripts during image build
4. Caches built images by profile combination hash
5. Uses `--template` flag to specify custom image

### CLI Structure (using streamingfast/cli)
```go
Run(
    "sbox",
    "Docker sandbox wrapper for Claude Code with enhanced sharing",

    ConfigureVersion(version),
    ConfigureViper("SBOX"),

    // Default command (no subcommand = run)
    Command(runE, "run", "Launch Claude in Docker sandbox",
        Flags(func(flags *pflag.FlagSet) {
            flags.Bool("docker-socket", false, "Mount Docker socket")
            flags.StringSlice("profile", nil, "Additional profiles to use")
            flags.Bool("rebuild", false, "Force rebuild custom template")
        }),
    ),

    Group("profile", "Manage development profiles",
        Command(profileAddE, "add <profiles...>", "Add profiles"),
        Command(profileRemoveE, "remove <profiles...>", "Remove profiles"),
        Command(profileListE, "list", "List profiles"),
    ),

    Command(configE, "config", "View/edit configuration"),
    Command(cleanE, "clean", "Clean up containers and images"),

    OnCommandErrorLogAndExit(zlog),
)
```

---

## Dependencies

### Go Modules Required
```
github.com/streamingfast/cli v0.0.0 (develop branch)
github.com/streamingfast/logging v0.0.0 (develop branch)
github.com/spf13/cobra (transitive via cli)
github.com/spf13/viper (transitive via cli)
github.com/spf13/pflag (transitive via cli)
go.uber.org/zap (transitive via logging)
```

### External Requirements
- Docker Desktop 4.50+ with Sandbox feature enabled
- Claude Code installed in sandbox (handled by base template)
- `docker` CLI available in PATH

---

## Design Decisions (Resolved)

1. **Credential location**: **Docker's default + mount custom file as fallback**
   - Primary: Use Docker sandbox's built-in credential handling via `docker-claude-sandbox-data` volume
   - Fallback: If user has `~/.claude/credentials.json`, mount it for persistent auth across sandbox recreations
   - Rationale: Simplest approach that works out of the box while allowing power users to maintain their own credentials

2. **Custom template vs runtime mounts**: **Build custom Docker images (profiles)**
   - Build custom Docker images extending `docker/sandbox-templates:claude-code` with profile installations
   - Cache built images by profile combination hash for reuse
   - Use `--template` flag to specify the custom image
   - Rationale: Faster startup is better UX; profile changes are infrequent so rebuild cost is acceptable

3. **Project identification**: **Hash of absolute path**
   - Use SHA256 hash of the absolute workspace path (truncated to 12 chars)
   - Store per-project settings in `~/.sbox/projects/<hash>/`
   - Rationale: Simpler than git remote detection, works for any directory, matches claudebox approach

4. **Default behavior**: **Run sandbox immediately**
   - `sbox` without arguments runs the sandbox immediately (equivalent to `sbox run`)
   - Rationale: Most common use case; users want to start working, not read help

---

## References

- [Docker Sandboxes Documentation](https://docs.docker.com/ai/sandboxes/)
- [Docker Sandbox Run CLI Reference](https://docs.docker.com/reference/cli/docker/sandbox/run/)
- [Docker Sandboxes Advanced Config](https://docs.docker.com/ai/sandboxes/advanced-config/)
- [Claude Code Sandboxing Docs](https://code.claude.com/docs/en/sandboxing)
- [streamingfast/cli GitHub](https://github.com/streamingfast/cli)
- [streamingfast/logging GitHub](https://github.com/streamingfast/logging)
- claudebox source code in `claudebox/` directory for inspiration

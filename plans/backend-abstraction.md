# Implementation Plan: Backend Abstraction

## ULTIMATE GOAL

Create an abstraction layer that allows `sbox` to run in two different modes:
1. **Sandbox Backend** (current default): Uses `docker sandbox` commands (MicroVM-based isolation)
2. **Container Backend** (new): Uses standard `docker run` commands with similar functionality

Key requirements:
- Default to `sandbox` backend (preserves current behavior)
- Projects can opt-in to `container` backend via `.sbox` file or CLI flag
- Container mode needs persistence for installed tools
- Minimal requirement: persist `.claude` folder for session resume and auth
- Both backends must support the full sbox feature set (profiles, env vars, plugins, agents, etc.)

## Status: Implemented (Core)

### Implementation Progress (Iteration 3 Complete)

**Completed:**
- [x] Backend interface definition (`backend.go`)
- [x] SandboxBackend implementation (`backend_sandbox.go`)
- [x] ContainerBackend implementation (`backend_container.go`)
- [x] Config types updated with Backend fields (`config.go`)
- [x] CLI commands updated:
  - [x] `run.go` - Added `--backend` flag, uses backend abstraction
  - [x] `shell.go` - Uses backend abstraction
  - [x] `stop.go` - Uses backend abstraction, handles volume cleanup
  - [x] `info.go` - Shows backend type and uses backend for status

**Remaining (Future work):**
- [ ] Integration tests for container backend
- [ ] `sbox config default_backend` command
- [ ] README documentation updates

---

## Current Architecture Analysis

### Core Components Affected

1. **sandbox.go** - Contains all Docker sandbox-specific logic:
   - `RunSandbox()` - Main entry point using `docker sandbox create/run`
   - `FindRunningSandbox()` / `FindDockerSandbox()` - Sandbox discovery via `docker sandbox ls`
   - `CreateDockerSandbox()` - Creates sandbox via `docker sandbox create`
   - `StopSandbox()` - Stops via `docker sandbox stop`
   - `ConnectShell()` - Connects via `docker sandbox exec`
   - `RemoveDockerSandbox()` - Removes via `docker sandbox rm`
   - `ListDockerSandboxes()` / `parseSandboxLsOutput()` - Lists sandboxes

2. **template.go** - Builds custom Docker images:
   - `TemplateBuilder` - Generates Dockerfiles with sbox entrypoint + profiles
   - `GenerateDockerfile()` - Creates Dockerfile content
   - `Build()` - Builds the Docker image
   - Note: Template building is shared; both backends use the same images

3. **entrypoint.go** - Runs inside the container:
   - `RunEntrypoint()` - Sets up plugins, agents, env vars, then execs claude
   - Note: Entrypoint logic is backend-agnostic; works the same in both modes

4. **config.go** - Configuration types:
   - `SboxFileConfig` - Needs new `backend` field
   - `ProjectConfig` - Needs new `Backend` field
   - `Config` - Needs new `DefaultBackend` field

5. **cmd/sbox/*.go** - CLI commands:
   - `run.go` - Needs `--backend` flag, route to appropriate backend
   - `shell.go` - Needs backend-aware shell connection
   - `stop.go` - Needs backend-aware stop logic
   - `info.go` - Needs backend-aware status display

### Key Differences Between Backends

| Aspect | Sandbox Backend | Container Backend |
|--------|-----------------|-------------------|
| Command | `docker sandbox run` | `docker run` |
| Isolation | MicroVM | Standard container |
| Persistence | Built-in (stateful sandbox) | Requires named volumes |
| Docker socket | Automatic | Explicit mount needed |
| Entrypoint | Wrapper script (replaces claude) | Can use standard ENTRYPOINT |
| Lifecycle | `sandbox create/run/stop/rm` | `docker run/exec/stop/rm` |
| Container naming | `sbox-claude-<workspace>` | Same naming convention |
| TTY/Interactive | Automatic | Need `-it` flags |

---

## Implementation Tasks

### Priority 1: Backend Interface Definition

- [ ] **Create Backend interface** - Define the abstraction interface for container execution backends
  - File: `backend.go` (new)
  - Interface methods:
    - `Name() string` - Returns backend name ("sandbox" or "container")
    - `Run(opts BackendOptions) error` - Start/attach to container
    - `Shell(workspaceDir string) error` - Connect shell to running container
    - `Stop(workspaceDir string, remove bool) (*ContainerInfo, error)` - Stop container
    - `Find(workspaceDir string) (*ContainerInfo, error)` - Find container for workspace
    - `FindRunning(workspaceDir string) (*ContainerInfo, error)` - Find running container
    - `List() ([]ContainerInfo, error)` - List all containers managed by this backend
    - `Remove(containerID string) error` - Remove container
  - Define `BackendOptions` struct (subset of current `SandboxOptions`)
  - Define `ContainerInfo` struct (unified container info for both backends)

- [ ] **Define backend types enum** - Create constants for backend types
  - File: `backend.go`
  - Values: `BackendSandbox = "sandbox"`, `BackendContainer = "container"`
  - Validation function: `ValidateBackend(name string) error`

### Priority 2: Refactor Sandbox Backend

- [ ] **Create SandboxBackend implementation** - Extract current sandbox logic into Backend implementation
  - File: `backend_sandbox.go` (new)
  - Move sandbox-specific functions from `sandbox.go`:
    - `RunSandbox()` -> `SandboxBackend.Run()`
    - `ConnectShell()` -> `SandboxBackend.Shell()`
    - `StopSandbox()` -> `SandboxBackend.Stop()`
    - `FindRunningSandbox()` -> `SandboxBackend.FindRunning()`
    - `FindDockerSandbox()` -> `SandboxBackend.Find()`
    - `ListDockerSandboxes()` -> `SandboxBackend.List()`
    - `CreateDockerSandbox()` -> internal helper
    - `RemoveDockerSandbox()` -> `SandboxBackend.Remove()`
  - Keep `sandbox.go` for shared utilities:
    - `GenerateSandboxName()` (rename to `GenerateContainerName()`)
    - `IsInsideSandbox()` (rename to `IsInsideContainer()`)
    - `GetSandboxContainerName()` (deprecate or keep for compat)
    - Volume mount helpers
    - Mount mismatch checking

- [ ] **Update sandbox.go exports** - Maintain backward compatibility
  - File: `sandbox.go`
  - Keep existing function signatures as wrappers that delegate to `SandboxBackend`
  - Mark as deprecated where appropriate
  - This allows gradual migration without breaking existing code

### Priority 3: Implement Container Backend

- [ ] **Create ContainerBackend implementation** - New backend using `docker run`
  - File: `backend_container.go` (new)
  - Implement all Backend interface methods
  - Key differences from SandboxBackend:
    - Uses `docker run` instead of `docker sandbox create/run`
    - Uses `docker exec` instead of `docker sandbox exec`
    - Uses `docker stop/rm` instead of `docker sandbox stop/rm`
    - Uses `docker ps` instead of `docker sandbox ls`
    - Needs explicit volume mounts for persistence

- [ ] **Implement container persistence strategy** - Design volume mount scheme for container backend
  - File: `backend_container.go`
  - Named volume for `.claude` folder: `sbox-claude-<workspace-hash>`
  - Named volume for home directory state: `sbox-home-<workspace-hash>` (optional)
  - Or use a single volume: `sbox-data-<workspace-hash>` mounted at `/home/agent`
  - Decision: Use **named volume for `/home/agent/.claude`** as minimum
    - Persists: credentials, settings, session state
    - Does NOT persist: installed tools (those come from profiles/template)
  - Optional: Add `persist_home: true` config option for full home persistence

- [ ] **Implement container run logic** - Build `docker run` command
  - File: `backend_container.go`
  - Command structure:
    ```
    docker run --rm -it \
      --name sbox-claude-<workspace> \
      -v <workspace>:<workspace> \
      -v sbox-claude-<hash>:/home/agent/.claude \
      -v ~/.ssh:/home/agent/.ssh:ro \
      -w <workspace> \
      -e WORKSPACE_DIR=<workspace> \
      [additional mounts from config] \
      [docker socket mount if enabled] \
      <template-image>
    ```
  - Handle existing container: check if running, attach or restart

- [ ] **Implement container attach/reattach logic** - Handle reconnecting to running containers
  - File: `backend_container.go`
  - If container exists and running: `docker attach`
  - If container exists but stopped: `docker start -ai`
  - If container doesn't exist: `docker run`

- [ ] **Implement container discovery** - Find containers by workspace
  - File: `backend_container.go`
  - Use `docker ps -a --filter name=sbox-claude-<workspace>`
  - Parse `docker inspect` output for mount info
  - Match by workspace mount path

### Priority 4: Configuration Updates

- [ ] **Add backend field to SboxFileConfig** - Allow per-project backend selection in sbox.yaml
  - File: `config.go`
  - Add `Backend string yaml:"backend"` field to `SboxFileConfig`
  - Values: "", "sandbox", "container" (empty = use default)

- [ ] **Add backend field to ProjectConfig** - Store per-project backend preference
  - File: `config.go`
  - Add `Backend string yaml:"backend"` field to `ProjectConfig`

- [ ] **Add default backend to global Config** - Allow changing global default
  - File: `config.go`
  - Add `DefaultBackend string yaml:"default_backend"` field to `Config`
  - Default value: "sandbox"

- [ ] **Update MergeProjectConfig** - Handle backend field merging
  - File: `config.go`
  - Merge priority: CLI flag > sbox.yaml > project config > global config

### Priority 5: CLI Command Updates

- [ ] **Add --backend flag to run command** - Allow runtime backend selection
  - File: `cmd/sbox/run.go`
  - Add flag: `--backend string` with choices "sandbox", "container"
  - Update `runE()` to:
    1. Determine backend from flag > sbox.yaml > project config > global default
    2. Instantiate appropriate backend implementation
    3. Call `backend.Run(opts)`

- [ ] **Create backend factory function** - Centralize backend instantiation
  - File: `backend.go`
  - `func GetBackend(name string, config *Config) (Backend, error)`
  - Returns `*SandboxBackend` or `*ContainerBackend` based on name

- [ ] **Update shell command** - Use backend abstraction
  - File: `cmd/sbox/shell.go`
  - Determine backend for workspace
  - Call `backend.Shell(workspaceDir)`

- [ ] **Update stop command** - Use backend abstraction
  - File: `cmd/sbox/stop.go`
  - Determine backend for workspace
  - Call `backend.Stop(workspaceDir, remove)`

- [ ] **Update info command** - Show backend type
  - File: `cmd/sbox/info.go`
  - Display which backend is configured/in use
  - Call `backend.Find()` for status info

- [ ] **Add sbox config backend command** - Allow setting default backend
  - File: `cmd/sbox/config.go`
  - Add support for `sbox config default_backend sandbox|container`

### Priority 6: Template Updates for Container Backend

- [ ] **Update template for container mode** - Adjust Dockerfile for standard container usage
  - File: `template.go`
  - Container backend can use standard ENTRYPOINT instead of wrapper script
  - Add option to generate container-optimized Dockerfile variant
  - Or: use same template, container backend just uses `docker run` with same image

- [ ] **Verify entrypoint works in container mode** - Test entrypoint execution
  - File: `entrypoint.go`
  - The claude wrapper script should work the same way
  - PWD/WORKSPACE_DIR handling should work with bind mounts
  - Test: plugins, agents, env vars all load correctly

### Priority 7: Volume Persistence Implementation

- [ ] **Implement named volume creation** - Create persistence volumes
  - File: `backend_container.go`
  - Create volume on first run: `docker volume create sbox-claude-<hash>`
  - Mount at `/home/agent/.claude`

- [ ] **Handle volume cleanup** - Clean up on full removal
  - File: `backend_container.go`
  - `sbox stop --rm` removes container but keeps volume
  - `sbox stop --rm --all` removes container AND volume
  - `sbox clean` should clean up orphaned volumes

- [ ] **Add volume listing to info command** - Show persistence volumes
  - File: `cmd/sbox/info.go`
  - For container backend, show associated volumes
  - Indicate volume size if easily available

### Priority 8: Testing and Documentation

- [ ] **Add unit tests for Backend interface** - Test interface compliance
  - File: `backend_test.go` (new)
  - Verify both backends implement interface correctly
  - Test backend factory function

- [ ] **Add integration tests for container backend** - Test full workflow
  - File: `integration_test.go`
  - Test container run, shell, stop, remove
  - Test persistence across restarts
  - Test profile installation

- [ ] **Update README documentation** - Document backend options
  - File: `README.md`
  - Add section on backend selection
  - Document when to use each backend
  - Document persistence behavior

- [ ] **Update sbox.yaml documentation** - Document backend field
  - File: `README.md`
  - Show example with `backend: container`

---

## Technical Design Details

### Backend Interface Definition

```go
// backend.go

// BackendType represents the container backend type
type BackendType string

const (
    BackendSandbox   BackendType = "sandbox"
    BackendContainer BackendType = "container"
)

// ContainerInfo contains information about a managed container
type ContainerInfo struct {
    ID        string
    Name      string
    Status    string // "running", "stopped", "created"
    Image     string
    Workspace string
    Backend   BackendType
}

// BackendOptions contains options for running a container
type BackendOptions struct {
    WorkspaceDir      string
    Profiles          []string
    ForceRebuild      bool
    Debug             bool
    Config            *Config
    ProjectConfig     *ProjectConfig
    SboxFile          *SboxFileLocation
    MountDockerSocket bool
}

// Backend defines the interface for container execution backends
type Backend interface {
    // Name returns the backend type name
    Name() BackendType

    // Run starts or attaches to a container for the given workspace
    Run(opts BackendOptions) error

    // Shell opens a shell in the running container
    Shell(workspaceDir string) error

    // Stop stops the container, optionally removing it
    Stop(workspaceDir string, remove bool) (*ContainerInfo, error)

    // Find returns container info for the given workspace (any state)
    Find(workspaceDir string) (*ContainerInfo, error)

    // FindRunning returns container info only if running
    FindRunning(workspaceDir string) (*ContainerInfo, error)

    // List returns all containers managed by this backend
    List() ([]ContainerInfo, error)

    // Remove removes a container by ID
    Remove(containerID string) error
}
```

### Container Backend Volume Strategy

```
Named Volumes for Container Backend:
===================================

1. Claude Home Volume (required):
   Name: sbox-claude-<workspace-hash>
   Mount: -v sbox-claude-<hash>:/home/agent/.claude
   Contents:
   - credentials.json (auth)
   - settings.json (MCP config)
   - sessions/ (session state)
   - agents/ (installed inside sandbox)

2. Workspace Volume (bind mount):
   Mount: -v <host-workspace>:<host-workspace>
   Same as sandbox mode

3. SSH Volume (bind mount, read-only):
   Mount: -v ~/.ssh:/home/agent/.ssh:ro
   Same as sandbox mode

4. Docker Socket (optional bind mount):
   Mount: -v /var/run/docker.sock:/var/run/docker.sock
   When docker_socket: always or flag is set
```

### Configuration Merging Order

```
Backend determination priority (highest to lowest):
1. CLI flag: --backend container
2. sbox.yaml: backend: container
3. Project config (~/.config/sbox/projects/<hash>/config.yaml): backend: container
4. Global config (~/.config/sbox/config.yaml): default_backend: container
5. Hardcoded default: sandbox
```

### Container Lifecycle (Container Backend)

```
First Run:
1. Build template image (same as sandbox mode)
2. Create named volume for .claude
3. docker run --rm -it --name <name> [volumes...] <image>
4. Entrypoint runs, sets up plugins/agents/env
5. Claude starts

Subsequent Runs:
1. Check if container exists: docker ps -a --filter name=<name>
2. If running: docker attach <name>
3. If stopped: docker start -ai <name>
4. If not exists: docker run (as above)

Shell:
1. docker exec -it <name> bash

Stop:
1. docker stop <name>
2. If --rm: docker rm <name>
3. If --rm --all: docker rm <name> && docker volume rm <volume>
```

---

## Migration Considerations

### Backward Compatibility

1. **Default behavior unchanged**: Without any configuration, sbox continues using sandbox backend
2. **Existing sandboxes continue to work**: No migration required for current users
3. **Gradual adoption**: Users can opt-in per project via sbox.yaml

### Potential Breaking Changes

1. None expected - this is purely additive functionality
2. Internal refactoring moves functions but maintains public API

### Future Enhancements (Out of Scope)

1. Kubernetes backend (run in k8s pods)
2. Remote Docker host support
3. Container image registry support
4. Multi-container support (sidecar services)

---

## File Changes Summary

### New Files
- `backend.go` - Interface definition and factory
- `backend_sandbox.go` - Sandbox backend implementation
- `backend_container.go` - Container backend implementation
- `backend_test.go` - Backend unit tests

### Modified Files
- `sandbox.go` - Refactored to shared utilities + deprecated wrappers
- `config.go` - Add backend fields to config types
- `cmd/sbox/run.go` - Add --backend flag, use backend abstraction
- `cmd/sbox/shell.go` - Use backend abstraction
- `cmd/sbox/stop.go` - Use backend abstraction
- `cmd/sbox/info.go` - Show backend info, use abstraction
- `cmd/sbox/config.go` - Support default_backend setting
- `cmd/sbox/clean.go` - Clean up container volumes
- `integration_test.go` - Add container backend tests
- `README.md` - Document backend options

---

## Dependencies

No new external dependencies required. Uses existing:
- Docker CLI (`docker run`, `docker ps`, `docker exec`, etc.)
- All existing Go dependencies

---

## References

- Current sbox codebase analysis (above)
- Docker run documentation: https://docs.docker.com/reference/cli/docker/run/
- Docker volumes documentation: https://docs.docker.com/storage/volumes/
- Docker sandbox documentation: https://docs.docker.com/ai/sandboxes/

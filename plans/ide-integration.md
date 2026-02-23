# IDE Integration Across Sandbox Boundary

Initial research and thought process for enabling IDE integration (VSCode, JetBrains) to work with Claude Code running inside sbox sandbox.

## The Problem

The VS Code extension expects to communicate directly with the Claude CLI process on the same host. With sbox, Claude runs inside a sandbox/container, breaking this assumption.

## Current State

**sbox architecture:**
- All communication is file-system or environment-variable based
- Workspace mounted at same absolute path (host path = container path)
- No socket/port-based communication exists
- Claude runs in isolated MicroVM (sandbox backend) or standard container

**Claude Code IDE extension:**
- Requires CLI installed on the same machine
- Acts as a "bridge" to the CLI process
- Uses native VS Code diff viewer, file watchers, etc.
- Shares conversation history between extension and CLI via `claude --resume`

## Possible Approaches

### Option 1: Proxy-Based Communication

- Host-side proxy process presents Claude CLI-compatible interface to extension
- Socket/port forwarding between proxy and sandboxed Claude
- New `sbox ide-proxy` command

### Option 2: VS Code Remote Containers

- Leverage VS Code's Remote Containers infrastructure
- Connect VS Code directly to sandbox as dev container
- Challenge: MicroVM may not support VS Code Server

### Option 3: MCP Server Bridge

- MCP server on host bridges IDE ↔ sandbox
- Port forwarding from sandbox to host MCP server
- IDE implements MCP client

### Option 4: File-Based Protocol

- Use `.sbox/` directory as communication channel
- IDE writes requests, Claude polls and responds
- Simplest but higher latency, no real-time streaming

## Key Challenges

| Challenge | Details |
|-----------|---------|
| Process communication | Extension expects local process, not networked service |
| Checkpoint streaming | Need real-time sync for rewind feature |
| Extension auto-install | Won't work inside sandbox |
| Authentication | Proxy needs to auth to both extension and sandboxed Claude |

## Existing Extension Points

1. **Environment variables** - Pass `IDE_SOCKET_PATH` to Claude
2. **Volume mounts** - Mount Unix socket at `.sbox/ide.sock`
3. **Entrypoint hooks** - Add IDE setup in `entrypoint.go`
4. **Custom profiles** - Install IDE server components

## Next Steps

1. Reverse engineer extension ↔ CLI communication (IPC, files, stdio?)
2. Determine if extension can be configured to use custom endpoint
3. Build minimal proxy prototype
4. Consider upstream contribution for "remote Claude" support

## References

- https://code.claude.com/docs/en/vs-code
- https://www.eesel.ai/blog/claude-code-ide-integration

# Host Environment Context (Host Backend)

You are running directly on the **host machine** via the **sbox host backend** — no Docker or MicroVM isolation is in use.

## Environment Overview

The host backend launches you as a regular process on the developer's machine. You share the same user, filesystem, and tools as the person running sbox. This is the most direct and least isolated execution mode — use it when Docker is unavailable or when you need native access to the host environment.

### User & Paths

- **User**: The host user who invoked `sbox run`
- **Home directory**: The host user's `$HOME`
- **Workspace**: The directory passed to `sbox run` (or current directory)

### Tools Available

All tools installed on the host are available to you — the PATH is inherited from the user's environment.

## Freedom & Limitations

### What You Can Do

- Full read/write access to the workspace directory and host filesystem
- Run any command available in the host's PATH
- Access the internet (subject to host firewall rules)

### Limitations

- **No isolation**: Actions affect the real host system directly
- **No profiles**: Tool installation profiles are not supported in host backend mode
- **No `sbox shell` / `sbox stop` / `sbox info`**: These commands are not supported for the host backend

### The `.sbox/` Directory

`.sbox/` is created in the workspace directory (same as other backends) and may contain:
- `CLAUDE.md` / `AGENTS.md` — concatenated instructions from the workspace hierarchy
- `loop.completion` — written by you to signal loop goal completion

## Additional Notes

- You are running with full permissions — be thoughtful about destructive operations
- The workspace is your current directory on the host

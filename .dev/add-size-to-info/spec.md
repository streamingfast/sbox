## Feature: add-size-to-info

### Goal

Add disk size information to `sbox info` output so that the user can see how much disk
space a project's sandbox/container is consuming.

### What Gets Shown

For **sandbox backend**:
- Sandbox VM size: `docker sandbox inspect <name>` or `docker system df` equivalent
  - Since docker sandbox doesn't have a direct disk size command, use the disk usage of
    the sandbox directory (typically under `~/.local/share/docker/sandbox/<name>/` or
    inferred from `docker system df`). If not deterministically available, skip.
  - Alternative: use `docker sandbox inspect` JSON and find a size field.

For **container backend**:
- Container size on disk: `docker inspect --format '{{.SizeRootFs}} {{.SizeRw}}'` (bytes)
  - SizeRootFs: size of the read-only image layers
  - SizeRw: size of the writable layer (changes made inside the container)
- Named volume size: `docker system df -v` to find the volume size, or
  `du -sh` of the volume mount path if accessible.

For **host backend**:
- Not applicable (agent runs on host, no isolated disk usage to report).

### Where It Appears

In `sbox info` (current project) and `sbox info --all` (all projects), add a "Size" field
after the Status field in the container/sandbox section.

### Output Format

```
  Sandbox:
    Name:   sbox-claude-myproject
    Status: running
    Size:   1.23 GB
    Image:  claude-sbox:latest
```

Or for containers with volume:
```
  Container:
    Name:   sbox-claude-myproject
    Status: running
    Size:   234 MB (container) + 1.1 GB (volume)
    Image:  ...
```

If size cannot be determined, omit the Size line (no error shown to user).

### Human-Readable Format

Use IEC units (KiB, MiB, GiB) or SI units (KB, MB, GB). Use SI units (MB, GB) for
simplicity and familiarity. Format: e.g. "1.23 GB", "456 MB", "12 KB".

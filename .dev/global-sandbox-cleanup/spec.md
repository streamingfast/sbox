## Feature: global-sandbox-cleanup

Sandboxes take a lot of space. We need a global way to clean up last-used sandboxes.

### Command

`sbox prune`

### Last-Used Tracking

Write a small timestamp file `.sbox/last-used` inside the workspace directory each time
`sbox run` (or any backend's `Run()`) is called. The file contains an RFC3339 timestamp.

### What Gets Removed

Everything associated with a sandbox entry:
1. The Docker sandbox itself (`docker sandbox rm <name>`)
2. The `.sbox/` directory inside the workspace
3. The `~/.config/sbox/projects/<hash>/` entry

### Selection Logic

Two modes for selecting which sandboxes to prune:

1. **Stale sandboxes** (workspace directory no longer exists on disk): always included in
   candidates regardless of `--keep`.

2. **Old sandboxes** (workspace still exists but last-used is old): selected according to
   `--keep N` (keep the N most recently used; prune the rest).

### Flags

- `--keep N` (default: 5) — keep the N most recently used sandboxes; prune the rest
- `--force` — actually perform deletions (default is dry-run mode)

### Dry-run (default)

Without `--force`, print what would be deleted and why, but do not actually delete anything.
Output should clearly indicate it is a dry-run.

### Stale Detection

Docker sandboxes whose workspace path no longer exists on disk are stale. Detection:
- Read the `Workspace` field from `docker sandbox ls` output (already parsed into `DockerSandbox.Workspace`)
- Cross-reference with known project entries from `~/.config/sbox/projects/`
- `os.Stat(workspacePath)` to check if the directory still exists

### Windows Support

Use `os.UserHomeDir()` and `filepath.Join()` for all path construction. The sandbox VM
directory location is only used as a fallback heuristic and is not strictly required.

### Output Format

```
sbox prune (dry-run, use --force to actually delete)

Would prune 3 sandboxes:

  sbox-claude-myproject   /home/user/myproject   last used: 2026-01-10   reason: workspace missing
  sbox-claude-oldwork     /home/user/oldwork     last used: 2026-02-01   reason: stale (outside keep=5)
  sbox-claude-abandoned   /home/user/abandoned   last used: 2026-02-15   reason: stale (outside keep=5)

Keeping 5 most recently used sandboxes.
Run with --force to delete.
```

When `--force` is given, same list with "Pruned" instead of "Would prune", and each line
prefixed with what was actually deleted.

## Feature: global-sandbox-cleanup

Sandboxes take a lot of space. We need a global way to clean up last-used sandboxes.

## Feedback 5

- I would like that we found orphan Docker sandbox and prune them too. Those will need special handling for sure but for example right now I see:

  ```
  sbox-claude-docker_container_opencode sbox-claude-sbox                      sbox-opencode-sbox
  sbox-claude-firehose-core             sbox-claude-substreams-solana
  ```

  But only three of them have active Sbox project, `sbox-claude-docker_container_opencode` and `sbox-opencode-sbox` are stalled.

  We can find orphan sandbox by checking for all of them that starts with `sbox-...`.

- Let's add support for `sbox prune container` and `all` would now means sandbox + container.

  Render as tables just line sandboxes, separate container from sandboxes. On prune, stop container and delete them + volume.

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

Three sections, each rendered as a `github.com/charmbracelet/lipgloss/table` table.
Sections only appear if they have entries. All tables have 3 columns: Sandbox, Workspace, Last Used.
No "Reason" column — the section title communicates the reason.

Section titles use `headerStyle`, separators use `dimStyle` (40 chars).

```
sbox prune (dry-run — use --force to actually delete)

Pruning 2 sandbox(es) | Missing
────────────────────────────────────────
  ┌──────────────────────────┬──────────────────────┬─────────────────────┐
  │ Sandbox                  │ Workspace            │ Last Used           │
  ├──────────────────────────┼──────────────────────┼─────────────────────┤
  │ sbox-claude-myproject    │ /home/user/myproject │ 2026-01-10 00:00:00 │
  └──────────────────────────┴──────────────────────┴─────────────────────┘

Pruning 1 sandbox(es) | Too old
────────────────────────────────────────
  ┌──────────────────────┬──────────────────────┬─────────────────────┐
  │ Sandbox              │ Workspace            │ Last Used           │
  ├──────────────────────┼──────────────────────┼─────────────────────┤
  │ sbox-claude-oldwork  │ /home/user/oldwork   │ 2026-02-01 00:00:00 │
  └──────────────────────┴──────────────────────┴─────────────────────┘

Keeping 2 sandbox(es)
────────────────────────────────────────
  ┌──────────────────────┬──────────────────────┬─────────────────────┐
  │ Sandbox              │ Workspace            │ Last Used           │
  ├──────────────────────┼──────────────────────┼─────────────────────┤
  │ sbox-claude-recent   │ /home/user/recent    │ 2026-04-01 00:00:00 │
  │ sbox-claude-active   │ /home/user/active    │ 2026-04-20 00:00:00 │
  └──────────────────────┴──────────────────────┴─────────────────────┘

Run with --force to delete.
```

When `--force` is given, same sections/tables but header says "Pruned N sandbox(es) | ..." instead of "Pruning".

### Styling

- New `stylex/` package: copy `stylex.go` from `https://github.com/streamingfast/firehose-core/blob/develop/cmd/tools/stylex/stylex.go` as-is.
- Use `github.com/charmbracelet/lipgloss/table` (`table.New().Border().BorderStyle().Headers().Rows().StyleFunc(...)`) for all three tables.
- Section title uses `stylex.Header(...)`, separator uses `stylex.Dim(strings.Repeat("─", 40))`.

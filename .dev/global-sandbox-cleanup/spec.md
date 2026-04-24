## Feature: global-sandbox-cleanup

Sandboxes take a lot of space. We need a global way to clean up last-used sandboxes.

## Feedback 2

- `prune UI`

 I would try in sections instead of a big table. Also, we should see a section for those kept. Let's copy https://github.com/streamingfast/firehose-core/blob/develop/cmd/tools/stylex/stylex.go to a stylex/ package in sbox so we have the same coloring sharing. Also for tables rendering, and use table pattern from https://github.com/streamingfast/streamingfast-comparator/blob/master/analyzer_report.go to manage/control the different tables.

  Questions before I consider the spec complete:

  > 1. Comparator table pattern — can you describe what specific pattern from that file you want? Or point
  me to an alternative location? Does it relate to how column widths are calculated, or how headers are
  styled?

  Yes here https://github.com/streamingfast/streamingfast-comparator/blob/master/analyzer_report.go#L390C1-L408C1


  2. Color per row vs per reason — should the entire prune-candidate row be colored, or only the reason
  column?

  Let's start with only the reason column to see out if look. For too old reason, shorter it to a single or two words.

  3. Column layout — same 4 columns (sandbox, workspace, last used, reason), or should the kept section
  drop the reason column?

  I think we should drop probably, it seems to me that in a "section", all reason will be the same. Will fit well with the
  "kept" table.

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

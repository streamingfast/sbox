# Fix Extra Blank Lines in Markdown Rendering

root_git: /Users/maoueh/work/sf/sbox
worktree: /Users/maoueh/work/sf/sbox
branch: master
target_branch: master

> **Resume protocol:** read **Dev Feedback** and the **State Tracker** below first, then jump to the
> step marked `Current`. Ensure that you are in the correct worktree and branch according to preamble here. Update current with Developer feedback and update the tracker after every meaningful change.
> Do not mutate completed steps; append a new entry instead.

---

## Dev Feedback

<Empty initially, developer add feedback and open questions answers' here>

## State Tracker

**Last Updated:** 2026-05-06 11:30:00
**Current Step:** Step 4 — Completed
**Status:** Done

### Step 1 — Investigate Root Cause (Completed)

Investigated how glamour renders markdown in `claude/stream.go` and `opencode/stream.go`.

**Root cause identified:** Two related issues in `printMarkdown`:

1. Glamour pads each line to the configured word wrap width (100) using ANSI-colored spaces: sequences like `\x1b[38;5;252m \x1b[m`. These lines appear blank visually but are NOT detected as blank by `strings.TrimSpace()` because the ANSI codes wrap the spaces and `TrimSpace` only removes raw whitespace.

2. With `WithWordWrap(100)`, every line gets padded to 100 characters wide with these ANSI-colored spaces, which then pass through the blank-line filter and appear as empty lines in the output.

### Step 2 — Implement Fix (Completed)

Applied two-part fix to both `claude/stream.go` and `opencode/stream.go`:

1. Changed `glamour.WithWordWrap(100)` to `glamour.WithWordWrap(0)` — disables glamour's line padding entirely, letting the terminal handle natural line wrapping. This eliminates the trailing ANSI-padded spaces on each line.

2. Changed blank-line detection from `strings.TrimSpace(line) == ""` to `strings.TrimSpace(xansi.Strip(line)) == ""` using `github.com/charmbracelet/x/ansi` — ANSI-aware blank detection as defense-in-depth for any remaining cases.

Also added import `xansi "github.com/charmbracelet/x/ansi"` in both files (this package was already a transitive dependency via charmbracelet/lipgloss).

### Step 3 — Add Unit Tests (Completed)

Created `/Users/maoueh/work/sf/sbox/claude/stream_test.go` with tests:
- `TestPrintMarkdown_NoExtraBlankLines` — verifies no consecutive blank lines for various markdown inputs including blockquotes, headings, lists
- `TestPrintMarkdown_FirstLineHasBulletPrefix` — verifies the ● prefix on the first line
- `TestPrintMarkdown_SubsequentLinesHaveIndent` — verifies 2-space indent on subsequent lines

All tests pass.

### Step 4 — Update CHANGELOG.md and Task File (Completed)

Added entry to `CHANGELOG.md` under `## Unreleased / ### Fixed`.

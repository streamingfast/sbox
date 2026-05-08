# Fix sbox run Command Hang

mode: bug
state: review
root_git: /Users/maoueh/work/sf/sbox
worktree: /tmp/worktrees/fix-sbox-run-hang
branch: fix/sbox-run-hang
target_branch: master

> **Resume protocol:** read **Dev Feedback** and the **State Tracker** below first, then jump to the
> step marked `Current`. Ensure that you are in the correct worktree and branch according to preamble here. Update current with Developer feedback and update the tracker after every meaningful change.
> Do not mutate completed steps; append a new entry instead.

---

## Initial Description

The `sbox run` command hangs for a while before completing or starting work. This is a bug where the command does not start promptly and instead hangs/delays for a noticeable period of time.

## Dev Feedback

## Spec & Implementation

### Root Cause

In `cmd/sbox/run.go`, when no prompt is provided via argument, the code checks if stdin is not a terminal and then calls `io.ReadAll(os.Stdin)`. This is intended to support piped input like `cat TASK.md | sbox run`.

However, when `sbox run` is launched from certain environments (IDEs, some shells, CI environments), stdin can be reported as non-terminal even though no data is being piped to it. In that case, `io.ReadAll(os.Stdin)` blocks indefinitely waiting for EOF — causing the observed hang.

### Fix

Added a `stdinHasData()` helper that uses `syscall.Select` with a zero timeout to check whether stdin has data immediately available for reading. The stdin read is now only performed when stdin is both non-terminal AND has data ready:

```go
if interactivePrompt == "" && !term.IsTerminal(int(os.Stdin.Fd())) && stdinHasData() {
    // read stdin...
}
```

This preserves the piped stdin feature (`cat TASK.md | sbox run`) while eliminating the hang in environments where stdin appears non-terminal but is empty.

## State Tracker

**Last Updated:** 2026-05-08
**Current Step:** Step 2 — Fix implemented and ready for review
**Status:** Complete

### Step 1 — Investigate hang cause ✅
Identified root cause: `io.ReadAll(os.Stdin)` blocks when stdin is non-terminal but no data is piped.

### Step 2 — Implement fix ✅
Added `stdinHasData()` using `syscall.Select` with zero timeout. All tests pass. CHANGELOG updated. Committed.

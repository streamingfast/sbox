# Feature: sbox loop --watch flag

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

**Last Updated:** 2026-05-05
**Current Step:** Step 3 — Complete
**Status:** Done

### Step 1 — Assess existing implementation
Reviewed `cmd/sbox/loop.go`. The `--watch` flag, `compileWatchPatterns`, `watchForChanges`,
and the watch loop in `loopE` are all fully implemented with `fsnotify`.

### Step 2 — Verify tests and build
`go test ./...` passes (cached). `go build ./cmd/sbox/...` succeeds.

### Step 3 — Complete
Feature is fully implemented on master. CHANGELOG.md already updated. Task moved to done.

---

## Original Spec

Support `sbox loop ... --watch "<regex_file1>" --watch "<regex_file2>"`. When a watch file is specified:
- When the sbox loop normal command would have exited with a report, still write the completion report
- Then launch watches on all the specified files that match the regex
- If any watched file is edited, relaunch the last sbox loop invocation
- While sbox loop is already running, watches should NOT be active

# Feature: Add --watch mode to `sbox run`

mode: feature
state: review
root_git: /Users/maoueh/work/sf/sbox
worktree: /Users/maoueh/work/sf/sbox/.worktrees/feature-watch-mode-sbox-run
branch: feature/feature-watch-mode-sbox-run
target_branch: master

> **Resume protocol:** read **Dev Feedback** and the **State Tracker** below first, then jump to the
> step marked `Current`. Ensure that you are in the correct worktree and branch according to preamble here. Update current with Developer feedback and update the tracker after every meaningful change.
> Do not mutate completed steps; append a new entry instead.

---

## Description

Add a `--watch` flag to `sbox run` command similar to the one that already exists in `sbox loop`.

When `--watch` is specified with `sbox run`, after the interactive session exits, the command stays alive and watches for changes to files matching the given regex patterns. Any matching file change triggers a new `sbox run` launch. Watches are inactive while the agent is running.

The `--watch` flag should accept regex patterns (same as `sbox loop --watch`), can be specified multiple times, and should work the same way as loop's watch mode — it reuses the `watchForChanges` and `compileWatchPatterns` helpers that already exist in `cmd/sbox/loop.go`.

## Implementation Plan

1. Add `--watch` flag to `RunCommand` in `cmd/sbox/run.go`
2. In `runE`, read `watchPatternStrs` and compile patterns using `compileWatchPatterns`
3. If no watch patterns: keep existing behavior (run once, return)
4. If watch patterns provided:
   - Set up signal handling (same pattern as `loopE`)
   - Run the backend
   - After run completes, enter watch loop: watch for file changes, relaunch on change
5. Update the command description in `RunCommand` to mention `--watch`
6. Update CHANGELOG.md

Note: `compileWatchPatterns` and `watchForChanges` are defined in `cmd/sbox/loop.go` in the same `main` package, so they are accessible from `run.go` directly.

## Dev Feedback

<empty>

## State Tracker

**Last Updated:** 2026-05-07
**Current Step:** Step 6 — Review Complete
**Status:** Implementation merged to master. `--watch` flag added to `sbox run` with full watch loop (signal handling, file watching via `watchForChanges`/`compileWatchPatterns`, relaunches on change). CHANGELOG updated. Both features (watch + prompt) were merged together in commit `60c59bd`. Ready for user sign-off to mark done.

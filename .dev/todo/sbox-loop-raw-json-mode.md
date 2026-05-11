# sbox loop Raw JSON Mode

mode: feature
state: review
root_git: .worktrees/feature/sbox-loop-raw-json-mode
worktree: .worktrees/feature/sbox-loop-raw-json-mode
branch: feature/sbox-loop-raw-json-mode
target_branch: master

> **Resume protocol:** read **Dev Feedback** and the **State Tracker** below first, then jump to the
> step marked `Current`. Ensure that you are in the correct worktree and branch according to preamble here. Update current with Developer feedback and update the tracker after every meaningful change.
> Do not mutate completed steps; append a new entry instead.

---

## Initial Description

We need to have a way to run `sbox loop` in raw JSON form mode so that for some specific run, I can collect some raw JSON to debug some rendering issues.

## Dev Feedback

## Spec & Implementation

### Feature: `--raw-json` flag for `sbox loop`

**Flag:** `--raw-json` (bool, default false)

**Usage:**
```
sbox loop --raw-json "my goal"
```

**How it works:**

The flag flows through the entire stack:

1. **`cmd/sbox/loop.go`** — Adds `--raw-json` flag to `LoopCommand`, reads it and passes it into `BackendOptions.RawJSON`.

2. **`backend.go`** — `BackendOptions` gains a `RawJSON bool` field.

3. **`entrypoint.go`** — `EntrypointConfig` gains a `RawJSON bool` field (YAML: `raw_json`). `PrepareSboxDirectory` copies it from `BackendOptions` to `EntrypointConfig`, which is written to `.sbox/entrypoint.yaml` and read inside the sandbox.

4. **Inside the sandbox** — `runAgentWithStreamTransformerEx` receives a `rawJSON bool` parameter. When true, it writes each JSON line directly to stdout via `fmt.Fprintln` instead of passing it through the `StreamPrinter`. This bypasses all rendering logic and outputs the raw agent JSON stream.

The same mechanism applies to single-prompt mode (non-loop) as well, so if `config.RawJSON` is set in any context, the raw output is produced.

### Files changed:
- `backend.go`: Added `RawJSON bool` to `BackendOptions`
- `entrypoint.go`: Added `RawJSON bool` to `EntrypointConfig`, updated `PrepareSboxDirectory`, `runAgentWithStreamTransformer`, `runAgentWithStreamTransformerEx`, and `runLoop` call sites
- `cmd/sbox/loop.go`: Added `--raw-json` flag and wired it through

## State Tracker

**Last Updated:** 2026-05-11
**Current Step:** Step 2 — Implementation complete, ready for review
**Status:** Done

### Step 1 — Implement raw JSON mode for sbox loop
**Status:** Completed

Added `--raw-json` flag to `sbox loop`. When set, the agent's JSON stream output is written directly to stdout as-is, bypassing all rendering/pretty-printing logic. Useful for collecting raw JSON to debug rendering issues.

# Feature: Support running `sbox run "<prompt>"` with prompt passed in

mode: feature
state: review
root_git: /Users/maoueh/work/sf/sbox
worktree: /Users/maoueh/work/sf/sbox/.worktrees/feature-sbox-run-prompt
branch: feature/feature-sbox-run-prompt
target_branch: master

> **Resume protocol:** read **Dev Feedback** and the **State Tracker** below first, then jump to the
> step marked `Current`. Ensure that you are in the correct worktree and branch according to preamble here. Update current with Developer feedback and update the tracker after every meaningful change.
> Do not mutate completed steps; append a new entry instead.

---

## Description

Support running `sbox run "<prompt>"` which launches interactively but with a prompt passed in.

- For Claude: `claude "<prompt>"` (pass prompt as positional arg to claude CLI)
- For OpenCode: `opencode --prompt "<prompt>"` (pass via --prompt flag)

When a prompt is provided as an argument to `sbox run`, the agent should launch with that prompt pre-filled (but the session remains interactive — the user can continue the conversation after the prompt is processed).

## Implementation Plan

1. Change `sbox run` to accept an optional positional argument (the prompt)
   - Currently uses `ArbitraryArgs()` for extra args passed after `--`; need to handle the first positional arg as a prompt
   - The current `ArbitraryArgs()` passes args to `AgentArgs` in `BackendOptions`
   - We need to distinguish: if arg is provided without `--`, it's the prompt; args after `--` are still extra agent args

2. Investigate how the prompt gets passed to the agent in the entrypoint/backend:
   - Look at `entrypoint.go`, `backend_host.go`, `backend_container.go`, `backend_sandbox.go` to understand how `BackendOptions.Prompt` and `AgentArgs` are used
   - The `Prompt` field in `BackendOptions` is used for loop mode (non-interactive), but we need interactive mode with a prompt
   - For Claude: passing the prompt as a positional arg to `claude` starts interactive mode with that prompt
   - For OpenCode: need to check if `opencode --prompt` flag exists

3. Add `--prompt` flag or accept positional arg in `sbox run`:
   - Option A: Accept first positional arg as prompt (like `sbox loop`)
   - Option B: Add `--prompt` flag
   - Recommendation: Use a `[prompt]` positional argument to match `sbox loop` UX

4. Pass the prompt to the agent in interactive mode:
   - Need a new mechanism separate from the existing `Prompt` field (which triggers non-interactive/loop mode)
   - Could add `InteractivePrompt string` to `BackendOptions`
   - Or reuse `AgentArgs` since for Claude passing a prompt as positional arg to `claude` works in interactive mode

5. Update `AgentSpec.ExecArgs` or add a new method to build args with a prompt in interactive mode

6. Update the command description and CHANGELOG.md

## Dev Feedback

<empty>

## State Tracker

**Last Updated:** 2026-05-07
**Current Step:** Step 6 — Review Complete
**Status:** Implementation merged to master. `sbox run "<prompt>"` positional argument support added. `InteractivePrompt` field added to `BackendOptions` and `EntrypointConfig`. `AgentSpec.InteractivePromptArgs()` implemented for Claude (positional arg) and OpenCode (`--prompt` flag). Entrypoint correctly applies prompt when launching agent. CHANGELOG updated. Both features (watch + prompt) were merged together in commit `60c59bd`. Ready for user sign-off to mark done.

# Support stdin as prompt in sbox run

mode: feature
state: in_progress
root_git: .worktrees/feature/stdin-prompt-sbox-run
worktree: .worktrees/feature/stdin-prompt-sbox-run
branch: feature/stdin-prompt-sbox-run
target_branch: master

> **Resume protocol:** read **Dev Feedback** and the **State Tracker** below first, then jump to the
> step marked `Current`. Ensure that you are in the correct worktree and branch according to preamble here. Update current with Developer feedback and update the tracker after every meaningful change.
> Do not mutate completed steps; append a new entry instead.

---

## Initial Description

Ensure `sbox run` accepts `stdin` as `<prompt>` value so users can do `cat <file.md> | sbox run` and this would be detected as the prompt if stdin can be read and has content. If stdin has content, it should be used as the prompt value. This should work in addition to the existing interactive prompt or argument-based prompt.

## Dev Feedback

<Empty initially, developer add feedback and open questions answers here>

## Spec & Implementation

<Agent managed>

## State Tracker

**Last Updated:** 2026-05-07
**Current Step:** Step 1 — Begin implementation
**Status:** Starting

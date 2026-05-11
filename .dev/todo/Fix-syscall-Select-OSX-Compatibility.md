# Fix syscall.Select OSX Compatibility

mode: bug
state: review
root_git: .worktrees/fix-syscall-select-osx
worktree: .worktrees/fix-syscall-select-osx
branch: fix/fix-syscall-select-osx
target_branch: master

> **Resume protocol:** read **Dev Feedback** and the **State Tracker** below first, then jump to the
> step marked `Current`. Ensure that you are in the correct worktree and branch according to preamble here. Update current with Developer feedback and update the tracker after every meaningful change.
> Do not mutate completed steps; append a new entry instead.

---

## Initial Description

The `syscall.Select` in `stdinHasData` has different signatures on OSX than on Linux. On Linux it is `n, err := syscall.Select(fd+1, fdSet, nil, nil, timeout)` but on OSX it's `err := syscall.Select(fd+1, fdSet, nil, nil, timeout)`. We need to adjust that to compile also on OSX correctly.

## Dev Feedback

<Empty initially>

## Spec & Implementation

Extracted `stdinHasData` and `fdSetAdd` from `cmd/sbox/run.go` into two
platform-specific files with `//go:build` constraints:

- **`cmd/sbox/stdin_linux.go`** (`//go:build linux`): Uses the Linux
  `syscall.Select` signature `(n int, err error)` and `FdSet.Bits` with
  `int64` elements (bit-index divisor 64).

- **`cmd/sbox/stdin_darwin.go`** (`//go:build darwin`): Uses the Darwin
  `syscall.Select` signature `(err error)` and `FdSet.Bits` with `int32`
  elements (bit-index divisor 32). Because Darwin's Select returns no count,
  readiness is determined by checking whether the fd bit is still set in the
  returned fdSet via `fdIsSet`.

Both `go build ./...` (linux) and `GOOS=darwin go build ./...` succeed.
All existing tests pass.

## State Tracker

**Last Updated:** 2026-05-11
**Current Step:** Step 2 — Implementation complete, ready for review
**Status:** Build and tests passing; committed `514c98d`

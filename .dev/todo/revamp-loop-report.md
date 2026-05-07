# Revamp Loop End Report

mode: feature
state: review
root_git: .worktrees/feature/revamp-loop-report
worktree: .worktrees/feature/revamp-loop-report
branch: feature/revamp-loop-report
target_branch: master

> **Resume protocol:** read **Dev Feedback** and the **State Tracker** below first, then jump to the
> step marked `Current`. Ensure that you are in the correct worktree and branch according to preamble here. Update current with Developer feedback and update the tracker after every meaningful change.
> Do not mutate completed steps; append a new entry instead.

---

## Initial Description

Currently the loop end report looks like this:

```
✓ Done (10 steps, 191507 tokens)
✓ Goal completed (1/2)
⚠ Reached maximum iterations (1)

── Timing Summary ──
Total: 35s
Iteration 1: 35s
```

I would like that it get revamped and improved. First I would like to have a single global section look. Propose me 3 possibilities.

## Dev Feedback

Let's go with proposal B with one slight asks:
- Indent iteration(s)
- If there is a single one, hide completely and only show Total:

## Spec & Implementation

### Context & Current Code

The loop end report is produced in two places:

1. **`opencode/stream.go` — `StreamPrinter.handleStepFinish()`** — prints the per-agent-run summary line once the agent finishes:
   ```
   ✓ Done (10 steps, 191507 tokens)
   ```

2. **`ui.go` — `UI.Confirmed()` / `UI.MaxReached()`** — called from `entrypoint.go:runLoop()` at end of the whole loop run. They print the outcome line then delegate to `printTimingSummary()`:
   ```
   ✓ Goal confirmed complete after 2 iterations   ← Confirmed()
   ── Timing Summary ──
   Total: 35s
   Iteration 1: 17s
   Iteration 2: 18s

   ⚠ Reached maximum iterations (1)              ← MaxReached()
   ── Timing Summary ──
   Total: 35s
   Iteration 1: 35s
   ```

The `ui.go` already has `lipgloss` imported for styled output. The `StyleHeader`, `StyleSuccess`, `StyleWarn`, `StyleDim`, `StyleLabel` styles are available.

---

### Design Proposals

All three proposals share the same goal: **collapse the scattered multi-section output into one visually unified box/section** that appears at the very end of the loop run. Only the visual style differs.

---

#### Proposal A — Bordered Box (lipgloss Border)

Use `lipgloss.Border` to draw a rounded box around the entire end-of-loop report. Inside: outcome on the first line, then a faint divider, then key-value rows for totals and per-iteration timing.

```
╭─ Loop Report ────────────────────────────────╮
│ ✓ Goal confirmed (2 iterations)              │
│ ─────────────────────────────────────────── │
│  Steps       10                              │
│  Tokens      191 507                         │
│  Total time  35s                             │
│  Iteration 1 17s                             │
│  Iteration 2 18s                             │
╰──────────────────────────────────────────────╯
```

Or for a warning outcome:

```
╭─ Loop Report ────────────────────────────────╮
│ ⚠ Max iterations reached (1)                │
│ ─────────────────────────────────────────── │
│  Steps       10                              │
│  Tokens      191 507                         │
│  Total time  35s                             │
│  Iteration 1 35s                             │
╰──────────────────────────────────────────────╯
```

**Implementation notes:**
- Introduce a `LoopReport` struct with all fields (outcome, steps, tokens, total, perIteration).
- `UI.PrintLoopReport(r LoopReport)` renders with `lipgloss.Style.Border(lipgloss.RoundedBorder())`.
- The `StreamPrinter.handleStepFinish()` no longer prints its own "✓ Done" line; instead it accumulates steps/tokens and exposes them so `runLoop()` can pass them into `LoopReport`.
- `Confirmed()` and `MaxReached()` are replaced by `PrintLoopReport()`.

---

#### Proposal B — Header + Indented Table (no border, compact)

A single bold section header line followed by indented label-value rows aligned in two columns. Clean, minimal, no box drawing characters. Similar in spirit to the current "Timing Summary" section but with everything in one cohesive block.

```
── Loop Summary ───────────────────────────────
  Status      ✓ Goal confirmed (2 iterations)
  Steps       10   Tokens  191 507
  Total       35s
  Iteration 1 17s
  Iteration 2 18s
```

Or for a warning:

```
── Loop Summary ───────────────────────────────
  Status      ⚠ Max iterations reached (1)
  Steps       10   Tokens  191 507
  Total       35s
  Iteration 1 35s
```

**Implementation notes:**
- Header uses `StyleHeader` with a longer dash rule `strings.Repeat("─", n)` to fill terminal width (or a fixed width like 48).
- Rows are printed with `fmt.Fprintf` using `%-12s %s` alignment.
- Steps + Tokens printed on same row to save vertical space.
- Same `LoopReport` struct approach as Proposal A.
- Removes the standalone `printTimingSummary()` helper and merges it here.

---

#### Proposal C — Emoji-Free, Structured Status Card (two-column grid)

A tight card-style layout: left column has fixed-width labels in dim style, right column has values. Outcome is highlighted at the top, no box. Vertical space is preserved by fitting iteration timings side-by-side when there are many.

```
  ◆ Loop complete · 2 iterations · 35s total
  ───────────────────────────────────────────
  Outcome   Goal confirmed (2/2 confirmations)
  Steps     10          Tokens    191 507
  ─  ─  ─  ─  ─  ─  ─  ─  ─  ─  ─  ─  ─  ─
  Iter 1    17s         Iter 2    18s
```

Or for a warning:

```
  ◆ Loop stopped · max iterations (1) · 35s total
  ──────────────────────────────────────────────
  Outcome   Max iterations reached
  Steps     10          Tokens    191 507
  ─  ─  ─  ─  ─  ─  ─  ─  ─  ─  ─  ─  ─  ─
  Iter 1    35s
```

**Implementation notes:**
- Summary "tagline" at top built with a special `StyleAccent` (bold cyan or magenta) rendered in one `fmt.Fprintf` call.
- Two-column table rendered with `tabwriter` or manual padding.
- Iterations laid out in pairs per row to reduce vertical footprint.
- Same `LoopReport` struct, same wiring.
- The `◆` marker (or `▶`) provides visual anchor without semantic color dependency.

---

### Shared Implementation Plan (all proposals)

1. **Add `LoopReport` struct** in `ui.go`:
   ```go
   type LoopReport struct {
       Outcome       string        // "confirmed" | "max_reached"
       Iterations    int
       MaxIterations int           // for max_reached
       Completions   int
       Required      int
       Steps         int
       Tokens        int
       TotalDuration time.Duration
       PerIteration  []time.Duration
   }
   ```

2. **Expose steps/tokens from `StreamPrinter`** — add getter or return value so `runLoop()` can collect them per iteration.

3. **Replace `Confirmed()` and `MaxReached()`** with `PrintLoopReport(r LoopReport)` in `ui.go`.

4. **Update `runLoop()`** in `entrypoint.go` to build a `LoopReport` and call `PrintLoopReport()`.

5. **Remove the inline "✓ Done (steps, tokens)"** from `stream.go` `handleStepFinish()` so it isn't double-reported (or keep it as a per-iteration summary distinct from the global end report — to be decided).

---

## State Tracker

**Last Updated:** 2026-05-07
**Current Step:** Step 2 — Implementation complete, ready for review
**Status:** Implementation done. `PrintLoopReport()` added to `ui.go`, `IterationStats()` added to both claude and opencode `StreamPrinter`. `runLoop()` updated to use `PrintLoopReport`. Tests pass.

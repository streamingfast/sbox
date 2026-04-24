package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/sbox"
)

// PruneCommand is the top-level `sbox prune` group command.
var PruneCommand = Group(
	"prune",
	"Remove old and stale sandboxes to reclaim disk space",
	Description(`
		Prune selects sandboxes to remove based on last-used timestamps and
		workspace existence, then optionally deletes them.

		For each known sandbox, prune considers:
		- Stale sandboxes: workspace directory no longer exists on disk.
		  These are always selected for removal.
		- Old sandboxes: workspace still exists but sandbox has not been used
		  recently. The N most recently used sandboxes are kept (--keep).

		By default this command performs a DRY RUN and prints what would be
		deleted without making any changes. Pass --force to actually delete.

		For each selected sandbox the following are removed:
		  1. The Docker sandbox (docker sandbox rm)
		  2. The .sbox/ directory inside the workspace (if workspace exists)
		  3. The project config entry in ~/.config/sbox/projects/

		Use 'sbox prune all' or 'sbox prune sandbox' — both currently behave
		identically (only one backend type is supported). Additional target
		types may be added in future releases.
	`),

	Command(pruneAllE,
		"all",
		"Prune all sandbox types (currently equivalent to 'sandbox')",
		Description(`
			Prunes all sandbox types. Currently this is equivalent to 'sbox prune sandbox'
			since only one backend type is supported, but the separation exists to
			accommodate future backend types.
		`),
		Flags(func(flags *pflag.FlagSet) {
			flags.Int("keep", 5, "Number of most-recently-used sandboxes to keep")
			flags.Bool("force", false, "Actually delete sandboxes (default is dry-run)")
		}),
	),

	Command(pruneSandboxE,
		"sandbox",
		"Prune Docker sandboxes based on last-used timestamps",
		Description(`
			Prunes Docker sandboxes whose workspace no longer exists or that have not
			been used recently. Keeps the N most recently used sandboxes (--keep).
		`),
		Flags(func(flags *pflag.FlagSet) {
			flags.Int("keep", 5, "Number of most-recently-used sandboxes to keep")
			flags.Bool("force", false, "Actually delete sandboxes (default is dry-run)")
		}),
	),
)

// pruneAllE handles `sbox prune all`.
func pruneAllE(cmd *cobra.Command, args []string) error {
	return pruneSandboxes(cmd)
}

// pruneSandboxE handles `sbox prune sandbox`.
func pruneSandboxE(cmd *cobra.Command, args []string) error {
	return pruneSandboxes(cmd)
}

// pruneSandboxes contains the shared prune logic for all target types.
func pruneSandboxes(cmd *cobra.Command) error {
	keep, _ := cmd.Flags().GetInt("keep")
	force, _ := cmd.Flags().GetBool("force")

	candidates, err := sbox.FindPruneCandidates(sbox.PruneOptions{Keep: keep})
	if err != nil {
		return fmt.Errorf("failed to find prune candidates: %w", err)
	}

	if len(candidates) == 0 {
		if force {
			cmd.Println("Nothing to prune.")
		} else {
			cmd.Printf("Nothing to prune (keeping %d most recently used).\n", keep)
		}
		return nil
	}

	// Print header.
	if !force {
		cmd.Printf("sbox prune (dry-run — use --force to actually delete)\n\n")
		cmd.Printf("Would prune %d sandbox(es):\n\n", len(candidates))
	} else {
		cmd.Printf("Pruning %d sandbox(es):\n\n", len(candidates))
	}

	// Compute column widths.
	maxName := len("SANDBOX")
	maxPath := len("WORKSPACE")
	for _, c := range candidates {
		name := cmp(c.SandboxName, "(no sandbox)")
		if len(name) > maxName {
			maxName = len(name)
		}
		path := cmp(c.WorkspacePath, "(unknown)")
		if len(path) > maxPath {
			maxPath = len(path)
		}
	}

	// Header row.
	cmd.Printf("  %-*s  %-*s  %-19s  %s\n",
		maxName, "SANDBOX",
		maxPath, "WORKSPACE",
		"LAST USED",
		"REASON")
	cmd.Printf("  %s  %s  %s  %s\n",
		strings.Repeat("-", maxName),
		strings.Repeat("-", maxPath),
		strings.Repeat("-", 19),
		strings.Repeat("-", 30))

	for _, c := range candidates {
		name := cmp(c.SandboxName, "(no sandbox)")
		path := cmp(c.WorkspacePath, "(unknown)")
		lastUsed := formatLastUsed(c.LastUsed)
		cmd.Printf("  %-*s  %-*s  %-19s  %s\n",
			maxName, name,
			maxPath, path,
			lastUsed,
			c.Reason)
	}

	cmd.Println()

	if !force {
		cmd.Printf("Keeping %d most recently used sandbox(es).\n", keep)
		cmd.Println("Run with --force to delete.")
		return nil
	}

	// Perform deletion.
	pruneErrs := sbox.PruneAll(candidates)
	if len(pruneErrs) == 0 {
		cmd.Printf("\nSuccessfully pruned %d sandbox(es).\n", len(candidates))
	} else {
		cmd.Printf("\nPruned %d sandbox(es) with %d error(s):\n", len(candidates)-len(pruneErrs), len(pruneErrs))
		for _, pe := range pruneErrs {
			cmd.Printf("  ERROR: %s: %v\n", pe.Candidate.SandboxName, pe.Err)
		}
		return fmt.Errorf("prune completed with errors")
	}

	return nil
}

// cmp returns s if non-empty, otherwise fallback.
func cmp(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

// formatLastUsed formats a last-used timestamp for display.
func formatLastUsed(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.UTC().Format("2006-01-02 15:04:05")
}

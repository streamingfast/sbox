package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/sbox"
	"github.com/streamingfast/sbox/stylex"
)

// PruneCommand is the top-level `sbox prune` group command.
var PruneCommand = Group(
	"prune",
	"Remove old and stale sandboxes and containers to reclaim disk space",
	Description(`
		Prune selects sandboxes and Docker containers to remove based on
		last-used timestamps and workspace existence, then optionally deletes them.

		For each known sandbox/container, prune considers:
		- Stale entries: workspace directory no longer exists on disk.
		  These are always selected for removal.
		- Old entries: workspace still exists but sandbox/container has not been
		  used recently. The N most recently used are kept (--keep).

		By default this command performs a DRY RUN and prints what would be
		deleted without making any changes. Pass --force to actually delete.

		For each selected sandbox the following are removed:
		  1. The Docker sandbox (docker sandbox rm)
		  2. The .sbox/ directory inside the workspace (if workspace exists)
		  3. The project config entry in ~/.config/sbox/projects/

		For each selected container the following are removed:
		  1. The Docker container (docker stop + docker rm)
		  2. The associated named sbox- volume (docker volume rm)

		Use 'sbox prune all' to prune both sandboxes and containers.
		Use 'sbox prune sandbox' or 'sbox prune container' for individual types.
	`),

	Command(pruneAllE,
		"all",
		"Prune all types: Docker MicroVM sandboxes and Docker containers",
		Description(`
			Prunes both Docker MicroVM sandboxes and Docker containers managed by sbox.
			Runs sandbox pruning first, then container pruning, showing separate tables
			for each type.
		`),
		Flags(func(flags *pflag.FlagSet) {
			flags.Int("keep", 5, "Number of most-recently-used entries to keep per type")
			flags.Bool("force", false, "Actually delete entries (default is dry-run)")
			flags.StringArrayP("protect", "p", nil, "Name(s) to never delete; comma-separated or repeated flag")
		}),
	),

	Command(pruneSandboxE,
		"sandbox",
		"Prune Docker MicroVM sandboxes based on last-used timestamps",
		Description(`
			Prunes Docker MicroVM sandboxes whose workspace no longer exists or that
			have not been used recently. Keeps the N most recently used sandboxes (--keep).

			When sandbox names are given as arguments, only those specific sandboxes
			are targeted. Unknown names are listed separately in a second table.

			Use --protect (-p) to exempt specific sandboxes from deletion even when
			they would otherwise be selected as candidates.
		`),
		Flags(func(flags *pflag.FlagSet) {
			flags.Int("keep", 5, "Number of most-recently-used sandboxes to keep")
			flags.Bool("force", false, "Actually delete sandboxes (default is dry-run)")
			flags.StringArrayP("protect", "p", nil, "Sandbox name(s) to never delete; comma-separated or repeated flag")
		}),
	),

	Command(pruneContainerE,
		"container",
		"Prune Docker containers (sbox- prefix) based on last-used timestamps",
		Description(`
			Prunes Docker containers whose name starts with "sbox-" and whose workspace
			no longer exists or that have not been used recently. Keeps the N most
			recently used containers (--keep).

			On prune: stops the container, removes it, then removes its associated
			named sbox- volume.

			Use --protect (-p) to exempt specific containers from deletion even when
			they would otherwise be selected as candidates.
		`),
		Flags(func(flags *pflag.FlagSet) {
			flags.Int("keep", 5, "Number of most-recently-used containers to keep")
			flags.Bool("force", false, "Actually delete containers (default is dry-run)")
			flags.StringArrayP("protect", "p", nil, "Container name(s) to never delete; comma-separated or repeated flag")
		}),
	),
)

// pruneAllE handles `sbox prune all`: runs sandbox pruning then container pruning.
func pruneAllE(cmd *cobra.Command, args []string) error {
	w := cmd.OutOrStdout()

	force, _ := cmd.Flags().GetBool("force")
	if !force {
		fmt.Fprintln(w, stylex.Note("sbox prune all (dry-run — use --force to actually delete)"))
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, stylex.Header("=== Sandboxes ==="))
	fmt.Fprintln(w)
	sbErr := pruneSandboxes(cmd, nil, true)

	fmt.Fprintln(w)
	fmt.Fprintln(w, stylex.Header("=== Containers ==="))
	fmt.Fprintln(w)
	ctErr := pruneContainers(cmd, nil, true)

	if sbErr != nil && ctErr != nil {
		return fmt.Errorf("sandbox pruning: %w; container pruning: %v", sbErr, ctErr)
	}
	if sbErr != nil {
		return sbErr
	}
	return ctErr
}

// pruneSandboxE handles `sbox prune sandbox`.
func pruneSandboxE(cmd *cobra.Command, args []string) error {
	return pruneSandboxes(cmd, args, false)
}

// pruneContainerE handles `sbox prune container`.
func pruneContainerE(cmd *cobra.Command, args []string) error {
	return pruneContainers(cmd, args, false)
}

// pruneSandboxes contains the shared prune logic for all target types.
// suppressDryRunHeader suppresses the "dry-run" note when called from pruneAllE
// which prints its own combined header.
func pruneSandboxes(cmd *cobra.Command, args []string, suppressDryRunHeader bool) error {
	keep, _ := cmd.Flags().GetInt("keep")
	force, _ := cmd.Flags().GetBool("force")
	protectRaw, _ := cmd.Flags().GetStringArray("protect")
	protect := parseProtectSet(protectRaw)
	w := cmd.OutOrStdout()

	if len(args) > 0 {
		return pruneSandboxesByName(w, args, force, protect)
	}

	candidates, kept, err := sbox.FindPruneCandidates(sbox.PruneOptions{Keep: keep})
	if err != nil {
		return fmt.Errorf("failed to find prune candidates: %w", err)
	}

	// Partition candidates: those shielded by --protect move to a protected slice.
	var protected []sbox.PruneCandidate
	candidates = filterProtected(candidates, protect, &protected)

	if len(candidates) == 0 && len(kept) == 0 && len(protected) == 0 {
		if force {
			fmt.Fprintln(w, "Nothing to prune.")
		} else {
			fmt.Fprintf(w, "Nothing to prune (keeping %d most recently used).\n", keep)
		}
		return nil
	}

	if !force && !suppressDryRunHeader {
		fmt.Fprintln(w, stylex.Note("sbox prune sandbox (dry-run — use --force to actually delete)"))
		fmt.Fprintln(w)
	}

	// Split remaining candidates into stale (workspace missing) and old (too old).
	var stale, old []sbox.PruneCandidate
	for _, c := range candidates {
		if c.WorkspaceMissing {
			stale = append(stale, c)
		} else {
			old = append(old, c)
		}
	}

	pruneVerb := "Pruning"
	if force {
		pruneVerb = "Pruned"
	}

	if len(stale) > 0 {
		printPruneSection(w, fmt.Sprintf("%s %d sandbox(es) | Workspace Gone", pruneVerb, len(stale)), stale, force)
		fmt.Fprintln(w)
	}

	if len(old) > 0 {
		printPruneSection(w, fmt.Sprintf("%s %d sandbox(es) | Too old", pruneVerb, len(old)), old, force)
		fmt.Fprintln(w)
	}

	if len(protected) > 0 {
		printCandidateSection(w, fmt.Sprintf("Protected %d sandbox(es)", len(protected)), protected)
		fmt.Fprintln(w)
	}

	if len(kept) > 0 {
		printCandidateSection(w, fmt.Sprintf("Keeping %d sandbox(es)", len(kept)), kept)
		fmt.Fprintln(w)
	}

	if !force {
		fmt.Fprintln(w, stylex.Note("Run with --force to delete."))
		fmt.Fprintln(w, stylex.Note("Use --protect/-p to exempt specific sandboxes."))
		return nil
	}

	// Perform deletion with per-item progress.
	fmt.Fprintln(w)
	pruneErrs := pruneWithProgress(w, candidates)
	if len(pruneErrs) == 0 {
		fmt.Fprintf(w, "\nSuccessfully pruned %d sandbox(es).\n", len(candidates))
	} else {
		fmt.Fprintf(w, "\nPruned %d sandbox(es) with %d error(s).\n", len(candidates)-len(pruneErrs), len(pruneErrs))
		return fmt.Errorf("prune completed with errors")
	}

	return nil
}

// pruneSandboxesByName handles targeted deletion when the user specifies sandbox names.
func pruneSandboxesByName(w io.Writer, names []string, force bool, protect map[string]bool) error {
	// Use a very large keep value so all active sandboxes land in "kept",
	// giving us a complete pool to match names against.
	candidates, kept, err := sbox.FindPruneCandidates(sbox.PruneOptions{Keep: 1 << 30})
	if err != nil {
		return fmt.Errorf("failed to find prune candidates: %w", err)
	}

	// Build lookup map: sandbox name -> PruneCandidate.
	allSandboxes := make(map[string]sbox.PruneCandidate, len(candidates)+len(kept))
	for _, c := range append(candidates, kept...) {
		if c.SandboxName != "" {
			allSandboxes[c.SandboxName] = c
		}
	}

	var matched, protected []sbox.PruneCandidate
	var unmatched []string
	for _, name := range names {
		c, ok := allSandboxes[name]
		if !ok {
			unmatched = append(unmatched, name)
			continue
		}
		if protect[name] {
			protected = append(protected, c)
		} else {
			matched = append(matched, c)
		}
	}

	if len(matched) == 0 && len(unmatched) == 0 && len(protected) == 0 {
		fmt.Fprintln(w, "No sandboxes specified.")
		return nil
	}

	if !force {
		fmt.Fprintln(w, stylex.Note("sbox prune sandbox (dry-run — use --force to actually delete)"))
		fmt.Fprintln(w)
	}

	pruneVerb := "Pruning"
	if force {
		pruneVerb = "Pruned"
	}

	if len(matched) > 0 {
		printPruneSection(w, fmt.Sprintf("%s %d sandbox(es) | Named", pruneVerb, len(matched)), matched, force)
		fmt.Fprintln(w)
	}

	if len(protected) > 0 {
		printCandidateSection(w, fmt.Sprintf("Protected %d sandbox(es)", len(protected)), protected)
		fmt.Fprintln(w)
	}

	if len(unmatched) > 0 {
		printUnmatchedSection(w, unmatched)
		fmt.Fprintln(w)
	}

	if !force {
		fmt.Fprintln(w, stylex.Note("Run with --force to delete."))
		fmt.Fprintln(w, stylex.Note("Use --protect/-p to exempt specific sandboxes."))
		return nil
	}

	fmt.Fprintln(w)
	pruneErrs := pruneWithProgress(w, matched)
	if len(pruneErrs) == 0 {
		fmt.Fprintf(w, "\nSuccessfully pruned %d sandbox(es).\n", len(matched))
	} else {
		fmt.Fprintf(w, "\nPruned %d sandbox(es) with %d error(s).\n", len(matched)-len(pruneErrs), len(pruneErrs))
		return fmt.Errorf("prune completed with errors")
	}

	return nil
}

// parseProtectSet converts raw --protect flag values into a set of sandbox names.
// Each value is split on "," so both --protect=a,b and --protect=a --protect=b work.
func parseProtectSet(values []string) map[string]bool {
	protect := make(map[string]bool)
	for _, v := range values {
		for part := range strings.SplitSeq(v, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				protect[part] = true
			}
		}
	}
	return protect
}

// filterProtected partitions candidates: those whose SandboxName is in protect
// are removed from the returned slice and appended to *out.
func filterProtected(candidates []sbox.PruneCandidate, protect map[string]bool, out *[]sbox.PruneCandidate) []sbox.PruneCandidate {
	if len(protect) == 0 {
		return candidates
	}
	kept := candidates[:0]
	for _, c := range candidates {
		if protect[c.SandboxName] {
			*out = append(*out, c)
		} else {
			kept = append(kept, c)
		}
	}
	return kept
}

// printPruneSection renders a titled table section for prune candidates.
func printPruneSection(w io.Writer, title string, candidates []sbox.PruneCandidate, performed bool) {
	printCandidateSection(w, title, candidates)
}

// printCandidateSection renders a titled table of PruneCandidates.
func printCandidateSection(w io.Writer, title string, candidates []sbox.PruneCandidate) {
	fmt.Fprintln(w, stylex.Header(title))
	fmt.Fprintln(w, stylex.Dim(strings.Repeat("─", 40)))

	var rows [][]string
	for _, c := range candidates {
		rows = append(rows, []string{
			cmp(c.SandboxName, "(no sandbox)"),
			cmp(c.WorkspacePath, "(unknown)"),
			formatLastUsed(c.LastUsed),
		})
	}

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(stylex.DimStyle).
		Headers(
			stylex.Header("Sandbox"),
			stylex.Header("Workspace"),
			stylex.Header("Last Used"),
		).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return stylex.HeaderStyle
			}
			return lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)
		})

	fmt.Fprintln(w, t.String())
}

// printUnmatchedSection renders a table of sandbox names that were not found.
func printUnmatchedSection(w io.Writer, names []string) {
	fmt.Fprintln(w, stylex.Header(fmt.Sprintf("Unknown %d sandbox(es)", len(names))))
	fmt.Fprintln(w, stylex.Dim(strings.Repeat("─", 40)))

	var rows [][]string
	for _, name := range names {
		rows = append(rows, []string{name, "(not found)", "unknown"})
	}

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(stylex.DimStyle).
		Headers(
			stylex.Header("Sandbox"),
			stylex.Header("Workspace"),
			stylex.Header("Last Used"),
		).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return stylex.HeaderStyle
			}
			return lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)
		})

	fmt.Fprintln(w, t.String())
}

// pruneWithProgress deletes each candidate and prints a status line per item.
func pruneWithProgress(w io.Writer, candidates []sbox.PruneCandidate) []sbox.PruneError {
	var errs []sbox.PruneError
	for _, c := range candidates {
		label := cmp(c.SandboxName, c.WorkspacePath)
		fmt.Fprintf(w, "  Removing %s ...", label)
		if err := sbox.PruneOne(c); err != nil {
			fmt.Fprintf(w, " ERROR: %v\n", err)
			errs = append(errs, sbox.PruneError{Candidate: c, Err: err})
		} else {
			fmt.Fprintln(w, " done")
		}
	}
	return errs
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

// pruneContainers contains the shared prune logic for Docker containers.
// suppressDryRunHeader suppresses the "dry-run" note when called from pruneAllE.
func pruneContainers(cmd *cobra.Command, args []string, suppressDryRunHeader bool) error {
	keep, _ := cmd.Flags().GetInt("keep")
	force, _ := cmd.Flags().GetBool("force")
	protectRaw, _ := cmd.Flags().GetStringArray("protect")
	protect := parseProtectSet(protectRaw)
	w := cmd.OutOrStdout()

	candidates, kept, err := sbox.FindContainerPruneCandidates(sbox.PruneOptions{Keep: keep})
	if err != nil {
		return fmt.Errorf("failed to find container prune candidates: %w", err)
	}

	// Partition candidates: those shielded by --protect move to a protected slice.
	var protected []sbox.ContainerPruneCandidate
	candidates = filterContainerProtected(candidates, protect, &protected)

	if len(candidates) == 0 && len(kept) == 0 && len(protected) == 0 {
		if force {
			fmt.Fprintln(w, "Nothing to prune.")
		} else {
			fmt.Fprintf(w, "Nothing to prune (keeping %d most recently used).\n", keep)
		}
		return nil
	}

	if !force && !suppressDryRunHeader {
		fmt.Fprintln(w, stylex.Note("sbox prune container (dry-run — use --force to actually delete)"))
		fmt.Fprintln(w)
	}

	// Split remaining candidates into stale (workspace missing) and old (too old).
	var stale, old []sbox.ContainerPruneCandidate
	for _, c := range candidates {
		if c.WorkspaceMissing {
			stale = append(stale, c)
		} else {
			old = append(old, c)
		}
	}

	pruneVerb := "Pruning"
	if force {
		pruneVerb = "Pruned"
	}

	if len(stale) > 0 {
		printContainerCandidateSection(w, fmt.Sprintf("%s %d container(s) | Workspace Gone", pruneVerb, len(stale)), stale)
		fmt.Fprintln(w)
	}

	if len(old) > 0 {
		printContainerCandidateSection(w, fmt.Sprintf("%s %d container(s) | Too old", pruneVerb, len(old)), old)
		fmt.Fprintln(w)
	}

	if len(protected) > 0 {
		printContainerCandidateSection(w, fmt.Sprintf("Protected %d container(s)", len(protected)), protected)
		fmt.Fprintln(w)
	}

	if len(kept) > 0 {
		printContainerCandidateSection(w, fmt.Sprintf("Keeping %d container(s)", len(kept)), kept)
		fmt.Fprintln(w)
	}

	if !force {
		fmt.Fprintln(w, stylex.Note("Run with --force to delete."))
		fmt.Fprintln(w, stylex.Note("Use --protect/-p to exempt specific containers."))
		return nil
	}

	// Perform deletion with per-item progress.
	fmt.Fprintln(w)
	pruneErrs := pruneContainersWithProgress(w, candidates)
	if len(pruneErrs) == 0 {
		fmt.Fprintf(w, "\nSuccessfully pruned %d container(s).\n", len(candidates))
	} else {
		fmt.Fprintf(w, "\nPruned %d container(s) with %d error(s).\n", len(candidates)-len(pruneErrs), len(pruneErrs))
		return fmt.Errorf("container prune completed with errors")
	}

	return nil
}

// filterContainerProtected partitions container candidates: those whose ContainerName
// is in protect are removed from the returned slice and appended to *out.
func filterContainerProtected(candidates []sbox.ContainerPruneCandidate, protect map[string]bool, out *[]sbox.ContainerPruneCandidate) []sbox.ContainerPruneCandidate {
	if len(protect) == 0 {
		return candidates
	}
	kept := candidates[:0]
	for _, c := range candidates {
		if protect[c.ContainerName] {
			*out = append(*out, c)
		} else {
			kept = append(kept, c)
		}
	}
	return kept
}

// printContainerCandidateSection renders a titled table of ContainerPruneCandidates.
func printContainerCandidateSection(w io.Writer, title string, candidates []sbox.ContainerPruneCandidate) {
	fmt.Fprintln(w, stylex.Header(title))
	fmt.Fprintln(w, stylex.Dim(strings.Repeat("─", 40)))

	var rows [][]string
	for _, c := range candidates {
		rows = append(rows, []string{
			cmp(c.ContainerName, "(no container)"),
			cmp(c.WorkspacePath, "(unknown)"),
			formatLastUsed(c.LastUsed),
		})
	}

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(stylex.DimStyle).
		Headers(
			stylex.Header("Container"),
			stylex.Header("Workspace"),
			stylex.Header("Last Used"),
		).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return stylex.HeaderStyle
			}
			return lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)
		})

	fmt.Fprintln(w, t.String())
}

// pruneContainersWithProgress deletes each container candidate and prints a status line per item.
func pruneContainersWithProgress(w io.Writer, candidates []sbox.ContainerPruneCandidate) []error {
	var errs []error
	for _, c := range candidates {
		label := cmp(c.ContainerName, c.WorkspacePath)
		fmt.Fprintf(w, "  Removing %s ...", label)
		if err := sbox.PruneOneContainer(c); err != nil {
			fmt.Fprintf(w, " ERROR: %v\n", err)
			errs = append(errs, err)
		} else {
			fmt.Fprintln(w, " done")
		}
	}
	return errs
}

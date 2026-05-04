package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/sbox"
	"github.com/streamingfast/sbox/stylex"
	"go.uber.org/zap"
)

// StopCommand is the top-level `sbox stop` command. It behaves as a leaf command
// for the current-project stop case (sbox stop [--rm] [--all]) AND as a parent
// for the global stop subcommands (sbox stop all / sandbox / container).
var StopCommand = CommandOptionFunc(func(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the running sandbox or container for this project",
		Long: strings.TrimSpace(`
Stops the sandbox, container, or host agent process for the current project.
If nothing is running, this command does nothing.

The backend type is determined from the project's configuration
(sbox.yaml, project config, or global default).

For the host backend, the agent PID is read from .sbox/host.pid. SIGTERM
is sent first; if the process does not exit within 5 seconds, SIGKILL is
sent. The PID file is removed after the process stops.

With --rm, also removes the Docker sandbox/container (or cleans up .sbox/)
after stopping. Project configuration (profiles, envs, etc.) is preserved.

With --rm --all, removes the sandbox/container AND all project configuration
data (profiles, envs, cached files). For container backend, this also
removes the persistence volume. Asks for confirmation before proceeding.

Use 'sbox stop all' to stop all least-recently-used sandboxes and containers.
Use 'sbox stop sandbox', 'sbox stop sbx', or 'sbox stop container' for individual types.
`),
		RunE: stopE,
	}

	cmd.Flags().Bool("rm", false, "Also remove the Docker sandbox/container after stopping")
	cmd.Flags().Bool("all", false, "Also remove all project configuration and volumes (requires --rm)")
	cmd.Flags().StringP("workspace", "w", "", "Workspace directory (default: current directory)")

	// Global stop subcommands.
	cmd.AddCommand(newStopAllCmd())
	cmd.AddCommand(newStopSandboxCmd())
	cmd.AddCommand(newStopContainerCmd())
	cmd.AddCommand(newStopSbxCmd())

	parent.AddCommand(cmd)
})

// stopE stops the running sandbox/container for the current project
func stopE(cmd *cobra.Command, args []string) error {
	ctx, err := LoadWorkspaceContext(cmd)
	if err != nil {
		return err
	}

	zlog.Debug("resolved backend type", zap.String("backend", string(ctx.BackendType)))

	removeSandbox, _ := cmd.Flags().GetBool("rm")
	removeAll, _ := cmd.Flags().GetBool("all")

	// --all requires --rm
	if removeAll && !removeSandbox {
		return fmt.Errorf("--all requires --rm (use 'sbox stop --rm --all')")
	}

	// --all asks for confirmation
	if removeAll {
		extraInfo := ""
		if ctx.BackendType == sbox.BackendContainer {
			extraInfo = " and persistence volume"
		}
		answeredYes, _ := AskConfirmation("This will remove the Docker %s%s AND all project configuration for %s. Continue?", ctx.Backend.Name(), extraInfo, ctx.WorkspaceDir)
		if !answeredYes {
			cmd.Println("Aborted.")
			return nil
		}
	}

	// Save agent cache before stopping (for persistence across recreations)
	// Skip if --rm is set (resources are being removed, no point caching)
	if !removeSandbox {
		if err := ctx.Backend.SaveCache(ctx.WorkspaceDir, ctx.AgentType); err != nil {
			zlog.Warn("failed to save cache", zap.Error(err))
			// Non-fatal - continue with stop
		}
	}

	// Stop the sandbox/container (and remove if --rm is set)
	info, err := ctx.Backend.Stop(ctx.WorkspaceDir, removeSandbox)
	if err != nil {
		return fmt.Errorf("failed to stop %s: %w", ctx.Backend.Name(), err)
	}

	if info != nil {
		// For sandbox backend, ID and Name are the same, so don't show redundant ID.
		// For container backend, ID is a full container ID so trim to 12 chars.
		if info.ID != info.Name {
			displayID := info.ID
			if len(displayID) > 12 {
				displayID = displayID[:12]
			}
			cmd.Printf("%s stopped: %s (%s)\n", ctx.BackendType.Capitalize(), info.Name, displayID)
		} else {
			cmd.Printf("%s stopped: %s\n", ctx.BackendType.Capitalize(), info.Name)
		}
		if removeSandbox {
			cmd.Printf("%s removed: %s\n", ctx.BackendType.Capitalize(), info.Name)
		}
	} else {
		cmd.Printf("No %s was running for this project\n", ctx.Backend.Name())
	}

	// When --rm is set, clean up associated resources
	if removeSandbox {
		if err := ctx.Backend.Cleanup(ctx.WorkspaceDir); err != nil {
			zlog.Warn("cleanup failed", zap.Error(err))
			// Non-fatal
		} else {
			cmd.Println("Workspace resources cleaned up")
		}
	}

	// Remove project config data only if --all was specified
	if removeAll {
		if err := sbox.RemoveProjectData(ctx.WorkspaceDir); err != nil {
			return fmt.Errorf("failed to remove project data: %w", err)
		}
		cmd.Println("Project configuration removed")
	}

	return nil
}

// newStopAllCmd builds the `sbox stop all` subcommand.
func newStopAllCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "all",
		Short: "Stop all least-recently-used sandboxes and containers",
		Long: strings.TrimSpace(`
Stop all running Docker MicroVM sandboxes (sandbox and sbx backends) and Docker
containers managed by sbox, keeping the N most recently used running (--keep).

By default this command performs a DRY RUN and prints what would be stopped
without making any changes. Pass --force to actually stop.

Sandboxes and containers are stopped but NOT removed. Use 'sbox prune' to also
delete them.
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			force, _ := cmd.Flags().GetBool("force")
			if !force {
				fmt.Fprintln(w, stylex.Note("sbox stop all (dry-run — use --force to actually stop)"))
				fmt.Fprintln(w)
			}

			fmt.Fprintln(w, stylex.Header("=== Sandboxes ==="))
			fmt.Fprintln(w)
			sbErr := stopSandboxes(cmd, true)

			fmt.Fprintln(w)
			fmt.Fprintln(w, stylex.Header("=== Sbx Sandboxes ==="))
			fmt.Fprintln(w)
			sbxErr := stopSbxSandboxes(cmd, true)

			fmt.Fprintln(w)
			fmt.Fprintln(w, stylex.Header("=== Containers ==="))
			fmt.Fprintln(w)
			ctErr := stopContainers(cmd, true)

			for _, e := range []error{sbErr, sbxErr, ctErr} {
				if e != nil {
					return e
				}
			}
			return nil
		},
	}
	addStopFlags(cmd.Flags())
	return cmd
}

// newStopSandboxCmd builds the `sbox stop sandbox` subcommand.
func newStopSandboxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Stop least-recently-used Docker MicroVM sandboxes",
		Long: strings.TrimSpace(`
Stop running Docker MicroVM sandboxes managed by sbox, keeping the N most
recently used running (--keep).

By default this command performs a DRY RUN and prints what would be stopped
without making any changes. Pass --force to actually stop.
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			return stopSandboxes(cmd, false)
		},
	}
	addStopFlags(cmd.Flags())
	return cmd
}

// newStopContainerCmd builds the `sbox stop container` subcommand.
func newStopContainerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "container",
		Short: "Stop least-recently-used Docker containers (sbox- prefix)",
		Long: strings.TrimSpace(`
Stop running Docker containers whose name starts with "sbox-", keeping the N
most recently used running (--keep).

By default this command performs a DRY RUN and prints what would be stopped
without making any changes. Pass --force to actually stop.
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			return stopContainers(cmd, false)
		},
	}
	addStopFlags(cmd.Flags())
	return cmd
}

// addStopFlags adds common flags to a global stop subcommand.
func addStopFlags(flags *pflag.FlagSet) {
	flags.Int("keep", 3, "Number of most-recently-used running entries to keep running")
	flags.Bool("force", false, "Actually stop entries (default is dry-run)")
}

// stopSandboxes implements the shared stop logic for Docker MicroVM sandboxes.
// suppressDryRunHeader suppresses the per-command dry-run note when called from stopAllCmd.
func stopSandboxes(cmd *cobra.Command, suppressDryRunHeader bool) error {
	keep, _ := cmd.Flags().GetInt("keep")
	force, _ := cmd.Flags().GetBool("force")
	w := cmd.OutOrStdout()

	candidates, kept, err := sbox.FindSandboxStopCandidates(sbox.StopOptions{Keep: keep})
	if err != nil {
		return fmt.Errorf("failed to find sandbox stop candidates: %w", err)
	}

	if len(candidates) == 0 && len(kept) == 0 {
		if force {
			fmt.Fprintln(w, "No running sandboxes to stop.")
		} else {
			fmt.Fprintf(w, "No running sandboxes to stop (keeping %d most recently used).\n", keep)
		}
		return nil
	}

	if !force && !suppressDryRunHeader {
		fmt.Fprintln(w, stylex.Note("sbox stop sandbox (dry-run — use --force to actually stop)"))
		fmt.Fprintln(w)
	}

	stopVerb := "Stopping"
	if force {
		stopVerb = "Stopped"
	}

	if len(candidates) > 0 {
		printSandboxStopSection(w, fmt.Sprintf("%s %d sandbox(es) | Least recently used", stopVerb, len(candidates)), candidates)
		fmt.Fprintln(w)
	}

	if len(kept) > 0 {
		printSandboxStopSection(w, fmt.Sprintf("Keeping %d sandbox(es) running", len(kept)), kept)
		fmt.Fprintln(w)
	}

	if !force {
		fmt.Fprintln(w, stylex.Note("Run with --force to stop."))
		return nil
	}

	// Perform stop with per-item progress.
	fmt.Fprintln(w)
	var stopErrs []error
	for _, c := range candidates {
		label := cmp(c.SandboxName, c.WorkspacePath)
		fmt.Fprintf(w, "  Stopping %s ...", label)
		if err := sbox.StopOneSandboxCandidate(c); err != nil {
			fmt.Fprintf(w, " ERROR: %v\n", err)
			stopErrs = append(stopErrs, err)
		} else {
			fmt.Fprintln(w, " done")
		}
	}

	if len(stopErrs) == 0 {
		fmt.Fprintf(w, "\nSuccessfully stopped %d sandbox(es).\n", len(candidates))
	} else {
		fmt.Fprintf(w, "\nStopped %d sandbox(es) with %d error(s).\n", len(candidates)-len(stopErrs), len(stopErrs))
		return fmt.Errorf("stop completed with errors")
	}

	return nil
}

// stopContainers implements the shared stop logic for Docker containers.
// suppressDryRunHeader suppresses the per-command dry-run note when called from stopAllCmd.
func stopContainers(cmd *cobra.Command, suppressDryRunHeader bool) error {
	keep, _ := cmd.Flags().GetInt("keep")
	force, _ := cmd.Flags().GetBool("force")
	w := cmd.OutOrStdout()

	candidates, kept, err := sbox.FindContainerStopCandidates(sbox.StopOptions{Keep: keep})
	if err != nil {
		return fmt.Errorf("failed to find container stop candidates: %w", err)
	}

	if len(candidates) == 0 && len(kept) == 0 {
		if force {
			fmt.Fprintln(w, "No running containers to stop.")
		} else {
			fmt.Fprintf(w, "No running containers to stop (keeping %d most recently used).\n", keep)
		}
		return nil
	}

	if !force && !suppressDryRunHeader {
		fmt.Fprintln(w, stylex.Note("sbox stop container (dry-run — use --force to actually stop)"))
		fmt.Fprintln(w)
	}

	stopVerb := "Stopping"
	if force {
		stopVerb = "Stopped"
	}

	if len(candidates) > 0 {
		printContainerStopSection(w, fmt.Sprintf("%s %d container(s) | Least recently used", stopVerb, len(candidates)), candidates)
		fmt.Fprintln(w)
	}

	if len(kept) > 0 {
		printContainerStopSection(w, fmt.Sprintf("Keeping %d container(s) running", len(kept)), kept)
		fmt.Fprintln(w)
	}

	if !force {
		fmt.Fprintln(w, stylex.Note("Run with --force to stop."))
		return nil
	}

	// Perform stop with per-item progress.
	fmt.Fprintln(w)
	var stopErrs []error
	for _, c := range candidates {
		label := cmp(c.ContainerName, c.WorkspacePath)
		fmt.Fprintf(w, "  Stopping %s ...", label)
		if err := sbox.StopOneContainerCandidate(c); err != nil {
			fmt.Fprintf(w, " ERROR: %v\n", err)
			stopErrs = append(stopErrs, err)
		} else {
			fmt.Fprintln(w, " done")
		}
	}

	if len(stopErrs) == 0 {
		fmt.Fprintf(w, "\nSuccessfully stopped %d container(s).\n", len(candidates))
	} else {
		fmt.Fprintf(w, "\nStopped %d container(s) with %d error(s).\n", len(candidates)-len(stopErrs), len(stopErrs))
		return fmt.Errorf("container stop completed with errors")
	}

	return nil
}

// printSandboxStopSection renders a titled table of PruneCandidates for the stop command.
func printSandboxStopSection(w io.Writer, title string, candidates []sbox.PruneCandidate) {
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

// newStopSbxCmd builds the `sbox stop sbx` subcommand.
func newStopSbxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sbx",
		Short: "Stop least-recently-used sbx MicroVM sandboxes",
		Long: strings.TrimSpace(`
Stop running sbx MicroVM sandboxes managed by sbox, keeping the N most
recently used running (--keep).

By default this command performs a DRY RUN and prints what would be stopped
without making any changes. Pass --force to actually stop.
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			return stopSbxSandboxes(cmd, false)
		},
	}
	addStopFlags(cmd.Flags())
	return cmd
}

// stopSbxSandboxes implements the shared stop logic for sbx MicroVM sandboxes.
// suppressDryRunHeader suppresses the per-command dry-run note when called from stopAllCmd.
func stopSbxSandboxes(cmd *cobra.Command, suppressDryRunHeader bool) error {
	keep, _ := cmd.Flags().GetInt("keep")
	force, _ := cmd.Flags().GetBool("force")
	w := cmd.OutOrStdout()

	candidates, kept, err := sbox.FindSbxStopCandidates(sbox.StopOptions{Keep: keep})
	if err != nil {
		return fmt.Errorf("failed to find sbx stop candidates: %w", err)
	}

	if len(candidates) == 0 && len(kept) == 0 {
		if force {
			fmt.Fprintln(w, "No running sbx sandboxes to stop.")
		} else {
			fmt.Fprintf(w, "No running sbx sandboxes to stop (keeping %d most recently used).\n", keep)
		}
		return nil
	}

	if !force && !suppressDryRunHeader {
		fmt.Fprintln(w, stylex.Note("sbox stop sbx (dry-run — use --force to actually stop)"))
		fmt.Fprintln(w)
	}

	stopVerb := "Stopping"
	if force {
		stopVerb = "Stopped"
	}

	if len(candidates) > 0 {
		printSandboxStopSection(w, fmt.Sprintf("%s %d sbx sandbox(es) | Least recently used", stopVerb, len(candidates)), candidates)
		fmt.Fprintln(w)
	}

	if len(kept) > 0 {
		printSandboxStopSection(w, fmt.Sprintf("Keeping %d sbx sandbox(es) running", len(kept)), kept)
		fmt.Fprintln(w)
	}

	if !force {
		fmt.Fprintln(w, stylex.Note("Run with --force to stop."))
		return nil
	}

	fmt.Fprintln(w)
	var stopErrs []error
	for _, c := range candidates {
		label := cmp(c.SandboxName, c.WorkspacePath)
		fmt.Fprintf(w, "  Stopping %s ...", label)
		if err := sbox.StopOneSbxCandidate(c); err != nil {
			fmt.Fprintf(w, " ERROR: %v\n", err)
			stopErrs = append(stopErrs, err)
		} else {
			fmt.Fprintln(w, " done")
		}
	}

	if len(stopErrs) == 0 {
		fmt.Fprintf(w, "\nSuccessfully stopped %d sbx sandbox(es).\n", len(candidates))
	} else {
		fmt.Fprintf(w, "\nStopped %d sbx sandbox(es) with %d error(s).\n", len(candidates)-len(stopErrs), len(stopErrs))
		return fmt.Errorf("sbx stop completed with errors")
	}

	return nil
}

// printContainerStopSection renders a titled table of ContainerPruneCandidates for stop.
func printContainerStopSection(w io.Writer, title string, candidates []sbox.ContainerPruneCandidate) {
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

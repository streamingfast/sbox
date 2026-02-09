package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/sbox"
	"go.uber.org/zap"
)

var StopCommand = Command(stopE,
	"stop",
	"Stop the running sandbox or container for this project",
	Description(`
		Stops the Docker sandbox or container for the current project.
		If no sandbox/container is running, this command does nothing.

		The backend type is determined from the project's configuration
		(sbox.yaml, project config, or global default).

		With --rm, also removes the Docker sandbox/container after stopping.
		Project configuration (profiles, envs, etc.) is preserved.

		With --rm --all, removes the Docker sandbox/container AND all project
		configuration data (profiles, envs, cached files). For container backend,
		this also removes the persistence volume. Asks for confirmation before
		proceeding.
	`),
	Flags(func(flags *pflag.FlagSet) {
		flags.Bool("rm", false, "Also remove the Docker sandbox/container after stopping")
		flags.Bool("all", false, "Also remove all project configuration and volumes (requires --rm)")
		flags.StringP("workspace", "w", "", "Workspace directory (default: current directory)")
	}),
)

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

	// Save .claude cache before stopping (for persistence across recreations)
	// Skip if --rm is set (resources are being removed, no point caching)
	if !removeSandbox {
		if err := ctx.Backend.SaveCache(ctx.WorkspaceDir); err != nil {
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
		cmd.Printf("%s stopped: %s (%s)\n", ctx.BackendType.Capitalize(), info.Name, info.ID[:12])
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

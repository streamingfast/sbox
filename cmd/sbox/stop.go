package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/sbox"
)

var StopCommand = Command(stopE,
	"stop",
	"Stop the running sandbox for this project",
	Description(`
		Stops the Docker sandbox container for the current project.
		If no sandbox is running, this command does nothing.

		With --rm, also removes the Docker sandbox after stopping.
		Project configuration (profiles, envs, etc.) is preserved.

		With --rm --all, removes the Docker sandbox AND all project
		configuration data (profiles, envs, cached files). Asks for
		confirmation before proceeding.
	`),
	Flags(func(flags *pflag.FlagSet) {
		flags.Bool("rm", false, "Also remove the Docker sandbox after stopping")
		flags.Bool("all", false, "Also remove all project configuration (requires --rm)")
		flags.StringP("workspace", "w", "", "Workspace directory (default: current directory)")
	}),
)

// stopE stops the running sandbox for the current project
func stopE(cmd *cobra.Command, args []string) error {
	// Get workspace directory
	workspaceDir, err := cmd.Flags().GetString("workspace")
	if err != nil {
		return fmt.Errorf("failed to get workspace flag: %w", err)
	}
	if workspaceDir == "" {
		workspaceDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
	}

	removeSandbox, _ := cmd.Flags().GetBool("rm")
	removeAll, _ := cmd.Flags().GetBool("all")

	// --all requires --rm
	if removeAll && !removeSandbox {
		return fmt.Errorf("--all requires --rm (use 'sbox stop --rm --all')")
	}

	// --all asks for confirmation
	if removeAll {
		answeredYes, _ := AskConfirmation("This will remove the Docker sandbox AND all project configuration (profiles, envs, cached files) for %s. Continue?", workspaceDir)
		if !answeredYes {
			cmd.Println("Aborted.")
			return nil
		}
	}

	// Stop the sandbox (and remove container if --rm is set)
	container, err := sbox.StopSandbox(workspaceDir, removeSandbox)
	if err != nil {
		return fmt.Errorf("failed to stop sandbox: %w", err)
	}

	if container != nil {
		cmd.Printf("Sandbox stopped: %s (%s)\n", container.Name, container.ID[:12])
		if removeSandbox {
			cmd.Printf("Container removed: %s\n", container.Name)
		}
	} else {
		cmd.Println("No sandbox was running for this project")
	}

	// Remove project config data only if --all was specified
	if removeAll {
		if err := sbox.RemoveProjectData(workspaceDir); err != nil {
			return fmt.Errorf("failed to remove project data: %w", err)
		}
		cmd.Println("Project configuration removed")
	}

	return nil
}

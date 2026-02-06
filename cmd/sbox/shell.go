package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/sbox"
)

var ShellCommand = Command(shellE,
	"shell",
	"Connect to a running sandbox container for this project",
	Description(`
		Opens a bash shell inside the running Docker sandbox container for the
		current project. Errors if no sandbox container is running for this project.

		This is equivalent to running: docker exec -it <container> bash
	`),
	Flags(func(flags *pflag.FlagSet) {
		flags.StringP("workspace", "w", "", "Workspace directory (default: current directory)")
	}),
)

// shellE opens a shell in the running sandbox container
func shellE(cmd *cobra.Command, args []string) error {
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

	return sbox.ConnectShell(workspaceDir)
}

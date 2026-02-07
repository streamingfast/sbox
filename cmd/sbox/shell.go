package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/sbox"
	"go.uber.org/zap"
)

var ShellCommand = Command(shellE,
	"shell",
	"Connect to a running sandbox or container for this project",
	Description(`
		Opens a bash shell inside the running Docker sandbox or container for the
		current project. Errors if no sandbox/container is running for this project.

		The backend type is determined from the project's configuration
		(sbox.yaml, project config, or global default).

		This is equivalent to running:
		- For sandbox backend: docker sandbox exec -it <name> bash
		- For container backend: docker exec -it <name> bash
	`),
	Flags(func(flags *pflag.FlagSet) {
		flags.StringP("workspace", "w", "", "Workspace directory (default: current directory)")
	}),
)

// shellE opens a shell in the running sandbox/container
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

	// Load configs to resolve backend type
	config, err := sbox.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	projectConfig, _, err := sbox.GetProjectConfig(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to load project config: %w", err)
	}

	sboxFile, err := sbox.FindSboxFile(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to load sbox.yaml file: %w", err)
	}

	projectConfig, err = sbox.MergeProjectConfig(projectConfig, sboxFile)
	if err != nil {
		return fmt.Errorf("failed to merge sbox.yaml config: %w", err)
	}

	// Resolve backend type from project configuration
	backendType := sbox.ResolveBackendType("", sboxFile, projectConfig, config)
	zlog.Debug("resolved backend type", zap.String("backend", string(backendType)))

	backend, err := sbox.GetBackend(string(backendType), config)
	if err != nil {
		return fmt.Errorf("failed to get backend: %w", err)
	}

	return backend.Shell(workspaceDir)
}

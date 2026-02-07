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

	// --all requires --rm
	if removeAll && !removeSandbox {
		return fmt.Errorf("--all requires --rm (use 'sbox stop --rm --all')")
	}

	// --all asks for confirmation
	if removeAll {
		extraInfo := ""
		if backendType == sbox.BackendContainer {
			extraInfo = " and persistence volume"
		}
		answeredYes, _ := AskConfirmation("This will remove the Docker %s%s AND all project configuration for %s. Continue?", backend.Name(), extraInfo, workspaceDir)
		if !answeredYes {
			cmd.Println("Aborted.")
			return nil
		}
	}

	// Stop the sandbox/container (and remove if --rm is set)
	info, err := backend.Stop(workspaceDir, removeSandbox)
	if err != nil {
		return fmt.Errorf("failed to stop %s: %w", backend.Name(), err)
	}

	if info != nil {
		cmd.Printf("%s stopped: %s (%s)\n", capitalizeBackend(backend.Name()), info.Name, info.ID[:12])
		if removeSandbox {
			cmd.Printf("%s removed: %s\n", capitalizeBackend(backend.Name()), info.Name)
		}
	} else {
		cmd.Printf("No %s was running for this project\n", backend.Name())
	}

	// For container backend with --all, also remove the persistence volume
	if removeAll && backendType == sbox.BackendContainer {
		if containerBackend, ok := backend.(*sbox.ContainerBackend); ok {
			if err := containerBackend.RemoveVolume(workspaceDir); err != nil {
				zlog.Warn("failed to remove persistence volume", zap.Error(err))
				// Non-fatal - volume might not exist
			} else {
				cmd.Println("Persistence volume removed")
			}
		}
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

// capitalizeBackend returns a capitalized version of the backend name for display
func capitalizeBackend(bt sbox.BackendType) string {
	switch bt {
	case sbox.BackendSandbox:
		return "Sandbox"
	case sbox.BackendContainer:
		return "Container"
	default:
		return string(bt)
	}
}

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/streamingfast/sbox"
)

// WorkspaceContext contains the resolved configuration for a workspace.
// This consolidates the common pattern of loading configs, sbox files,
// and resolving the backend that appears in shell, stop, and info commands.
type WorkspaceContext struct {
	WorkspaceDir  string
	Config        *sbox.Config
	ProjectConfig *sbox.ProjectConfig
	SboxFile      *sbox.SboxFileLocation
	BackendType   sbox.BackendType
	Backend       sbox.Backend
}

// LoadWorkspaceContext loads all configuration and resolves the backend for a workspace.
// This extracts the common pattern used by shell, stop, and info commands.
func LoadWorkspaceContext(cmd *cobra.Command) (*WorkspaceContext, error) {
	workspaceDir, err := getWorkspaceDir(cmd)
	if err != nil {
		return nil, err
	}

	config, err := sbox.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	projectConfig, _, err := sbox.GetProjectConfig(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load project config: %w", err)
	}

	sboxFile, err := sbox.FindSboxFile(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load sbox.yaml file: %w", err)
	}

	projectConfig, err = sbox.MergeProjectConfig(projectConfig, sboxFile)
	if err != nil {
		return nil, fmt.Errorf("failed to merge sbox.yaml config: %w", err)
	}

	backendType := sbox.ResolveBackendType("", sboxFile, projectConfig, config)

	backend, err := sbox.GetBackend(string(backendType), config)
	if err != nil {
		return nil, fmt.Errorf("failed to get backend: %w", err)
	}

	return &WorkspaceContext{
		WorkspaceDir:  workspaceDir,
		Config:        config,
		ProjectConfig: projectConfig,
		SboxFile:      sboxFile,
		BackendType:   backendType,
		Backend:       backend,
	}, nil
}

// getWorkspaceDir extracts the workspace directory from the --workspace flag
// or defaults to the current working directory.
func getWorkspaceDir(cmd *cobra.Command) (string, error) {
	workspaceDir, err := cmd.Flags().GetString("workspace")
	if err != nil {
		return "", fmt.Errorf("failed to get workspace flag: %w", err)
	}
	if workspaceDir == "" {
		workspaceDir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current directory: %w", err)
		}
	}
	return workspaceDir, nil
}

// formatDockerCommand formats docker command arguments for display.
// Long arguments (like JSON) are truncated for readability.
func formatDockerCommand(args []string) string {
	var result []string
	for i := 0; i < len(args); i++ {
		arg := args[i]

		// Check if this is a flag that takes a value
		if strings.HasPrefix(arg, "--") && i+1 < len(args) {
			nextArg := args[i+1]
			// Truncate very long values (like JSON)
			if len(nextArg) > 80 {
				nextArg = nextArg[:77] + "..."
			}
			// Quote values with spaces
			if strings.Contains(nextArg, " ") {
				nextArg = fmt.Sprintf("%q", nextArg)
			}
			result = append(result, arg, nextArg)
			i++ // Skip the next arg since we already processed it
		} else {
			result = append(result, arg)
		}
	}
	return strings.Join(result, " ")
}

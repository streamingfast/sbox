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

var RunCommand = Command(runE,
	"run",
	"Launch Claude in Docker sandbox or container with configured mounts and profiles",
	Description(`
		Launches a Docker sandbox or container running Claude Code with:
		- Shared ~/.claude/agents and ~/.claude/plugins directories
		- Concatenated CLAUDE.md/AGENTS.md hierarchy from parent directories
		- Persistent credentials across sessions
		- Optional Docker socket access
		- Custom profiles for additional tool installations (Go, Rust, etc.)

		Backend types:
		- sandbox (default): Uses Docker sandbox MicroVM for enhanced isolation
		- container: Uses standard Docker container with named volume persistence
	`),
	Flags(func(flags *pflag.FlagSet) {
		flags.Bool("docker-socket", false, "Mount Docker socket into sandbox/container")
		flags.StringSlice("profile", nil, "Additional profiles to use for this session")
		flags.Bool("recreate", false, "Force rebuild of custom template image and recreate sandbox/container")
		flags.StringP("workspace", "w", "", "Workspace directory (default: current directory)")
		flags.Bool("debug", false, "Enable debug mode for docker commands")
		flags.String("backend", "", "Backend type: 'sandbox' (default) or 'container'")
	}),
)

// runE launches the Docker sandbox with configured settings
func runE(cmd *cobra.Command, args []string) error {
	zlog.Debug("starting sbox run command")

	// Get workspace directory (default to current directory)
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

	// Load global configuration
	config, err := sbox.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Load project configuration
	projectConfig, _, err := sbox.GetProjectConfig(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to load project config: %w", err)
	}

	// Find and merge sbox.yaml file configuration
	sboxFile, err := sbox.FindSboxFile(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to load sbox.yaml file: %w", err)
	}
	projectConfig, err = sbox.MergeProjectConfig(projectConfig, sboxFile)
	if err != nil {
		return fmt.Errorf("failed to merge sbox.yaml config: %w", err)
	}

	// Get flags
	dockerSocket, err := cmd.Flags().GetBool("docker-socket")
	if err != nil {
		return fmt.Errorf("failed to get docker-socket flag: %w", err)
	}

	profiles, err := cmd.Flags().GetStringSlice("profile")
	if err != nil {
		return fmt.Errorf("failed to get profile flag: %w", err)
	}

	recreate, err := cmd.Flags().GetBool("recreate")
	if err != nil {
		return fmt.Errorf("failed to get recreate flag: %w", err)
	}

	debug, err := cmd.Flags().GetBool("debug")
	if err != nil {
		return fmt.Errorf("failed to get debug flag: %w", err)
	}

	backendFlag, err := cmd.Flags().GetString("backend")
	if err != nil {
		return fmt.Errorf("failed to get backend flag: %w", err)
	}

	// Validate backend flag if provided
	if backendFlag != "" {
		if err := sbox.ValidateBackend(backendFlag); err != nil {
			return err
		}
	}

	// Resolve which backend to use (CLI > sbox.yaml > project > global > default)
	backendType := sbox.ResolveBackendType(backendFlag, sboxFile, projectConfig, config)
	zlog.Debug("resolved backend type", zap.String("backend", string(backendType)))

	// Persist the resolved backend to project config so shell/stop/info can find it
	projectConfig.Backend = string(backendType)

	// Get the backend implementation
	backend, err := sbox.GetBackend(string(backendType), config)
	if err != nil {
		return fmt.Errorf("failed to get backend: %w", err)
	}

	// Generate sandbox name first (needed for lookup)
	if projectConfig.SandboxName == "" {
		sandboxName, err := sbox.GenerateSandboxName(workspaceDir)
		if err != nil {
			return fmt.Errorf("failed to generate sandbox name: %w", err)
		}
		projectConfig.SandboxName = sandboxName
		zlog.Debug("generated sandbox name", zap.String("name", sandboxName))
	}

	// Check for existing sandbox by name first, then by workspace path
	existingSandbox, err := sbox.FindDockerSandboxByName(projectConfig.SandboxName)
	if err != nil {
		zlog.Debug("failed to check for existing sandbox by name", zap.Error(err))
	}
	if existingSandbox == nil {
		// Fallback: check by workspace path for backwards compatibility
		existingSandbox, err = sbox.FindDockerSandbox(workspaceDir)
		if err != nil {
			zlog.Debug("failed to check for existing sandbox by workspace", zap.Error(err))
		}
	}

	if recreate {
		// Remove existing sandbox so a fresh one is created
		if existingSandbox != nil {
			// Save .claude cache before removing (for persistence across recreate)
			if existingSandbox.Status == "running" {
				if err := backend.SaveCache(workspaceDir); err != nil {
					zlog.Warn("failed to save cache", zap.Error(err))
					// Non-fatal - continue with recreate
				}
			}

			cmd.Printf("Removing existing sandbox '%s' (%s)...\n", existingSandbox.Name, existingSandbox.ID)
			if err := sbox.RemoveDockerSandbox(existingSandbox.ID); err != nil {
				return fmt.Errorf("failed to remove existing sandbox: %w", err)
			}
			cmd.Println("Existing sandbox removed")
			existingSandbox = nil // Clear so we don't check mounts on removed sandbox
		} else {
			// Sandbox lookup might have failed, but sandbox might still exist
			// Try to remove by name directly
			cmd.Printf("Removing sandbox '%s' (if exists)...\n", projectConfig.SandboxName)
			if err := sbox.RemoveDockerSandboxByName(projectConfig.SandboxName); err != nil {
				// Non-fatal: sandbox might not exist
				zlog.Debug("failed to remove sandbox by name (may not exist)", zap.Error(err))
			} else {
				cmd.Println("Existing sandbox removed")
			}
		}
	}

	// Save project config to register this project (for sbox info)
	// This must happen BEFORE running the sandbox so other terminals can see it
	if err := sbox.SaveProjectConfig(workspaceDir, projectConfig); err != nil {
		zlog.Warn("failed to save project config", zap.Error(err))
		// Non-fatal: continue running sandbox even if we can't save config
	}

	// Build backend options
	opts := sbox.BackendOptions{
		WorkspaceDir:      workspaceDir,
		MountDockerSocket: dockerSocket,
		Profiles:          profiles,
		ForceRebuild:      recreate,
		Debug:             debug,
		Config:            config,
		ProjectConfig:     projectConfig,
		SboxFile:          sboxFile,
	}

	// Run using the selected backend
	cmd.Printf("Using %s backend\n", backend.Name())
	return backend.Run(opts)
}



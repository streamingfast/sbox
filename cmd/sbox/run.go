package main

import (
	"fmt"
	"os"
	"strings"

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

		Agent types:
		- claude (default): Uses Claude Code AI agent
		- opencode: Uses OpenCode AI agent
	`),
	Flags(func(flags *pflag.FlagSet) {
		flags.Bool("docker-socket", false, "Mount Docker socket into sandbox/container")
		flags.StringSlice("profile", nil, "Additional profiles to use for this session")
		flags.Bool("recreate", false, "Force rebuild of custom template image and recreate sandbox/container (pulls latest base image)")
		flags.StringP("workspace", "w", "", "Workspace directory (default: current directory)")
		flags.Bool("debug", false, "Enable debug mode for docker commands")
		flags.String("backend", "", "Backend type: 'sandbox' (default) or 'container'")
		flags.String("agent", "", "Agent type: 'claude' (default) or 'opencode'")
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

	agentFlag, err := cmd.Flags().GetString("agent")
	if err != nil {
		return fmt.Errorf("failed to get agent flag: %w", err)
	}

	// Validate agent flag if provided
	if agentFlag != "" {
		if err := sbox.ValidateAgent(agentFlag); err != nil {
			return err
		}
	}

	// Resolve which backend to use (CLI > sbox.yaml > project > global > default)
	backendType := sbox.ResolveBackendType(backendFlag, sboxFile, projectConfig, config)
	zlog.Debug("resolved backend type", zap.String("backend", string(backendType)))

	// Resolve which agent to use (CLI > sbox.yaml > project > global > default)
	agentType := sbox.ResolveAgentType(agentFlag, sboxFile, projectConfig, config)
	zlog.Debug("resolved agent type", zap.String("agent", string(agentType)))

	// Persist the resolved backend and agent to project config so shell/stop/info can find it
	projectConfig.Backend = string(backendType)
	projectConfig.Agent = string(agentType)

	// Get the backend implementation
	backend, err := sbox.GetBackend(string(backendType), config)
	if err != nil {
		return fmt.Errorf("failed to get backend: %w", err)
	}

	// Generate sandbox name first (needed for lookup)
	// Regenerate if empty OR if the agent has changed (detected by checking if the name contains the current agent)
	needsRegeneration := projectConfig.SandboxName == ""
	if !needsRegeneration && projectConfig.SandboxName != "" {
		// Check if the sandbox name matches the current agent
		expectedPrefix := "sbox-" + string(agentType) + "-"
		if !strings.HasPrefix(projectConfig.SandboxName, expectedPrefix) {
			zlog.Info("agent changed, regenerating sandbox name",
				zap.String("old_name", projectConfig.SandboxName),
				zap.String("agent", string(agentType)))
			needsRegeneration = true
		}
	}

	if needsRegeneration {
		sandboxName, err := sbox.GenerateSandboxName(workspaceDir, agentType)
		if err != nil {
			return fmt.Errorf("failed to generate sandbox name: %w", err)
		}
		projectConfig.SandboxName = sandboxName
		zlog.Debug("generated sandbox name", zap.String("name", sandboxName))
	}

	if recreate {
		// Remove existing container/sandbox so a fresh one is created
		// Use backend's Find() method to check for existing instance
		existing, err := backend.Find(workspaceDir)
		if err != nil {
			zlog.Debug("failed to check for existing container/sandbox", zap.Error(err))
		}

		if existing != nil {
			// Save .claude cache before removing (for persistence across recreate)
			if existing.Status == "running" {
				if err := backend.SaveCache(workspaceDir); err != nil {
					zlog.Warn("failed to save cache", zap.Error(err))
					// Non-fatal - continue with recreate
				}
			}

			cmd.Printf("Removing existing %s '%s' (%s)...\n", backendType, existing.Name, existing.ID)
			if err := backend.Remove(existing.ID); err != nil {
				return fmt.Errorf("failed to remove existing %s: %w", backendType, err)
			}
			cmd.Printf("Existing %s removed\n", backendType)
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



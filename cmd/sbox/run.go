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
	"Launch Claude in Docker sandbox with configured mounts and profiles",
	Description(`
		Launches a Docker sandbox running Claude Code with:
		- Shared ~/.claude/agents and ~/.claude/plugins directories
		- Concatenated CLAUDE.md/AGENTS.md hierarchy from parent directories
		- Persistent credentials across sessions
		- Optional Docker socket access
		- Custom profiles for additional tool installations (Go, Rust, etc.)
	`),
	Flags(func(flags *pflag.FlagSet) {
		flags.Bool("docker-socket", false, "Mount Docker socket into sandbox")
		flags.StringSlice("profile", nil, "Additional profiles to use for this session")
		flags.Bool("recreate", false, "Force rebuild of custom template image and recreate sandbox")
		flags.StringP("workspace", "w", "", "Workspace directory (default: current directory)")
		flags.Bool("debug", false, "Enable debug mode for docker sandbox commands")
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

	if existingSandbox != nil {
		// Check for broken mounts that would prevent the sandbox from starting
		brokenMounts, err := sbox.CheckBrokenMounts(existingSandbox.ID)
		if err != nil {
			zlog.Debug("failed to check broken mounts", zap.Error(err))
		} else if len(brokenMounts) > 0 {
			cmd.Println()
			cmd.Println("WARNING: Sandbox has broken mount configurations that will prevent it from starting:")
			for _, m := range brokenMounts {
				cmd.Printf("  - %s -> %s\n", m.Source, m.Destination)
				cmd.Printf("    Reason: %s\n", m.Reason)
			}
			cmd.Println()
			answeredYes, _ := AskConfirmation("Recreate sandbox to fix broken mounts?")
			if answeredYes {
				cmd.Printf("Removing stale sandbox %s...\n", existingSandbox.ID)
				if err := sbox.RemoveDockerSandbox(existingSandbox.ID); err != nil {
					return fmt.Errorf("failed to remove stale sandbox: %w", err)
				}
				cmd.Println("Stale sandbox removed, a new one will be created")
			} else {
				cmd.Println("Continuing with existing sandbox...")
			}
		} else {
			// Check for mount mismatches and warn
			if err := checkAndWarnMountMismatch(cmd, workspaceDir, existingSandbox, config, projectConfig); err != nil {
				zlog.Debug("failed to check mount mismatch", zap.Error(err))
			}
		}
	}

	// Save project config to register this project (for sbox info)
	// This must happen BEFORE running the sandbox so other terminals can see it
	if err := sbox.SaveProjectConfig(workspaceDir, projectConfig); err != nil {
		zlog.Warn("failed to save project config", zap.Error(err))
		// Non-fatal: continue running sandbox even if we can't save config
	}

	// Build sandbox options
	opts := sbox.SandboxOptions{
		WorkspaceDir:      workspaceDir,
		MountDockerSocket: dockerSocket,
		Profiles:          profiles,
		ForceRebuild:      recreate,
		Debug:             debug,
		Config:            config,
		ProjectConfig:     projectConfig,
		SboxFile:          sboxFile,
	}

	// Run the sandbox
	return sbox.RunSandbox(opts)
}

// checkAndWarnMountMismatch checks if the running sandbox has different mounts than expected
// and warns the user if there's a mismatch
func checkAndWarnMountMismatch(cmd *cobra.Command, workspaceDir string, sandbox *sbox.DockerSandbox, config *sbox.Config, projectConfig *sbox.ProjectConfig) error {
	// Find the running container to inspect mounts
	container, err := sbox.FindRunningSandbox(workspaceDir)
	if err != nil || container == nil {
		// Not running or can't find - no mismatch to warn about
		return nil
	}

	// Prepare CLAUDE.md for mount comparison
	claudeMDPath, err := sbox.PrepareMDForSandbox(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to prepare CLAUDE.md: %w", err)
	}

	// Build expected mounts
	opts := sbox.SandboxOptions{
		WorkspaceDir:  workspaceDir,
		Config:        config,
		ProjectConfig: projectConfig,
	}

	expectedMounts, err := sbox.GetExpectedMounts(opts, claudeMDPath)
	if err != nil {
		return fmt.Errorf("failed to get expected mounts: %w", err)
	}

	// Check for mismatches
	mismatch, err := sbox.CheckMountMismatch(container.ID, expectedMounts)
	if err != nil {
		return fmt.Errorf("failed to check mount mismatch: %w", err)
	}

	if mismatch != nil && len(mismatch.Missing) > 0 {
		cmd.Println()
		cmd.Println("WARNING: Sandbox mount configuration has changed.")
		cmd.Println("The following mounts are missing from the running sandbox:")
		for _, m := range mismatch.Missing {
			roStr := ""
			if m.ReadOnly {
				roStr = " (read-only)"
			}
			cmd.Printf("  - %s -> %s%s\n", m.Source, m.Destination, roStr)
		}
		cmd.Println()
		cmd.Println("Docker sandboxes remember their initial mount configuration.")
		cmd.Println("To apply new mounts, use: sbox run --recreate")
		cmd.Println()
	}

	return nil
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

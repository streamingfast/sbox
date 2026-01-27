package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/logging"
	"github.com/streamingfast/sbox"
	"go.uber.org/zap"
)

// Version is set via ldflags at build time
var version = "dev"

var zlog, _ = logging.PackageLogger("sbox", "github.com/streamingfast/sbox/cmd/main")

func init() {
	logging.InstantiateLoggers(logging.WithDefaultLevel(zap.DPanicLevel))
}

func main() {
	Run(
		"sbox <command>",
		"Docker sandbox wrapper for Claude Code with enhanced sharing and profiles",

		ConfigureVersion(version),
		ConfigureViper("SBOX"),

		// Default command (no subcommand = run)
		Execute(runE),

		Command(runE,
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
			}),
		),

		Group("profile", "Manage development profiles",
			Command(profileListE,
				"list",
				"List available and installed profiles",
			),
			Command(profileAddE,
				"add <profiles...>",
				"Add profiles to the current project",
				ExactArgs(1),
			),
			Command(profileRemoveE,
				"remove <profiles...>",
				"Remove profiles from the current project",
				ExactArgs(1),
			),
		),

		Group("env", "Manage environment variables for the sandbox",
			Command(envListE,
				"list",
				"List environment variables for this project",
			),
			Command(envAddE,
				"add <envs...>",
				"Add environment variables (NAME for host passthrough, NAME=VALUE for explicit)",
				MinimumNArgs(1),
				Flags(func(flags *pflag.FlagSet) {
					flags.Bool("global", false, "Add to global config (shared across all projects)")
				}),
			),
			Command(envRemoveE,
				"remove <names...>",
				"Remove environment variables by name",
				MinimumNArgs(1),
				Flags(func(flags *pflag.FlagSet) {
					flags.Bool("global", false, "Remove from global config")
				}),
			),
		),

		Command(configE,
			"config [key] [value]",
			"View or edit configuration settings",
			Description(`
				Without arguments, displays the current configuration.
				With a key, displays that setting's value.
				With key and value, sets the configuration option.
			`),
		),

		Command(cleanE,
			"clean",
			"Clean up sandbox containers, images, and cached data",
			Flags(func(flags *pflag.FlagSet) {
				flags.Bool("all", false, "Remove all cached images and project data")
				flags.Bool("images", false, "Remove cached profile images only")
			}),
		),

		Command(shellE,
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
		),

		Command(authE,
			"auth",
			"Authenticate with Claude (shared across all sandboxes)",
			Description(`
				Sets up authentication that is shared across all sandbox sessions.

				Without flags, runs the interactive authentication flow:
				1. Opens browser for OAuth authentication
				2. Generates a long-lived token (valid for 1 year)
				3. Stores the token for use by all sandboxes

				With --status, shows the current authentication status.
				With --logout, removes the stored authentication token.
			`),
			Flags(func(flags *pflag.FlagSet) {
				flags.Bool("status", false, "Show authentication status")
				flags.Bool("logout", false, "Remove stored authentication token")
			}),
		),

		Command(infoE,
			"info",
			"Show project information",
			Description(`
				Shows information about the sandbox project for the current directory.

				Without flags, shows the current project's status including:
				- Workspace path and hash
				- Running status (stopped/running with container info)
				- Configured profiles
				- Additional volumes
				- Docker socket setting

				With --all, lists all known projects that have been used with sbox.
			`),
			Flags(func(flags *pflag.FlagSet) {
				flags.Bool("all", false, "Show all known projects")
				flags.StringP("workspace", "w", "", "Workspace directory (default: current directory)")
			}),
		),

		Command(stopE,
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
		),

		OnCommandError(func(err error) {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			zlog.Debug("command error", zap.Error(err))
			os.Exit(1)
		}),
	)
}

// runE launches the Docker sandbox with configured settings
func runE(cmd *cobra.Command, args []string) error {
	sbox.SetupLogging()

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

	// Find and merge .sbox file configuration
	sboxFile, err := sbox.FindSboxFile(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to load .sbox file: %w", err)
	}
	projectConfig, err = sbox.MergeProjectConfig(projectConfig, sboxFile)
	if err != nil {
		return fmt.Errorf("failed to merge .sbox config: %w", err)
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

	// Check for existing sandbox and handle recreate/mount mismatch
	existingSandbox, err := sbox.FindDockerSandbox(workspaceDir)
	if err != nil {
		// Non-fatal: docker sandbox ls might not be available
		zlog.Debug("failed to check for existing sandbox", zap.Error(err))
	}

	if recreate {
		// Remove existing sandbox so a fresh one is created
		if existingSandbox != nil {
			cmd.Printf("Removing existing sandbox %s...\n", existingSandbox.ID)
			if err := sbox.RemoveDockerSandbox(existingSandbox.ID); err != nil {
				return fmt.Errorf("failed to remove existing sandbox: %w", err)
			}
			cmd.Println("Existing sandbox removed")
		}
	} else if existingSandbox != nil {
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
		Config:            config,
		ProjectConfig:     projectConfig,
	}

	// Run the sandbox
	return sbox.RunSandbox(opts)
}

// profileListE lists available and installed profiles
func profileListE(cmd *cobra.Command, args []string) error {
	sbox.SetupLogging()

	// Get current workspace to show project-specific profiles
	workspaceDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Load project config
	projectConfig, _, err := sbox.GetProjectConfig(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to load project config: %w", err)
	}

	// Create a set of installed profiles for quick lookup
	installedSet := make(map[string]bool)
	for _, p := range projectConfig.Profiles {
		installedSet[p] = true
	}

	cmd.Println("Available profiles:")
	cmd.Println()

	// List all profiles with their status
	for _, name := range sbox.ListProfiles() {
		profile, _ := sbox.GetProfile(name)
		status := "[ ]"
		if installedSet[name] {
			status = "[✓]"
		}
		cmd.Printf("  %s %s\n", status, name)
		cmd.Printf("      %s\n", profile.Description)
	}

	if len(projectConfig.Profiles) > 0 {
		cmd.Println()
		cmd.Printf("Project profiles: %v\n", projectConfig.Profiles)
	}

	return nil
}

// profileAddE adds profiles to the current project
func profileAddE(cmd *cobra.Command, args []string) error {
	sbox.SetupLogging()

	profileName := args[0]

	// Validate profile exists
	if _, ok := sbox.GetProfile(profileName); !ok {
		return fmt.Errorf("unknown profile: %s\nAvailable profiles: %v", profileName, sbox.ListProfiles())
	}

	// Get workspace directory
	workspaceDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Load project config
	projectConfig, _, err := sbox.GetProjectConfig(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to load project config: %w", err)
	}

	// Check if already added
	for _, p := range projectConfig.Profiles {
		if p == profileName {
			cmd.Printf("Profile '%s' is already added to this project\n", profileName)
			return nil
		}
	}

	// Add profile
	projectConfig.Profiles = append(projectConfig.Profiles, profileName)

	// Save project config
	if err := sbox.SaveProjectConfig(workspaceDir, projectConfig); err != nil {
		return fmt.Errorf("failed to save project config: %w", err)
	}

	cmd.Printf("Added profile '%s' to project\n", profileName)
	cmd.Println("Run 'sbox run --recreate' to rebuild and recreate the sandbox with this profile")
	return nil
}

// profileRemoveE removes profiles from the current project
func profileRemoveE(cmd *cobra.Command, args []string) error {
	sbox.SetupLogging()

	profileName := args[0]

	// Get workspace directory
	workspaceDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Load project config
	projectConfig, _, err := sbox.GetProjectConfig(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to load project config: %w", err)
	}

	// Find and remove profile
	found := false
	newProfiles := make([]string, 0, len(projectConfig.Profiles))
	for _, p := range projectConfig.Profiles {
		if p == profileName {
			found = true
		} else {
			newProfiles = append(newProfiles, p)
		}
	}

	if !found {
		cmd.Printf("Profile '%s' is not in this project\n", profileName)
		return nil
	}

	projectConfig.Profiles = newProfiles

	// Save project config
	if err := sbox.SaveProjectConfig(workspaceDir, projectConfig); err != nil {
		return fmt.Errorf("failed to save project config: %w", err)
	}

	cmd.Printf("Removed profile '%s' from project\n", profileName)
	cmd.Println("Run 'sbox run --recreate' to rebuild and recreate the sandbox without this profile")
	return nil
}

// envListE lists environment variables for the current project
func envListE(cmd *cobra.Command, args []string) error {
	sbox.SetupLogging()

	workspaceDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

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
		return fmt.Errorf("failed to load .sbox file: %w", err)
	}

	var sboxEnvs []string
	if sboxFile != nil && sboxFile.Config != nil {
		sboxEnvs = sboxFile.Config.Envs
	}

	_, resolved := sbox.MergeEnvs(config.Envs, projectConfig.Envs, sboxEnvs)

	if len(resolved) == 0 {
		cmd.Println("No environment variables configured.")
		cmd.Println("Use 'sbox env add NAME=VALUE' or 'sbox env add --global NAME' to add one.")
		return nil
	}

	cmd.Println("Environment variables:")
	cmd.Println()
	printResolvedEnvs(cmd, resolved, "  ")

	return nil
}

// envAddE adds environment variables to the current project or global config
func envAddE(cmd *cobra.Command, args []string) error {
	sbox.SetupLogging()

	global, _ := cmd.Flags().GetBool("global")

	if global {
		return envAddGlobal(cmd, args)
	}
	return envAddProject(cmd, args)
}

func envAddGlobal(cmd *cobra.Command, args []string) error {
	config, err := sbox.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	envMap := make(map[string]int)
	for i, e := range config.Envs {
		envMap[sbox.EnvName(e)] = i
	}

	for _, arg := range args {
		name := sbox.EnvName(arg)
		if name == "" {
			return fmt.Errorf("invalid environment variable: %q", arg)
		}

		if idx, exists := envMap[name]; exists {
			config.Envs[idx] = arg
			cmd.Printf("Updated '%s' (global)\n", name)
		} else {
			config.Envs = append(config.Envs, arg)
			envMap[name] = len(config.Envs) - 1
			cmd.Printf("Added '%s' (global)\n", name)
		}
	}

	if err := sbox.SaveConfig(config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	cmd.Println("Run 'sbox run --recreate' to apply changes to the running sandbox.")
	return nil
}

func envAddProject(cmd *cobra.Command, args []string) error {
	workspaceDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	projectConfig, _, err := sbox.GetProjectConfig(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to load project config: %w", err)
	}

	envMap := make(map[string]int)
	for i, e := range projectConfig.Envs {
		envMap[sbox.EnvName(e)] = i
	}

	for _, arg := range args {
		name := sbox.EnvName(arg)
		if name == "" {
			return fmt.Errorf("invalid environment variable: %q", arg)
		}

		if idx, exists := envMap[name]; exists {
			projectConfig.Envs[idx] = arg
			cmd.Printf("Updated '%s' (project)\n", name)
		} else {
			projectConfig.Envs = append(projectConfig.Envs, arg)
			envMap[name] = len(projectConfig.Envs) - 1
			cmd.Printf("Added '%s' (project)\n", name)
		}
	}

	if err := sbox.SaveProjectConfig(workspaceDir, projectConfig); err != nil {
		return fmt.Errorf("failed to save project config: %w", err)
	}

	cmd.Println("Run 'sbox run --recreate' to apply changes to the running sandbox.")
	return nil
}

// envRemoveE removes environment variables from the current project or global config
func envRemoveE(cmd *cobra.Command, args []string) error {
	sbox.SetupLogging()

	global, _ := cmd.Flags().GetBool("global")

	if global {
		return envRemoveGlobal(cmd, args)
	}
	return envRemoveProject(cmd, args)
}

func envRemoveGlobal(cmd *cobra.Command, args []string) error {
	config, err := sbox.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	removeSet := make(map[string]bool)
	for _, name := range args {
		removeSet[name] = true
	}

	var kept []string
	removed := 0
	for _, env := range config.Envs {
		if removeSet[sbox.EnvName(env)] {
			cmd.Printf("Removed '%s' (global)\n", sbox.EnvName(env))
			removed++
		} else {
			kept = append(kept, env)
		}
	}

	if removed == 0 {
		cmd.Println("No matching global environment variables found.")
		return nil
	}

	config.Envs = kept

	if err := sbox.SaveConfig(config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	cmd.Println("Run 'sbox run --recreate' to apply changes to the running sandbox.")
	return nil
}

func envRemoveProject(cmd *cobra.Command, args []string) error {
	workspaceDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	projectConfig, _, err := sbox.GetProjectConfig(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to load project config: %w", err)
	}

	removeSet := make(map[string]bool)
	for _, name := range args {
		removeSet[name] = true
	}

	var kept []string
	removed := 0
	for _, env := range projectConfig.Envs {
		if removeSet[sbox.EnvName(env)] {
			cmd.Printf("Removed '%s' (project)\n", sbox.EnvName(env))
			removed++
		} else {
			kept = append(kept, env)
		}
	}

	if removed == 0 {
		cmd.Println("No matching project environment variables found.")
		return nil
	}

	projectConfig.Envs = kept

	if err := sbox.SaveProjectConfig(workspaceDir, projectConfig); err != nil {
		return fmt.Errorf("failed to save project config: %w", err)
	}

	cmd.Println("Run 'sbox run --recreate' to apply changes to the running sandbox.")
	return nil
}

// configE views or edits configuration
func configE(cmd *cobra.Command, args []string) error {
	sbox.SetupLogging()

	config, err := sbox.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if len(args) == 0 {
		// Show all configuration
		cmd.Println("Global configuration:")
		cmd.Printf("  claude_home: %s\n", config.ClaudeHome)
		cmd.Printf("  sbox_data_dir: %s\n", config.SboxDataDir)
		cmd.Printf("  docker_socket: %s\n", config.DockerSocket)
		cmd.Printf("  default_profiles: %v\n", config.DefaultProfiles)
		return nil
	}

	key := args[0]

	if len(args) == 1 {
		// Show specific key
		switch key {
		case "claude_home":
			cmd.Println(config.ClaudeHome)
		case "sbox_data_dir":
			cmd.Println(config.SboxDataDir)
		case "docker_socket":
			cmd.Println(config.DockerSocket)
		case "default_profiles":
			cmd.Printf("%v\n", config.DefaultProfiles)
		default:
			return fmt.Errorf("unknown config key: %s", key)
		}
		return nil
	}

	// Set value
	value := args[1]
	switch key {
	case "claude_home":
		config.ClaudeHome = value
	case "docker_socket":
		if value != "auto" && value != "always" && value != "never" {
			return fmt.Errorf("docker_socket must be one of: auto, always, never")
		}
		config.DockerSocket = value
	default:
		return fmt.Errorf("cannot set config key: %s (read-only or unknown)", key)
	}

	if err := sbox.SaveConfig(config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	cmd.Printf("Set %s = %s\n", key, value)
	return nil
}

// cleanE cleans up sandbox resources
func cleanE(cmd *cobra.Command, args []string) error {
	sbox.SetupLogging()

	cleanAll, _ := cmd.Flags().GetBool("all")
	cleanImages, _ := cmd.Flags().GetBool("images")

	if !cleanAll && !cleanImages {
		// Default to cleaning images
		cleanImages = true
	}

	if cleanImages || cleanAll {
		cmd.Println("Cleaning cached template images...")
		if err := sbox.CleanTemplates(); err != nil {
			return fmt.Errorf("failed to clean templates: %w", err)
		}
		cmd.Println("Template images cleaned")
	}

	if cleanAll {
		cmd.Println("Cleaning project data...")
		config, err := sbox.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		projectsDir := config.SboxDataDir + "/projects"
		if err := os.RemoveAll(projectsDir); err != nil {
			return fmt.Errorf("failed to clean projects: %w", err)
		}
		cmd.Println("Project data cleaned")
	}

	cmd.Println("Cleanup complete")
	return nil
}

// shellE opens a shell in the running sandbox container
func shellE(cmd *cobra.Command, args []string) error {
	sbox.SetupLogging()

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

// authE handles authentication management
func authE(cmd *cobra.Command, args []string) error {
	sbox.SetupLogging()

	showStatus, _ := cmd.Flags().GetBool("status")
	logout, _ := cmd.Flags().GetBool("logout")

	if showStatus {
		return authStatus(cmd)
	}

	if logout {
		return authLogout(cmd)
	}

	return authLogin(cmd)
}

// authStatus shows the current authentication status
func authStatus(cmd *cobra.Command) error {
	if sbox.HasCredentials() {
		cmd.Println("Status: Authenticated")

		config, err := sbox.LoadConfig()
		if err == nil {
			credentialsPath := sbox.GetCredentialsPath(config)
			cmd.Printf("Credentials stored at: %s\n", credentialsPath)
		}

		cmd.Println("Credentials will be used for all sandbox sessions.")
	} else {
		cmd.Println("Status: Not authenticated")
		cmd.Println("Run 'sbox auth' to authenticate.")
	}
	return nil
}

// authLogout removes the stored authentication credentials
func authLogout(cmd *cobra.Command) error {
	if !sbox.HasCredentials() {
		cmd.Println("No authentication credentials stored.")
		return nil
	}

	// Remove credentials file from sbox config directory
	config, err := sbox.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	credentialsPath := sbox.GetCredentialsPath(config)
	if err := os.Remove(credentialsPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove credentials: %w", err)
	}

	// Also remove legacy token file if it exists
	if err := sbox.RemoveAuthToken(); err != nil && !os.IsNotExist(err) {
		// Non-fatal, just log
		cmd.Printf("Warning: failed to remove legacy token file: %v\n", err)
	}

	cmd.Println("Authentication credentials removed.")
	return nil
}

// authLogin runs the interactive authentication flow
func authLogin(cmd *cobra.Command) error {
	if sbox.HasCredentials() {
		cmd.Println("Already authenticated. Use 'sbox auth --logout' first to re-authenticate.")
		return nil
	}

	cmd.Println("Setting up Claude authentication...")
	cmd.Println()
	cmd.Println("This will run 'claude setup-token' to generate credentials.")
	cmd.Println("The credentials will be shared across all sandbox sessions.")
	cmd.Println()

	// Load config to get sbox data directory
	config, err := sbox.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create a temporary directory for the sandbox to write credentials
	tempDir, err := os.MkdirTemp("", "sbox-auth-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Run docker sandbox with setup-token command, mounting temp directory as ~/.claude
	dockerCmd := exec.Command("docker", "sandbox", "run",
		"-v", fmt.Sprintf("%s:/home/agent/.claude", tempDir),
		"claude", "--", "setup-token")
	dockerCmd.Stdin = os.Stdin
	dockerCmd.Stdout = os.Stdout
	dockerCmd.Stderr = os.Stderr

	if err := dockerCmd.Run(); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Copy credentials from temp directory to sbox config directory
	tempCredentialsPath := filepath.Join(tempDir, ".credentials.json")
	if _, err := os.Stat(tempCredentialsPath); err != nil {
		return fmt.Errorf("credentials file was not created: %w", err)
	}

	credData, err := os.ReadFile(tempCredentialsPath)
	if err != nil {
		return fmt.Errorf("failed to read credentials: %w", err)
	}

	credentialsPath := sbox.GetCredentialsPath(config)
	if err := os.WriteFile(credentialsPath, credData, 0600); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	cmd.Println()
	cmd.Println("Authentication successful!")
	cmd.Printf("Credentials saved to: %s\n", credentialsPath)
	cmd.Println("All future sandbox sessions will use these credentials automatically.")

	return nil
}

// infoE shows project information
func infoE(cmd *cobra.Command, args []string) error {
	sbox.SetupLogging()

	showAll, err := cmd.Flags().GetBool("all")
	if err != nil {
		return fmt.Errorf("failed to get all flag: %w", err)
	}

	if showAll {
		return infoAllProjects(cmd)
	}

	return infoCurrentProject(cmd)
}

// infoCurrentProject shows information for the current project
func infoCurrentProject(cmd *cobra.Command) error {
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

	// Check if this workspace is a known project
	projects, err := sbox.ListProjects()
	if err != nil {
		return fmt.Errorf("failed to list projects: %w", err)
	}

	var project *sbox.ProjectInfo
	for i := range projects {
		if projects[i].WorkspacePath == workspaceDir {
			project = &projects[i]
			break
		}
	}

	if project == nil {
		cmd.Println("No sandbox has been run in this directory yet.")
		cmd.Println("Run 'sbox' or 'sbox run' to create a sandbox for this project.")
		cmd.Println()
		cmd.Println("Use 'sbox info --all' to list all known projects.")
		return nil
	}

	// Load global config
	config, err := sbox.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	cmd.Printf("Project: %s\n", workspaceDir)
	cmd.Printf("  Hash:   %s\n", project.Hash)

	// Show container if running, otherwise show sandbox info
	container, containerErr := sbox.FindRunningSandbox(workspaceDir)
	if containerErr != nil {
		cmd.Printf("  Container: error: %s\n", containerErr)
	} else if container != nil {
		cmd.Printf("  Container:\n")
		cmd.Printf("    ID:     %s\n", container.ID[:12])
		cmd.Printf("    Name:   %s\n", container.Name)
		cmd.Printf("    Status: running\n")
	} else {
		sandbox, sandboxErr := sbox.FindDockerSandbox(workspaceDir)
		if sandboxErr != nil {
			cmd.Printf("  Sandbox: error: %s\n", sandboxErr)
		} else if sandbox != nil {
			cmd.Printf("  Sandbox:\n")
			cmd.Printf("    ID:     %s\n", sandbox.ID)
			if sandbox.Name != "" {
				cmd.Printf("    Name:   %s\n", sandbox.Name)
			}
			cmd.Printf("    Status: stopped\n")
			if sandbox.Image != "" {
				cmd.Printf("    Image:  %s\n", sandbox.Image)
			}
		} else {
			cmd.Printf("  Sandbox: none\n")
		}
	}

	if len(project.Config.Profiles) > 0 {
		cmd.Printf("  Profiles:\n")
		for _, p := range project.Config.Profiles {
			cmd.Printf("    - %s\n", p)
		}
	}
	if len(project.Config.Volumes) > 0 {
		cmd.Printf("  Volumes:  %v\n", project.Config.Volumes)
	}
	// Show merged envs with sources
	{
		var sboxEnvs []string
		if sboxFile, err := sbox.FindSboxFile(workspaceDir); err == nil && sboxFile != nil && sboxFile.Config != nil {
			sboxEnvs = sboxFile.Config.Envs
		}
		_, resolved := sbox.MergeEnvs(config.Envs, project.Config.Envs, sboxEnvs)
		if len(resolved) > 0 {
			cmd.Printf("  Envs:\n")
			printResolvedEnvs(cmd, resolved, "    ")
		}
	}
	if project.Config.DockerSocket != "" {
		cmd.Printf("  Docker:   %s\n", project.Config.DockerSocket)
	}

	// Build and display the docker command
	opts := sbox.SandboxOptions{
		WorkspaceDir:  workspaceDir,
		Config:        config,
		ProjectConfig: project.Config,
	}
	if dockerArgs, err := sbox.BuildDockerCommand(opts); err == nil {
		cmd.Printf("  Command:\n    docker %s\n", formatDockerCommand(dockerArgs))
	}

	return nil
}

// infoAllProjects lists all known projects with their status
func infoAllProjects(cmd *cobra.Command) error {
	projects, err := sbox.ListProjects()
	if err != nil {
		return fmt.Errorf("failed to list projects: %w", err)
	}

	if len(projects) == 0 {
		cmd.Println("No known projects.")
		cmd.Println("Run 'sbox' or 'sbox run' in a directory to create a project.")
		return nil
	}

	cmd.Println("Known projects:")
	cmd.Println()

	for _, project := range projects {
		// Handle empty workspace path (from old configs before WorkspacePath was added)
		workspacePath := project.WorkspacePath
		pathDisplay := workspacePath

		if workspacePath == "" {
			pathDisplay = "(unknown path - legacy project)"
		} else {
			// Check if workspace path still exists
			if _, err := os.Stat(workspacePath); os.IsNotExist(err) {
				pathDisplay += " (path not found)"
			}
		}

		cmd.Printf("  %s\n", pathDisplay)
		cmd.Printf("    Hash:     %s\n", project.Hash)

		// Show container if running, otherwise show sandbox info
		if workspacePath != "" {
			container, containerErr := sbox.FindRunningSandbox(workspacePath)
			if containerErr != nil {
				cmd.Printf("    Container: error: %s\n", containerErr)
			} else if container != nil {
				cmd.Printf("    Container:\n")
				cmd.Printf("      ID:     %s\n", container.ID[:12])
				cmd.Printf("      Name:   %s\n", container.Name)
				cmd.Printf("      Status: running\n")
			} else {
				sandbox, sandboxErr := sbox.FindDockerSandbox(workspacePath)
				if sandboxErr != nil {
					cmd.Printf("    Sandbox:  error: %s\n", sandboxErr)
				} else if sandbox != nil {
					cmd.Printf("    Sandbox:\n")
					cmd.Printf("      ID:     %s\n", sandbox.ID)
					if sandbox.Name != "" {
						cmd.Printf("      Name:   %s\n", sandbox.Name)
					}
					cmd.Printf("      Status: stopped\n")
					if sandbox.Image != "" {
						cmd.Printf("      Image:  %s\n", sandbox.Image)
					}
				} else {
					cmd.Printf("    Sandbox:  none\n")
				}
			}
		} else {
			cmd.Printf("    Sandbox:  unknown\n")
		}

		if len(project.Config.Profiles) > 0 {
			cmd.Printf("    Profiles:\n")
			for _, p := range project.Config.Profiles {
				cmd.Printf("      - %s\n", p)
			}
		}
		if len(project.Config.Volumes) > 0 {
			cmd.Printf("    Volumes:  %v\n", project.Config.Volumes)
		}
		// Show merged envs with sources
		{
			globalConfig, err := sbox.LoadConfig()
			var globalEnvs []string
			if err == nil {
				globalEnvs = globalConfig.Envs
			}
			_, resolved := sbox.MergeEnvs(globalEnvs, project.Config.Envs, nil)
			if len(resolved) > 0 {
				cmd.Printf("    Envs:\n")
				printResolvedEnvs(cmd, resolved, "      ")
			}
		}
		if project.Config.DockerSocket != "" {
			cmd.Printf("    Docker:   %s\n", project.Config.DockerSocket)
		}

		// Build and display the docker command if workspace exists
		if workspacePath != "" {
			if _, err := os.Stat(workspacePath); err == nil {
				config, err := sbox.LoadConfig()
				if err == nil {
					opts := sbox.SandboxOptions{
						WorkspaceDir:  workspacePath,
						Config:        config,
						ProjectConfig: project.Config,
					}
					if dockerArgs, err := sbox.BuildDockerCommand(opts); err == nil {
						cmd.Printf("    Command:\n    docker %s\n", formatDockerCommand(dockerArgs))
					}
				}
			}
		}
		cmd.Println()
	}

	return nil
}

// stopE stops the running sandbox for the current project
func stopE(cmd *cobra.Command, args []string) error {
	sbox.SetupLogging()

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

// printResolvedEnvs prints resolved environment variables with source tags and host resolution hints.
// The prefix is prepended to each line for indentation.
func printResolvedEnvs(cmd *cobra.Command, resolved []sbox.ResolvedEnv, prefix string) {
	hasPassthrough := false
	hasUnset := false

	for _, r := range resolved {
		name := sbox.EnvName(r.Spec)
		sourceTag := fmt.Sprintf("  [%s]", r.Source)

		if strings.Contains(r.Spec, "=") {
			value := r.Spec[len(name)+1:]
			cmd.Printf("%s%s=%s%s\n", prefix, name, value, sourceTag)
		} else {
			hostValue, found := os.LookupEnv(name)
			if found {
				cmd.Printf("%s%s=%s  (from host*)%s\n", prefix, name, hostValue, sourceTag)
				hasPassthrough = true
			} else {
				cmd.Printf("%s%s  (not set on host, will be empty in sandbox)%s\n", prefix, name, sourceTag)
				hasUnset = true
			}
		}
	}

	if hasPassthrough || hasUnset {
		cmd.Println()
	}
	if hasPassthrough {
		cmd.Printf("%s* Value resolved from current host environment; may differ at 'sbox run' time.\n", prefix)
	}
	if hasUnset {
		cmd.Printf("%sHint: set missing variables on your host or use 'sbox env add NAME=VALUE' to set an explicit value.\n", prefix)
	}
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

package main

import (
	"fmt"
	"os"
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
				flags.Bool("rebuild", false, "Force rebuild of custom template image")
				flags.Bool("recreate", false, "Remove existing sandbox to apply new mount configuration")
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

		Command(infoE,
			"info",
			"List all known projects with their status",
			Description(`
				Lists all projects that have been used with sbox, showing:
				- Workspace path
				- Sandbox running status
				- Configured profiles
				- Additional volumes
			`),
		),

		Command(stopE,
			"stop",
			"Stop the running sandbox for this project",
			Description(`
				Stops the Docker sandbox container for the current project.
				If no sandbox is running, this command does nothing.

				With --rm, also removes:
				- The Docker container itself
				- Project configuration
				- Cached CLAUDE.md files
			`),
			Flags(func(flags *pflag.FlagSet) {
				flags.Bool("rm", false, "Also remove container and project data after stopping")
				flags.StringP("workspace", "w", "", "Workspace directory (default: current directory)")
			}),
		),

		Command(statusE,
			"status",
			"Show status of the sandbox for this project",
			Description(`
				Shows the sandbox status for the current project including:
				- Workspace path and hash
				- Running status (stopped/running with container info)
				- Configured profiles
				- Additional volumes
				- Docker socket setting
			`),
			Flags(func(flags *pflag.FlagSet) {
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

	rebuild, err := cmd.Flags().GetBool("rebuild")
	if err != nil {
		return fmt.Errorf("failed to get rebuild flag: %w", err)
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

	if existingSandbox != nil {
		if recreate {
			// User explicitly requested recreate - remove existing sandbox
			cmd.Printf("Removing existing sandbox %s to apply new mount configuration...\n", existingSandbox.ID)
			if err := sbox.RemoveDockerSandbox(existingSandbox.ID); err != nil {
				return fmt.Errorf("failed to remove existing sandbox: %w", err)
			}
			cmd.Println("Existing sandbox removed")
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
		ForceRebuild:      rebuild,
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
	cmd.Println("Run 'sbox run --rebuild' to rebuild the sandbox with this profile")
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
	cmd.Println("Run 'sbox run --rebuild' to rebuild the sandbox without this profile")
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

// infoE lists all known projects with their status
func infoE(cmd *cobra.Command, args []string) error {
	sbox.SetupLogging()

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

		// Check if sandbox is running for this project (only if we have a path)
		status := "stopped"
		containerName := ""
		if workspacePath != "" {
			container, _ := sbox.FindRunningSandbox(workspacePath)
			if container != nil {
				status = fmt.Sprintf("running (%s)", container.ID[:12])
				containerName = container.Name
			}
		} else {
			status = "unknown"
		}

		cmd.Printf("  %s\n", pathDisplay)
		cmd.Printf("    Hash:     %s\n", project.Hash)
		cmd.Printf("    Status:   %s\n", status)
		if containerName != "" {
			cmd.Printf("    Container: %s\n", containerName)
		}

		if len(project.Config.Profiles) > 0 {
			cmd.Printf("    Profiles: %v\n", project.Config.Profiles)
		}
		if len(project.Config.Volumes) > 0 {
			cmd.Printf("    Volumes:  %v\n", project.Config.Volumes)
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
						cmd.Printf("    Command:  docker %s\n", formatDockerCommand(dockerArgs))
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

	// Get --rm flag
	removeData, err := cmd.Flags().GetBool("rm")
	if err != nil {
		return fmt.Errorf("failed to get rm flag: %w", err)
	}

	// Stop the sandbox (and remove container if --rm is set)
	container, err := sbox.StopSandbox(workspaceDir, removeData)
	if err != nil {
		return fmt.Errorf("failed to stop sandbox: %w", err)
	}

	if container != nil {
		cmd.Printf("Sandbox stopped: %s (%s)\n", container.Name, container.ID[:12])
		if removeData {
			cmd.Printf("Container removed: %s\n", container.Name)
		}
	} else {
		cmd.Println("No sandbox was running for this project")
	}

	// Remove project data if requested
	if removeData {
		if err := sbox.RemoveProjectData(workspaceDir); err != nil {
			return fmt.Errorf("failed to remove project data: %w", err)
		}
		cmd.Println("Project data removed")
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

// statusE shows status for the current project
func statusE(cmd *cobra.Command, args []string) error {
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

	// Load global config
	config, err := sbox.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Load project config (this also gives us the hash)
	projectConfig, projectHash, err := sbox.GetProjectConfig(workspaceDir)
	if err != nil {
		return fmt.Errorf("failed to load project config: %w", err)
	}

	// Check if sandbox is running for this project
	status := "stopped"
	containerName := ""
	containerID := ""
	containerImage := ""
	container, _ := sbox.FindRunningSandbox(workspaceDir)
	if container != nil {
		status = "running"
		containerName = container.Name
		containerID = container.ID
		containerImage = container.Image
	}

	cmd.Printf("Project: %s\n", workspaceDir)
	cmd.Printf("  Hash:   %s\n", projectHash)
	cmd.Printf("  Status: %s\n", status)

	if container != nil {
		cmd.Printf("  Container:\n")
		cmd.Printf("    Name:  %s\n", containerName)
		cmd.Printf("    ID:    %s\n", containerID[:12])
		cmd.Printf("    Image: %s\n", containerImage)
	}

	if len(projectConfig.Profiles) > 0 {
		cmd.Printf("  Profiles: %v\n", projectConfig.Profiles)
	}
	if len(projectConfig.Volumes) > 0 {
		cmd.Printf("  Volumes:  %v\n", projectConfig.Volumes)
	}
	if projectConfig.DockerSocket != "" {
		cmd.Printf("  Docker:   %s\n", projectConfig.DockerSocket)
	}

	// Build and display the docker command
	opts := sbox.SandboxOptions{
		WorkspaceDir:  workspaceDir,
		Config:        config,
		ProjectConfig: projectConfig,
	}
	if dockerArgs, err := sbox.BuildDockerCommand(opts); err == nil {
		cmd.Printf("  Command:  docker %s\n", formatDockerCommand(dockerArgs))
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

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/sbox"
)

var InfoCommand = Command(infoE,
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
)

// infoE shows project information
func infoE(cmd *cobra.Command, args []string) error {
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
	cmd.Printf("  Hash:    %s\n", project.Hash)

	printSandboxStatus(cmd, workspaceDir, project.Config.SandboxName, "  ")

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
		cmd.Printf("    Hash:    %s\n", project.Hash)

		printSandboxStatus(cmd, workspacePath, project.Config.SandboxName, "    ")

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

// printSandboxStatus prints the Sandbox section with consistent formatting.
// sandboxName is always derived from config; workspaceDir may be empty for legacy projects.
func printSandboxStatus(cmd *cobra.Command, workspaceDir string, sandboxName string, prefix string) {
	// Always derive a name to display
	name := sandboxName
	if name == "" && workspaceDir != "" {
		derived, err := sbox.GenerateSandboxName(workspaceDir)
		if err == nil {
			name = derived
		}
	}

	cmd.Printf("%sSandbox:\n", prefix)
	if name != "" {
		cmd.Printf("%s  Name:   %s\n", prefix, name)
	}

	if workspaceDir == "" {
		cmd.Printf("%s  Status: unknown\n", prefix)
		return
	}

	container, containerErr := sbox.FindRunningSandbox(workspaceDir)
	if containerErr != nil {
		cmd.Printf("%s  Status: error (%s)\n", prefix, containerErr)
		return
	}

	if container != nil {
		cmd.Printf("%s  Status: running\n", prefix)
		if container.Image != "" {
			cmd.Printf("%s  Image:  %s\n", prefix, container.Image)
		}
		return
	}

	sandbox, sandboxErr := sbox.FindDockerSandbox(workspaceDir)
	if sandboxErr != nil {
		cmd.Printf("%s  Status: error (%s)\n", prefix, sandboxErr)
		return
	}

	if sandbox != nil {
		cmd.Printf("%s  Status: stopped\n", prefix)
		if sandbox.Image != "" {
			cmd.Printf("%s  Image:  %s\n", prefix, sandbox.Image)
		}
		return
	}

	cmd.Printf("%s  Status: not created\n", prefix)
}

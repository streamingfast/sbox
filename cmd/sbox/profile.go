package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/sbox"
)

var ProfileGroup = Group("profile", "Manage development profiles",
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
)

// profileListE lists available and installed profiles
func profileListE(cmd *cobra.Command, args []string) error {
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

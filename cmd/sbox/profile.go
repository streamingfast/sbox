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
		MinimumNArgs(1),
	),
	Command(profileRemoveE,
		"remove <profiles...>",
		"Remove profiles from the current project",
		MinimumNArgs(1),
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
	// Validate all profiles exist first
	for _, profileName := range args {
		if _, ok := sbox.GetProfile(profileName); !ok {
			return fmt.Errorf("unknown profile: %s\nAvailable profiles: %v", profileName, sbox.ListProfiles())
		}
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

	// Create a set of existing profiles for quick lookup
	existingSet := make(map[string]bool)
	for _, p := range projectConfig.Profiles {
		existingSet[p] = true
	}

	// Add each profile
	added := 0
	for _, profileName := range args {
		if existingSet[profileName] {
			cmd.Printf("Profile '%s' is already added to this project\n", profileName)
			continue
		}
		projectConfig.Profiles = append(projectConfig.Profiles, profileName)
		existingSet[profileName] = true
		cmd.Printf("Added profile '%s' to project\n", profileName)
		added++
	}

	if added == 0 {
		return nil
	}

	// Save project config
	if err := sbox.SaveProjectConfig(workspaceDir, projectConfig); err != nil {
		return fmt.Errorf("failed to save project config: %w", err)
	}

	cmd.Println("Run 'sbox run --recreate' to rebuild and recreate the sandbox with the new profiles")
	return nil
}

// profileRemoveE removes profiles from the current project
func profileRemoveE(cmd *cobra.Command, args []string) error {
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

	// Create a set of profiles to remove
	removeSet := make(map[string]bool)
	for _, name := range args {
		removeSet[name] = true
	}

	// Find and remove profiles
	var kept []string
	removed := 0
	for _, p := range projectConfig.Profiles {
		if removeSet[p] {
			cmd.Printf("Removed profile '%s' from project\n", p)
			removed++
		} else {
			kept = append(kept, p)
		}
	}

	if removed == 0 {
		cmd.Println("No matching profiles found in this project")
		return nil
	}

	projectConfig.Profiles = kept

	// Save project config
	if err := sbox.SaveProjectConfig(workspaceDir, projectConfig); err != nil {
		return fmt.Errorf("failed to save project config: %w", err)
	}

	cmd.Println("Run 'sbox run --recreate' to rebuild and recreate the sandbox without these profiles")
	return nil
}
